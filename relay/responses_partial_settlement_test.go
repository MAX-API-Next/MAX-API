package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Unlike a settlement recorder, this drives the real Responses relay and the
// durable funding/effect seam against an isolated SQLite ledger.
func TestResponsesHelperPartialSettlementUsesDurableLedger(t *testing.T) {
	for _, provider := range []struct {
		name        string
		channelType int
		frames      []string
	}{
		{name: "responses_to_chat_global", channelType: constant.ChannelTypeOpenAI, frames: []string{
			`{"type":"response.created","response":{"id":"resp_partial_chat","status":"in_progress","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`,
			`{"type":"response.output_text.delta","delta":"partial output"}`,
			`{broken`,
		}},
		{name: "responses_to_chat", channelType: constant.ChannelTypeAdvancedCustom, frames: []string{
			`{"type":"response.created","response":{"id":"resp_partial_chat","status":"in_progress","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`,
			`{"type":"response.output_text.delta","delta":"partial output"}`,
			`{broken`,
		}},
		{name: "claude", channelType: constant.ChannelTypeAnthropic, frames: []string{
			`{"type":"message_start","message":{"id":"msg_test","model":"gpt-test","usage":{"input_tokens":10,"output_tokens":2}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"error","error":{"type":"overloaded_error","message":"synthetic provider failure"}}`,
		}},
		{name: "gemini", channelType: constant.ChannelTypeGemini, frames: []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial output"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`,
		}},
		{name: "openai", channelType: constant.ChannelTypeOpenAI, frames: []string{
			`{"type":"response.output_text.delta","delta":"partial output"}`,
			`{"type":"response.failed","response":{"status":"failed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12},"error":{"type":"server_error","message":"synthetic provider failure"}}}`,
		}},
	} {
		for _, scenario := range []struct {
			name      string
			ratio     float64
			quota     int
			zeroUsage bool
			expr      string
		}{
			{name: "paid", ratio: 1, quota: 12},
			{name: "free", ratio: 0, quota: 0},
			{name: "minimum_quota", ratio: 0.001, quota: 1},
			{name: "additional_debit", ratio: 10, quota: 120},
			{name: "explicit_zero", ratio: 1, quota: 0, zeroUsage: true},
			{name: "tiered_zero", ratio: 1, quota: 0, expr: `tier("free", 0)`},
			{name: "tiered_rounding_zero", ratio: 1, quota: 0, expr: `tier("small", 0.01)`},
			{name: "expression_failure", ratio: 1, quota: 100, expr: `unknown_function()`},
		} {
			for _, disconnected := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/disconnect_%t", provider.name, scenario.name, disconnected), func(t *testing.T) {
					finalQuota := scenario.quota
					if strings.HasPrefix(provider.name, "responses_to_chat") && disconnected && finalQuota == 0 {
						// Chat retains its original abnormal-stream reservation
						// fallback; Responses-only zero-price exemptions do not expand.
						finalQuota = 100
					}
					promptTokens, completionTokens := 10, 2
					frames := append([]string(nil), provider.frames...)
					if strings.HasPrefix(provider.name, "responses_to_chat") && disconnected {
						// Isolate a transport error without a competing malformed
						// handler stop, keeping the existing EndReason deterministic.
						frames = frames[:2]
					}
					if scenario.zeroUsage {
						promptTokens, completionTokens = 0, 0
						paths := map[string][]string{
							"responses_to_chat_global": {"response.usage.input_tokens", "response.usage.output_tokens", "response.usage.total_tokens"},
							"responses_to_chat":        {"response.usage.input_tokens", "response.usage.output_tokens", "response.usage.total_tokens"},
							"claude":                   {"message.usage.input_tokens", "message.usage.output_tokens", "usage.output_tokens"},
							"gemini":                   {"usageMetadata.promptTokenCount", "usageMetadata.candidatesTokenCount", "usageMetadata.totalTokenCount"},
							"openai":                   {"response.usage.input_tokens", "response.usage.output_tokens", "response.usage.total_tokens"},
						}
						for idx, frame := range frames {
							for _, path := range paths[provider.name] {
								// Only replace existing counters, preserving absent usage on text frames.
								if !strings.Contains(frame, "usage") {
									continue
								}
								if provider.name == "claude" && ((idx == 0 && strings.HasPrefix(path, "usage.")) || (idx != 0 && strings.HasPrefix(path, "message."))) {
									continue
								}
								var err error
								frame, err = sjson.Set(frame, path, 0)
								require.NoError(t, err)
							}
							frames[idx] = frame
						}
					}
					db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
					require.NoError(t, err)
					sqlDB, err := db.DB()
					require.NoError(t, err)
					sqlDB.SetMaxOpenConns(1)
					require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{},
						&model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{},
						&model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
					oldDB, oldLogDB := model.DB, model.LOG_DB
					oldSQLite, oldRedis, oldBatch, oldLogConsume := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
					model.DB, model.LOG_DB = db, db
					common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
					t.Cleanup(func() {
						model.DB, model.LOG_DB = oldDB, oldLogDB
						common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldLogConsume
						_ = sqlDB.Close()
					})
					require.NoError(t, db.Create(&model.User{Id: 101, Username: "responses-ledger-test", Quota: 1000}).Error)
					require.NoError(t, db.Create(&model.Token{Id: 102, UserId: 101, Key: "synthetic-responses-ledger-key", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
					require.NoError(t, db.Create(&model.Channel{Id: 103}).Error)
					gin.SetMode(gin.TestMode)
					service.InitHttpClient()
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						if disconnected {
							w.Header().Set("Content-Length", "100000")
						}
						_, _ = w.Write([]byte("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))
					}))
					t.Cleanup(upstream.Close)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					c.Set(string(constant.ContextKeyChannelType), provider.channelType)
					c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
					c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
					c.Set(string(constant.ContextKeyChannelId), 103)
					c.Set(string(constant.ContextKeyOriginalModel), "gpt-test")
					requestID := "responses-durable-" + provider.name
					c.Set(common.RequestIdKey, requestID)
					info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 101, TokenId: 102, TokenKey: "synthetic-responses-ledger-key", ForcePreConsume: true,
						RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true,
						OriginModelName: "gpt-test", StartTime: time.Now(), UsingGroup: "default",
						UserSetting: dto.UserSetting{BillingPreference: "wallet_only", QuotaWarningThreshold: 1},
						PriceData:   types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
						Request:     &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: []byte(`"test"`), Stream: common.GetPointer(true)}}
					info.SetEstimatePromptTokens(10)
					if strings.HasPrefix(provider.name, "responses_to_chat") {
						oldGlobal := *model_setting.GetGlobalSettings()
						*model_setting.GetGlobalSettings() = model_setting.GlobalSettings{}
						t.Cleanup(func() { *model_setting.GetGlobalSettings() = oldGlobal })
						if provider.name == "responses_to_chat_global" {
							model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-test$"}}
						}
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
						c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
							{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses},
						}}})
						info.RelayMode, info.RelayFormat = relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI
						info.Request = &dto.GeneralOpenAIRequest{Model: "gpt-test", Stream: common.GetPointer(true)}
					}
					info.PriceData.ModelRatio = scenario.ratio
					if scenario.expr != "" {
						info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: scenario.expr, ExprHash: billingexpr.ExprHashString(scenario.expr), GroupRatio: 1, QuotaPerUnit: common.QuotaPerUnit, EstimatedQuotaAfterGroup: 100}
					}
					require.Nil(t, service.PreConsumeBilling(c, 100, info))
					var apiErr *types.MaxAPIError
					if strings.HasPrefix(provider.name, "responses_to_chat") {
						apiErr = TextHelper(c, info)
					} else {
						apiErr = ResponsesHelper(c, info)
					}
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
					require.EqualValues(t, 1, calls.Load())
					service.HandleFailedBilling(c, info, apiErr)
					service.HandleFailedBilling(c, info, apiErr)
					var settlement model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
					require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
					require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
					require.EqualValues(t, finalQuota-100, settlement.FundingDelta)
					require.EqualValues(t, finalQuota-100, settlement.TokenDelta)
					var effect model.BillingSettlementEffect
					require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
					require.Equal(t, promptTokens, effect.PromptTokens)
					require.Equal(t, completionTokens, effect.CompletionTokens)
					require.True(t, effect.QuotaIsActual)
					require.EqualValues(t, finalQuota, effect.Quota)
					// Reconstruct a session to exercise durable duplicate handling rather
					// than relying only on the original session's in-memory settled bit.
					replayInfo := &relaycommon.RelayInfo{RequestId: info.RequestId, UserId: info.UserId, TokenId: info.TokenId,
						TokenKey: info.TokenKey, OriginModelName: info.OriginModelName, UserSetting: info.UserSetting, ForcePreConsume: true}
					replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
					require.Nil(t, replayErr)
					require.NoError(t, replay.SettleWithEffect(finalQuota, &effect))
					require.NoError(t, replay.SettleWithEffect(finalQuota, &effect))
					replay.Refund(c)
					var user model.User
					require.NoError(t, db.First(&user, 101).Error)
					var token model.Token
					require.NoError(t, db.First(&token, 102).Error)
					var channel model.Channel
					require.NoError(t, db.First(&channel, 103).Error)
					require.EqualValues(t, 1000-finalQuota, user.Quota)
					require.EqualValues(t, finalQuota, user.UsedQuota)
					requestCount := 1
					if scenario.zeroUsage && finalQuota == 0 {
						requestCount = 0
					}
					require.EqualValues(t, requestCount, user.RequestCount)
					require.EqualValues(t, 1000-finalQuota, token.RemainQuota)
					require.EqualValues(t, finalQuota, token.UsedQuota)
					require.EqualValues(t, finalQuota, channel.UsedQuota)
					var count int64
					require.NoError(t, db.Model(&model.Log{}).Count(&count).Error)
					require.EqualValues(t, 1, count, fmt.Sprintf("duplicate consume log for %s", provider.name))
					var log model.Log
					require.NoError(t, db.First(&log).Error)
					require.Equal(t, finalQuota, log.Quota)
					require.Equal(t, promptTokens, log.PromptTokens)
					require.Equal(t, completionTokens, log.CompletionTokens)
				})
			}
		}
	}
}

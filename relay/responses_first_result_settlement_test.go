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
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Drive the production adapters, timeout watchdog and durable settlement seam.
// Whitespace is delivered generation data even though it is not a first result.
func TestResponsesFirstResultTimeoutSettlesDeliveredWhitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	for _, provider := range []struct {
		name         string
		channelType  int
		frames       []string
		textFrame    int
		textPath     string
		usageRoots   map[int]string
		counterPaths map[int][]string
	}{
		{"native", constant.ChannelTypeOpenAI, []string{
			`{"type":"response.in_progress","response":{"id":"resp_timeout","status":"in_progress","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`,
			`{"type":"response.output_text.delta","delta":"\n "}`,
		}, 1, "delta", map[int]string{0: "response.usage"}, map[int][]string{0: {"response.usage.input_tokens", "response.usage.output_tokens", "response.usage.total_tokens"}}},
		{"claude", constant.ChannelTypeAnthropic, []string{
			`{"type":"message_start","message":{"id":"msg_timeout","model":"gpt-test","usage":{"input_tokens":10,"output_tokens":2}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"\n "}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		}, 1, "delta.text", map[int]string{0: "message.usage", 2: "usage"}, map[int][]string{0: {"message.usage.input_tokens", "message.usage.output_tokens"}, 2: {"usage.output_tokens"}}},
		{"gemini", constant.ChannelTypeGemini, []string{
			`{"candidates":[{"content":{"parts":[{"text":"\n "}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`,
		}, 0, "candidates.0.content.parts.0.text", map[int]string{0: "usageMetadata"}, map[int][]string{0: {"usageMetadata.promptTokenCount", "usageMetadata.candidatesTokenCount", "usageMetadata.totalTokenCount"}}},
		{"chat_bridge", constant.ChannelTypeAdvancedCustom, []string{
			`{"choices":[{"delta":{"content":"\n "}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		}, 0, "choices.0.delta.content", map[int]string{0: "usage"}, map[int][]string{0: {"usage.prompt_tokens", "usage.completion_tokens", "usage.total_tokens"}}},
	} {
		for _, scenario := range []string{"paid", "free", "explicit_zero", "missing_usage", "metadata_only"} {
			for _, funding := range []string{"wallet_only", "subscription_only"} {
				t.Run(provider.name+"/"+scenario+"/"+funding, func(t *testing.T) {
					oldMonitor := *operation_setting.GetMonitorSetting()
					*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{StreamingFirstResultTimeoutSeconds: 1}
					t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldMonitor })
					frames := append([]string(nil), provider.frames...)
					prompt, completion := 10, 2
					if scenario == "explicit_zero" {
						prompt, completion = 0, 0
						for idx, paths := range provider.counterPaths {
							for _, path := range paths {
								var err error
								frames[idx], err = sjson.Set(frames[idx], path, 0)
								require.NoError(t, err)
							}
						}
					}
					if scenario == "missing_usage" {
						for idx, root := range provider.usageRoots {
							var err error
							frames[idx], err = sjson.Delete(frames[idx], root)
							require.NoError(t, err)
						}
						// The existing local fallback estimates this two-character
						// whitespace string as one token; it does not use tiktoken.
						completion = 1
						if provider.name == "native" {
							// Native Responses retains its existing tiktoken fallback,
							// which counts this whitespace as two tokens.
							completion = 2
						}
					}
					if scenario == "metadata_only" {
						var err error
						frames[provider.textFrame], err = sjson.Delete(frames[provider.textFrame], provider.textPath)
						require.NoError(t, err)
					}
					finalQuota := prompt + completion
					if scenario == "free" || scenario == "metadata_only" {
						finalQuota = 0
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
					oldSQLite, oldRedis, oldBatch, oldConsume := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
					model.DB, model.LOG_DB = db, db
					common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
					t.Cleanup(func() {
						model.DB, model.LOG_DB = oldDB, oldLogDB
						common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsume
						_ = sqlDB.Close()
					})
					require.NoError(t, db.Create(&model.User{Id: 201, Username: "first-result-ledger", Quota: 1000}).Error)
					require.NoError(t, db.Create(&model.Token{Id: 202, UserId: 201, Key: "synthetic-timeout-ledger-key", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
					require.NoError(t, db.Create(&model.Channel{Id: 203}).Error)
					if funding == "subscription_only" {
						require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 204, Title: "timeout-ledger-plan", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
						require.NoError(t, db.Create(&model.UserSubscription{Id: 205, UserId: 201, PlanId: 204, AmountTotal: 1000, Status: "active",
							StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}).Error)
					}
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
						w.(http.Flusher).Flush()
						select {
						case <-r.Context().Done():
						case <-time.After(3 * time.Second):
						}
					}))
					t.Cleanup(upstream.Close)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					c.Set(string(constant.ContextKeyChannelType), provider.channelType)
					c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
					c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
					c.Set(string(constant.ContextKeyChannelId), 203)
					c.Set(string(constant.ContextKeyOriginalModel), "gpt-test")
					if provider.name == "chat_bridge" {
						c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
							Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: "/v1/chat/completions", Converter: dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions}},
						}})
					}
					requestID := "timeout-ledger-" + provider.name + "-" + scenario + "-" + funding
					c.Set(common.RequestIdKey, requestID)
					info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 201, TokenId: 202, TokenKey: "synthetic-timeout-ledger-key", ForcePreConsume: true,
						RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true,
						OriginModelName: "gpt-test", StartTime: time.Now(), UsingGroup: "default", UserSetting: dto.UserSetting{BillingPreference: funding, QuotaWarningThreshold: 1},
						PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
						Request:   &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: []byte(`"test"`), Stream: common.GetPointer(true)}}
					info.SetEstimatePromptTokens(10)
					if scenario == "free" {
						info.PriceData.ModelRatio = 0
					}
					require.Nil(t, service.PreConsumeBilling(c, 100, info))
					apiErr := ResponsesHelper(c, info)
					require.NotNil(t, apiErr)
					require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, apiErr.GetErrorCode())
					require.True(t, types.IsSkipRetryError(apiErr))
					require.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
					require.False(t, info.HasRecordedChannelFirstResult())
					require.True(t, info.FirstResultTime.IsZero())
					require.EqualValues(t, 1, calls.Load())
					require.NotContains(t, recorder.Body.String(), "event: response.completed")
					service.HandleFailedBilling(c, info, apiErr)
					service.HandleFailedBilling(c, info, apiErr)
					var settlement model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
					require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
					require.EqualValues(t, finalQuota-100, settlement.FundingDelta, "delivered whitespace must not receive a full failure refund")
					require.EqualValues(t, finalQuota-100, settlement.TokenDelta)
					if scenario != "metadata_only" {
						require.Contains(t, recorder.Body.String(), "event: response.output_text.delta")
						require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
						var effect model.BillingSettlementEffect
						require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
						require.Equal(t, prompt, effect.PromptTokens)
						require.Equal(t, completion, effect.CompletionTokens)
						require.True(t, effect.QuotaIsActual)
						require.EqualValues(t, finalQuota, effect.Quota)
						replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: info.UserId, TokenId: info.TokenId, TokenKey: info.TokenKey,
							OriginModelName: info.OriginModelName, UserSetting: info.UserSetting, ForcePreConsume: true}
						replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
						require.Nil(t, replayErr)
						require.NoError(t, replay.SettleWithEffect(finalQuota, &effect))
						require.NoError(t, replay.SettleWithEffect(finalQuota, &effect))
						replay.Refund(c)
					}
					var user model.User
					var token model.Token
					var channel model.Channel
					require.NoError(t, db.First(&user, 201).Error)
					require.NoError(t, db.First(&token, 202).Error)
					require.NoError(t, db.First(&channel, 203).Error)
					walletQuota := 1000 - finalQuota
					if funding == "subscription_only" {
						walletQuota = 1000
						var sub model.UserSubscription
						require.NoError(t, db.First(&sub, 205).Error)
						require.EqualValues(t, finalQuota, sub.AmountUsed)
						require.Equal(t, model.BillingSettlementSourceSubscription, settlement.Source)
					}
					require.EqualValues(t, walletQuota, user.Quota)
					require.EqualValues(t, 1000-finalQuota, token.RemainQuota)
					require.EqualValues(t, finalQuota, user.UsedQuota)
					require.EqualValues(t, finalQuota, token.UsedQuota)
					require.EqualValues(t, finalQuota, channel.UsedQuota)
					var count int64
					require.NoError(t, db.Model(&model.Log{}).Count(&count).Error)
					wantLogs := 1
					if scenario == "metadata_only" {
						wantLogs = 0
					}
					require.EqualValues(t, wantLogs, count)
					wantRequests := wantLogs
					if scenario == "explicit_zero" {
						wantRequests = 0
					}
					require.EqualValues(t, wantRequests, user.RequestCount)
				})
			}
		}
	}
}

package relay

import (
	"crypto/sha256"
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
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type streamFundingCase struct {
	name, preference, source string
	wallet, subscriptionUsed int64
	allowOverflow            bool
}

func streamFundingCases() []streamFundingCase {
	return []streamFundingCase{
		{name: "subscription_only", preference: "subscription_only", source: "subscription", wallet: 1000},
		{name: "subscription_first", preference: "subscription_first", source: "subscription", wallet: 1000},
		{name: "wallet_first_to_subscription", preference: "wallet_first", source: "subscription"},
		{name: "wallet_only", preference: "wallet_only", source: "wallet", wallet: 1000},
		{name: "wallet_first", preference: "wallet_first", source: "wallet", wallet: 1000},
		{name: "subscription_first_to_wallet", preference: "subscription_first", source: "wallet", wallet: 1000, subscriptionUsed: 1000, allowOverflow: true},
	}
}

func setupStreamFundingLedger(t *testing.T, funding streamFundingCase) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
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
	})
	require.NoError(t, db.Create(&model.User{Id: 951, Username: "stream-funding", Quota: funding.wallet}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 952, UserId: 951, Key: "synthetic-stream-funding", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 953}).Error)
	require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 954, Title: "stream-funding-plan", Enabled: true,
		TotalAmount: 1000, AllowWalletOverflow: &funding.allowOverflow, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.UserSubscription{Id: 955, UserId: 951, PlanId: 954, Status: "active",
		AmountTotal: 1000, AmountUsed: funding.subscriptionUsed, AllowWalletOverflow: funding.allowOverflow,
		StartTime: now - 3600, EndTime: now + 3600, LastResetTime: now - 3600}).Error)
	return db
}

func streamFundingFrames(t *testing.T, route, usage string) (int, []string) {
	t.Helper()
	prompt, completion := 4, 2
	if usage == "zero" {
		prompt, completion = 0, 0
	} else if usage == "above_reservation" {
		prompt, completion = 80, 50
	} else if usage == "at_reservation" {
		prompt, completion = 80, 20
	}
	encode := func(v map[string]any) string {
		b, err := common.Marshal(v)
		require.NoError(t, err)
		return string(b)
	}
	switch route {
	case "chat_native", "chat_to_responses":
		frame := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "partial output"}}}}
		if usage != "missing" {
			frame["usage"] = map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
		}
		channel := constant.ChannelTypeOpenAI
		if route == "chat_to_responses" {
			channel = constant.ChannelTypeAdvancedCustom
		}
		return channel, []string{encode(frame)}
	case "claude_to_responses":
		message := map[string]any{"id": "msg_funding", "model": "gpt-test"}
		if usage != "missing" {
			message["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": 0}
		}
		frames := []string{encode(map[string]any{"type": "message_start", "message": message}),
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}`}
		if usage != "missing" {
			frames = append(frames, encode(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": completion}}))
		}
		return constant.ChannelTypeAnthropic, frames
	case "gemini_to_responses":
		frame := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "partial output"}}}}}}
		if usage != "missing" {
			frame["usageMetadata"] = map[string]any{"promptTokenCount": prompt, "candidatesTokenCount": completion, "totalTokenCount": prompt + completion}
		}
		return constant.ChannelTypeGemini, []string{encode(frame)}
	default:
		response := map[string]any{"id": "resp_funding", "model": "gpt-test", "status": "in_progress", "output": []any{}}
		if usage != "missing" {
			response["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}
		}
		channel := constant.ChannelTypeOpenAI
		if route == "responses_to_chat_custom" {
			channel = constant.ChannelTypeAdvancedCustom
		}
		return channel, []string{encode(map[string]any{"type": "response.created", "response": response}),
			`{"type":"response.output_text.delta","delta":"partial output"}`}
	}
}

func streamFundingContext(t *testing.T, route, requestID, upstream string, channelType int, funding streamFundingCase) (*gin.Context, *relaycommon.RelayInfo, *httptest.ResponseRecorder) {
	t.Helper()
	oldGlobal := *model_setting.GetGlobalSettings()
	*model_setting.GetGlobalSettings() = model_setting.GlobalSettings{}
	t.Cleanup(func() { *model_setting.GetGlobalSettings() = oldGlobal })
	if route == "responses_to_chat_global" {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-test$"}}
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	chat := route == "chat_native" || strings.HasPrefix(route, "responses_to_chat_")
	path := "/v1/responses"
	if chat {
		path = "/v1/chat/completions"
	}
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set(common.RequestIdKey, requestID)
	c.Set(string(constant.ContextKeyChannelType), channelType)
	c.Set(string(constant.ContextKeyChannelBaseUrl), upstream)
	c.Set(string(constant.ContextKeyChannelKey), "synthetic-stream-key")
	c.Set(string(constant.ContextKeyChannelId), 953)
	c.Set(string(constant.ContextKeyOriginalModel), "gpt-test")
	if channelType == constant.ChannelTypeAdvancedCustom {
		upstreamPath, converter := "/v1/chat/completions", dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions
		if chat {
			upstreamPath, converter = "/v1/responses", dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses
		}
		c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: path, UpstreamPath: upstreamPath, Converter: converter},
		}}})
	}
	info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 951, TokenId: 952, TokenKey: "synthetic-stream-funding", ForcePreConsume: true,
		RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true,
		OriginModelName: "gpt-test", StartTime: time.Now(), UsingGroup: "default", UserSetting: dto.UserSetting{BillingPreference: funding.preference, QuotaWarningThreshold: 1},
		PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
		Request:   &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: []byte(`"test"`), Stream: common.GetPointer(true)}}
	if chat {
		info.RelayMode, info.RelayFormat = relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI
		info.Request = &dto.GeneralOpenAIRequest{Model: "gpt-test", Messages: []dto.Message{{Role: "user", Content: "test"}}, Stream: common.GetPointer(true)}
	}
	info.SetEstimatePromptTokens(10)
	return c, info, recorder
}

// Test the complete relay-to-ledger seam rather than a mock BillingSettler.
// Reconstructed sessions must retain the source even after preferences change
// and a more attractive subscription becomes available.
func TestStreamSettlementRetainsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	for _, route := range []string{"responses_native", "chat_native", "chat_to_responses", "responses_to_chat_custom", "responses_to_chat_global", "claude_to_responses", "gemini_to_responses"} {
		for _, funding := range streamFundingCases() {
			for _, usageKind := range []string{"known", "zero", "missing", "above_reservation", "at_reservation"} {
				for _, ending := range []string{"malformed", "transport", "eof", "no_delivery"} {
					t.Run(strings.Join([]string{route, funding.name, usageKind, ending}, "/"), func(t *testing.T) {
						db := setupStreamFundingLedger(t, funding)
						channelType, frames := streamFundingFrames(t, route, usageKind)
						if ending == "malformed" {
							frames = append(frames, `{broken`)
						} else if ending == "no_delivery" {
							frames = []string{`{broken`}
						}
						var calls atomic.Int32
						upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							w.Header().Set("Content-Type", "text/event-stream")
							if ending == "transport" {
								w.Header().Set("Content-Length", "100000")
							}
							_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
						}))
						t.Cleanup(upstream.Close)
						fingerprint := sha256.Sum256([]byte(t.Name()))
						requestID := fmt.Sprintf("funding-%x", fingerprint[:16])
						c, info, recorder := streamFundingContext(t, route, requestID, upstream.URL, channelType, funding)
						require.Nil(t, service.PreConsumeBilling(c, 100, info))
						require.Equal(t, funding.source, info.BillingSource)
						require.Equal(t, 100, info.FinalPreConsumedQuota)
						var selection model.BillingPreConsumeSelection
						require.NoError(t, db.Where("request_id = ?", requestID).First(&selection).Error)
						require.Equal(t, funding.source, selection.Source)
						require.EqualValues(t, 100, selection.RequestedQuota)
						require.EqualValues(t, 100, selection.EffectiveQuota)
						var apiErr *types.MaxAPIError
						if info.RelayFormat == types.RelayFormatOpenAI {
							apiErr = TextHelper(c, info)
						} else {
							apiErr = ResponsesHelper(c, info)
						}
						if apiErr != nil {
							if ending != "no_delivery" {
								require.True(t, types.IsSkipRetryError(apiErr))
							}
							service.HandleFailedBilling(c, info, apiErr)
							service.HandleFailedBilling(c, info, apiErr)
						} else {
							require.Equal(t, "eof", ending)
						}
						require.EqualValues(t, 1, calls.Load())
						prompt, completion := 4, 2
						if usageKind == "zero" {
							prompt, completion = 0, 0
						} else if usageKind == "above_reservation" {
							prompt, completion = 80, 50
						} else if usageKind == "at_reservation" {
							prompt, completion = 80, 20
						} else if usageKind == "missing" {
							prompt, completion = 10, service.EstimateTokenByModel("gpt-test", "partial output")
							if route == "responses_native" {
								completion = service.CountTextToken("partial output", "gpt-test")
							}
						}
						quota := prompt + completion
						if usageKind == "zero" && ending == "transport" && info.RelayFormat == types.RelayFormatOpenAI {
							quota = 100 // Preserve Chat's abnormal zero-usage reservation policy.
						}
						if ending == "no_delivery" {
							quota = 0
							require.NotContains(t, recorder.Body.String(), "partial output")
						} else {
							require.Contains(t, recorder.Body.String(), "partial output")
						}
						var settlement model.BillingSettlement
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
						require.Equal(t, funding.source, settlement.Source)
						require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
						require.EqualValues(t, quota-100, settlement.FundingDelta)
						require.EqualValues(t, quota-100, settlement.AppliedFundingDelta)
						require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
						if ending != "no_delivery" {
							var effect model.BillingSettlementEffect
							require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
							require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
							require.Equal(t, prompt, effect.PromptTokens)
							require.Equal(t, completion, effect.CompletionTokens)
							require.True(t, effect.QuotaIsActual)
							require.EqualValues(t, quota, effect.Quota)
							// This new earlier-expiring subscription must never own the replay.
							require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
								StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
							replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: 951, TokenId: 952, TokenKey: info.TokenKey,
								OriginModelName: info.OriginModelName, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
							if funding.source == "wallet" {
								replayInfo.UserSetting.BillingPreference = "subscription_only"
							}
							var beforeOutbox int64
							require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&beforeOutbox).Error)
							replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
							require.Nil(t, replayErr)
							require.Equal(t, funding.source, replayInfo.BillingSource)
							require.NoError(t, replay.SettleWithEffect(quota, &effect))
							require.NoError(t, replay.SettleWithEffect(quota, &effect))
							replay.Refund(c)
							var afterOutbox int64
							require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&afterOutbox).Error)
							require.Equal(t, beforeOutbox, afterOutbox, "replay must not duplicate invalidation work")
							var decoy model.UserSubscription
							require.NoError(t, db.First(&decoy, 956).Error)
							require.Zero(t, decoy.AmountUsed)
						} else {
							require.Empty(t, settlement.EffectPayload)
						}
						assertStreamFundingBalances(t, db, funding, requestID, quota, ending != "no_delivery", prompt, completion)
					})
				}
			}
		}
	}
}

func assertStreamFundingBalances(t *testing.T, db *gorm.DB, funding streamFundingCase, requestID string, quota int, delivered bool, prompt, completion int) {
	t.Helper()
	var user model.User
	var token model.Token
	var channel model.Channel
	var sub model.UserSubscription
	require.NoError(t, db.First(&user, 951).Error)
	require.NoError(t, db.First(&token, 952).Error)
	require.NoError(t, db.First(&channel, 953).Error)
	require.NoError(t, db.First(&sub, 955).Error)
	wallet, subUsed := funding.wallet, funding.subscriptionUsed
	if funding.source == "subscription" {
		subUsed += int64(quota)
		var record model.SubscriptionPreConsumeRecord
		require.NoError(t, db.Where("request_id = ?", requestID).First(&record).Error)
		require.Equal(t, sub.Id, record.UserSubscriptionId)
		require.Equal(t, sub.LastResetTime, record.SubscriptionLastResetTime)
		require.EqualValues(t, 100, record.PreConsumed)
		wantStatus := "consumed"
		if !delivered {
			wantStatus = "refunded"
		}
		require.Equal(t, wantStatus, record.Status)
	} else {
		wallet -= int64(quota)
		var records int64
		require.NoError(t, db.Model(&model.SubscriptionPreConsumeRecord{}).Count(&records).Error)
		require.Zero(t, records)
	}
	require.EqualValues(t, wallet, user.Quota)
	require.EqualValues(t, subUsed, sub.AmountUsed)
	require.EqualValues(t, 1000-quota, token.RemainQuota)
	require.EqualValues(t, quota, token.UsedQuota)
	require.EqualValues(t, quota, user.UsedQuota)
	require.EqualValues(t, quota, channel.UsedQuota)
	wantLogs, wantRequests := 0, 0
	if delivered {
		wantLogs = 1
		if quota != 0 {
			wantRequests = 1
		}
	}
	require.EqualValues(t, wantRequests, user.RequestCount)
	var logs, selections, finalizes int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
	require.NoError(t, db.Model(&model.BillingPreConsumeSelection{}).Count(&selections).Error)
	require.NoError(t, db.Model(&model.BillingSettlement{}).Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).Count(&finalizes).Error)
	require.EqualValues(t, wantLogs, logs)
	require.EqualValues(t, 1, selections)
	require.EqualValues(t, 1, finalizes)
	if delivered {
		var log model.Log
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
		require.Equal(t, quota, log.Quota)
		require.Equal(t, prompt, log.PromptTokens)
		require.Equal(t, completion, log.CompletionTokens)
	}
}

func TestStreamFundingAdmissionRespectsSubscriptionOverflow(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed_subscriptions_%t", mixed), func(t *testing.T) {
			funding := streamFundingCase{preference: "subscription_first", wallet: 1000, subscriptionUsed: 1000}
			db := setupStreamFundingLedger(t, funding)
			if mixed {
				require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
					AmountUsed: 1000, AllowWalletOverflow: true, EndTime: time.Now().Unix() + 1800}).Error)
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			t.Cleanup(upstream.Close)
			c, info, _ := streamFundingContext(t, "responses_native", "strict-stream-overflow", upstream.URL, constant.ChannelTypeOpenAI, funding)
			apiErr := service.PreConsumeBilling(c, 100, info)
			require.NotNil(t, apiErr)
			require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
			require.Zero(t, calls.Load())
			var user model.User
			var token model.Token
			require.NoError(t, db.First(&user, 951).Error)
			require.NoError(t, db.First(&token, 952).Error)
			require.EqualValues(t, 1000, user.Quota)
			require.EqualValues(t, 1000, token.RemainQuota)
			for _, table := range []any{&model.Log{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.SubscriptionPreConsumeRecord{}, &model.CacheInvalidationTask{}} {
				var count int64
				require.NoError(t, db.Model(table).Count(&count).Error)
				require.Zero(t, count)
			}
		})
	}
}

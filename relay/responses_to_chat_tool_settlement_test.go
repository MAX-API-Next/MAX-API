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
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise both real TextHelper routes and the original funding/effect ledger.
// Unknown usage must count only delivered tool data, including after write failure.
func TestResponsesToChatToolPartialSettlementUsesDurableLedger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	const args = `{"query":"weather tomorrow","city":"Shanghai"}`
	for _, route := range []string{"custom", "global"} {
		for _, funding := range []string{"wallet_only", "subscription_only"} {
			for _, usageKind := range []string{"missing", "known", "zero"} {
				for _, ending := range []string{"eof", "malformed", "write_failure", "no_delivery"} {
					t.Run(strings.Join([]string{route, funding, usageKind, ending}, "/"), func(t *testing.T) {
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
						oldGlobal := *model_setting.GetGlobalSettings()
						model.DB, model.LOG_DB = db, db
						common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
						*model_setting.GetGlobalSettings() = model_setting.GlobalSettings{}
						t.Cleanup(func() {
							model.DB, model.LOG_DB = oldDB, oldLogDB
							common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsume
							*model_setting.GetGlobalSettings() = oldGlobal
							_ = sqlDB.Close()
						})
						require.NoError(t, db.Create(&model.User{Id: 801, Username: "tool-ledger-test", Quota: 1000}).Error)
						require.NoError(t, db.Create(&model.Token{Id: 802, UserId: 801, Key: "synthetic-tool-ledger", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
						require.NoError(t, db.Create(&model.Channel{Id: 803}).Error)
						if funding == "subscription_only" {
							require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 804, Title: "tool-ledger-plan", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
							require.NoError(t, db.Create(&model.UserSubscription{Id: 805, UserId: 801, PlanId: 804, AmountTotal: 1000, Status: "active",
								StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}).Error)
						}
						response := map[string]any{"id": "resp_tool_partial", "status": "in_progress"}
						prompt, completion := 10, service.EstimateTokenByModel("gpt-test", "lookup"+args)
						require.Positive(t, completion)
						if usageKind != "missing" {
							completion = 2
							if usageKind == "zero" {
								prompt, completion = 0, 0
							}
							response["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}
						}
						created, err := common.Marshal(map[string]any{"type": "response.created", "response": response})
						require.NoError(t, err)
						argumentDelta, err := common.Marshal(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_partial", "delta": args})
						require.NoError(t, err)
						frames := []string{string(created), `{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_partial","call_id":"call_partial","name":"lookup","arguments":""}}`, string(argumentDelta)}
						if ending == "malformed" {
							frames = append(frames, `{broken`)
						}
						if ending == "write_failure" {
							frames = append(frames, `{"type":"response.function_call_arguments.delta","item_id":"fc_partial","delta":"undelivered"}`)
						}
						var calls atomic.Int32
						upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
						}))
						t.Cleanup(upstream.Close)
						recorder := httptest.NewRecorder()
						var writer http.ResponseWriter = recorder
						if ending == "write_failure" {
							writer = &responsesSelectiveFailureWriter{recorder, "undelivered"}
						}
						if ending == "no_delivery" {
							writer = &responsesSelectiveFailureWriter{recorder, `"name":"lookup"`}
						}
						c, _ := gin.CreateTestContext(writer)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
						channelType := constant.ChannelTypeAdvancedCustom
						if route == "global" {
							channelType = constant.ChannelTypeOpenAI
							model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-test$"}}
						}
						c.Set(string(constant.ContextKeyChannelType), channelType)
						c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
						c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
						c.Set(string(constant.ContextKeyChannelId), 803)
						c.Set(string(constant.ContextKeyOriginalModel), "gpt-test")
						c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
							{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses},
						}}})
						requestID := "tool-ledger-" + strings.Join([]string{route, funding, usageKind, ending}, "-")
						c.Set(common.RequestIdKey, requestID)
						info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 801, TokenId: 802, TokenKey: "synthetic-tool-ledger", ForcePreConsume: true,
							RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, IsStream: true, DisablePing: true,
							OriginModelName: "gpt-test", StartTime: time.Now(), UsingGroup: "default", UserSetting: dto.UserSetting{BillingPreference: funding, QuotaWarningThreshold: 1},
							PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
							Request:   &dto.GeneralOpenAIRequest{Model: "gpt-test", Stream: common.GetPointer(true)}}
						info.SetEstimatePromptTokens(10)
						require.Nil(t, service.PreConsumeBilling(c, 100, info))
						apiErr := TextHelper(c, info)
						require.NotNil(t, apiErr)
						require.True(t, types.IsSkipRetryError(apiErr))
						require.EqualValues(t, 1, calls.Load())
						require.NotContains(t, recorder.Body.String(), "undelivered")
						require.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
						service.HandleFailedBilling(c, info, apiErr)
						service.HandleFailedBilling(c, info, apiErr)
						quota := prompt + completion
						if ending == "no_delivery" {
							quota = 0
						}
						var settlement model.BillingSettlement
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
						require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
						require.EqualValues(t, quota-100, settlement.FundingDelta, "delivered tool data must participate in actual settlement")
						require.EqualValues(t, quota-100, settlement.TokenDelta)
						if ending != "no_delivery" {
							require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
							var effect model.BillingSettlementEffect
							require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
							require.Equal(t, prompt, effect.PromptTokens)
							require.Equal(t, completion, effect.CompletionTokens)
							require.EqualValues(t, quota, effect.Quota)
							require.True(t, effect.QuotaIsActual)
							replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: info.UserId, TokenId: info.TokenId, TokenKey: info.TokenKey,
								OriginModelName: info.OriginModelName, UserSetting: info.UserSetting, ForcePreConsume: true}
							replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
							require.Nil(t, replayErr)
							require.NoError(t, replay.SettleWithEffect(quota, &effect))
							require.NoError(t, replay.SettleWithEffect(quota, &effect))
							replay.Refund(c)
						}
						var user model.User
						var token model.Token
						var channel model.Channel
						require.NoError(t, db.First(&user, 801).Error)
						require.NoError(t, db.First(&token, 802).Error)
						require.NoError(t, db.First(&channel, 803).Error)
						walletQuota := 1000 - quota
						if funding == "subscription_only" {
							walletQuota = 1000
							var subscription model.UserSubscription
							require.NoError(t, db.First(&subscription, 805).Error)
							require.EqualValues(t, quota, subscription.AmountUsed)
							require.Equal(t, model.BillingSettlementSourceSubscription, settlement.Source)
						}
						require.EqualValues(t, walletQuota, user.Quota)
						require.EqualValues(t, quota, user.UsedQuota)
						require.EqualValues(t, 1000-quota, token.RemainQuota)
						require.EqualValues(t, quota, token.UsedQuota)
						require.EqualValues(t, quota, channel.UsedQuota)
						var count int64
						require.NoError(t, db.Model(&model.Log{}).Count(&count).Error)
						wantLogs, wantRequests := 1, 1
						if ending == "no_delivery" {
							wantLogs, wantRequests = 0, 0
						} else if usageKind == "zero" {
							wantRequests = 0
						}
						require.EqualValues(t, wantLogs, count)
						require.EqualValues(t, wantRequests, user.RequestCount)
					})
				}
			}
		}
	}
}

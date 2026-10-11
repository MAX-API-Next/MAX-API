package controller

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
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Paid requests exercise controller admission, adapter selection, partial
// settlement, the deferred refund, and the client-protocol error writer.
func TestRelaySubscriptionStreamFundingAndRefund(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	oldFloor, oldRetry, oldCount, oldEmpty := common.PreConsumedQuota, common.RetryTimes, constant.CountToken, common.EmptyCompletionRetryEnabled
	common.PreConsumedQuota, constant.CountToken, common.EmptyCompletionRetryEnabled = 100, false, false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-funding-controller":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"gpt-funding-controller":1}`))
	t.Cleanup(func() {
		common.PreConsumedQuota, common.RetryTimes, constant.CountToken, common.EmptyCompletionRetryEnabled = oldFloor, oldRetry, oldCount, oldEmpty
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	for _, route := range []string{"responses_native", "chat_native", "chat_to_responses", "responses_to_chat"} {
		for _, funding := range []struct {
			name, preference, source string
			wallet, subUsed          int64
			allow                    bool
		}{
			{"subscription_only", "subscription_only", "subscription", 1000, 0, false},
			{"wallet_only", "wallet_only", "wallet", 1000, 0, false},
			{"wallet_first_fallback", "wallet_first", "subscription", 0, 0, false},
			{"subscription_first_fallback", "subscription_first", "wallet", 1000, 1000, true},
		} {
			for _, usageKind := range []string{"known", "zero", "missing", "above_reservation", "at_reservation"} {
				for _, delivered := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/delivered_%t", route, funding.name, usageKind, delivered), func(t *testing.T) {
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
						oldGlobal := *model_setting.GetGlobalSettings()
						model.DB, model.LOG_DB = db, db
						common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
						*model_setting.GetGlobalSettings() = model_setting.GlobalSettings{}
						// Unstarted failures test the selected attempt's refund. Existing
						// retry-routing tests separately cover selecting a new channel.
						common.RetryTimes = 0
						if delivered {
							common.RetryTimes = 2
						}
						t.Cleanup(func() {
							model.DB, model.LOG_DB = oldDB, oldLogDB
							common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsume
							*model_setting.GetGlobalSettings() = oldGlobal
						})
						require.NoError(t, db.Create(&model.User{Id: 971, Username: "controller-stream-funding", Quota: funding.wallet}).Error)
						require.NoError(t, db.Create(&model.Token{Id: 972, UserId: 971, Key: "synthetic-controller-funding", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
						require.NoError(t, db.Create(&model.Channel{Id: 973}).Error)
						require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 974, Title: "controller-funding-plan", Enabled: true, TotalAmount: 1000,
							AllowWalletOverflow: &funding.allow, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
						now := time.Now().Unix()
						require.NoError(t, db.Create(&model.UserSubscription{Id: 975, UserId: 971, PlanId: 974, Status: "active", AmountTotal: 1000,
							AmountUsed: funding.subUsed, AllowWalletOverflow: funding.allow, StartTime: now - 3600, EndTime: now + 3600, LastResetTime: now - 3600}).Error)
						prompt, completion := 4, 2
						if usageKind == "zero" {
							prompt, completion = 0, 0
						} else if usageKind == "above_reservation" {
							prompt, completion = 80, 50
						} else if usageKind == "at_reservation" {
							prompt, completion = 80, 20
						} else if usageKind == "missing" {
							prompt, completion = 0, service.EstimateTokenByModel("gpt-funding-controller", "partial output")
							if route == "responses_native" {
								completion = service.CountTextToken("partial output", "gpt-funding-controller")
							}
						}
						chatUpstream := route == "chat_native" || route == "chat_to_responses"
						frames := []string{}
						if delivered {
							if chatUpstream {
								frame := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "partial output"}}}}
								if usageKind != "missing" {
									frame["usage"] = map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
								}
								b, marshalErr := common.Marshal(frame)
								require.NoError(t, marshalErr)
								frames = append(frames, string(b))
							} else {
								response := map[string]any{"id": "resp_controller_funding", "model": "gpt-funding-controller", "status": "in_progress", "output": []any{}}
								if usageKind != "missing" {
									response["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}
								}
								b, marshalErr := common.Marshal(map[string]any{"type": "response.created", "response": response})
								require.NoError(t, marshalErr)
								frames = append(frames, string(b), `{"type":"response.output_text.delta","delta":"partial output"}`)
							}
						}
						frames = append(frames, `{broken`)
						var calls atomic.Int32
						upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
						}))
						t.Cleanup(upstream.Close)
						fingerprint := sha256.Sum256([]byte(t.Name()))
						requestID := fmt.Sprintf("controller-funding-%x", fingerprint[:16])
						path, format := "/v1/responses", types.RelayFormat(types.RelayFormatOpenAIResponses)
						requestBody := `{"model":"gpt-funding-controller","input":"test","stream":true,"max_output_tokens":16}`
						if route == "chat_native" || route == "responses_to_chat" {
							path, format = "/v1/chat/completions", types.RelayFormatOpenAI
							requestBody = `{"model":"gpt-funding-controller","messages":[{"role":"user","content":"test"}],"stream":true,"max_tokens":16}`
						}
						channelType := constant.ChannelTypeOpenAI
						if route == "chat_to_responses" || route == "responses_to_chat" {
							channelType = constant.ChannelTypeAdvancedCustom
						}
						router := gin.New()
						router.POST(path, func(c *gin.Context) {
							c.Set(common.RequestIdKey, requestID)
							c.Set(string(constant.ContextKeyUserId), 971)
							c.Set(string(constant.ContextKeyUserQuota), funding.wallet)
							c.Set(string(constant.ContextKeyTokenId), 972)
							c.Set(string(constant.ContextKeyTokenKey), "synthetic-controller-funding")
							c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{BillingPreference: funding.preference, QuotaWarningThreshold: 1})
							c.Set(string(constant.ContextKeyOriginalModel), "gpt-funding-controller")
							c.Set(string(constant.ContextKeyUsingGroup), "default")
							c.Set(string(constant.ContextKeyUserGroup), "default")
							c.Set(string(constant.ContextKeyChannelId), 973)
							c.Set(string(constant.ContextKeyChannelType), channelType)
							c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
							c.Set(string(constant.ContextKeyChannelKey), "synthetic-controller-key")
							if channelType == constant.ChannelTypeAdvancedCustom {
								upstreamPath, converter := "/v1/chat/completions", dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions
								if route == "responses_to_chat" {
									upstreamPath, converter = "/v1/responses", dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses
								}
								c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
									{IncomingPath: path, UpstreamPath: upstreamPath, Converter: converter},
								}}})
							}
							Relay(c, format)
						})
						recorder := httptest.NewRecorder()
						request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(requestBody))
						request.Header.Set("Content-Type", "application/json")
						router.ServeHTTP(recorder, request)
						require.EqualValues(t, 1, calls.Load(), recorder.Body.String())
						quota, logs, requests := 0, 0, 0
						if delivered {
							quota, logs = prompt+completion, 1
							if quota != 0 {
								requests = 1
							}
							require.Equal(t, http.StatusOK, recorder.Code)
							require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
							require.Contains(t, recorder.Body.String(), "partial output")
							if format == types.RelayFormatOpenAIResponses {
								require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
								require.NotContains(t, recorder.Body.String(), "[DONE]")
							} else {
								require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
								require.NotContains(t, recorder.Body.String(), "event: response.")
							}
						} else {
							require.GreaterOrEqual(t, recorder.Code, 400)
							require.True(t, gjson.Valid(recorder.Body.String()))
							require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
						}
						var selection model.BillingPreConsumeSelection
						var settlement model.BillingSettlement
						require.NoError(t, db.Where("request_id = ?", requestID).First(&selection).Error)
						require.Equal(t, funding.source, selection.Source)
						require.EqualValues(t, 100, selection.EffectiveQuota)
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
						require.Equal(t, funding.source, settlement.Source)
						require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
						require.EqualValues(t, quota-100, settlement.AppliedFundingDelta)
						require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
						var user model.User
						var token model.Token
						var channel model.Channel
						var sub model.UserSubscription
						require.NoError(t, db.First(&user, 971).Error)
						require.NoError(t, db.First(&token, 972).Error)
						require.NoError(t, db.First(&channel, 973).Error)
						require.NoError(t, db.First(&sub, 975).Error)
						wallet, subUsed := funding.wallet, funding.subUsed
						if funding.source == "subscription" {
							subUsed += int64(quota)
							var record model.SubscriptionPreConsumeRecord
							require.NoError(t, db.Where("request_id = ?", requestID).First(&record).Error)
							require.Equal(t, sub.LastResetTime, record.SubscriptionLastResetTime)
						} else {
							wallet -= int64(quota)
						}
						require.EqualValues(t, wallet, user.Quota)
						require.EqualValues(t, subUsed, sub.AmountUsed)
						require.EqualValues(t, quota, user.UsedQuota)
						require.EqualValues(t, requests, user.RequestCount)
						require.EqualValues(t, 1000-quota, token.RemainQuota)
						require.EqualValues(t, quota, token.UsedQuota)
						require.EqualValues(t, quota, channel.UsedQuota)
						var count int64
						require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
						require.EqualValues(t, logs, count)
						if delivered {
							require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
							var log model.Log
							require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
							require.Equal(t, quota, log.Quota)
							require.Equal(t, prompt, log.PromptTokens)
							require.Equal(t, completion, log.CompletionTokens)
						} else {
							require.Empty(t, settlement.EffectPayload)
						}
					})
				}
			}
		}
	}
}

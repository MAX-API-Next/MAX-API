package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Real paid controller requests include admission, protocol errors and the
// deferred refund before workers replay or attempt to replace their finalize.
func TestRelayFinalizeIdentityAndConcurrentReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	oldFloor, oldCount, oldRetry, oldEmpty := common.PreConsumedQuota, constant.CountToken, common.RetryTimes, common.EmptyCompletionRetryEnabled
	common.PreConsumedQuota, constant.CountToken, common.RetryTimes, common.EmptyCompletionRetryEnabled = 100, false, 2, false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-finalize-controller":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"gpt-finalize-controller":1}`))
	t.Cleanup(func() {
		common.PreConsumedQuota, constant.CountToken, common.RetryTimes, common.EmptyCompletionRetryEnabled = oldFloor, oldCount, oldRetry, oldEmpty
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	for _, chat := range []bool{false, true} {
		for _, funding := range []struct {
			name, preference, source string
			wallet, subUsed          int64
			allow                    bool
		}{
			{"wallet", "wallet_only", "wallet", 1000, 0, false},
			{"subscription", "subscription_only", "subscription", 1000, 0, false},
			{"wallet_fallback", "subscription_first", "wallet", 1000, 1000, true},
			{"subscription_fallback", "wallet_first", "subscription", 0, 0, false},
		} {
			for _, usage := range []struct {
				name         string
				prompt, comp int
			}{{"negative", 4, 2}, {"zero_delta", 80, 20}, {"positive", 80, 50}, {"zero_quota", 0, 0}} {
				t.Run(fmt.Sprintf("chat_%t/%s/%s", chat, funding.name, usage.name), func(t *testing.T) {
					db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
					require.NoError(t, err)
					sqlDB, err := db.DB()
					require.NoError(t, err)
					sqlDB.SetMaxOpenConns(1)
					t.Cleanup(func() { _ = sqlDB.Close() })
					require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{},
						&model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{},
						&model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
					oldDB, oldLogDB, oldSQLite := model.DB, model.LOG_DB, common.UsingSQLite
					oldRedis, oldBatch, oldConsume := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
					oldGlobal := *model_setting.GetGlobalSettings()
					model.DB, model.LOG_DB, common.UsingSQLite = db, db, true
					common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
					*model_setting.GetGlobalSettings() = model_setting.GlobalSettings{}
					t.Cleanup(func() {
						model.DB, model.LOG_DB, common.UsingSQLite = oldDB, oldLogDB, oldSQLite
						common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldConsume
						*model_setting.GetGlobalSettings() = oldGlobal
					})
					require.NoError(t, db.Create(&model.User{Id: 971, Username: "controller-finalize", Quota: funding.wallet}).Error)
					require.NoError(t, db.Create(&model.Token{Id: 972, UserId: 971, Key: "synthetic-controller-finalize", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
					require.NoError(t, db.Create(&model.Channel{Id: 973}).Error)
					require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 974, Title: "controller-finalize", Enabled: true, TotalAmount: 1000,
						AllowWalletOverflow: &funding.allow, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					now := time.Now().Unix()
					require.NoError(t, db.Create(&model.UserSubscription{Id: 975, UserId: 971, PlanId: 974, Status: "active", AmountTotal: 1000,
						AmountUsed: funding.subUsed, AllowWalletOverflow: funding.allow, StartTime: now - 3600, EndTime: now + 3600, LastResetTime: now - 3600}).Error)
					frame := map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_controller_finalize",
						"usage": map[string]any{"input_tokens": usage.prompt, "output_tokens": usage.comp, "total_tokens": usage.prompt + usage.comp}}}
					output := `{"type":"response.output_text.delta","delta":"partial output"}`
					if chat {
						frame = map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "partial output"}}},
							"usage": map[string]any{"prompt_tokens": usage.prompt, "completion_tokens": usage.comp, "total_tokens": usage.prompt + usage.comp}}
						output = ""
					}
					encoded, err := common.Marshal(frame)
					require.NoError(t, err)
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
						if output != "" {
							_, _ = fmt.Fprintf(w, "data: %s\n\n", output)
						}
						_, _ = fmt.Fprint(w, "data: {broken\n\n")
					}))
					t.Cleanup(upstream.Close)
					fingerprint := sha256.Sum256([]byte(t.Name()))
					requestID := fmt.Sprintf("controller-replay-%x", fingerprint[:16])
					path, format := "/v1/responses", types.RelayFormat(types.RelayFormatOpenAIResponses)
					body := `{"model":"gpt-finalize-controller","input":"test","stream":true,"max_output_tokens":16}`
					if chat {
						path, format = "/v1/chat/completions", types.RelayFormatOpenAI
						body = `{"model":"gpt-finalize-controller","messages":[{"role":"user","content":"test"}],"stream":true,"max_tokens":16}`
					}
					router := gin.New()
					router.POST(path, func(c *gin.Context) {
						c.Set(common.RequestIdKey, requestID)
						c.Set(string(constant.ContextKeyUserId), 971)
						c.Set(string(constant.ContextKeyUserQuota), funding.wallet)
						c.Set(string(constant.ContextKeyTokenId), 972)
						c.Set(string(constant.ContextKeyTokenKey), "synthetic-controller-finalize")
						c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{BillingPreference: funding.preference, QuotaWarningThreshold: 1})
						c.Set(string(constant.ContextKeyOriginalModel), "gpt-finalize-controller")
						c.Set(string(constant.ContextKeyUsingGroup), "default")
						c.Set(string(constant.ContextKeyUserGroup), "default")
						c.Set(string(constant.ContextKeyChannelId), 973)
						c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
						c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
						c.Set(string(constant.ContextKeyChannelKey), "synthetic-controller-finalize-key")
						Relay(c, format)
					})
					recorder := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					router.ServeHTTP(recorder, request)
					require.EqualValues(t, 1, calls.Load())
					require.Contains(t, recorder.Body.String(), "partial output")
					var record model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&record).Error)
					quota := usage.prompt + usage.comp
					require.Equal(t, funding.source, record.Source)
					require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
					require.EqualValues(t, quota-100, record.AppliedFundingDelta)
					canonical := model.BillingSettlementInput{OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID,
						TokenID: record.TokenID, SubscriptionID: record.SubscriptionID, FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta,
						SubscriptionPreConsumeRequestID: record.SubscriptionPreConsumeRequestID, FinalizeSubscriptionPreConsume: record.FinalizeSubscriptionPreConsume}
					var workers sync.WaitGroup
					results := make(chan error, 8)
					start := make(chan struct{})
					for worker := range 8 {
						workers.Add(1)
						go func() {
							defer workers.Done()
							<-start
							input := canonical
							if worker >= 4 {
								input.FundingDelta, input.TokenDelta = -100, -100
								input.AllowMissingToken = true
								input.FinalizeSubscriptionPreConsume = input.Source == model.BillingSettlementSourceSubscription
							}
							applied, already, applyErr := model.ApplyBillingSettlementOnce(input)
							if worker >= 4 {
								if !errors.Is(applyErr, model.ErrBillingSettlementOperationConflict) || applied != 0 || already {
									results <- fmt.Errorf("refund replaced original finalize: applied=%d already=%t err=%v", applied, already, applyErr)
									return
								}
							} else if applyErr != nil || applied != int64(quota-100) || !already {
								results <- fmt.Errorf("canonical replay differs: applied=%d already=%t err=%v", applied, already, applyErr)
								return
							}
							model.ProcessPendingBillingSettlementsOnce()
							results <- nil
						}()
					}
					close(start)
					workers.Wait()
					close(results)
					for workerErr := range results {
						require.NoError(t, workerErr)
					}
					var after model.BillingSettlement
					require.NoError(t, db.Where("id = ?", record.ID).First(&after).Error)
					require.Equal(t, record, after)
					var user model.User
					var token model.Token
					var sub model.UserSubscription
					var channel model.Channel
					require.NoError(t, db.First(&user, 971).Error)
					require.NoError(t, db.First(&token, 972).Error)
					require.NoError(t, db.First(&sub, 975).Error)
					require.NoError(t, db.First(&channel, 973).Error)
					wallet, subUsed := funding.wallet, funding.subUsed
					if funding.source == "wallet" {
						wallet -= int64(quota)
					} else {
						subUsed += int64(quota)
					}
					require.EqualValues(t, wallet, user.Quota)
					require.EqualValues(t, subUsed, sub.AmountUsed)
					require.EqualValues(t, 1000-quota, token.RemainQuota)
					require.EqualValues(t, quota, token.UsedQuota)
					require.EqualValues(t, quota, user.UsedQuota)
					require.EqualValues(t, quota, channel.UsedQuota)
					requests := 1
					if quota == 0 {
						requests = 0
					}
					require.EqualValues(t, requests, user.RequestCount)
					for _, table := range []any{&model.BillingLogReceipt{}, &model.Log{}} {
						var count int64
						require.NoError(t, db.Model(table).Count(&count).Error)
						require.EqualValues(t, 1, count)
					}
				})
			}
		}
	}
}

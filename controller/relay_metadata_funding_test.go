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
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRelayMetadataOnlyFailureRefundsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	oldFloor, oldRetry, oldCount, oldEmpty := common.PreConsumedQuota, common.RetryTimes, constant.CountToken, common.EmptyCompletionRetryEnabled
	common.PreConsumedQuota, common.RetryTimes, constant.CountToken, common.EmptyCompletionRetryEnabled = 100, 2, false, false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-metadata-controller":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"gpt-metadata-controller":1}`))
	t.Cleanup(func() {
		common.PreConsumedQuota, common.RetryTimes, constant.CountToken, common.EmptyCompletionRetryEnabled = oldFloor, oldRetry, oldCount, oldEmpty
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	for _, funding := range []struct {
		name, preference, source string
		wallet, subUsed          int64
		allow                    bool
	}{
		{"subscription_only", "subscription_only", "subscription", 1000, 0, false},
		{"subscription_first", "subscription_first", "subscription", 1000, 0, false},
		{"wallet_first_fallback", "wallet_first", "subscription", 0, 0, false},
		{"wallet_only", "wallet_only", "wallet", 1000, 0, false},
		{"wallet_first", "wallet_first", "wallet", 1000, 0, false},
		{"subscription_first_fallback", "subscription_first", "wallet", 1000, 1000, true},
	} {
		for _, usage := range []string{"known", "zero", "missing"} {
			for _, ending := range []string{"eof", "transport", "malformed"} {
				t.Run(strings.Join([]string{funding.name, usage, ending}, "/"), func(t *testing.T) {
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
					t.Cleanup(func() {
						model.DB, model.LOG_DB = oldDB, oldLogDB
						common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsume
						*model_setting.GetGlobalSettings() = oldGlobal
					})
					require.NoError(t, db.Create(&model.User{Id: 981, Username: "metadata-controller", Quota: funding.wallet}).Error)
					require.NoError(t, db.Create(&model.Token{Id: 982, UserId: 981, Key: "synthetic-metadata-controller", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
					require.NoError(t, db.Create(&model.Channel{Id: 983}).Error)
					require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 984, Title: "metadata-controller", Enabled: true, TotalAmount: 1000,
						AllowWalletOverflow: &funding.allow, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					now := time.Now().Unix()
					require.NoError(t, db.Create(&model.UserSubscription{Id: 985, UserId: 981, PlanId: 984, Status: "active", AmountTotal: 1000,
						AmountUsed: funding.subUsed, AllowWalletOverflow: funding.allow, StartTime: now - 3600, EndTime: now + 3600, LastResetTime: now - 3600}).Error)
					response := map[string]any{"id": "resp_metadata_controller", "status": "in_progress", "model": "gpt-metadata-controller", "output": []any{}}
					if usage != "missing" {
						prompt, completion := 4, 2
						if usage == "zero" {
							prompt, completion = 0, 0
						}
						response["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}
					}
					frame, err := common.Marshal(map[string]any{"type": "response.created", "response": response})
					require.NoError(t, err)
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						calls.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						if ending == "transport" {
							w.Header().Set("Content-Length", "100000")
						}
						_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
						if ending == "malformed" {
							_, _ = fmt.Fprint(w, "data: {broken\n\n")
						}
					}))
					t.Cleanup(upstream.Close)
					fingerprint := sha256.Sum256([]byte(t.Name()))
					requestID := fmt.Sprintf("metadata-controller-%x", fingerprint[:16])
					router := gin.New()
					router.POST("/v1/responses", func(c *gin.Context) {
						c.Set(common.RequestIdKey, requestID)
						c.Set(string(constant.ContextKeyUserId), 981)
						c.Set(string(constant.ContextKeyUserQuota), funding.wallet)
						c.Set(string(constant.ContextKeyTokenId), 982)
						c.Set(string(constant.ContextKeyTokenKey), "synthetic-metadata-controller")
						c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{BillingPreference: funding.preference, QuotaWarningThreshold: 1})
						c.Set(string(constant.ContextKeyOriginalModel), "gpt-metadata-controller")
						c.Set(string(constant.ContextKeyUsingGroup), "default")
						c.Set(string(constant.ContextKeyUserGroup), "default")
						c.Set(string(constant.ContextKeyChannelId), 983)
						c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
						c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
						c.Set(string(constant.ContextKeyChannelKey), "synthetic-metadata-upstream")
						Relay(c, types.RelayFormatOpenAIResponses)
					})
					recorder := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-metadata-controller","input":"test","stream":true}`))
					request.Header.Set("Content-Type", "application/json")
					router.ServeHTTP(recorder, request)
					require.EqualValues(t, 1, calls.Load())
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
					require.NotContains(t, recorder.Body.String(), "response.completed")
					model.ProcessPendingBillingSettlementsOnce()
					model.ProcessPendingBillingSettlementsOnce()
					var settlement model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
					require.Equal(t, funding.source, settlement.Source)
					require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
					require.EqualValues(t, -100, settlement.AppliedFundingDelta)
					require.EqualValues(t, -100, settlement.AppliedTokenDelta)
					require.Empty(t, settlement.EffectPayload)
					var user model.User
					var token model.Token
					var channel model.Channel
					var sub model.UserSubscription
					require.NoError(t, db.First(&user, 981).Error)
					require.NoError(t, db.First(&token, 982).Error)
					require.NoError(t, db.First(&channel, 983).Error)
					require.NoError(t, db.First(&sub, 985).Error)
					require.EqualValues(t, funding.wallet, user.Quota)
					require.EqualValues(t, funding.subUsed, sub.AmountUsed)
					require.EqualValues(t, 1000, token.RemainQuota)
					require.Zero(t, token.UsedQuota)
					require.Zero(t, user.UsedQuota)
					require.Zero(t, user.RequestCount)
					require.Zero(t, channel.UsedQuota)
					var logs, finalizes int64
					require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
					require.Zero(t, logs)
					require.NoError(t, db.Model(&model.BillingSettlement{}).Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).Count(&finalizes).Error)
					require.EqualValues(t, 1, finalizes)
					if funding.source == "subscription" {
						var record model.SubscriptionPreConsumeRecord
						require.NoError(t, db.Where("request_id = ?", requestID).First(&record).Error)
						require.Equal(t, "refunded", record.Status)
						require.Equal(t, sub.LastResetTime, record.SubscriptionLastResetTime)
					}
				})
			}
		}
	}
}

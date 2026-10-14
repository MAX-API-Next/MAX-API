package relay

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionResetPreservesUnsettledStreamReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	for _, advance := range []bool{false, true} {
		t.Run(fmt.Sprintf("advance_reset_time_%t", advance), func(t *testing.T) {
			funding := streamFundingCases()[0]
			db := setupStreamFundingLedger(t, funding)
			// Never-reset plans keep a zero period anchor even when the admin
			// requests a new reset time. This makes the boundary deterministic.
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 955).
				Updates(map[string]any{"last_reset_time": 0, "next_reset_time": 0}).Error)
			channelType, frames := streamFundingFrames(t, "responses_native", "known")
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			requestID := fmt.Sprintf("period-pending-%t", advance)
			c, info, _ := streamFundingContext(t, "responses_native", requestID, upstream.URL, channelType, funding)
			require.Nil(t, service.PreConsumeBilling(c, 100, info))
			var beforeOutbox int64
			require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&beforeOutbox).Error)

			_, resetErr := model.AdminResetUserSubscriptionsByPlan(951, 954, advance)
			require.Error(t, resetErr, "an unchanged period anchor must not erase an unsettled reservation")
			var sub model.UserSubscription
			require.NoError(t, db.First(&sub, 955).Error)
			require.EqualValues(t, 100, sub.AmountUsed)
			require.Zero(t, sub.LastResetTime)
			var afterOutbox int64
			require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&afterOutbox).Error)
			require.Equal(t, beforeOutbox, afterOutbox)

			apiErr := ResponsesHelper(c, info)
			require.NotNil(t, apiErr) // The synthetic stream has no terminal event.
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertStreamFundingBalances(t, db, funding, requestID, 6, true, 4, 2)
			_, resetErr = model.AdminResetUserSubscriptionsByPlan(951, 954, advance)
			require.NoError(t, resetErr, "a completed funding settlement no longer blocks the reset")
			require.NoError(t, db.First(&sub, 955).Error)
			require.Zero(t, sub.AmountUsed)
			model.ProcessPendingBillingSettlementsOnce()
			require.NoError(t, db.First(&sub, 955).Error)
			require.Zero(t, sub.AmountUsed, "completed settlement replay must not debit the reset quota")
		})
	}
}

func TestSubscriptionPeriodBoundaryDelayedStreamSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	for _, route := range []string{"responses_native", "chat_native", "chat_to_responses", "responses_to_chat_custom", "responses_to_chat_global", "claude_to_responses", "gemini_to_responses"} {
		for _, usage := range []string{"known", "zero", "missing", "above_reservation", "at_reservation"} {
			for _, boundary := range []string{"keep_time_rejected", "admin_advanced", "due_reset", "expired"} {
				t.Run(strings.Join([]string{route, usage, boundary}, "/"), func(t *testing.T) {
					funding := streamFundingCases()[0]
					db := setupStreamFundingLedger(t, funding)
					now := model.GetDBTimestamp()
					require.NoError(t, db.Model(&model.SubscriptionPlan{}).Where("id = ?", 954).
						Update("quota_reset_period", model.SubscriptionResetDaily).Error)
					model.InvalidateSubscriptionPlanCache(954)
					t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(954) })
					anchor := now - 3600
					if boundary == "due_reset" {
						// Advance persisted scheduler timestamps instead of sleeping
						// a day; the reservation captures the actual old-period anchor.
						anchor = now - 86400 - 60
					}
					require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 955).
						Updates(map[string]any{"last_reset_time": anchor, "next_reset_time": now + 86400, "end_time": now + 172800}).Error)
					channelType, frames := streamFundingFrames(t, route, usage)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
					}))
					t.Cleanup(upstream.Close)
					fingerprint := sha256.Sum256([]byte(t.Name()))
					requestID := fmt.Sprintf("period-%x", fingerprint[:16])
					c, info, recorder := streamFundingContext(t, route, requestID, upstream.URL, channelType, funding)
					require.Nil(t, service.PreConsumeBilling(c, 100, info))
					switch boundary {
					case "keep_time_rejected":
						_, err := model.AdminResetUserSubscriptionsByPlan(951, 954, false)
						require.ErrorIs(t, err, model.ErrSubscriptionResetPendingBilling)
					case "admin_advanced":
						_, err := model.AdminResetUserSubscriptionsByPlan(951, 954, true)
						require.NoError(t, err)
					case "due_reset":
						require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 955).Update("next_reset_time", now-1).Error)
						count, err := model.ResetDueSubscriptions(10)
						require.NoError(t, err)
						require.Equal(t, 1, count)
						var reset model.UserSubscription
						require.NoError(t, db.First(&reset, 955).Error)
						require.NotEqual(t, anchor, reset.LastResetTime)
					case "expired":
						require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 955).Update("end_time", now-1).Error)
						count, err := model.ExpireDueSubscriptions(10)
						require.NoError(t, err)
						require.Equal(t, 1, count)
						require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
							StartTime: now - 3600, EndTime: now + 3600, LastResetTime: now}).Error)
					}
					newInfo := &relaycommon.RelayInfo{RequestId: requestID + "-new", UserId: 951, TokenId: 952, TokenKey: info.TokenKey,
						OriginModelName: "gpt-test", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
					_, admissionErr := service.NewBillingSession(c, newInfo, 200)
					require.Nil(t, admissionErr)
					require.NoError(t, db.Create(&model.UserSubscription{Id: 957, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
						StartTime: now - 1800, EndTime: now + 1800, LastResetTime: now}).Error)
					info.UserSetting.BillingPreference = "wallet_only"
					var apiErr *types.MaxAPIError
					if info.RelayFormat == types.RelayFormatOpenAI {
						apiErr = TextHelper(c, info)
					} else {
						apiErr = ResponsesHelper(c, info)
					}
					if apiErr != nil {
						require.True(t, types.IsSkipRetryError(apiErr))
						service.HandleFailedBilling(c, info, apiErr)
						service.HandleFailedBilling(c, info, apiErr)
					}
					require.Contains(t, recorder.Body.String(), "partial output")
					quota := 6
					switch usage {
					case "zero":
						quota = 0
					case "missing":
						quota = 10 + service.EstimateTokenByModel("gpt-test", "partial output")
						if route == "responses_native" {
							quota = 10 + service.CountTextToken("partial output", "gpt-test")
						}
					case "above_reservation":
						quota = 130
					case "at_reservation":
						quota = 100
					}
					var settlement model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
					require.Equal(t, "subscription", settlement.Source)
					require.Equal(t, 955, settlement.SubscriptionID)
					require.EqualValues(t, quota-100, settlement.FundingDelta)
					crossed := boundary == "admin_advanced" || boundary == "due_reset"
					if crossed {
						require.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
						require.Contains(t, settlement.LastError, model.ErrSubscriptionSettlementPeriodChanged.Error())
						require.Zero(t, settlement.AppliedFundingDelta)
						require.Zero(t, settlement.AppliedTokenDelta)
					} else {
						require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
						require.EqualValues(t, quota-100, settlement.AppliedFundingDelta)
						require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
					}
					var outboxBefore int64
					require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&outboxBefore).Error)
					for range 2 {
						_, already, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
							OperationKey: settlement.OperationKey, Source: settlement.Source, UserID: settlement.UserID,
							SubscriptionID: settlement.SubscriptionID, TokenID: settlement.TokenID, TokenKey: info.TokenKey,
							FundingDelta: settlement.FundingDelta, TokenDelta: settlement.TokenDelta, SubscriptionPreConsumeRequestID: requestID,
						})
						if crossed {
							require.ErrorIs(t, err, model.ErrBillingSettlementManualReview)
							require.False(t, already)
						} else {
							require.NoError(t, err)
							require.True(t, already)
						}
						model.ProcessPendingBillingSettlementsOnce()
					}
					var outboxAfter int64
					require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&outboxAfter).Error)
					require.Equal(t, outboxBefore, outboxAfter)
					var user model.User
					var token model.Token
					var channel model.Channel
					var sub, other model.UserSubscription
					require.NoError(t, db.First(&user, 951).Error)
					require.NoError(t, db.First(&token, 952).Error)
					require.NoError(t, db.First(&channel, 953).Error)
					require.NoError(t, db.First(&sub, 955).Error)
					require.NoError(t, db.First(&other, 957).Error)
					require.EqualValues(t, 1000, user.Quota)
					require.Zero(t, other.AmountUsed)
					wantQuota, wantUsed, wantLogs := quota, quota, 1
					if boundary == "keep_time_rejected" {
						wantUsed += 200
					} else if crossed {
						wantQuota, wantUsed, wantLogs = 100, 200, 0
					}
					require.EqualValues(t, wantUsed, sub.AmountUsed)
					require.EqualValues(t, 800-wantQuota, token.RemainQuota)
					require.EqualValues(t, 200+wantQuota, token.UsedQuota)
					projectedQuota, requests := quota, 1
					if crossed {
						projectedQuota, requests = 0, 0
					} else if quota == 0 {
						requests = 0
					}
					require.EqualValues(t, projectedQuota, user.UsedQuota)
					require.EqualValues(t, requests, user.RequestCount)
					require.EqualValues(t, projectedQuota, channel.UsedQuota)
					var logs, finalizes int64
					require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
					require.EqualValues(t, wantLogs, logs)
					require.NoError(t, db.Model(&model.BillingSettlement{}).Where("operation_key = ?", settlement.OperationKey).Count(&finalizes).Error)
					require.EqualValues(t, 1, finalizes)
					if boundary == "expired" {
						require.Equal(t, "expired", sub.Status)
						other = model.UserSubscription{}
						require.NoError(t, db.First(&other, 956).Error)
						require.EqualValues(t, 200, other.AmountUsed)
					}
				})
			}
		}
	}
}

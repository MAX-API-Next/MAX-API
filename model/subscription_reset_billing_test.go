package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedResetBillingReservation(t *testing.T) (SubscriptionPlan, UserSubscription, string) {
	t.Helper()
	setupUserUpdateTestState(t)
	now := GetDBTimestamp()
	plan := SubscriptionPlan{Id: 4503, Title: "reset-billing", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetDaily}
	sub := UserSubscription{Id: 4504, UserId: 4501, PlanId: plan.Id, Status: "active", AmountTotal: 1000,
		StartTime: now - 3600, EndTime: now + 172800, LastResetTime: now, NextResetTime: now + 86400}
	require.NoError(t, DB.Create(&User{Id: sub.UserId, Username: "reset-billing-user", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&Token{Id: 4502, UserId: sub.UserId, Key: "synthetic-reset-billing", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, DB.Create(&plan).Error)
	require.NoError(t, DB.Create(&sub).Error)
	requestID := "reset-billing-reservation"
	_, err := PreConsumeTokenAndUserSubscription(requestID, sub.UserId, 4502, "synthetic-reset-billing", "gpt-test", 0, 100)
	require.NoError(t, err)
	return plan, sub, requestID
}

func resetBillingFinalize(sub UserSubscription, requestID string) BillingSettlementInput {
	return BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey(requestID), Source: BillingSettlementSourceSubscription,
		UserID: sub.UserId, SubscriptionID: sub.Id, TokenID: 4502, TokenKey: "synthetic-reset-billing", FundingDelta: -94, TokenDelta: -94,
		SubscriptionPreConsumeRequestID: requestID}
}

func TestSubscriptionResetScopesProvenEarlierPeriod(t *testing.T) {
	for _, state := range []string{BillingSettlementStatusManual, BillingSettlementStatusPending, "outcome_unknown"} {
		for _, evidence := range []string{"earlier", "missing", "wrong_user", "future", "current"} {
			for _, resetPlan := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/plan_%t", state, evidence, resetPlan), func(t *testing.T) {
					plan, sub, requestID := seedResetBillingReservation(t)
					require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
						var locked UserSubscription
						if err := withRowLock(tx).First(&locked, sub.Id).Error; err != nil {
							return err
						}
						return resetUserSubscriptionTx(tx, &locked, &plan, sub.LastResetTime+1, true)
					}))
					input := resetBillingFinalize(sub, requestID)
					_, _, err := ApplyBillingSettlementOnce(input)
					require.ErrorIs(t, err, ErrSubscriptionSettlementPeriodChanged)
					require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", input.OperationKey).Update("status", state).Error)
					var current UserSubscription
					require.NoError(t, DB.First(&current, sub.Id).Error)
					switch evidence {
					case "missing":
						require.NoError(t, DB.Where("request_id = ?", requestID).Delete(&SubscriptionPreConsumeRecord{}).Error)
					case "wrong_user":
						require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).Update("user_id", 9999).Error)
					case "future", "current":
						anchor := current.LastResetTime
						if evidence == "future" {
							anchor++
						}
						require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).Update("subscription_last_reset_time", anchor).Error)
					}
					// New-period usage is independently reserved and durably finalized.
					currentID := requestID + "-current"
					_, err = PreConsumeTokenAndUserSubscription(currentID, sub.UserId, 4502, "synthetic-reset-billing", "gpt-test", 0, 200)
					require.NoError(t, err)
					currentInput := resetBillingFinalize(current, currentID)
					currentInput.FundingDelta, currentInput.TokenDelta = 0, 0
					_, _, err = ApplyBillingSettlementOnce(currentInput)
					require.NoError(t, err)
					if resetPlan {
						_, err = AdminResetPlanSubscriptions(plan.Id, false)
					} else {
						_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
					}
					wantUsed := int64(200)
					if evidence == "earlier" {
						require.NoError(t, err, "a proven earlier-period intent cannot mutate the current period")
						wantUsed = 0
						for range 2 {
							_, _, replayErr := ApplyBillingSettlementOnce(input)
							require.Error(t, replayErr, "old-period replay remains unapplied")
						}
					} else {
						require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
					}
					var got UserSubscription
					require.NoError(t, DB.First(&got, sub.Id).Error)
					require.Equal(t, wantUsed, got.AmountUsed)
					require.Equal(t, current.LastResetTime, got.LastResetTime)
					var token Token
					require.NoError(t, DB.First(&token, 4502).Error)
					require.EqualValues(t, 700, token.RemainQuota)
					var old BillingSettlement
					require.NoError(t, DB.Where("operation_key = ?", input.OperationKey).First(&old).Error)
					require.NotEqual(t, BillingSettlementStatusApplied, old.Status)
					require.Zero(t, old.AppliedFundingDelta)
					require.Zero(t, old.AppliedTokenDelta)
					var count int64
					require.NoError(t, DB.Model(&BillingSettlement{}).Count(&count).Error)
					require.EqualValues(t, 2, count)
				})
			}
		}
	}
}

func TestSubscriptionResetNeverKeepsCurrentZeroAnchorGuard(t *testing.T) {
	for _, state := range []string{BillingSettlementStatusManual, BillingSettlementStatusPending, "outcome_unknown"} {
		for _, advance := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/advance_%t", state, advance), func(t *testing.T) {
				plan, sub, requestID := seedResetBillingReservation(t)
				require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Update("quota_reset_period", SubscriptionResetNever).Error)
				InvalidateSubscriptionPlanCache(plan.Id)
				t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
				require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]any{"last_reset_time": 0, "next_reset_time": 0}).Error)
				require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).Update("subscription_last_reset_time", 0).Error)
				input := resetBillingFinalize(sub, requestID)
				input.FundingDelta, input.TokenDelta = 2000, 2000
				_, _, err := ApplyBillingSettlementOnce(input)
				require.ErrorIs(t, err, ErrSubscriptionQuotaInsufficient)
				require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", input.OperationKey).Update("status", state).Error)
				_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, advance)
				require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
				var got UserSubscription
				require.NoError(t, DB.First(&got, sub.Id).Error)
				require.EqualValues(t, 100, got.AmountUsed)
				require.Zero(t, got.LastResetTime)
				var token Token
				require.NoError(t, DB.First(&token, 4502).Error)
				require.EqualValues(t, 900, token.RemainQuota)
			})
		}
	}
}

func TestSubscriptionResetValidatesFundingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates map[string]any
		missing bool
		allowed bool
	}{
		{name: "missing_finalize", missing: true},
		{name: "funding_pending", updates: map[string]any{"status": BillingSettlementStatusPending}},
		{name: "funding_manual", updates: map[string]any{"status": BillingSettlementStatusManual}},
		{name: "funding_unknown", updates: map[string]any{"status": "outcome_unknown"}},
		{name: "applied", allowed: true},
		{name: "legacy_applied", updates: map[string]any{"status": ""}, allowed: true},
		{name: "effect_pending", updates: map[string]any{"effect_status": BillingSettlementEffectPending}, allowed: true},
		{name: "wrong_source", updates: map[string]any{"source": BillingSettlementSourceWallet}},
		{name: "wrong_user", updates: map[string]any{"user_id": 9999}},
		{name: "wrong_subscription", updates: map[string]any{"subscription_id": 9999}},
		{name: "wrong_request", updates: map[string]any{"subscription_pre_consume_request_id": "another-request"}},
		{name: "wrong_operation", updates: map[string]any{"operation_key": BillingRequestFinalizeOperationKey("another-request")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, sub, requestID := seedResetBillingReservation(t)
			wantUsed := int64(100)
			if !tc.missing {
				_, _, err := ApplyBillingSettlementOnce(resetBillingFinalize(sub, requestID))
				require.NoError(t, err)
				wantUsed = 6
				if tc.updates != nil {
					require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", BillingRequestFinalizeOperationKey(requestID)).Updates(tc.updates).Error)
				}
			}
			_, err := AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
			if tc.allowed {
				require.NoError(t, err)
				wantUsed = 0
			} else {
				require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
			}
			var got UserSubscription
			require.NoError(t, DB.First(&got, sub.Id).Error)
			require.Equal(t, wantUsed, got.AmountUsed)
			require.Equal(t, sub.LastResetTime, got.LastResetTime)
			require.Equal(t, sub.NextResetTime, got.NextResetTime)
			var token Token
			require.NoError(t, DB.First(&token, 4502).Error)
			wantToken := 994
			if tc.missing {
				wantToken = 900
			}
			require.EqualValues(t, wantToken, token.RemainQuota)
		})
	}
}

func TestSubscriptionResetSameSecondRefusesUnsettledReservation(t *testing.T) {
	plan, sub, _ := seedResetBillingReservation(t)
	err := DB.Transaction(func(tx *gorm.DB) error {
		var locked UserSubscription
		if err := withRowLock(tx).First(&locked, sub.Id).Error; err != nil {
			return err
		}
		return resetUserSubscriptionTx(tx, &locked, &plan, sub.LastResetTime, true)
	})
	require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
	var got UserSubscription
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.EqualValues(t, 100, got.AmountUsed)
	require.Equal(t, sub.LastResetTime, got.LastResetTime)
}

func TestSubscriptionPlanResetRollsBackEarlierSubscriptions(t *testing.T) {
	plan, sub, _ := seedResetBillingReservation(t)
	first := sub
	first.Id, first.AmountUsed, first.EndTime = 4505, 25, sub.EndTime-1
	require.NoError(t, DB.Create(&first).Error)
	_, err := AdminResetPlanSubscriptions(plan.Id, false)
	require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
	var got UserSubscription
	require.NoError(t, DB.First(&got, first.Id).Error)
	require.EqualValues(t, 25, got.AmountUsed, "an earlier reset must roll back when a later subscription is blocked")
	got = UserSubscription{}
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.EqualValues(t, 100, got.AmountUsed)
}

func TestSubscriptionResetLookupFailureDoesNotEraseReservation(t *testing.T) {
	plan, sub, _ := seedResetBillingReservation(t)
	injected := errors.New("reset funding lookup unavailable")
	const callback = "test:reset_billing_lookup_failure"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_settlements" {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })
	_, err := AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
	require.ErrorIs(t, err, injected)
	var got UserSubscription
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.EqualValues(t, 100, got.AmountUsed)
}

func TestSubscriptionResetRetainsManualFundingGuardAfterTombstoneCleanup(t *testing.T) {
	plan, sub, requestID := seedResetBillingReservation(t)
	input := resetBillingFinalize(sub, requestID)
	input.FundingDelta, input.TokenDelta = 2000, 2000
	_, _, err := ApplyBillingSettlementOnce(input)
	require.ErrorIs(t, err, ErrSubscriptionQuotaInsufficient)
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).
		Update("created_at", GetDBTimestamp()-86400).Error)
	deleted, err := CleanupSubscriptionPreConsumeRecords(3600)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
	require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
	var got UserSubscription
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.EqualValues(t, 100, got.AmountUsed)
}

func TestSubscriptionResetChecksAllReservationsBeyondOneBatch(t *testing.T) {
	plan, sub, requestID := seedResetBillingReservation(t)
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).Update("id", 2000).Error)
	var records []SubscriptionPreConsumeRecord
	var applied []BillingSettlement
	for i := 1; i <= 1050; i++ {
		id := fmt.Sprintf("reset-history-%d", i)
		records = append(records, SubscriptionPreConsumeRecord{Id: i, RequestId: id, UserId: sub.UserId, UserSubscriptionId: sub.Id,
			SubscriptionLastResetTime: sub.LastResetTime, PreConsumed: 1, Status: "consumed"})
		applied = append(applied, BillingSettlement{OperationKey: BillingRequestFinalizeOperationKey(id), Source: BillingSettlementSourceSubscription,
			UserID: sub.UserId, SubscriptionID: sub.Id, SubscriptionPreConsumeRequestID: id, Status: BillingSettlementStatusApplied})
	}
	require.NoError(t, DB.CreateInBatches(&records, 100).Error)
	require.NoError(t, DB.CreateInBatches(&applied, 100).Error)
	_, err := AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
	require.ErrorIs(t, err, ErrSubscriptionResetPendingBilling)
	var got UserSubscription
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.EqualValues(t, 100, got.AmountUsed)
}

func TestSubscriptionResetAllowsEffectOnlyRecovery(t *testing.T) {
	plan, sub, requestID := seedResetBillingReservation(t)
	input := resetBillingFinalize(sub, requestID)
	input.Effect = &BillingSettlementEffect{LogType: LogTypeConsume, TokenID: 4502, ModelName: "gpt-test", UpdateUsage: true,
		PromptTokens: 4, CompletionTokens: 2, Quota: 6, QuotaIsActual: true}
	_, _, err := ApplyBillingSettlementOnce(input)
	require.NoError(t, err)
	_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, ProcessBillingSettlementEffect(input.OperationKey))
		_, already, err := ApplyBillingSettlementOnce(input)
		require.NoError(t, err)
		require.True(t, already)
	}
	var got UserSubscription
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.Zero(t, got.AmountUsed)
	var token Token
	require.NoError(t, DB.First(&token, 4502).Error)
	require.EqualValues(t, 994, token.RemainQuota)
	var count int64
	require.NoError(t, DB.Model(&Log{}).Where("type = ?", LogTypeConsume).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSubscriptionResetAllowsAppliedTaskFunding(t *testing.T) {
	plan, sub, requestID := seedResetBillingReservation(t)
	require.NoError(t, DB.Create(&Task{ID: 4506, Quota: 100}).Error)
	input := resetBillingFinalize(sub, requestID)
	input.TaskID, input.TaskQuota, input.TaskQuotaTarget = 4506, 100, 6
	input.OperationKey = BillingTaskFinalizeOperationKey(input.TaskID)
	_, _, err := ApplyBillingSettlementOnce(input)
	require.NoError(t, err)
	_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
	require.NoError(t, err, "an applied task finalize owns its original subscription reservation")
	var got UserSubscription
	require.NoError(t, DB.First(&got, sub.Id).Error)
	require.Zero(t, got.AmountUsed)
}

func TestSubscriptionResetConcurrentFinalize(t *testing.T) {
	for round := range 12 {
		t.Run(fmt.Sprintf("round_%02d", round), func(t *testing.T) {
			plan, sub, requestID := seedResetBillingReservation(t)
			input := resetBillingFinalize(sub, requestID)
			start := make(chan struct{})
			resetDone, settleDone := make(chan error, 1), make(chan error, 1)
			go func() {
				<-start
				_, err := AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
				resetDone <- err
			}()
			go func() {
				<-start
				_, _, err := ApplyBillingSettlementOnce(input)
				settleDone <- err
			}()
			close(start)
			settleErr, resetErr := <-settleDone, <-resetDone
			require.NoError(t, settleErr)
			wantUsed := int64(0)
			if resetErr != nil {
				require.ErrorIs(t, resetErr, ErrSubscriptionResetPendingBilling)
				wantUsed = 6
			}
			var got UserSubscription
			require.NoError(t, DB.First(&got, sub.Id).Error)
			require.Equal(t, wantUsed, got.AmountUsed)
			var token Token
			require.NoError(t, DB.First(&token, 4502).Error)
			require.EqualValues(t, 994, token.RemainQuota)
			delta, already, err := ApplyBillingSettlementOnce(input)
			require.NoError(t, err)
			require.True(t, already)
			require.EqualValues(t, -94, delta)
			_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, false)
			require.NoError(t, err)
			require.NoError(t, DB.First(&got, sub.Id).Error)
			require.Zero(t, got.AmountUsed)
		})
	}
}

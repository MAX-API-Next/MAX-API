package model

import (
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConcurrentBillingPreConsumeSelectionAllowsOnlyOneFundingSource(t *testing.T) {
	setupUserUpdateTestState(t)
	const requestID = "regression:concurrent-funding-source"

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, source := range []string{BillingSettlementSourceWallet, BillingSettlementSourceSubscription} {
		source := source
		go func() {
			<-start
			results <- DB.Transaction(func(tx *gorm.DB) error {
				return claimBillingPreConsumeSourceTx(tx, BillingPreConsumeSelection{
					RequestID: requestID, Source: source, UserID: 940, TokenID: 941,
					ModelName: "concurrent-model", RequestedQuota: 10, EffectiveQuota: 10,
				})
			})
		}()
	}
	close(start)

	var succeeded, conflicted int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrBillingSettlementOperationConflict):
			conflicted++
		default:
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, conflicted)

	source, found, err := ResolveBillingPreConsumeSource(requestID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Contains(t, []string{BillingSettlementSourceWallet, BillingSettlementSourceSubscription}, source)
}

func TestHasUnresolvedPositiveFinalizeSettlement(t *testing.T) {
	setupUserUpdateTestState(t)

	const blockingUserID = 941
	const nonBlockingUserID = 942
	now := time.Now().Unix()
	records := []BillingSettlement{
		{
			OperationKey: "request:pending-finalize:finalize", Source: BillingSettlementSourceWallet,
			UserID: blockingUserID, FundingDelta: 10, TokenDelta: 10,
			Status: BillingSettlementStatusPending, NextAttempt: now, CreatedAt: now, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:manual-finalize:finalize", Source: BillingSettlementSourceWallet,
			UserID: blockingUserID, FundingDelta: 5, TokenDelta: 5,
			Status: BillingSettlementStatusManual, CreatedAt: now, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:refund-finalize:finalize", Source: BillingSettlementSourceWallet,
			UserID: nonBlockingUserID, FundingDelta: -5, TokenDelta: -5,
			Status: BillingSettlementStatusManual, CreatedAt: now, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:failed-reserve:reserve:1", Source: BillingSettlementSourceWallet,
			UserID: nonBlockingUserID, FundingDelta: 5, TokenDelta: 5,
			Status: BillingSettlementStatusManual, CreatedAt: now, UpdatedAt: now, Revision: 1,
		},
	}
	require.NoError(t, DB.Create(&records).Error)

	blocked, err := HasUnresolvedPositiveFinalizeSettlement(blockingUserID)

	require.NoError(t, err)
	assert.True(t, blocked)

	blocked, err = HasUnresolvedPositiveFinalizeSettlement(nonBlockingUserID)
	require.NoError(t, err)
	assert.False(t, blocked)
}

func TestBillingRequestFinalizeOperationKey(t *testing.T) {
	assert.Equal(t, "request:example-request:finalize", BillingRequestFinalizeOperationKey("example-request"))
	assert.Equal(t, "request:%:finalize", BillingRequestFinalizeOperationKey("%"))
}

func TestSubscriptionSettlementUsesActualAppliedRefundForToken(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{Id: 901, Username: "settlement-refund-user", Quota: 0, Status: common.UserStatusEnabled}
	token := Token{Id: 902, UserId: user.Id, Key: "settlement-refund-token", RemainQuota: 100, UsedQuota: 10, Status: common.TokenStatusEnabled}
	subscription := UserSubscription{
		Id: 903, UserId: user.Id, AmountTotal: 100, AmountUsed: 0,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active",
	}
	preConsume := SubscriptionPreConsumeRecord{
		RequestId: "regression:subscription-partial-refund-request", UserId: user.Id,
		TokenId: token.Id, UserSubscriptionId: subscription.Id, PreConsumed: 10, Status: "consumed",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&preConsume).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey:                    "regression:subscription-partial-refund",
		Source:                          BillingSettlementSourceSubscription,
		UserID:                          user.Id,
		SubscriptionID:                  subscription.Id,
		TokenID:                         token.Id,
		FundingDelta:                    -10,
		TokenDelta:                      -10,
		SubscriptionPreConsumeRequestID: preConsume.RequestId,
	})
	require.NoError(t, err)

	var gotToken Token
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	require.EqualValues(t, 100, gotToken.RemainQuota)
	require.EqualValues(t, 10, gotToken.UsedQuota)
}

func TestFailedSettlementLeavesDurablePendingOperation(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 904, Username: "pending-settlement-user", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	input := BillingSettlementInput{
		OperationKey: "regression:pending-settlement",
		Source:       BillingSettlementSourceWallet,
		UserID:       user.Id,
		TokenID:      905,
		FundingDelta: 10,
		TokenDelta:   10,
	}
	_, _, err := ApplyBillingSettlementOnce(input)
	require.Error(t, err)

	var pending BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", input.OperationKey).First(&pending).Error)
	require.Equal(t, "pending", pending.Status)

	token := Token{Id: 905, UserId: user.Id, Key: "pending-settlement-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	_, _, err = ApplyBillingSettlementOnce(input)
	require.NoError(t, err)
	require.EqualValues(t, 90, getRegressionUserQuota(t, user.Id))
	require.EqualValues(t, 90, getRegressionTokenRemainQuota(t, token.Id))
}

func TestBillingSettlementPendingInvalidationBypassesStaleUserCache(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 977, Username: "settlement-pending-cache-user", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	client := useCacheMutationRedis(t)
	cacheUserForRetryTest(t, client, user)
	hook := newBlockingCacheInvalidationHook()
	client.AddHook(hook)
	t.Cleanup(hook.unblock)
	done := make(chan error, 1)

	go func() {
		_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
			OperationKey: "regression:settlement-pending-cache",
			Source:       BillingSettlementSourceWallet,
			UserID:       user.Id,
			FundingDelta: 10,
		})
		done <- err
	}()
	waitForCacheHook(t, hook.started, "settlement invalidation start")

	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.EqualValues(t, 90, quota)

	hook.unblock()
	require.NoError(t, <-done)
}

func TestPendingPreConsumeSettlementRetryClaimsPersistedSelection(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 966, Username: "pending-pre-consume-user", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 967, UserId: user.Id, Key: "pending-pre-consume-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	input := BillingSettlementInput{
		OperationKey:             "request:pending-pre-consume-selection:pre-consume",
		Source:                   BillingSettlementSourceWallet,
		UserID:                   user.Id,
		TokenID:                  token.Id,
		TokenKey:                 token.Key,
		FundingDelta:             10,
		TokenDelta:               10,
		ManualOnFailure:          true,
		PreConsumeRequestID:      "pending-pre-consume-selection",
		PreConsumeModelName:      "selection-model",
		PreConsumeRequestedQuota: 12,
		PreConsumeEffectiveQuota: 10,
	}
	_, alreadyApplied, err := ensureBillingSettlementRecord(input)
	require.NoError(t, err)
	require.False(t, alreadyApplied)

	ProcessPendingBillingSettlementsOnce()

	var selection BillingPreConsumeSelection
	require.NoError(t, DB.Where("request_id = ?", input.PreConsumeRequestID).First(&selection).Error)
	assert.Equal(t, BillingSettlementSourceWallet, selection.Source)
	assert.EqualValues(t, user.Id, selection.UserID)
	assert.EqualValues(t, token.Id, selection.TokenID)
	assert.Equal(t, "selection-model", selection.ModelName)
	assert.EqualValues(t, 12, selection.RequestedQuota)
	assert.EqualValues(t, 10, selection.EffectiveQuota)
	assert.EqualValues(t, 90, getRegressionUserQuota(t, user.Id))
	assert.EqualValues(t, 90, getRegressionTokenRemainQuota(t, token.Id))
}

func TestWalletSettlementRollsBackWhenCacheOutboxCannotPersist(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 960, Username: "wallet-outbox-rollback", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 961, UserId: user.Id, Key: "wallet-outbox-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	failCacheInvalidationOutboxInserts(t)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "regression:wallet-cache-outbox",
		Source:       BillingSettlementSourceWallet,
		UserID:       user.Id,
		TokenID:      token.Id,
		FundingDelta: 10,
		TokenDelta:   10,
	})
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionUserQuota(t, user.Id))
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
}

func TestSubscriptionPreConsumeRollsBackWhenCacheOutboxCannotPersist(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now()
	user := User{Id: 962, Username: "subscription-outbox-rollback", Status: common.UserStatusEnabled}
	plan := SubscriptionPlan{Id: 963, Title: "outbox-plan", Enabled: true, TotalAmount: 100}
	subscription := UserSubscription{
		Id: 964, UserId: user.Id, PlanId: plan.Id, AmountTotal: 100,
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(), Status: "active",
	}
	token := Token{Id: 965, UserId: user.Id, Key: "subscription-outbox-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&plan).Error)
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&token).Error)
	failCacheInvalidationOutboxInserts(t)

	_, err := PreConsumeTokenAndUserSubscription(
		"regression:subscription-cache-outbox",
		user.Id,
		token.Id,
		token.Key,
		"outbox-model",
		0,
		10,
	)
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
	var storedSubscription UserSubscription
	require.NoError(t, DB.First(&storedSubscription, subscription.Id).Error)
	assert.Zero(t, storedSubscription.AmountUsed)
	var preConsumeCount int64
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).
		Where("request_id = ?", "regression:subscription-cache-outbox").
		Count(&preConsumeCount).Error)
	assert.Zero(t, preConsumeCount)
}

func failCacheInvalidationOutboxInserts(t *testing.T) {
	t.Helper()
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })
	failCacheOutboxInserts(t)
}

func TestFailedRealtimeReserveIsNotRetriedAfterRequestAborts(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 906, Username: "failed-reserve-user", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	input := BillingSettlementInput{
		OperationKey: "request:failed-reserve:reserve:1", Source: BillingSettlementSourceWallet,
		UserID: user.Id, TokenID: 907, FundingDelta: 10, TokenDelta: 10, ManualOnFailure: true,
	}
	_, _, err := ApplyBillingSettlementOnce(input)
	require.Error(t, err)

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", input.OperationKey).First(&settlement).Error)
	require.Equal(t, BillingSettlementStatusManual, settlement.Status)
	token := Token{Id: 907, UserId: user.Id, Key: "failed-reserve-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	processPendingBillingSettlements()
	require.EqualValues(t, 100, getRegressionUserQuota(t, user.Id))
	require.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
}

func TestBillingSettlementRejectsOperationKeyReuseWithoutSecondMutation(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 915, Username: "settlement-operation-reuse-user", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 916, UserId: user.Id, Key: "settlement-operation-reuse-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	input := BillingSettlementInput{
		OperationKey: "regression:operation-reuse",
		Source:       BillingSettlementSourceWallet,
		UserID:       user.Id,
		TokenID:      token.Id,
		FundingDelta: 10,
		TokenDelta:   10,
	}
	_, _, err := ApplyBillingSettlementOnce(input)
	require.NoError(t, err)

	input.FundingDelta = 20
	input.TokenDelta = 20
	_, _, err = ApplyBillingSettlementOnce(input)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBillingSettlementOperationConflict)
	assert.EqualValues(t, 90, getRegressionUserQuota(t, user.Id))
	assert.EqualValues(t, 90, getRegressionTokenRemainQuota(t, token.Id))

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "regression:operation-reuse").First(&settlement).Error)
	assert.Equal(t, BillingSettlementStatusApplied, settlement.Status)
	assert.EqualValues(t, 10, settlement.FundingDelta)
	assert.EqualValues(t, 10, settlement.AppliedFundingDelta)
}

func TestBillingSettlementDoesNotTrustDuplicateInsertRowsAffected(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 917, Username: "settlement-found-rows-user", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 918, UserId: user.Id, Key: "settlement-found-rows-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	input := BillingSettlementInput{
		OperationKey: "regression:duplicate-found-rows", Source: BillingSettlementSourceWallet,
		UserID: user.Id, TokenID: token.Id, FundingDelta: 10, TokenDelta: 10,
	}
	_, _, err := ApplyBillingSettlementOnce(input)
	require.NoError(t, err)

	callbackName := "test:billing-settlement-duplicate-found-rows"
	require.NoError(t, DB.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "billing_settlements" {
			tx.RowsAffected = 1
		}
	}))
	t.Cleanup(func() { DB.Callback().Create().Remove(callbackName) })

	applied, alreadyApplied, err := ApplyBillingSettlementOnce(input)
	require.NoError(t, err)
	assert.True(t, alreadyApplied)
	assert.EqualValues(t, 10, applied)
	assert.EqualValues(t, 90, getRegressionUserQuota(t, user.Id))
	assert.EqualValues(t, 90, getRegressionTokenRemainQuota(t, token.Id))
}

func TestBillingSettlementUpdatesTaskQuotaAtomically(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 906, Username: "task-settlement-user", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 907, UserId: user.Id, Key: "task-settlement-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	task := Task{ID: 908, TaskID: "task-settlement", UserId: user.Id, Quota: 10, Status: TaskStatusSuccess}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DB.Create(&task).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey:    "regression:task-settlement",
		Source:          BillingSettlementSourceWallet,
		UserID:          user.Id,
		TokenID:         token.Id,
		FundingDelta:    5,
		TokenDelta:      5,
		TaskID:          task.ID,
		TaskQuota:       10,
		TaskQuotaTarget: 15,
	})
	require.NoError(t, err)
	require.EqualValues(t, 15, getRegressionTaskQuota(t, task.ID))
	require.EqualValues(t, 95, getRegressionUserQuota(t, user.Id))
}

func TestBillingSettlementTaskConflictRollsBackAllLedgerLegs(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 909, Username: "task-conflict-user", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 910, UserId: user.Id, Key: "task-conflict-token", RemainQuota: 100, UsedQuota: 10, Status: common.TokenStatusEnabled}
	task := Task{ID: 911, TaskID: "task-conflict", UserId: user.Id, Quota: 20, Status: TaskStatusSuccess}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DB.Create(&task).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "regression:task-conflict", Source: BillingSettlementSourceWallet,
		UserID: user.Id, TokenID: token.Id, FundingDelta: 5, TokenDelta: 5,
		TaskID: task.ID, TaskQuota: 10, TaskQuotaTarget: 15,
	})
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionUserQuota(t, user.Id))
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
	assert.EqualValues(t, 20, getRegressionTaskQuota(t, task.ID))
	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "regression:task-conflict").First(&settlement).Error)
	assert.Equal(t, BillingSettlementStatusManual, settlement.Status)
}

func TestSubscriptionSettlementRefusesCrossResetPeriodReplay(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 912, Username: "subscription-period-user", Quota: 0, Status: common.UserStatusEnabled}
	token := Token{Id: 913, UserId: user.Id, Key: "subscription-period-token", RemainQuota: 100, UsedQuota: 10, Status: common.TokenStatusEnabled}
	subscription := UserSubscription{
		Id: 914, UserId: user.Id, AmountTotal: 100, AmountUsed: 10,
		LastResetTime: time.Now().Add(time.Hour).Unix(), StartTime: time.Now().Add(-time.Hour).Unix(),
		EndTime: time.Now().Add(time.Hour).Unix(), Status: "active",
	}
	preConsume := SubscriptionPreConsumeRecord{
		RequestId: "regression:period-request", UserId: user.Id,
		TokenId: token.Id, UserSubscriptionId: subscription.Id, PreConsumed: 10, Status: "consumed",
		CreatedAt: time.Now().Add(-time.Hour).Unix(), UpdatedAt: time.Now().Add(-time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&preConsume).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "regression:period-replay", Source: BillingSettlementSourceSubscription,
		UserID: user.Id, SubscriptionID: subscription.Id, TokenID: token.Id,
		FundingDelta: -10, TokenDelta: -10,
		SubscriptionPreConsumeRequestID: preConsume.RequestId,
	})
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
	var gotSub UserSubscription
	require.NoError(t, DB.First(&gotSub, subscription.Id).Error)
	assert.EqualValues(t, 10, gotSub.AmountUsed)
	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "regression:period-replay").First(&settlement).Error)
	assert.Equal(t, BillingSettlementStatusManual, settlement.Status)
}

func TestSubscriptionSettlementRefusesSameSecondResetReplay(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 919, Username: "subscription-same-second-user", Quota: 0, Status: common.UserStatusEnabled}
	token := Token{Id: 920, UserId: user.Id, Key: "subscription-same-second-token", RemainQuota: 100, UsedQuota: 10, Status: common.TokenStatusEnabled}
	subscription := UserSubscription{
		Id: 921, UserId: user.Id, AmountTotal: 100, AmountUsed: 10,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active",
	}
	preConsume := SubscriptionPreConsumeRecord{
		RequestId: "regression:same-second-period-request", UserId: user.Id,
		TokenId: token.Id, UserSubscriptionId: subscription.Id, PreConsumed: 10, Status: "consumed",
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&preConsume).Error)
	periodBoundary := time.Now().Unix()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subscription.Id).Update("last_reset_time", periodBoundary).Error)
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("id = ?", preConsume.Id).Update("created_at", periodBoundary).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "regression:same-second-period-replay", Source: BillingSettlementSourceSubscription,
		UserID: user.Id, SubscriptionID: subscription.Id, TokenID: token.Id,
		FundingDelta: -10, TokenDelta: -10,
		SubscriptionPreConsumeRequestID: preConsume.RequestId,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSubscriptionSettlementPeriodChanged)
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
	var gotSub UserSubscription
	require.NoError(t, DB.First(&gotSub, subscription.Id).Error)
	assert.EqualValues(t, 10, gotSub.AmountUsed)
}

func TestSubscriptionPreConsumeReplayRejectsChangedUserAndAmount(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now()
	subscription := UserSubscription{
		Id: 921, UserId: 922, AmountTotal: 100, AmountUsed: 10,
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(), Status: "active",
	}
	record := SubscriptionPreConsumeRecord{
		RequestId: "regression:pre-consume-identity", UserId: subscription.UserId,
		UserSubscriptionId: subscription.Id, PreConsumed: 10, Status: "consumed",
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&record).Error)

	_, err := PreConsumeUserSubscription(record.RequestId, subscription.UserId, "model-a", 0, 20)
	require.Error(t, err)
	_, err = PreConsumeUserSubscription(record.RequestId, subscription.UserId+1, "model-a", 0, 10)
	require.Error(t, err)
}

func TestSubscriptionPreConsumeReplayRejectsChangedToken(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now()
	user := User{Id: 923, Username: "subscription-replay-user", Status: common.UserStatusEnabled}
	plan := SubscriptionPlan{Id: 924, Title: "replay-plan", Enabled: true, TotalAmount: 100}
	subscription := UserSubscription{
		Id: 925, UserId: user.Id, PlanId: plan.Id, AmountTotal: 100,
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(), Status: "active",
	}
	firstToken := Token{Id: 926, UserId: user.Id, Key: "subscription-first-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	secondToken := Token{Id: 927, UserId: user.Id, Key: "subscription-second-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&plan).Error)
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&firstToken).Error)
	require.NoError(t, DB.Create(&secondToken).Error)

	const requestID = "regression:pre-consume-token"
	_, err := PreConsumeTokenAndUserSubscription(requestID, user.Id, firstToken.Id, firstToken.Key, "model-a", 0, 10)
	require.NoError(t, err)
	_, err = PreConsumeTokenAndUserSubscription(requestID, user.Id, secondToken.Id, secondToken.Key, "model-a", 0, 10)
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, secondToken.Id))
}

func TestBillingSettlementRejectsTokenOwnedByAnotherUser(t *testing.T) {
	setupUserUpdateTestState(t)
	owner := User{Id: 928, Username: "settlement-owner", AffCode: "settlement-owner", Quota: 100, Status: common.UserStatusEnabled}
	other := User{Id: 929, Username: "settlement-other", AffCode: "settlement-other", Quota: 100, Status: common.UserStatusEnabled}
	otherToken := Token{Id: 930, UserId: other.Id, Key: "settlement-other-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&other).Error)
	require.NoError(t, DB.Create(&otherToken).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "regression:cross-user-token", Source: BillingSettlementSourceWallet,
		UserID: owner.Id, TokenID: otherToken.Id, FundingDelta: 10, TokenDelta: 10,
	})
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionUserQuota(t, owner.Id))
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, otherToken.Id))
}

func TestSubscriptionSettlementRejectsPreConsumeOwnedByAnotherUser(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now()
	owner := User{Id: 931, Username: "subscription-owner", AffCode: "subscription-owner", Status: common.UserStatusEnabled}
	other := User{Id: 932, Username: "subscription-other", AffCode: "subscription-other", Status: common.UserStatusEnabled}
	subscription := UserSubscription{
		Id: 933, UserId: owner.Id, AmountTotal: 100, AmountUsed: 10,
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(), Status: "active",
	}
	record := SubscriptionPreConsumeRecord{
		RequestId: "regression:cross-user-subscription", UserId: owner.Id,
		UserSubscriptionId: subscription.Id, PreConsumed: 10, Status: "consumed",
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	token := Token{Id: 934, UserId: other.Id, Key: "subscription-other-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&other).Error)
	require.NoError(t, DB.Create(&subscription).Error)
	require.NoError(t, DB.Create(&record).Error)
	require.NoError(t, DB.Create(&token).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "regression:cross-user-subscription-settlement", Source: BillingSettlementSourceSubscription,
		UserID: other.Id, SubscriptionID: subscription.Id, TokenID: token.Id,
		FundingDelta: 10, TokenDelta: 10, SubscriptionPreConsumeRequestID: record.RequestId,
	})
	require.Error(t, err)
	assert.EqualValues(t, 100, getRegressionTokenRemainQuota(t, token.Id))
	var got UserSubscription
	require.NoError(t, DB.First(&got, subscription.Id).Error)
	assert.EqualValues(t, 10, got.AmountUsed)
}

func TestSubscriptionPreConsumeCleanupRetainsIdempotencyTombstones(t *testing.T) {
	setupUserUpdateTestState(t)
	record := SubscriptionPreConsumeRecord{
		RequestId: "regression:retained-pre-consume", UserId: 935, UserSubscriptionId: 936,
		PreConsumed: 10, Status: "consumed",
		CreatedAt: time.Now().Add(-30 * 24 * time.Hour).Unix(), UpdatedAt: time.Now().Add(-30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&record).Error)

	deleted, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	var count int64
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", record.RequestId).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestSubscriptionPreConsumeCleanupDeletesOldTerminalTombstones(t *testing.T) {
	setupUserUpdateTestState(t)
	record := SubscriptionPreConsumeRecord{
		RequestId: "regression:cleanup-terminal-pre-consume", UserId: 937, UserSubscriptionId: 938,
		PreConsumed: 10, Status: "consumed",
	}
	require.NoError(t, DB.Create(&record).Error)
	oldTimestamp := time.Now().Add(-30 * 24 * time.Hour).Unix()
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).
		Where("request_id = ?", record.RequestId).
		UpdateColumns(map[string]interface{}{"created_at": oldTimestamp, "updated_at": oldTimestamp}).Error)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&BillingSettlement{
		OperationKey:                    "regression:cleanup-terminal-settlement",
		Source:                          BillingSettlementSourceSubscription,
		UserID:                          record.UserId,
		SubscriptionID:                  record.UserSubscriptionId,
		FundingDelta:                    -10,
		AppliedFundingDelta:             -10,
		Status:                          BillingSettlementStatusApplied,
		SubscriptionPreConsumeRequestID: record.RequestId,
		CreatedAt:                       now,
		UpdatedAt:                       now,
		Revision:                        1,
	}).Error)

	deleted, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	var count int64
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", record.RequestId).Count(&count).Error)
	assert.Zero(t, count)
}

func getRegressionUserQuota(t *testing.T, userID int) int64 {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	return user.Quota
}

func getRegressionTokenRemainQuota(t *testing.T, tokenID int) int64 {
	t.Helper()
	var token Token
	require.NoError(t, DB.First(&token, tokenID).Error)
	return token.RemainQuota
}

func getRegressionTaskQuota(t *testing.T, taskID int64) int {
	t.Helper()
	var task Task
	require.NoError(t, DB.First(&task, taskID).Error)
	return task.Quota
}

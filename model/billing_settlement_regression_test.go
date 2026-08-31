package model

import (
	"encoding/base64"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/billing_reconciliation_setting"
	"github.com/glebarez/sqlite"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestTaskSchemaUsesPortableAutoIncrementPrimaryKeyAndTimeoutCursorIndex(t *testing.T) {
	parsed, err := schema.Parse(&Task{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	id := parsed.LookUpField("ID")
	require.NotNil(t, id)
	assert.True(t, id.PrimaryKey)
	assert.True(t, id.AutoIncrement)

	index, ok := parsed.ParseIndexes()["idx_task_timeout_cursor"]
	require.True(t, ok)
	require.Len(t, index.Fields, 2)
	assert.Equal(t, "submit_time", index.Fields[0].DBName)
	assert.Equal(t, "id", index.Fields[1].DBName)
}

func TestTaskSQLiteCreateGeneratesID(t *testing.T) {
	truncateTables(t)
	task := &Task{TaskID: "portable-auto-increment", Status: TaskStatusNotStart}

	require.NoError(t, DB.Create(task).Error)
	assert.Positive(t, task.ID)
}

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
	setBillingReconciliationBlockDefaultForTest(t, true)
	require.True(t, DB.Migrator().HasIndex(&BillingSettlement{}, "idx_billing_settlement_admission"))
	require.True(t, DB.Migrator().HasIndex(&BillingSettlement{}, "idx_billing_settlement_status_funding"))
	require.True(t, DB.Migrator().HasIndex(&BillingSettlement{}, "idx_billing_settlement_reconciliation"))

	const blockingUserID = 941
	const nonBlockingUserID = 942
	now := time.Now().Unix()
	oldest := now - int64(time.Hour/time.Second)
	records := []BillingSettlement{
		{
			OperationKey: "request:pending-finalize:finalize", Source: BillingSettlementSourceWallet,
			UserID: blockingUserID, FundingDelta: 10, TokenDelta: 10,
			Status: BillingSettlementStatusPending, LastError: "upstream https://api.example.com/v1?api_key=secret failed",
			NextAttempt: now, CreatedAt: oldest, UpdatedAt: now, Revision: 1,
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

	stats, err := GetUnresolvedPositiveFinalizeSettlementStats()
	require.NoError(t, err)
	assert.EqualValues(t, 2, stats.Count)
	assert.Equal(t, oldest, stats.OldestCreatedAt)

	reconciliation, err := GetUnresolvedPositiveFinalizeSettlements(1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, reconciliation.TotalCount)
	assert.EqualValues(t, 1, reconciliation.PendingCount)
	assert.EqualValues(t, 1, reconciliation.ManualCount)
	assert.EqualValues(t, 2, reconciliation.OpenAlertCount)
	assert.Zero(t, reconciliation.ReviewedCount)
	assert.EqualValues(t, 2, reconciliation.BlockingRecordCount)
	assert.EqualValues(t, 1, reconciliation.BlockedUserCount)
	assert.True(t, reconciliation.BlockUserByDefault)
	assert.Equal(t, oldest, reconciliation.OldestCreatedAt)
	assert.True(t, reconciliation.Truncated)
	require.Len(t, reconciliation.Items, 1)
	assert.Equal(t, "request:pending-finalize:finalize", reconciliation.Items[0].OperationKey)
	assert.Equal(t, BillingSettlementStatusPending, reconciliation.Items[0].Status)
	assert.EqualValues(t, 10, reconciliation.Items[0].FundingDelta)
	assert.NotContains(t, reconciliation.Items[0].LastError, "api.example.com")
	assert.NotContains(t, reconciliation.Items[0].LastError, "secret")
	assert.Contains(t, reconciliation.Items[0].LastError, "***")

	require.NoError(t, DB.Where("user_id = ?", blockingUserID).Delete(&BillingSettlement{}).Error)
	stats, err = GetUnresolvedPositiveFinalizeSettlementStats()
	require.NoError(t, err)
	assert.Zero(t, stats.Count)
	assert.Zero(t, stats.OldestCreatedAt)
}

func TestBillingSettlementBlockingPolicyAndReviewAreIndependentFromFinancialState(t *testing.T) {
	setupUserUpdateTestState(t)
	setBillingReconciliationBlockDefaultForTest(t, false)

	now := time.Now().Unix()
	block := true
	allow := false
	records := []BillingSettlement{
		{
			OperationKey: "request:inherit-policy:finalize", Source: BillingSettlementSourceWallet,
			UserID: 951, FundingDelta: 10, AppliedFundingDelta: 2, TokenDelta: 10, AppliedTokenDelta: 2,
			Status: BillingSettlementStatusPending, CreatedAt: now - 30, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:force-block:finalize", Source: BillingSettlementSourceWallet,
			UserID: 952, FundingDelta: 11, TokenDelta: 11, Status: BillingSettlementStatusManual,
			UserBlockingOverride: &block, CreatedAt: now - 20, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:force-allow:finalize", Source: BillingSettlementSourceWallet,
			UserID: 953, FundingDelta: 12, TokenDelta: 12, Status: BillingSettlementStatusPending,
			UserBlockingOverride: &allow, CreatedAt: now - 10, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:force-allow-shared-user:finalize", Source: BillingSettlementSourceWallet,
			UserID: 954, FundingDelta: 13, TokenDelta: 13, Status: BillingSettlementStatusPending,
			UserBlockingOverride: &allow, CreatedAt: now - 5, UpdatedAt: now, Revision: 1,
		},
		{
			OperationKey: "request:force-block-shared-user:finalize", Source: BillingSettlementSourceWallet,
			UserID: 954, FundingDelta: 14, TokenDelta: 14, Status: BillingSettlementStatusManual,
			UserBlockingOverride: &block, CreatedAt: now - 1, UpdatedAt: now, Revision: 1,
		},
	}
	require.NoError(t, DB.Create(&records).Error)

	blocked, err := HasUnresolvedPositiveFinalizeSettlement(951)
	require.NoError(t, err)
	assert.False(t, blocked, "nil override must inherit the disabled global policy")
	blocked, err = HasUnresolvedPositiveFinalizeSettlement(952)
	require.NoError(t, err)
	assert.True(t, blocked, "an explicit per-record block must override the global policy")
	blocked, err = HasUnresolvedPositiveFinalizeSettlement(953)
	require.NoError(t, err)
	assert.False(t, blocked, "an explicit per-record allow must remain non-blocking")
	blocked, err = HasUnresolvedPositiveFinalizeSettlement(954)
	require.NoError(t, err)
	assert.True(t, blocked, "any blocking record must keep the same user blocked")

	reviewed, err := ReviewBillingSettlement(records[0].ID, 7001, false, "Verified provider usage against the invoice")
	require.NoError(t, err)
	assert.Equal(t, records[0].ID, reviewed.ID)
	assert.Equal(t, records[0].UserID, reviewed.UserID)
	assert.Equal(t, BillingSettlementStatusPending, reviewed.Status)
	assert.EqualValues(t, 10, reviewed.FundingDelta)
	assert.EqualValues(t, 2, reviewed.AppliedFundingDelta)
	assert.EqualValues(t, 10, reviewed.TokenDelta)
	assert.EqualValues(t, 2, reviewed.AppliedTokenDelta)
	assert.Positive(t, reviewed.ReconciliationReviewedAt)
	assert.Equal(t, 7001, reviewed.ReconciliationReviewedBy)
	assert.Equal(t, "Verified provider usage against the invoice", reviewed.ReconciliationReviewNote)
	require.NotNil(t, reviewed.UserBlockingOverride)
	assert.False(t, *reviewed.UserBlockingOverride, "explicit false must be durably persisted")
	assert.Equal(t, records[0].UpdatedAt, reviewed.UpdatedAt, "review metadata must not replace the financial update timestamp")
	assert.Equal(t, records[0].Revision, reviewed.Revision, "review metadata must not advance the financial settlement revision")

	stats, err := GetUnresolvedPositiveFinalizeSettlementStats()
	require.NoError(t, err)
	assert.EqualValues(t, 4, stats.Count, "reviewed records must leave the open-alert projection")

	reconciliation, err := GetUnresolvedPositiveFinalizeSettlements(100)
	require.NoError(t, err)
	assert.EqualValues(t, 5, reconciliation.TotalCount, "review does not complete or delete a settlement")
	assert.EqualValues(t, 4, reconciliation.OpenAlertCount)
	assert.EqualValues(t, 1, reconciliation.ReviewedCount)
	assert.EqualValues(t, 2, reconciliation.BlockingRecordCount)
	assert.EqualValues(t, 2, reconciliation.BlockedUserCount)
	assert.False(t, reconciliation.BlockUserByDefault)
	require.Len(t, reconciliation.Items, 5)

	var reviewedItem *BillingSettlementReconciliationItem
	var sharedAllowItem *BillingSettlementReconciliationItem
	var sharedBlockItem *BillingSettlementReconciliationItem
	for index := range reconciliation.Items {
		switch reconciliation.Items[index].ID {
		case records[0].ID:
			reviewedItem = &reconciliation.Items[index]
		case records[3].ID:
			sharedAllowItem = &reconciliation.Items[index]
		case records[4].ID:
			sharedBlockItem = &reconciliation.Items[index]
		}
	}
	require.NotNil(t, reviewedItem)
	assert.False(t, reviewedItem.RecordBlocksUser)
	assert.False(t, reviewedItem.BlocksUser)
	assert.Equal(t, 7001, reviewedItem.ReconciliationReviewedBy)
	assert.Equal(t, "Verified provider usage against the invoice", reviewedItem.ReconciliationReviewNote)
	require.NotNil(t, sharedAllowItem)
	assert.False(t, sharedAllowItem.RecordBlocksUser, "the row-level decision must remain allow")
	assert.True(t, sharedAllowItem.BlocksUser, "the user remains blocked by another unresolved record")
	require.NotNil(t, sharedBlockItem)
	assert.True(t, sharedBlockItem.RecordBlocksUser)
	assert.True(t, sharedBlockItem.BlocksUser)
}

func TestBillingSettlementBlockingScopeDoesNotLeakAcrossUsers(t *testing.T) {
	setupUserUpdateTestState(t)
	setBillingReconciliationBlockDefaultForTest(t, true)

	block := true
	record := BillingSettlement{
		OperationKey: "request:cross-user-blocking:finalize", Source: BillingSettlementSourceWallet,
		UserID: 961, FundingDelta: 10, TokenDelta: 10, Status: BillingSettlementStatusPending,
		UserBlockingOverride: &block, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Revision: 1,
	}
	require.NoError(t, DB.Create(&record).Error)

	blocked, err := HasUnresolvedPositiveFinalizeSettlement(record.UserID)
	require.NoError(t, err)
	assert.True(t, blocked)

	blocked, err = HasUnresolvedPositiveFinalizeSettlement(962)
	require.NoError(t, err)
	assert.False(t, blocked, "another user's explicit block must not leak across the user_id predicate")
}

func TestBillingSettlementFailureReopensReviewedRecord(t *testing.T) {
	setupUserUpdateTestState(t)
	setBillingReconciliationBlockDefaultForTest(t, true)

	now := time.Now().Unix()
	allow := false
	record := BillingSettlement{
		OperationKey: "request:reviewed-retry-failure:finalize", Source: BillingSettlementSourceWallet,
		UserID: 963, FundingDelta: 10, AppliedFundingDelta: 2, TokenDelta: 10, AppliedTokenDelta: 2,
		Status: BillingSettlementStatusPending, Attempts: 1, LastError: "previous failure", NextAttempt: now - 1,
		ReconciliationReviewedAt: now - 60, ReconciliationReviewedBy: 7005,
		ReconciliationReviewNote: "Reviewed previous failure evidence", UserBlockingOverride: &allow,
		CreatedAt: now - 120, UpdatedAt: now - 60, Revision: 7,
	}
	require.NoError(t, DB.Create(&record).Error)

	stats, err := GetUnresolvedPositiveFinalizeSettlementStats()
	require.NoError(t, err)
	assert.Zero(t, stats.Count)
	blocked, err := HasUnresolvedPositiveFinalizeSettlement(record.UserID)
	require.NoError(t, err)
	assert.False(t, blocked, "the reviewed allow decision is active before new evidence arrives")

	markBillingSettlementFailure(record.OperationKey, errors.New("new retry failure"))

	var stored BillingSettlement
	require.NoError(t, DB.First(&stored, record.ID).Error)
	assert.Equal(t, 2, stored.Attempts)
	assert.Equal(t, "new retry failure", stored.LastError)
	assert.Equal(t, BillingSettlementStatusPending, stored.Status)
	assert.GreaterOrEqual(t, stored.NextAttempt, now)
	assert.Zero(t, stored.ReconciliationReviewedAt)
	assert.Zero(t, stored.ReconciliationReviewedBy)
	assert.Empty(t, stored.ReconciliationReviewNote)
	assert.Nil(t, stored.UserBlockingOverride)
	assert.EqualValues(t, 10, stored.FundingDelta)
	assert.EqualValues(t, 2, stored.AppliedFundingDelta)
	assert.EqualValues(t, 10, stored.TokenDelta)
	assert.EqualValues(t, 2, stored.AppliedTokenDelta)
	assert.EqualValues(t, 8, stored.Revision)

	stats, err = GetUnresolvedPositiveFinalizeSettlementStats()
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.Count, "new failure evidence must reopen the operational alert")
	blocked, err = HasUnresolvedPositiveFinalizeSettlement(record.UserID)
	require.NoError(t, err)
	assert.True(t, blocked, "reopened records must inherit the fail-closed global policy")
}

func TestReviewBillingSettlementRejectsAlreadyAppliedRecord(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now().Unix()
	record := BillingSettlement{
		OperationKey: "request:review-race:finalize", Source: BillingSettlementSourceWallet,
		UserID: 954, FundingDelta: 10, AppliedFundingDelta: 10, TokenDelta: 10, AppliedTokenDelta: 10,
		Status: BillingSettlementStatusApplied, CreatedAt: now, UpdatedAt: now, Revision: 2,
	}
	require.NoError(t, DB.Create(&record).Error)

	_, err := ReviewBillingSettlement(record.ID, 7002, false, "Already reconciled elsewhere")

	assert.ErrorIs(t, err, ErrBillingSettlementReviewConflict)
	var stored BillingSettlement
	require.NoError(t, DB.First(&stored, record.ID).Error)
	assert.Zero(t, stored.ReconciliationReviewedAt)
	assert.Empty(t, stored.ReconciliationReviewNote)
}

func TestReviewBillingSettlementRejectsStaleFailureEvidence(t *testing.T) {
	setupUserUpdateTestState(t)
	setBillingReconciliationBlockDefaultForTest(t, true)

	now := time.Now().Unix()
	record := BillingSettlement{
		OperationKey: "request:stale-review-race:finalize", Source: BillingSettlementSourceWallet,
		UserID: 964, FundingDelta: 10, TokenDelta: 10, Status: BillingSettlementStatusPending,
		Attempts: 1, LastError: "previous failure", NextAttempt: now - 1,
		CreatedAt: now - 60, UpdatedAt: now - 30, Revision: 4,
	}
	require.NoError(t, DB.Create(&record).Error)

	callbackName := "test:billing-settlement-review-stale-failure"
	var failureInjected atomic.Bool
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "BillingSettlement" && failureInjected.CompareAndSwap(false, true) {
			markBillingSettlementFailure(record.OperationKey, errors.New("new failure evidence"))
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Query().Remove(callbackName)
	})

	_, err := ReviewBillingSettlement(record.ID, 7006, false, "Reviewed superseded evidence")

	assert.ErrorIs(t, err, ErrBillingSettlementReviewConflict)
	var stored BillingSettlement
	require.NoError(t, DB.First(&stored, record.ID).Error)
	assert.Equal(t, 2, stored.Attempts)
	assert.Equal(t, "new failure evidence", stored.LastError)
	assert.Zero(t, stored.ReconciliationReviewedAt)
	assert.Zero(t, stored.ReconciliationReviewedBy)
	assert.Empty(t, stored.ReconciliationReviewNote)
	assert.Nil(t, stored.UserBlockingOverride)
	assert.EqualValues(t, 5, stored.Revision)

	stats, statsErr := GetUnresolvedPositiveFinalizeSettlementStats()
	require.NoError(t, statsErr)
	assert.EqualValues(t, 1, stats.Count, "the newer failure must remain visible for administrator review")
}

func TestReviewBillingSettlementAllowsEditingCurrentReview(t *testing.T) {
	setupUserUpdateTestState(t)

	now := time.Now().Unix()
	allow := false
	record := BillingSettlement{
		OperationKey: "request:edit-current-review:finalize", Source: BillingSettlementSourceWallet,
		UserID: 965, FundingDelta: 10, TokenDelta: 10, Status: BillingSettlementStatusPending,
		ReconciliationReviewedAt: now - 60, ReconciliationReviewedBy: 7007,
		ReconciliationReviewNote: "Initial review", UserBlockingOverride: &allow,
		CreatedAt: now - 120, UpdatedAt: now - 60, Revision: 3,
	}
	require.NoError(t, DB.Create(&record).Error)

	reviewed, err := ReviewBillingSettlement(record.ID, 7008, true, "Updated review after checking more evidence")

	require.NoError(t, err)
	assert.Equal(t, 7008, reviewed.ReconciliationReviewedBy)
	assert.Equal(t, "Updated review after checking more evidence", reviewed.ReconciliationReviewNote)
	require.NotNil(t, reviewed.UserBlockingOverride)
	assert.True(t, *reviewed.UserBlockingOverride)
	assert.EqualValues(t, record.Revision, reviewed.Revision, "review metadata must remain independent from financial revision")
}

func TestReviewBillingSettlementAcceptsMatchingUpdateWhenDriverReportsZeroRows(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now().Unix()
	record := BillingSettlement{
		OperationKey: "request:review-same-value:finalize", Source: BillingSettlementSourceWallet,
		UserID: 955, FundingDelta: 10, AppliedFundingDelta: 2, TokenDelta: 10, AppliedTokenDelta: 2,
		Status: BillingSettlementStatusPending, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	require.NoError(t, DB.Create(&record).Error)

	callbackName := "test:billing-settlement-review-zero-rows"
	require.NoError(t, DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "BillingSettlement" {
			tx.RowsAffected = 0
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove(callbackName)
	})

	reviewed, err := ReviewBillingSettlement(record.ID, 7004, false, "Verified duplicate review submission")

	require.NoError(t, err)
	assert.Equal(t, record.ID, reviewed.ID)
	assert.Equal(t, 7004, reviewed.ReconciliationReviewedBy)
	assert.Equal(t, "Verified duplicate review submission", reviewed.ReconciliationReviewNote)
	require.NotNil(t, reviewed.UserBlockingOverride)
	assert.False(t, *reviewed.UserBlockingOverride)
}

func TestBillingSettlementReviewUpdateMapUsesSchemaColumns(t *testing.T) {
	parsed, err := schema.Parse(&BillingSettlement{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	updateMaps := []map[string]interface{}{
		billingSettlementReviewUpdates(7003, false, "reviewed", time.Unix(123, 0)),
		billingSettlementFailureUpdates(2, "failed", BillingSettlementStatusPending, 456, 123),
	}
	for _, updates := range updateMaps {
		for key := range updates {
			assert.NotNilf(t, parsed.LookUpField(key), "billing settlement update key %q must map to a BillingSettlement column", key)
		}
	}
}

func setBillingReconciliationBlockDefaultForTest(t *testing.T, enabled bool) {
	t.Helper()
	key := billing_reconciliation_setting.OptionKeyBlockUserByDefault
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original, existed := common.OptionMap[key]
	common.OptionMap[key] = map[bool]string{true: "true", false: "false"}[enabled]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[key] = original
		} else {
			delete(common.OptionMap, key)
		}
	})
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

func TestBillingSettlementEffectSerializationFailureIsNotDurable(t *testing.T) {
	setupUserUpdateTestState(t)
	input := BillingSettlementInput{
		OperationKey: "regression:effect-serialization-failure",
		Source:       BillingSettlementSourceWallet,
		UserID:       905,
		FundingDelta: 10,
		Effect: &BillingSettlementEffect{
			Other: map[string]interface{}{"unsupported": func() {}},
		},
	}

	_, _, err := ApplyBillingSettlementOnce(input)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBillingSettlementRecordNotDurable)

	var count int64
	require.NoError(t, DB.Model(&BillingSettlement{}).
		Where("operation_key = ?", input.OperationKey).
		Count(&count).Error)
	assert.Zero(t, count)
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

func TestBillingSettlementZeroDeltaTaskConflictIsRejected(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 912, Username: "zero-delta-task-conflict-user", Quota: 100, Status: common.UserStatusEnabled}
	task := Task{ID: 913, TaskID: "zero-delta-task-conflict", UserId: user.Id, Quota: 20, Status: TaskStatusSubmitted}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&task).Error)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey:    "regression:zero-delta-task-conflict",
		Source:          BillingSettlementSourceWallet,
		UserID:          user.Id,
		TaskID:          task.ID,
		TaskQuota:       10,
		TaskQuotaTarget: 10,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBillingSettlementTaskConflict)
	assert.EqualValues(t, 20, getRegressionTaskQuota(t, task.ID))
	assert.EqualValues(t, 100, getRegressionUserQuota(t, user.Id))

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "regression:zero-delta-task-conflict").First(&settlement).Error)
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

func setupSecurityCredentialTestState(t *testing.T) {
	t.Helper()
	setupUserUpdateTestState(t)
	require.NoError(t, DB.AutoMigrate(
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&AuthFlow{},
	))
	clearSecurityCredentialTestState(t)
	t.Cleanup(func() {
		clearSecurityCredentialTestState(t)
	})
}

func clearSecurityCredentialTestState(t *testing.T) {
	t.Helper()
	for _, target := range []interface{}{
		&PasskeyCredential{},
		&TwoFABackupCode{},
		&TwoFA{},
		&AuthFlow{},
	} {
		require.NoError(t, DB.Unscoped().Where("1 = 1").Delete(target).Error)
	}
}

func TestPasswordSecurityChangesBumpGenerationAndRecoveryRevokesTokens(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:                8801,
		Username:          "password-security-user",
		Password:          "OldPassword123",
		Email:             "password-security@example.com",
		Role:              common.RoleCommonUser,
		Status:            common.UserStatusEnabled,
		SessionGeneration: 3,
	}
	user.SetAccessToken("management-access-token")
	require.NoError(t, DB.Create(&user).Error)

	loaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	loaded.Password = "ChangedPassword123"
	require.NoError(t, loaded.Update(true))
	require.EqualValues(t, 4, loaded.SessionGeneration)
	require.True(t, common.ValidatePasswordAndHash("ChangedPassword123", loaded.Password))

	tokens := []Token{
		{Id: 8811, UserId: user.Id, Name: "enabled", Key: "password-reset-enabled", Status: common.TokenStatusEnabled},
		{Id: 8812, UserId: user.Id, Name: "disabled", Key: "password-reset-disabled", Status: common.TokenStatusDisabled},
	}
	require.NoError(t, DB.Create(&tokens).Error)

	require.NoError(t, ResetUserPasswordByEmail(user.Email, "RecoveredPassword123"))

	var recovered User
	require.NoError(t, DB.First(&recovered, user.Id).Error)
	require.EqualValues(t, 5, recovered.SessionGeneration)
	require.Empty(t, recovered.GetAccessToken())
	require.True(t, common.ValidatePasswordAndHash("RecoveredPassword123", recovered.Password))

	var storedTokens []Token
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("id asc").Find(&storedTokens).Error)
	require.Len(t, storedTokens, 2)
	for _, token := range storedTokens {
		require.Equal(t, common.TokenStatusDisabled, token.Status)
	}
}

func TestPasskeyAndTwoFASecurityChangesBumpSessionGeneration(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:       8821,
		Username: "credential-generation-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	firstCredential := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "Y3JlZGVudGlhbC0x",
		PublicKey:    "cHVibGljLWtleS0x",
	}
	generation, err := ReplacePasskeyCredentialAndBumpSessionGeneration(firstCredential)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)

	replacement := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "Y3JlZGVudGlhbC0y",
		PublicKey:    "cHVibGljLWtleS0y",
	}
	generation, err = ReplacePasskeyCredentialAndBumpSessionGeneration(replacement)
	require.NoError(t, err)
	require.EqualValues(t, 2, generation)

	var credentials []PasskeyCredential
	require.NoError(t, DB.Unscoped().Where("user_id = ?", user.Id).Find(&credentials).Error)
	require.Len(t, credentials, 1)
	require.Equal(t, replacement.CredentialID, credentials[0].CredentialID)

	generation, err = DeletePasskeyAndBumpSessionGeneration(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 3, generation)

	twoFA := &TwoFA{UserId: user.Id, Secret: "TESTSECRET", IsEnabled: false}
	require.NoError(t, DB.Create(twoFA).Error)
	generation, err = twoFA.EnableAndBumpSessionGeneration()
	require.NoError(t, err)
	require.EqualValues(t, 4, generation)

	generation, err = ReplaceBackupCodesAndBumpSessionGeneration(user.Id, []string{"ABCD-EFGH"})
	require.NoError(t, err)
	require.EqualValues(t, 5, generation)
	var backupCode TwoFABackupCode
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&backupCode).Error)
	require.NotEqual(t, "ABCD-EFGH", backupCode.CodeHash)
	require.True(t, common.ValidatePasswordAndHash("ABCD-EFGH", backupCode.CodeHash))
	valid, err := ValidateBackupCode(user.Id, "ABCD-EFGH")
	require.NoError(t, err)
	require.True(t, valid)
	valid, err = ValidateBackupCode(user.Id, "ABCD-EFGH")
	require.NoError(t, err)
	require.False(t, valid)

	generation, err = DisableTwoFAAndBumpSessionGeneration(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 6, generation)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.EqualValues(t, 6, stored.SessionGeneration)
}

func TestBackupCodeMutationsRequireCredentialOwner(t *testing.T) {
	setupSecurityCredentialTestState(t)
	const missingUserID = 8899

	require.EqualError(t, (&TwoFA{Id: 1}).Delete(), "2FA用户ID不能为空")
	require.EqualError(t, (&TwoFA{Id: 1, UserId: -1}).Delete(), "2FA用户ID不能为空")
	require.ErrorIs(t, CreateBackupCodes(missingUserID, []string{"ABCD-EFGH"}), gorm.ErrRecordNotFound)
	require.ErrorIs(t, (&TwoFA{Id: 1, UserId: missingUserID}).Delete(), gorm.ErrRecordNotFound)
}

func TestPasskeyUsageUpdateCannotRestoreReplacedCredential(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:       8822,
		Username: "passkey-usage-race-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	staleCredential := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "c3RhbGUtY3JlZGVudGlhbA==",
		PublicKey:    "c3RhbGUtcHVibGljLWtleQ==",
		SignCount:    1,
	}
	_, err := ReplacePasskeyCredentialAndBumpSessionGeneration(staleCredential)
	require.NoError(t, err)
	staleUsageUpdate := *staleCredential

	replacement := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "cmVwbGFjZW1lbnQtY3JlZGVudGlhbA==",
		PublicKey:    "cmVwbGFjZW1lbnQtcHVibGljLWtleQ==",
		SignCount:    10,
	}
	_, err = ReplacePasskeyCredentialAndBumpSessionGeneration(replacement)
	require.NoError(t, err)

	now := time.Now()
	staleUsageUpdate.SignCount = 2
	staleUsageUpdate.LastUsedAt = &now
	require.ErrorIs(t, UpdatePasskeyCredentialAfterAuthentication(&staleUsageUpdate), ErrPasskeyCredentialChanged)

	stored, err := GetPasskeyByUserID(user.Id)
	require.NoError(t, err)
	require.Equal(t, replacement.CredentialID, stored.CredentialID)
	require.Equal(t, replacement.PublicKey, stored.PublicKey)
	require.EqualValues(t, 10, stored.SignCount)

	later := now.Add(time.Second)
	matchingUsageUpdate := *replacement
	matchingUsageUpdate.ID = 0
	matchingUsageUpdate.SignCount = 11
	matchingUsageUpdate.LastUsedAt = &later
	require.NoError(t, UpdatePasskeyCredentialAfterAuthentication(&matchingUsageUpdate))
	stored, err = GetPasskeyByUserID(user.Id)
	require.NoError(t, err)
	require.Equal(t, replacement.CredentialID, stored.CredentialID)
	require.EqualValues(t, 11, stored.SignCount)
	require.WithinDuration(t, later, *stored.LastUsedAt, time.Second)
}

func TestPasskeyUsageUpdateMergesOrderedAuthenticationStateMonotonically(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:       8824,
		Username: "passkey-monotonic-auth-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	credential := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: "bW9ub3RvbmljLWNlcnQ=",
		PublicKey:    "bW9ub3RvbmljLXB1YmxpYy1rZXk=",
		SignCount:    9,
	}
	_, err := ReplacePasskeyCredentialAndBumpSessionGeneration(credential)
	require.NoError(t, err)

	older := time.Now()
	newer := older.Add(time.Second)
	higherCounter := *credential
	higherCounter.SignCount = 11
	higherCounter.CloneWarning = true
	higherCounter.LastUsedAt = &newer
	lowerCounter := *credential
	lowerCounter.SignCount = 10
	lowerCounter.CloneWarning = false
	lowerCounter.LastUsedAt = &older

	require.NoError(t, UpdatePasskeyCredentialAfterAuthentication(&higherCounter))
	require.NoError(t, UpdatePasskeyCredentialAfterAuthentication(&lowerCounter))

	stored, err := GetPasskeyByUserID(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 11, stored.SignCount)
	require.True(t, stored.CloneWarning)
	require.WithinDuration(t, newer, *stored.LastUsedAt, time.Second)
}

func TestPasskeyAuthenticationUpdateKeysMatchSchema(t *testing.T) {
	setupSecurityCredentialTestState(t)
	now := time.Now()
	updates := passkeyAuthenticationUpdates(&PasskeyCredential{
		PublicKey: "schema-public-key", CloneWarning: true, LastUsedAt: &now,
	})
	statement := &gorm.Statement{DB: DB}
	require.NoError(t, statement.Parse(&PasskeyCredential{}))
	for key := range updates {
		require.Contains(t, statement.Schema.DBNames, key)
	}
}

func TestTimedOutTaskCursorRemainsGroupedWithStatusPredicate(t *testing.T) {
	truncateTables(t)
	require.True(t, DB.Migrator().HasIndex(&Task{}, "idx_task_timeout_cursor"))
	const submitTime = int64(100)
	tasks := []Task{
		{ID: 401, TaskID: "cursor-unfinished", Status: TaskStatusInProgress, SubmitTime: submitTime},
		{ID: 402, TaskID: "cursor-success", Status: TaskStatusSuccess, SubmitTime: submitTime},
		{ID: 403, TaskID: "cursor-failure", Status: TaskStatusFailure, SubmitTime: submitTime},
	}
	require.NoError(t, DB.Create(&tasks).Error)

	got, err := GetTimedOutUnfinishedTasksAfter(200, submitTime, 400, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "cursor-unfinished", got[0].TaskID)
}

func TestTimedOutTaskCursorReturnsQueryError(t *testing.T) {
	failingDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := failingDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	originalDB := DB
	DB = failingDB
	t.Cleanup(func() { DB = originalDB })

	tasks, queryErr := GetTimedOutUnfinishedTasksAfter(200, 0, 0, 10)
	require.Error(t, queryErr)
	require.Nil(t, tasks)
}

func TestPasskeyValidatedCredentialStateIsPersisted(t *testing.T) {
	setupSecurityCredentialTestState(t)

	user := User{
		Id:       8823,
		Username: "passkey-validated-state-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	credentialID := []byte("validated-state-credential")
	storedCredential := &PasskeyCredential{
		UserID:       user.Id,
		CredentialID: base64.StdEncoding.EncodeToString(credentialID),
		PublicKey:    base64.StdEncoding.EncodeToString([]byte("old-public-key")),
		SignCount:    1,
	}
	_, err := ReplacePasskeyCredentialAndBumpSessionGeneration(storedCredential)
	require.NoError(t, err)

	validatedCredential := &webauthn.Credential{
		ID:              credentialID,
		PublicKey:       []byte("updated-public-key"),
		AttestationType: "none",
		Transport:       []protocol.AuthenticatorTransport{protocol.USB},
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:       []byte("updated-aaguid"),
			SignCount:    2,
			CloneWarning: true,
			Attachment:   protocol.Platform,
		},
	}

	storedCredential.ApplyValidatedCredential(validatedCredential)
	now := time.Now()
	storedCredential.LastUsedAt = &now
	require.NoError(t, UpdatePasskeyCredentialAfterAuthentication(storedCredential))

	updated, err := GetPasskeyByUserID(user.Id)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(credentialID), updated.CredentialID)
	require.Equal(t, base64.StdEncoding.EncodeToString(validatedCredential.PublicKey), updated.PublicKey)
	require.EqualValues(t, 2, updated.SignCount)
	require.True(t, updated.CloneWarning)
	require.True(t, updated.UserPresent)
	require.True(t, updated.UserVerified)
	require.True(t, updated.BackupEligible)
	require.True(t, updated.BackupState)
	require.Equal(t, string(protocol.Platform), updated.Attachment)
	require.Equal(t, []protocol.AuthenticatorTransport{protocol.USB}, updated.TransportList())
	require.WithinDuration(t, now, *updated.LastUsedAt, time.Second)
}

func TestTelegramBindingStateIsUserBoundAndConsumedOnce(t *testing.T) {
	setupSecurityCredentialTestState(t)

	owner := User{Id: 8831, Username: "telegram-owner", AffCode: "telegram-owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	other := User{Id: 8832, Username: "telegram-other", AffCode: "telegram-other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&other).Error)

	state, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose:   AuthFlowPurposeOAuth,
		Provider:  "telegram",
		Intent:    AuthFlowIntentBind,
		UserId:    owner.Id,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)

	_, err = BindTelegramIdentityWithAuthFlow(other.Id, "123456", state)
	require.ErrorIs(t, err, ErrAuthFlowInvalid)

	generation, err := BindTelegramIdentityWithAuthFlow(owner.Id, "123456", state)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)

	_, err = BindTelegramIdentityWithAuthFlow(owner.Id, "123456", state)
	require.ErrorIs(t, err, ErrAuthFlowConsumed)

	var stored User
	require.NoError(t, DB.First(&stored, owner.Id).Error)
	require.Equal(t, "123456", stored.TelegramId)
	require.EqualValues(t, 1, stored.SessionGeneration)
}

func TestOAuthReauthenticationBootstrapRequiresNoExistingCredentials(t *testing.T) {
	setupSecurityCredentialTestState(t)

	users := []User{
		{Id: 8841, Username: "oauth-bootstrap-user", AffCode: "oauth-bootstrap", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 8842, Username: "oauth-password-user", Password: "existing-password-hash", AffCode: "oauth-password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 8843, Username: "oauth-two-fa-user", AffCode: "oauth-two-fa", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 8844, Username: "oauth-passkey-user", AffCode: "oauth-passkey", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
	}
	require.NoError(t, DB.Create(&users).Error)
	require.NoError(t, DB.Create(&TwoFA{
		UserId:    users[2].Id,
		Secret:    "oauth-reauth-test-secret",
		IsEnabled: true,
	}).Error)
	require.NoError(t, DB.Create(&PasskeyCredential{
		UserID:       users[3].Id,
		CredentialID: "oauth-reauth-credential-id",
		PublicKey:    "oauth-reauth-public-key",
	}).Error)

	tests := []struct {
		name    string
		userID  int
		allowed bool
	}{
		{name: "oauth only", userID: users[0].Id, allowed: true},
		{name: "password", userID: users[1].Id, allowed: false},
		{name: "enabled 2FA", userID: users[2].Id, allowed: false},
		{name: "Passkey", userID: users[3].Id, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allowed, err := CanUseOAuthReauthentication(test.userID)
			require.NoError(t, err)
			require.Equal(t, test.allowed, allowed)
		})
	}
}

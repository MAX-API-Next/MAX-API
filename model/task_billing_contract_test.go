package model

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type taskBillingContractFixture struct {
	user    User
	token   Token
	channel Channel
	task    Task
	sub     UserSubscription
	input   BillingSettlementInput
}

func newTaskBillingContractFixture(t *testing.T, db *gorm.DB, number int, source string, delta int64) taskBillingContractFixture {
	t.Helper()
	label := fmt.Sprintf("tb%04d", number)
	f := taskBillingContractFixture{
		user:    User{Username: label, AffCode: label, Password: "synthetic-fixture", Quota: 900},
		channel: Channel{Name: label, Key: "synthetic-fixture"},
	}
	taskBillingTestCheck(t, db.Create(&f.user).Error, "create fixture user")
	f.token = Token{UserId: f.user.Id, Key: label, RemainQuota: 900, UsedQuota: 100}
	taskBillingTestCheck(t, db.Create(&f.token).Error, "create fixture token")
	taskBillingTestCheck(t, db.Create(&f.channel).Error, "create fixture channel")
	zero := int64(0)
	f.task = Task{TaskID: label, UserId: f.user.Id, ChannelId: f.channel.Id, Quota: 100, Status: TaskStatusInProgress, UpdatedAt: 1,
		PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{
			OriginModelName: "contract-model",
			TaskUsageEnvelope: &types.TaskUsageEnvelope{SchemaVersion: 1, SourceID: "minimax",
				Usage: &types.TaskUsage{OutputDurationMs: &zero}},
		}},
	}
	taskBillingTestCheck(t, db.Create(&f.task).Error, "create fixture task")
	f.input = BillingSettlementInput{
		OperationKey: BillingTaskFinalizeOperationKey(f.task.ID), Source: source, UserID: f.user.Id,
		TokenID: f.token.Id, FundingDelta: delta, TokenDelta: delta, TaskID: f.task.ID, TaskQuota: 100, TaskQuotaTarget: 100 + delta,
		Effect: &BillingSettlementEffect{LogType: LogTypeConsume, ModelName: "contract-model", TokenID: f.token.Id,
			ChannelID: f.channel.Id, Quota: 100 + delta, QuotaIsActual: true, UpdateUsage: true, PromptTokens: 2, CompletionTokens: 3},
	}
	if source == BillingSettlementSourceSubscription {
		f.sub = UserSubscription{UserId: f.user.Id, AmountTotal: 1000, AmountUsed: 100, Status: "active",
			StartTime: 1, EndTime: time.Now().Add(time.Hour).Unix(), LastResetTime: 1}
		taskBillingTestCheck(t, db.Create(&f.sub).Error, "create fixture subscription")
		pre := SubscriptionPreConsumeRecord{RequestId: label, UserId: f.user.Id, TokenId: f.token.Id,
			UserSubscriptionId: f.sub.Id, SubscriptionLastResetTime: 1, PreConsumed: 100, Status: "consumed"}
		taskBillingTestCheck(t, db.Create(&pre).Error, "create fixture preconsume")
		f.input.SubscriptionID, f.input.SubscriptionPreConsumeRequestID = f.sub.Id, pre.RequestId
	}
	return f
}

func (f taskBillingContractFixture) assertBalances(t *testing.T, db *gorm.DB, delta int64) {
	t.Helper()
	var user User
	var token Token
	var task Task
	taskBillingTestCheck(t, db.First(&user, f.user.Id).Error, "read wallet")
	taskBillingTestCheck(t, db.First(&token, f.token.Id).Error, "read token")
	taskBillingTestCheck(t, db.First(&task, f.task.ID).Error, "read task")
	wallet := int64(900)
	if f.input.Source == BillingSettlementSourceWallet {
		wallet -= delta
	} else {
		var sub UserSubscription
		taskBillingTestCheck(t, db.First(&sub, f.sub.Id).Error, "read subscription")
		require.Equal(t, 100+delta, sub.AmountUsed)
	}
	require.Equal(t, wallet, user.Quota)
	require.Equal(t, 900-delta, token.RemainQuota)
	require.Equal(t, 100+delta, token.UsedQuota)
	require.EqualValues(t, 100+delta, task.Quota)
	// Funding/effect execution must not replace the provider-driven lifecycle.
	require.Equal(t, TaskStatus(TaskStatusInProgress), task.Status)
	require.NotNil(t, task.PrivateData.BillingContext)
	require.NotNil(t, task.PrivateData.BillingContext.TaskUsageEnvelope)
	usage := task.PrivateData.BillingContext.TaskUsageEnvelope.Usage
	require.NotNil(t, usage)
	require.NotNil(t, usage.OutputDurationMs)
	require.Zero(t, *usage.OutputDurationMs)
}

func (f taskBillingContractFixture) assertProjection(t *testing.T, db *gorm.DB, quota int64, count int) {
	t.Helper()
	var user User
	var channel Channel
	var logs []Log
	var receipts int64
	taskBillingTestCheck(t, db.First(&user, f.user.Id).Error, "read usage counters")
	taskBillingTestCheck(t, db.First(&channel, f.channel.Id).Error, "read channel usage")
	taskBillingTestCheck(t, db.Where("user_id = ?", f.user.Id).Find(&logs).Error, "read consume logs")
	taskBillingTestCheck(t, db.Model(&BillingLogReceipt{}).Where("operation_key = ?", f.input.OperationKey).Count(&receipts).Error, "read log receipt")
	require.Equal(t, quota, user.UsedQuota)
	require.Equal(t, quota, channel.UsedQuota)
	require.Equal(t, count, user.RequestCount)
	require.Len(t, logs, count)
	require.EqualValues(t, count, receipts)
	if count == 1 {
		require.EqualValues(t, quota, logs[0].Quota)
		require.Equal(t, 2, logs[0].PromptTokens)
		require.Equal(t, 3, logs[0].CompletionTokens)
	}
}

func taskBillingContractRecord(t *testing.T, db *gorm.DB, key string) BillingSettlement {
	t.Helper()
	var records []BillingSettlement
	taskBillingTestCheck(t, db.Where("operation_key = ?", key).Find(&records).Error, "read durable settlement")
	require.Len(t, records, 1, "one stable operation identity must own the settlement")
	return records[0]
}

func runTaskBillingSettlementContracts(t *testing.T, db *gorm.DB) {
	t.Helper()
	sequence := 0
	fixture := func(t *testing.T, source string, delta int64) taskBillingContractFixture {
		sequence++
		return newTaskBillingContractFixture(t, db, sequence, source, delta)
	}
	for _, source := range []string{BillingSettlementSourceWallet, BillingSettlementSourceSubscription} {
		for _, delta := range []int64{-100, -20, 0, 20} {
			t.Run(fmt.Sprintf("%s/delta_%d", source, delta), func(t *testing.T) {
				f := fixture(t, source, delta)
				for attempt := 0; attempt < 2; attempt++ {
					applied, replay, err := ApplyBillingSettlementOnce(f.input)
					taskBillingTestCheck(t, err, "apply funding")
					require.Equal(t, delta, applied)
					require.Equal(t, attempt > 0, replay)
				}
				f.assertBalances(t, db, delta)
				f.assertProjection(t, db, 0, 0)
				record := taskBillingContractRecord(t, db, f.input.OperationKey)
				require.Equal(t, BillingSettlementStatusApplied, record.Status)
				require.Equal(t, BillingSettlementEffectPending, record.EffectStatus)
				require.Equal(t, delta, record.AppliedFundingDelta)
				require.Equal(t, delta, record.AppliedTokenDelta)
				changed := f.input
				changed.FundingDelta++
				changed.TokenDelta++
				changed.TaskQuotaTarget++
				_, _, err := ApplyBillingSettlementOnce(changed)
				require.True(t, errors.Is(err, ErrBillingSettlementOperationConflict), "changed frozen amount must be rejected")
				for attempt := 0; attempt < 2; attempt++ {
					taskBillingTestCheck(t, ProcessBillingSettlementEffect(f.input.OperationKey), "replay effect")
				}
				f.assertBalances(t, db, delta)
				f.assertProjection(t, db, 100+delta, 1)
				require.Equal(t, BillingSettlementEffectApplied, taskBillingContractRecord(t, db, f.input.OperationKey).EffectStatus)
			})
		}
	}

	t.Run("concurrent_duplicate_funding", func(t *testing.T) {
		f := fixture(t, BillingSettlementSourceWallet, -20)
		type result struct {
			delta  int64
			replay bool
			err    error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				delta, replay, err := ApplyBillingSettlementOnce(f.input)
				results <- result{delta, replay, err}
			}()
		}
		close(start)
		// Join both workers before assertions/cleanup can restore package globals.
		first, second := <-results, <-results
		for _, got := range []result{first, second} {
			taskBillingTestCheck(t, got.err, "concurrent funding")
			require.EqualValues(t, -20, got.delta)
		}
		require.NotEqual(t, first.replay, second.replay)
		f.assertBalances(t, db, -20)
		require.Equal(t, BillingSettlementStatusApplied, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
	})

	t.Run("cas_winner_stale_loser", func(t *testing.T) {
		f := fixture(t, BillingSettlementSourceWallet, -20)
		stale := f.task
		won, err := f.task.UpdateWithStatusAndSettlementIntent(TaskStatusInProgress, 1, f.input)
		taskBillingTestCheck(t, err, "CAS winner")
		require.True(t, won)
		won, err = stale.UpdateWithStatusAndSettlementIntent(TaskStatusInProgress, 1, f.input)
		taskBillingTestCheck(t, err, "CAS loser")
		require.False(t, won)
		f.assertBalances(t, db, 0)
		require.Equal(t, BillingSettlementStatusPending, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
	})

	for _, source := range []string{BillingSettlementSourceWallet, BillingSettlementSourceSubscription} {
		t.Run(source+"/manual_promotion_replay", func(t *testing.T) {
			f := fixture(t, source, -20)
			manual := f.input
			manual.FundingDelta, manual.TokenDelta, manual.TaskQuotaTarget, manual.Effect = 0, 0, 100, nil
			const reason = "H3 terminal usage requires manual reconciliation:"
			won, err := f.task.UpdateWithStatusAndManualSettlement(TaskStatusInProgress, 1, manual, reason+" missing")
			taskBillingTestCheck(t, err, "persist manual intent")
			require.True(t, won)
			require.Equal(t, BillingSettlementStatusManual, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
			for i := 0; i < 2; i++ {
				ready, err := PromoteManualTaskBillingSettlement(f.input, reason, "synthetic complete evidence")
				taskBillingTestCheck(t, err, "promote manual intent")
				require.True(t, ready)
			}
			f.assertBalances(t, db, 0)
			require.Equal(t, BillingSettlementStatusPending, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
			_, _, err = ApplyBillingSettlementOnce(f.input)
			taskBillingTestCheck(t, err, "apply promoted intent")
			taskBillingTestCheck(t, ProcessBillingSettlementEffect(f.input.OperationKey), "apply promoted effect")
			f.assertBalances(t, db, -20)
			f.assertProjection(t, db, 80, 1)
		})
	}

	t.Run("funding_transaction_rollback_and_retry", func(t *testing.T) {
		f := fixture(t, BillingSettlementSourceWallet, -20)
		injected := errors.New("injected token update failure")
		callback := "test:task-billing-token-failure"
		removed := false
		taskBillingTestCheck(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "tokens" {
				_ = tx.AddError(injected)
			}
		}), "register transaction fault")
		t.Cleanup(func() {
			if !removed {
				taskBillingTestCheck(t, db.Callback().Update().Remove(callback), "remove transaction fault")
			}
		})
		_, _, err := ApplyBillingSettlementOnce(f.input)
		require.True(t, errors.Is(err, injected))
		f.assertBalances(t, db, 0)
		f.assertProjection(t, db, 0, 0)
		require.Equal(t, BillingSettlementStatusPending, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
		taskBillingTestCheck(t, db.Callback().Update().Remove(callback), "disable transaction fault")
		removed = true
		_, replay, err := ApplyBillingSettlementOnce(f.input)
		taskBillingTestCheck(t, err, "retry rolled back funding")
		require.False(t, replay)
		f.assertBalances(t, db, -20)
	})

	t.Run("effect_failure_after_log_and_retry", func(t *testing.T) {
		f := fixture(t, BillingSettlementSourceWallet, -100)
		_, _, err := ApplyBillingSettlementOnce(f.input)
		taskBillingTestCheck(t, err, "apply zero final funding")
		injected := errors.New("injected usage projection failure")
		callback := "test:task-billing-effect-failure"
		removed := false
		taskBillingTestCheck(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "users" {
				_ = tx.AddError(injected)
			}
		}), "register effect fault")
		t.Cleanup(func() {
			if !removed {
				taskBillingTestCheck(t, db.Callback().Update().Remove(callback), "remove effect fault")
			}
		})
		err = ProcessBillingSettlementEffect(f.input.OperationKey)
		require.True(t, errors.Is(err, injected))
		record := taskBillingContractRecord(t, db, f.input.OperationKey)
		require.Equal(t, BillingSettlementStatusApplied, record.Status)
		require.Equal(t, BillingSettlementEffectPending, record.EffectStatus)
		f.assertBalances(t, db, -100)
		var user User
		taskBillingTestCheck(t, db.First(&user, f.user.Id).Error, "read rolled back projection")
		require.Zero(t, user.RequestCount)
		var logs int64
		taskBillingTestCheck(t, db.Model(&Log{}).Where("user_id = ?", f.user.Id).Count(&logs).Error, "read committed log")
		require.EqualValues(t, 1, logs)
		taskBillingTestCheck(t, db.Callback().Update().Remove(callback), "disable effect fault")
		removed = true
		for i := 0; i < 2; i++ {
			taskBillingTestCheck(t, ProcessBillingSettlementEffect(f.input.OperationKey), "recover effect")
		}
		f.assertBalances(t, db, -100)
		f.assertProjection(t, db, 0, 1)
	})
}

// Guard parsing is tested without a live server or any user credentials.
func TestTaskBillingExternalDatabaseGuard(t *testing.T) {
	for _, test := range []struct {
		name, dialect, dsn string
		safe               bool
	}{
		{"mysql_safe", "mysql", "fixture@tcp(127.0.0.1:3306)/maxapi_task_billing_test_fixture", true},
		{"postgres_safe", "postgres", "postgres://fixture@127.0.0.1/maxapi_task_billing_test_fixture", true},
		{"mysql_wrong_database", "mysql", "maxapi_task_billing_test_fixture@tcp(127.0.0.1:3306)/production", false},
		{"postgres_wrong_database", "postgres", "postgres://maxapi_task_billing_test_fixture@127.0.0.1/production", false},
		{"mysql_no_database", "mysql", "fixture@tcp(127.0.0.1:3306)/", false},
		{"unknown_dialect", "other", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, name, err := openTaskBillingTestSQL(test.dialect, test.dsn)
			if test.safe {
				taskBillingTestCheck(t, err, "parse synthetic DSN")
				require.Equal(t, "maxapi_task_billing_test_fixture", name)
				taskBillingTestCheck(t, db.Close(), "close unconnected guard fixture")
			} else {
				require.True(t, errors.Is(err, errTaskBillingUnsafeDatabase))
				require.Nil(t, db)
			}
		})
	}
}

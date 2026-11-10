// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TEST ONLY: this sidecar models the proposed receipt column without changing
// BillingSettlement or startup migration. Production workers do not consult
// this table. These tests are not proof of a deployed durable writer/guard.
type isolatedTaskPricingReceipt struct {
	OperationKey string `gorm:"primaryKey;type:varchar(191)"`
	Payload      string `gorm:"type:text;not null"`
	Provenance   string `gorm:"type:text;not null"`
}

func (isolatedTaskPricingReceipt) TableName() string { return "isolated_task_pricing_receipts" }

func stageTaskPricingReceiptFixture(db *gorm.DB, task *model.Task, input model.BillingSettlementInput, beforeCommit func()) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		// Only this serial fixture/child swaps the existing package DB handle.
		// The original primitive joins our test transaction via a savepoint.
		previous := model.DB
		model.DB = tx
		defer func() { model.DB = previous }()
		if err := task.UpdateWithSettlementIntent(&input); err != nil {
			return err
		}
		receipt, err := PrepareTaskPricingWalletReceipt(task, input)
		if err != nil {
			return err
		}
		frozen, err := taskPricingWalletReceiptInput(task, input)
		if err != nil {
			return err
		}
		provenance, err := taskpricing.PrepareReceiptProvenance(taskpricing.ReceiptWalletV1, frozen)
		if err != nil {
			return err
		}
		wire, err := common.Marshal(receipt)
		if err != nil {
			return err
		}
		origin, err := common.Marshal(provenance)
		if err != nil {
			return err
		}
		if err := tx.Create(&isolatedTaskPricingReceipt{OperationKey: input.OperationKey, Payload: string(wire), Provenance: string(origin)}).Error; err != nil {
			return err
		}
		if beforeCommit != nil {
			beforeCommit()
		}
		return nil
	})
}

func readTaskPricingReceiptFixture(db *gorm.DB, id int64) error {
	var task model.Task
	if err := db.First(&task, id).Error; err != nil {
		return err
	}
	op := "request:" + task.PrivateData.BillingRequestId + ":pre-consume"
	var stored isolatedTaskPricingReceipt
	if err := db.Where("operation_key = ?", op).First(&stored).Error; err != nil {
		return err
	}
	var receipt taskpricing.TaskPricingReceipt
	if err := common.UnmarshalJsonStr(stored.Payload, &receipt); err != nil {
		return err
	}
	var provenance *taskpricing.ReceiptProvenance
	if err := common.UnmarshalJsonStr(stored.Provenance, &provenance); err != nil {
		return err
	}
	var record model.BillingSettlement
	if err := db.Where("operation_key = ?", op).First(&record).Error; err != nil {
		return err
	}
	input := model.BillingSettlementInput{OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID,
		TokenID: record.TokenID, SubscriptionID: record.SubscriptionID, TaskID: record.TaskID, TaskQuota: record.TaskQuota,
		TaskQuotaTarget: record.TaskQuotaTarget, FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta,
		SubscriptionPreConsumeRequestID: record.SubscriptionPreConsumeRequestID, FinalizeSubscriptionPreConsume: record.FinalizeSubscriptionPreConsume,
		AllowMissingToken: record.AllowMissingToken, PreConsumeRequestID: record.PreConsumeRequestID,
		PreConsumeModelName: record.PreConsumeModelName, PreConsumeRequestedQuota: record.PreConsumeRequestedQuota,
		PreConsumeEffectiveQuota: record.PreConsumeEffectiveQuota}
	if record.EffectPayload != "" {
		return taskpricing.ErrTaskPricingReceiptConflict
	}
	frozen, err := taskPricingWalletReceiptInput(&task, input)
	if err != nil {
		return err
	}
	return taskpricing.ValidateReceiptProvenance(provenance, &receipt, frozen)
}

type receiptProcessConfig struct {
	Database, Ready, Phase, Release string
	Task                            model.Task
	Private                         model.TaskPrivateData // Task's normal JSON intentionally omits this.
	Input                           model.BillingSettlementInput
}

const receiptProcessEnv = "MAXAPI_TASK_PRICING_RECEIPT_PROCESS"

func startReceiptProcess(t *testing.T, dir, phase string, task *model.Task, input model.BillingSettlementInput) *billingTestProcess {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	file, err := os.CreateTemp(dir, "receipt-child-*.json")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	cfg := receiptProcessConfig{Database: filepath.Join(dir, "ledger.sqlite"), Ready: file.Name() + ".ready", Phase: phase, Task: *task, Private: task.PrivateData, Input: input}
	if phase == "recover" {
		cfg.Release = filepath.Join(dir, "release")
	}
	writeBillingProcessJSON(t, file.Name(), cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestTaskPricingReceiptProcessHelper$", "-test.timeout=40s", "-test.v")
	cmd.Env = append(os.Environ(), receiptProcessEnv+"="+file.Name())
	p := &billingTestProcess{cmd: cmd, done: make(chan error, 1), exited: make(chan struct{}), output: &billingProcessOutput{}, ready: cfg.Ready}
	cmd.Stdout, cmd.Stderr = p.output, p.output
	require.NoError(t, cmd.Start())
	go func() { p.done <- cmd.Wait(); close(p.exited) }()
	t.Cleanup(func() { cancel(); <-p.exited })
	return p
}

func TestTaskPricingReceiptProcessHelper(t *testing.T) {
	config := os.Getenv(receiptProcessEnv)
	if config == "" {
		t.Skip("subprocess helper; parent runs isolated SQLite contracts")
	}
	wire, err := os.ReadFile(config)
	require.NoError(t, err)
	var cfg receiptProcessConfig
	require.NoError(t, common.Unmarshal(wire, &cfg))
	require.Equal(t, filepath.Join(filepath.Dir(config), "ledger.sqlite"), cfg.Database)
	require.Equal(t, config+".ready", cfg.Ready)
	marker, err := os.ReadFile(filepath.Join(filepath.Dir(config), "receipt-isolated"))
	require.NoError(t, err)
	require.Equal(t, "MAX test-only receipt fixture", string(marker))
	db := openBillingProcessDB(t, cfg.Database)
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled = true, false, false
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	if cfg.Phase == "recover" {
		// This explicit fixture guard is NOT installed in the original worker.
		require.NoError(t, readTaskPricingReceiptFixture(db, cfg.Task.ID))
		require.Equal(t, filepath.Join(filepath.Dir(config), "release"), cfg.Release)
		writeBillingProcessJSON(t, cfg.Ready, billingProcessBarrier{Phase: cfg.Phase, PID: os.Getpid()})
		deadline := time.NewTimer(25 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(cfg.Release); err == nil {
				break
			} else {
				require.True(t, os.IsNotExist(err))
			}
			select {
			case <-deadline.C:
				t.Fatal("parent did not release both recovery contenders")
			case <-tick.C:
			}
		}
		model.ProcessPendingBillingSettlementsOnce()
		return
	}
	cfg.Task.PrivateData = cfg.Private
	var barrier func()
	if cfg.Phase == "before-commit" {
		barrier = func() {
			pauseBillingProcess(t, billingProcessConfig{Ready: cfg.Ready}, billingProcessBarrier{Phase: cfg.Phase, InsideTransaction: true})
		}
	} else {
		require.Equal(t, "after-commit", cfg.Phase)
	}
	require.NoError(t, stageTaskPricingReceiptFixture(db, &cfg.Task, cfg.Input, barrier))
	pauseBillingProcess(t, billingProcessConfig{Ready: cfg.Ready}, billingProcessBarrier{Phase: cfg.Phase})
}

func newTaskPricingReceiptFixture(t *testing.T) (string, *boundedPricingFixture, *model.Task, model.BillingSettlementInput) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "receipt-isolated"), []byte("MAX test-only receipt fixture"), 0600))
	f := newBoundedPricingFixtureAt(t, filepath.Join(dir, "ledger.sqlite"))
	require.NoError(t, f.db.AutoMigrate(&isolatedTaskPricingReceipt{}))
	task, input := seedTaskPricingHandoff(t, f)
	// Retain the prepared host-owned candidate, but leave no durable task so
	// the tested outer transaction must create all three artifacts or none.
	require.NoError(t, f.db.Delete(task).Error)
	return dir, f, task, input
}

func TestTaskPricingReceiptFixtureRollbackAndOperationConflict(t *testing.T) {
	_, f, task, input := newTaskPricingReceiptFixture(t)
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("receipt-insert-fault", func(tx *gorm.DB) {
		if tx.Statement.Table == "isolated_task_pricing_receipts" {
			tx.AddError(errors.New("isolated receipt SQL failure"))
		}
	}))
	require.Error(t, stageTaskPricingReceiptFixture(f.db, task, input, nil))
	for _, table := range []any{&model.Task{}, &model.BillingSettlement{}, &isolatedTaskPricingReceipt{}} {
		var count int64
		require.NoError(t, f.db.Model(table).Count(&count).Error)
		require.Zero(t, count)
	}
	require.NoError(t, f.db.Callback().Create().Remove("receipt-insert-fault"))
	require.NoError(t, stageTaskPricingReceiptFixture(f.db, task, input, nil))
	require.NoError(t, readTaskPricingReceiptFixture(f.db, task.ID))
	other := *task
	other.ID++
	otherInput := input
	otherInput.TaskID = other.ID
	require.Error(t, stageTaskPricingReceiptFixture(f.db, &other, otherInput, nil), "same request cannot acquire another task")
	var count int64
	require.NoError(t, f.db.Model(&model.Task{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.EqualValues(t, 3000, getUserQuota(t, 901))
	require.NoError(t, readTaskPricingReceiptFixture(f.db, task.ID))
	// A self-consistent 7-second plan still reserves1500; its original receipt
	// refuses it, leaving this test's guarded replay untouched.
	task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan.SubmitFacts["seconds"] = 7
	task.PrivateData.BillingContext.TaskBillingPlan.EstimateQuota = 700
	require.NoError(t, task.Update())
	require.ErrorIs(t, readTaskPricingReceiptFixture(f.db, task.ID), taskpricing.ErrTaskPricingReceiptConflict)
	require.EqualValues(t, 3000, getUserQuota(t, 901))
}

// TestTaskPricingReceiptFixtureCrashAndRecovery kills writers on either side of
// commit, then verifies recovery applies one debit using the original receipt.
func TestTaskPricingReceiptFixtureCrashAndRecovery(t *testing.T) {
	for _, phase := range []string{"before-commit", "after-commit"} {
		t.Run(phase, func(t *testing.T) {
			dir, f, task, input := newTaskPricingReceiptFixture(t)
			child := startReceiptProcess(t, dir, phase, task, input)
			barrier := awaitBillingProcessBarrier(t, child)
			require.Equal(t, phase == "before-commit", barrier.InsideTransaction)
			require.NoError(t, child.cmd.Process.Kill())
			require.Error(t, <-child.done, child.output.text())
			want := int64(0)
			if phase == "after-commit" {
				want = 1
			}
			for _, table := range []any{&model.Task{}, &model.BillingSettlement{}, &isolatedTaskPricingReceipt{}} {
				var count int64
				require.NoError(t, f.db.Model(table).Count(&count).Error)
				require.Equal(t, want, count)
			}
			require.EqualValues(t, 3000, getUserQuota(t, 901))
			if want == 0 {
				return
			}
			// The writer died after commit without returning success. Read the
			// committed original; do not submit another request or invent a plan.
			require.NoError(t, readTaskPricingReceiptFixture(f.db, task.ID))
			var original isolatedTaskPricingReceipt
			require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&original).Error)
			wantProvenance := fmt.Sprintf(`{"version":1,"requirement":"wallet_receipt_v1","task_id":%d,"operation_key":%q}`, task.ID, input.OperationKey)
			require.JSONEq(t, wantProvenance, original.Provenance, "the killed writer must have durably retained its original source")
			// Two independently restarted workers use the original financial
			// engine. SQLite contention is reported, not retried until green.
			a := startReceiptProcess(t, dir, "recover", task, input)
			b := startReceiptProcess(t, dir, "recover", task, input)
			awaitBillingProcessBarrier(t, a)
			awaitBillingProcessBarrier(t, b)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "release"), nil, 0600))
			require.NoError(t, <-a.done, a.output.text())
			require.NoError(t, <-b.done, b.output.text())
			var record model.BillingSettlement
			require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&record).Error)
			if record.Status == model.BillingSettlementStatusPending {
				// One contender may durably schedule a retry after SQLite reports
				// writer contention. Only that transient state is acceptable; let
				// one later worker consume the recorded backoff before asserting the
				// final financial result.
				require.Positive(t, record.Attempts)
				require.Positive(t, record.NextAttempt)
				require.True(t, strings.Contains(record.LastError, "SQLITE_BUSY") || strings.Contains(record.LastError, "database is locked"), record.LastError)
				wait := time.Until(time.Unix(record.NextAttempt, 0))
				require.LessOrEqual(t, wait, 64*time.Second)
				if wait > 0 {
					timer := time.NewTimer(wait)
					<-timer.C
				}
				recovery := startReceiptProcess(t, dir, "recover", task, input)
				awaitBillingProcessBarrier(t, recovery)
				require.NoError(t, <-recovery.done, recovery.output.text())
				require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&record).Error)
			}
			require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
			require.EqualValues(t, 1500, record.AppliedFundingDelta)
			require.EqualValues(t, 1500, getUserQuota(t, 901))
			require.Equal(t, 1500, getTokenRemainQuota(t, 903))
			require.NoError(t, readTaskPricingReceiptFixture(f.db, task.ID))
			var after isolatedTaskPricingReceipt
			require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&after).Error)
			require.Equal(t, original, after, "restart recovery must not regenerate either original field")
			settleTaskPricingHandoffFixture(t, f, task.ID)
		})
	}
}

func TestTaskPricingReceiptFixtureRejectsMissingProvenance(t *testing.T) {
	_, f, task, input := newTaskPricingReceiptFixture(t)
	require.NoError(t, stageTaskPricingReceiptFixture(f.db, task, input, nil))
	require.NoError(t, f.db.Model(&isolatedTaskPricingReceipt{}).Where("operation_key = ?", input.OperationKey).Update("provenance", "null").Error)
	require.ErrorIs(t, readTaskPricingReceiptFixture(f.db, task.ID), taskpricing.ErrTaskPricingReceiptConflict)
	require.EqualValues(t, 3000, getUserQuota(t, 901))
	var after isolatedTaskPricingReceipt
	require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&after).Error)
	require.Equal(t, "null", after.Provenance)
}

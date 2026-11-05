// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TEST ONLY: map a candidate column onto the disposable real settlement table.
// Neither BillingSettlement nor startup migration/worker is changed by this.
type taskPricingReceiptCandidate struct {
	BillingSettlement
	TaskPricingReceipt    *string `gorm:"type:text"`
	TaskPricingProvenance *string `gorm:"type:text"`
}

func (taskPricingReceiptCandidate) TableName() string { return "billing_settlements" }

type taskPricingReceiptFixture struct {
	task  Task
	input BillingSettlementInput
}

func newTaskPricingReceiptCandidate(t *testing.T, db *gorm.DB, number int, ratio float64) taskPricingReceiptFixture {
	t.Helper()
	f := newTaskBillingContractFixture(t, db, number, BillingSettlementSourceWallet, 0)
	taskBillingTestCheck(t, db.Model(&User{}).Where("id = ?", f.user.Id).Update("quota", 3000).Error, "seed receipt wallet")
	taskBillingTestCheck(t, db.Model(&Token{}).Where("id = ?", f.token.Id).Updates(map[string]interface{}{"remain_quota": 3000, "used_quota": 0}).Error, "seed receipt token")
	request := fmt.Sprintf("receipt-contract-%d", number)
	contract, ok := taskpricing.KnownContract("hailuo", "MiniMax-H3")
	require.True(t, ok)
	digest, err := taskpricing.ContractDigest(contract)
	taskBillingTestCheck(t, err, "construct synthetic fact contract")
	guards := taskpricing.Guards{PriceVersion: "receipt-fixture-price", ContractDigest: digest, BindingVersion: "receipt-fixture-binding"}
	plan, err := taskpricing.FreezeBounded(taskpricing.FreezeInput{
		PreviewInput: taskpricing.PreviewInput{Profile: taskpricing.NewAPITask,
			Prices:        types.TaskPricingMaps{PluginBillingExpr: map[string]string{"hailuo::receipt-model": `u("seconds") * 0.1`}},
			Source:        taskpricing.Source{Driver: types.TaskUsageProducerKindTaskPlugin, PluginKey: "hailuo"},
			OriginalModel: "receipt-model", MappedModel: "MiniMax-H3", Contract: contract, Current: guards, Expected: guards,
			Submit:    types.TaskPricingFacts{"seconds": 6, "resolution": "768P", "input_images": 0, "input_video_seconds": 0},
			PricingAt: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: ratio, ReservationFloor: 700},
		ChannelID: f.channel.Id, Action: "generate", RequestID: request, SourceID: "hailuo", PluginVersion: "1.0.0", PackageDigest: strings.Repeat("a", 64),
	})
	taskBillingTestCheck(t, err, "freeze synthetic receipt plan")
	wantReserve := 1500
	if ratio == 0 {
		wantReserve = 0
	}
	require.Equal(t, wantReserve, plan.ReserveQuota) // independent: max(15*.1*1000,700), or free group0
	binding := &types.TaskPluginBinding{Profile: types.TaskPluginExpressionProfile, Action: "generate", PhaseApprovalID: strings.Repeat("b", 64),
		PluginKey: "hailuo", Version: "1.0.0", PackageDigest: strings.Repeat("a", 64), SourceID: "hailuo", SchemaVersion: 1,
		ContractDigest: digest, Model: "receipt-model", UpstreamModel: "MiniMax-H3", UserID: f.user.Id, TokenID: f.token.Id,
		ChannelID: f.channel.Id, RequestID: request}
	f.task.Status, f.task.Quota, f.task.Group, f.task.Action = TaskStatusNotStart, 0, "default", "generate"
	f.task.Properties.OriginModelName, f.task.Properties.UpstreamModelName = "receipt-model", "MiniMax-H3"
	f.task.PrivateData = TaskPrivateData{TokenId: f.token.Id, BillingSource: BillingSettlementSourceWallet, BillingRequestId: request,
		AwaitingUpstreamID: true, PluginBinding: binding, BillingContext: &TaskBillingContext{OriginModelName: "receipt-model", GroupRatio: ratio, TaskBillingPlan: plan}}
	taskBillingTestCheck(t, db.Delete(&f.task).Error, "remove uncommitted candidate task")
	q := int64(wantReserve)
	return taskPricingReceiptFixture{task: f.task, input: BillingSettlementInput{OperationKey: "request:" + request + ":pre-consume",
		Source: BillingSettlementSourceWallet, UserID: f.user.Id, TokenID: f.token.Id, TaskID: f.task.ID,
		FundingDelta: q, TokenDelta: q, TaskQuotaTarget: q, ManualOnFailure: true,
		PreConsumeRequestID: request, PreConsumeModelName: "receipt-model", PreConsumeRequestedQuota: q, PreConsumeEffectiveQuota: q}}
}

func receiptWalletInput(task *Task, operation string) taskpricing.WalletReceiptInput {
	var plan *types.TaskBillingPlan
	if task.PrivateData.BillingContext != nil {
		plan = task.PrivateData.BillingContext.TaskBillingPlan
	}
	return taskpricing.WalletReceiptInput{TaskID: task.ID, OperationKey: operation, Group: task.Group, Plan: plan, Binding: task.PrivateData.PluginBinding}
}

func stageTaskPricingReceiptCandidate(db *gorm.DB, f taskPricingReceiptFixture, fault string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&f.task).Error; err != nil {
			return err
		}
		f.input.TaskID = f.task.ID
		if fault == "task" {
			return errors.New("isolated after task")
		}
		if err := validateBillingSettlementInput(f.input); err != nil {
			return err
		}
		receipt, err := taskpricing.PrepareWalletReservationReceipt(receiptWalletInput(&f.task, f.input.OperationKey))
		if err != nil {
			return err
		}
		provenance, err := taskpricing.PrepareReceiptProvenance(taskpricing.ReceiptWalletV1, receiptWalletInput(&f.task, f.input.OperationKey))
		if err != nil {
			return err
		}
		if err := taskpricing.ValidateReceiptProvenance(provenance, receipt, receiptWalletInput(&f.task, f.input.OperationKey)); err != nil {
			return err
		}
		row, _, err := ensureBillingSettlementRecordDB(tx, f.input)
		if err != nil {
			return err
		}
		if fault == "intent" {
			return errors.New("isolated after intent")
		}
		wire, err := common.Marshal(receipt)
		if err != nil {
			return err
		}
		origin, err := common.Marshal(provenance)
		if err != nil {
			return err
		}
		updated := tx.Model(&taskPricingReceiptCandidate{}).
			Where("id = ? AND task_pricing_receipt IS NULL AND task_pricing_provenance IS NULL", row.ID).
			Updates(map[string]interface{}{"task_pricing_receipt": string(wire), "task_pricing_provenance": string(origin)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return taskpricing.ErrTaskPricingReceiptConflict
		}
		if err := validateTaskPricingReceiptCandidate(tx, f.input); err != nil {
			return err
		}
		if fault == "receipt" {
			return errors.New("isolated after receipt")
		}
		return nil
	})
}

func validateTaskPricingReceiptCandidate(db *gorm.DB, expected BillingSettlementInput) error {
	var row taskPricingReceiptCandidate
	if err := db.Where("operation_key = ?", expected.OperationKey).First(&row).Error; err != nil {
		return err
	}
	if err := validateBillingSettlement(row.BillingSettlement, expected); err != nil {
		return err
	}
	var provenance *taskpricing.ReceiptProvenance
	if row.TaskPricingProvenance != nil {
		if err := common.UnmarshalJsonStr(*row.TaskPricingProvenance, &provenance); err != nil {
			return taskpricing.ErrTaskPricingReceiptConflict
		}
	}
	var task Task
	if err := db.First(&task, row.TaskID).Error; err != nil {
		return err
	}
	if row.UserID != task.UserId || row.TokenID != task.PrivateData.TokenId {
		return taskpricing.ErrTaskPricingReceiptConflict
	}
	b := task.PrivateData.PluginBinding
	if b == nil || task.UserId != b.UserID || task.PrivateData.TokenId != b.TokenID || task.ChannelId != b.ChannelID ||
		task.PrivateData.BillingRequestId != b.RequestID || task.Action != b.Action || task.Properties.OriginModelName != b.Model ||
		task.Properties.UpstreamModelName != b.UpstreamModel {
		return taskpricing.ErrTaskPricingReceiptConflict
	}
	var receipt *taskpricing.TaskPricingReceipt
	if row.TaskPricingReceipt != nil {
		if err := common.UnmarshalJsonStr(*row.TaskPricingReceipt, &receipt); err != nil {
			return taskpricing.ErrTaskPricingReceiptConflict
		}
	}
	// Read the original host decision; never synthesize it from the current
	// plan/receipt. Only this isolated fixture persists the candidate column.
	frozen := receiptWalletInput(&task, row.OperationKey)
	if err := taskpricing.ValidateReceiptProvenance(provenance, receipt, frozen); err != nil {
		return err
	}
	quota := int64(frozen.Plan.ReserveQuota)
	if row.Source != BillingSettlementSourceWallet || task.PrivateData.BillingSource != BillingSettlementSourceWallet ||
		row.SubscriptionID != 0 || task.PrivateData.SubscriptionId != 0 || row.SubscriptionPreConsumeRequestID != "" ||
		row.FinalizeSubscriptionPreConsume || row.AllowMissingToken || row.EffectPayload != "" ||
		row.TaskQuota != 0 || row.TaskQuotaTarget != quota || row.FundingDelta != quota || row.TokenDelta != quota ||
		row.PreConsumeRequestID != task.PrivateData.BillingRequestId || row.PreConsumeModelName != frozen.Plan.ExpressionPlan.OriginalModel ||
		row.PreConsumeRequestedQuota != quota || row.PreConsumeEffectiveQuota != quota {
		return taskpricing.ErrTaskPricingReceiptConflict
	}
	return nil
}

// TEST ONLY: check even an already-applied replay. The in-transaction guard
// separately repeats validation before pending funds can commit.
func replayTaskPricingReceiptCandidate(db *gorm.DB, input BillingSettlementInput) (int64, bool, error) {
	if err := validateTaskPricingReceiptCandidate(db, input); err != nil {
		return 0, false, err
	}
	return ApplyBillingSettlementOnce(input)
}

func installTaskPricingReceiptTransactionGuard(t *testing.T, db *gorm.DB, input BillingSettlementInput) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	name := "test:receipt-in-funding-transaction"
	// Keep existing fixture rows outside this candidate's worker scan.
	taskBillingTestCheck(t, db.Callback().Query().Before("gorm:query").Register(name+":scan", func(tx *gorm.DB) {
		if _, scan := tx.Statement.Dest.(*[]BillingSettlement); scan {
			tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Eq{Column: "operation_key", Value: input.OperationKey}}})
		}
	}), "scope worker scan to the isolated receipt candidate")
	taskBillingTestCheck(t, db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil {
			return
		}
		updates, _ := tx.Statement.Dest.(map[string]interface{})
		appliedTransition := tx.Statement.Schema.Table == "billing_settlements" && updates["status"] == BillingSettlementStatusApplied
		if tx.Statement.Schema.Table != "users" && !appliedTransition {
			return
		}
		// Only the test installs this guard. Validate in the existing funding
		// SQL transaction, including its applied transition for a zero delta.
		_, inTx := tx.Statement.ConnPool.(gorm.TxCommitter)
		if !inTx {
			tx.AddError(errors.New("receipt fixture guard is outside transaction"))
			return
		}
		calls.Add(1)
		if err := validateTaskPricingReceiptCandidate(tx.Session(&gorm.Session{NewDB: true}), input); err != nil {
			if errors.Is(err, taskpricing.ErrTaskPricingReceiptConflict) || errors.Is(err, gorm.ErrRecordNotFound) || isPermanentBillingSettlementError(err) {
				err = permanentBillingSettlement(err)
			}
			tx.AddError(err)
		}
	}), "install test-only receipt transaction guard")
	t.Cleanup(func() {
		taskBillingTestCheck(t, db.Callback().Update().Remove(name), "remove test-only receipt guard")
		taskBillingTestCheck(t, db.Callback().Query().Remove(name+":scan"), "remove fixture worker scan scope")
	})
	return calls
}

type receiptCandidateLostPool struct {
	gorm.ConnPool
	lost atomic.Bool
}

func (p *receiptCandidateLostPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	beginner, ok := p.ConnPool.(gorm.TxBeginner)
	if !ok {
		return nil, errors.New("receipt fixture connection cannot begin a transaction")
	}
	tx, err := beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &receiptCandidateLostTx{Tx: tx, pool: p}, nil
}

type receiptCandidateLostTx struct {
	*sql.Tx
	pool *receiptCandidateLostPool
}

var errReceiptCandidateLost = errors.New("isolated lost receipt after real commit")

func TestReceiptCandidateLostPoolRejectsUnsupportedTransactionPool(t *testing.T) {
	pool := &receiptCandidateLostPool{}
	tx, err := pool.BeginTx(context.Background(), nil)
	require.Error(t, err)
	require.Nil(t, tx)
}

func (tx *receiptCandidateLostTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.lost.CompareAndSwap(false, true) {
		return errReceiptCandidateLost
	}
	return nil
}

func runTaskPricingReceiptContracts(t *testing.T, db *gorm.DB) {
	// Seed the migration witness here: filtering to this subtest must not
	// depend on an earlier funding subtest leaving a settlement behind.
	legacy := BillingSettlement{OperationKey: "receipt-contract-legacy-before-migration",
		Source: BillingSettlementSourceWallet, Status: BillingSettlementStatusApplied}
	taskBillingTestCheck(t, db.Create(&legacy).Error, "seed isolated pre-migration legacy record")
	taskBillingTestCheck(t, db.Migrator().AddColumn(&taskPricingReceiptCandidate{}, "TaskPricingReceipt"), "add test-only nullable TEXT receipt")
	taskBillingTestCheck(t, db.Migrator().AddColumn(&taskPricingReceiptCandidate{}, "TaskPricingProvenance"), "add test-only nullable TEXT provenance")
	require.True(t, db.Migrator().HasColumn(&taskPricingReceiptCandidate{}, "TaskPricingReceipt"))
	require.True(t, db.Migrator().HasColumn(&taskPricingReceiptCandidate{}, "TaskPricingProvenance"))
	for _, column := range []string{"task_pricing_receipt", "task_pricing_provenance"} {
		require.True(t, db.Migrator().HasColumn(&taskPricingReceiptCandidate{}, column), "candidate update map keys must name real columns")
	}
	for _, column := range []string{"remain_quota", "used_quota"} {
		require.True(t, db.Migrator().HasColumn(&Token{}, column), "receipt fixture update must name a real column")
	}
	var migrated taskPricingReceiptCandidate
	taskBillingTestCheck(t, db.First(&migrated, legacy.ID).Error, "read legacy after candidate migration")
	require.Nil(t, migrated.TaskPricingReceipt)
	require.Nil(t, migrated.TaskPricingProvenance)
	require.Equal(t, legacy, migrated.BillingSettlement)
	sequence := 8800
	fixture := func(t *testing.T, ratio float64) taskPricingReceiptFixture {
		sequence++
		return newTaskPricingReceiptCandidate(t, db, sequence, ratio)
	}
	t.Run("writer_stamps_original_provenance", func(t *testing.T) {
		f := fixture(t, 1)
		taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage provenance candidate")
		var row taskPricingReceiptCandidate
		taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&row).Error, "read writer provenance")
		require.NotNil(t, row.TaskPricingProvenance)
		want := fmt.Sprintf(`{"version":1,"requirement":"wallet_receipt_v1","task_id":%d,"operation_key":%q}`, f.task.ID, f.input.OperationKey)
		require.JSONEq(t, want, *row.TaskPricingProvenance)
	})
	t.Run("provenance_required_by_guard_and_replay", func(t *testing.T) {
		for _, change := range []string{"missing", "json-null", "malformed", "unknown-version", "unknown-policy", "another-task", "another-operation", "legacy-downgrade", "missing-zero"} {
			t.Run(change, func(t *testing.T) {
				ratio := 1.0
				if change == "missing-zero" {
					ratio = 0
				}
				f := fixture(t, ratio)
				taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage original provenance")
				var raw any
				switch change {
				case "json-null":
					raw = "null"
				case "malformed":
					raw = "{retained-malformed"
				case "unknown-version":
					raw = fmt.Sprintf(`{"version":999,"requirement":"wallet_receipt_v1","task_id":%d,"operation_key":%q}`, f.task.ID, f.input.OperationKey)
				case "unknown-policy":
					raw = fmt.Sprintf(`{"version":1,"requirement":"future","task_id":%d,"operation_key":%q}`, f.task.ID, f.input.OperationKey)
				case "another-task":
					raw = fmt.Sprintf(`{"version":1,"requirement":"wallet_receipt_v1","task_id":%d,"operation_key":%q}`, f.task.ID+1, f.input.OperationKey)
				case "another-operation":
					raw = fmt.Sprintf(`{"version":1,"requirement":"wallet_receipt_v1","task_id":%d,"operation_key":"task:77:finalize"}`, f.task.ID)
				case "legacy-downgrade":
					raw = fmt.Sprintf(`{"version":1,"requirement":"legacy","task_id":%d,"operation_key":%q}`, f.task.ID, f.input.OperationKey)
				}
				taskBillingTestCheck(t, db.Model(&taskPricingReceiptCandidate{}).Where("operation_key = ?", f.input.OperationKey).Update("task_pricing_provenance", raw).Error, "seed invalid provenance")
				require.Error(t, validateTaskPricingReceiptCandidate(db, f.input), "cannot infer provenance from a valid receipt or zero")
				guard := installTaskPricingReceiptTransactionGuard(t, db, f.input)
				ProcessPendingBillingSettlementsOnce()
				require.Positive(t, guard.Load())
				require.Equal(t, BillingSettlementStatusManual, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
				_, _, err := replayTaskPricingReceiptCandidate(db, f.input)
				require.Error(t, err)
				var user User
				var token Token
				var after taskPricingReceiptCandidate
				taskBillingTestCheck(t, db.First(&user, f.input.UserID).Error, "read rejected provenance wallet")
				taskBillingTestCheck(t, db.First(&token, f.input.TokenID).Error, "read rejected provenance token")
				taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&after).Error, "read preserved provenance")
				require.EqualValues(t, 3000, user.Quota)
				require.EqualValues(t, 3000, token.RemainQuota)
				if raw == nil {
					require.Nil(t, after.TaskPricingProvenance)
				} else {
					require.Equal(t, raw, *after.TaskPricingProvenance)
				}
			})
		}
	})
	for _, ratio := range []float64{1, 0} {
		for _, layer := range []string{"writer", "reader"} {
			t.Run(fmt.Sprintf("%s_rejects_reservation_drift_ratio_%g", layer, ratio), func(t *testing.T) {
				f := fixture(t, ratio)
				if layer == "reader" {
					taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage fixed reservation")
				}
				q := int64(f.task.PrivateData.BillingContext.TaskBillingPlan.ReserveQuota) + 1
				f.input.FundingDelta, f.input.TokenDelta, f.input.TaskQuotaTarget = q, q, q
				f.input.PreConsumeRequestedQuota, f.input.PreConsumeEffectiveQuota = q, q
				if layer == "writer" {
					require.ErrorIs(t, stageTaskPricingReceiptCandidate(db, f, ""), taskpricing.ErrTaskPricingReceiptConflict)
					return
				}
				taskBillingTestCheck(t, db.Model(&BillingSettlement{}).Where("operation_key = ?", f.input.OperationKey).
					Select("FundingDelta", "TokenDelta", "TaskQuotaTarget", "PreConsumeRequestedQuota", "PreConsumeEffectiveQuota").
					Updates(&BillingSettlement{FundingDelta: q, TokenDelta: q, TaskQuotaTarget: q, PreConsumeRequestedQuota: q, PreConsumeEffectiveQuota: q}).Error, "seed coherent input/row amount drift")
				require.ErrorIs(t, validateTaskPricingReceiptCandidate(db, f.input), taskpricing.ErrTaskPricingReceiptConflict)
			})
		}
	}
	// The input/record pair must belong to the same account as Task/binding;
	// two independently self-consistent identity pairs are not sufficient.
	for _, mismatch := range []string{"user", "token", "user-and-token"} {
		t.Run("writer_rejects_settlement_owner_"+mismatch, func(t *testing.T) {
			f, other := fixture(t, 1), fixture(t, 1)
			if mismatch != "token" {
				f.input.UserID = other.input.UserID
			}
			if mismatch != "user" {
				f.input.TokenID = other.input.TokenID
			}
			require.ErrorIs(t, stageTaskPricingReceiptCandidate(db, f, ""), taskpricing.ErrTaskPricingReceiptConflict)
			var tasks, intents int64
			taskBillingTestCheck(t, db.Model(&Task{}).Where("id = ?", f.task.ID).Count(&tasks).Error, "count rejected owner task")
			taskBillingTestCheck(t, db.Model(&BillingSettlement{}).Where("operation_key = ?", f.input.OperationKey).Count(&intents).Error, "count rejected owner intent")
			require.Zero(t, tasks)
			require.Zero(t, intents)
		})
		t.Run("reader_rejects_settlement_owner_"+mismatch, func(t *testing.T) {
			f, other := fixture(t, 1), fixture(t, 1)
			taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage original owner candidate")
			if mismatch != "token" {
				f.input.UserID = other.input.UserID
			}
			if mismatch != "user" {
				f.input.TokenID = other.input.TokenID
			}
			taskBillingTestCheck(t, db.Model(&BillingSettlement{}).Where("operation_key = ?", f.input.OperationKey).
				Select("UserID", "TokenID").Updates(&BillingSettlement{UserID: f.input.UserID, TokenID: f.input.TokenID}).Error, "seed cross-account record and matching input")
			require.ErrorIs(t, validateTaskPricingReceiptCandidate(db, f.input), taskpricing.ErrTaskPricingReceiptConflict)
			if mismatch != "user-and-token" {
				return
			}
			// The second account's token really belongs to it. Only the missing
			// row-to-task edge rejects this case before a valid pair is debited.
			guard := installTaskPricingReceiptTransactionGuard(t, db, f.input)
			ProcessPendingBillingSettlementsOnce()
			require.Positive(t, guard.Load())
			record := taskBillingContractRecord(t, db, f.input.OperationKey)
			require.Equal(t, BillingSettlementStatusManual, record.Status)
			var users []User
			var tokens []Token
			var logs int64
			taskBillingTestCheck(t, db.Where("id IN ?", []int{f.task.UserId, other.input.UserID}).Find(&users).Error, "read both untouched wallets")
			taskBillingTestCheck(t, db.Where("id IN ?", []int{f.task.PrivateData.TokenId, other.input.TokenID}).Find(&tokens).Error, "read both untouched tokens")
			taskBillingTestCheck(t, db.Model(&Log{}).Where("user_id IN ?", []int{f.task.UserId, other.input.UserID}).Count(&logs).Error, "count cross-account logs")
			require.Len(t, users, 2)
			require.Len(t, tokens, 2)
			for _, user := range users {
				require.EqualValues(t, 3000, user.Quota)
			}
			for _, token := range tokens {
				require.Equal(t, int64(3000), token.RemainQuota)
			}
			require.Zero(t, logs)
		})
	}
	t.Run("all_artifacts_rollback", func(t *testing.T) {
		for _, phase := range []string{"task", "intent", "receipt"} {
			f := fixture(t, 1)
			require.Error(t, stageTaskPricingReceiptCandidate(db, f, phase))
			var tasks, rows int64
			taskBillingTestCheck(t, db.Model(&Task{}).Where("id = ?", f.task.ID).Count(&tasks).Error, "count rolled back task")
			taskBillingTestCheck(t, db.Model(&BillingSettlement{}).Where("operation_key = ?", f.input.OperationKey).Count(&rows).Error, "count rolled back intent")
			require.Zero(t, tasks)
			require.Zero(t, rows)
		}
	})
	t.Run("unknown_commit_uses_original_and_cas", func(t *testing.T) {
		f := fixture(t, 1)
		pool := &receiptCandidateLostPool{ConnPool: db.ConnPool}
		faultDB := db.Session(&gorm.Session{NewDB: true})
		faultDB.ConnPool, faultDB.Statement.ConnPool = pool, pool
		require.ErrorIs(t, stageTaskPricingReceiptCandidate(faultDB, f, ""), errReceiptCandidateLost)
		require.True(t, pool.lost.Load())
		taskBillingTestCheck(t, validateTaskPricingReceiptCandidate(db, f.input), "read committed original after lost reply")
		var row taskPricingReceiptCandidate
		taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&row).Error, "read original receipt bytes")
		cas := db.Model(&taskPricingReceiptCandidate{}).
			Where("id = ? AND task_pricing_receipt IS NULL AND task_pricing_provenance IS NULL", row.ID).
			Updates(map[string]interface{}{"task_pricing_receipt": "replacement", "task_pricing_provenance": "replacement"})
		taskBillingTestCheck(t, cas.Error, "attempt conflicting receipt CAS")
		require.Zero(t, cas.RowsAffected)
		var preserved taskPricingReceiptCandidate
		taskBillingTestCheck(t, db.First(&preserved, row.ID).Error, "read both original evidence columns after conflicting CAS")
		require.Equal(t, row.TaskPricingReceipt, preserved.TaskPricingReceipt)
		require.Equal(t, row.TaskPricingProvenance, preserved.TaskPricingProvenance)
		other := f
		other.task.ID = 0
		other.task.TaskID += "-other"
		require.Error(t, stageTaskPricingReceiptCandidate(db, other, ""))
		var tasks int64
		taskBillingTestCheck(t, db.Model(&Task{}).Where("task_id = ?", other.task.TaskID).Count(&tasks).Error, "count conflicting task")
		require.Zero(t, tasks)
		guard := installTaskPricingReceiptTransactionGuard(t, db, f.input)
		ProcessPendingBillingSettlementsOnce()
		require.Positive(t, guard.Load())
		applied, replay, err := replayTaskPricingReceiptCandidate(db, f.input)
		taskBillingTestCheck(t, err, "replay original reservation")
		require.True(t, replay)
		require.EqualValues(t, 1500, applied)
		// Finalize is a separate lifecycle operation; its own receipt cannot be
		// replaced by the pre-consume receipt, even when the amounts match.
		var original taskpricing.TaskPricingReceipt
		taskBillingTestCheck(t, common.UnmarshalJsonStr(*row.TaskPricingReceipt, &original), "decode original receipt")
		wrongChild := receiptWalletInput(&f.task, BillingTaskFinalizeOperationKey(f.task.ID))
		require.ErrorIs(t, taskpricing.ValidateReceiptCompatibility(taskpricing.ReceiptWalletV1, &original, wrongChild), taskpricing.ErrTaskPricingReceiptConflict)
		taskBillingTestCheck(t, db.Callback().Update().Remove("test:receipt-in-funding-transaction"), "remove guard before independent finalization")
		taskBillingTestCheck(t, db.Callback().Query().Remove("test:receipt-in-funding-transaction:scan"), "remove worker scope before independent finalization")
		final := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(f.task.ID), Source: BillingSettlementSourceWallet,
			UserID: f.input.UserID, TokenID: f.input.TokenID, TaskID: f.task.ID, TaskQuota: 1500, TaskQuotaTarget: 800, FundingDelta: -700, TokenDelta: -700,
			Effect: &BillingSettlementEffect{LogType: LogTypeConsume, ModelName: "receipt-model", TokenID: f.input.TokenID, ChannelID: f.task.ChannelId,
				Quota: 800, QuotaIsActual: true, UpdateUsage: true}}
		for i := 0; i < 2; i++ {
			delta, replay, err := ApplyBillingSettlementOnce(final)
			taskBillingTestCheck(t, err, "settle independent 8-second target")
			require.EqualValues(t, -700, delta)
			require.Equal(t, i > 0, replay)
			taskBillingTestCheck(t, ProcessBillingSettlementEffect(final.OperationKey), "apply final effects once")
		}
		var user User
		var token Token
		var logs int64
		taskBillingTestCheck(t, db.First(&user, f.input.UserID).Error, "read final wallet")
		taskBillingTestCheck(t, db.First(&token, f.input.TokenID).Error, "read final token")
		taskBillingTestCheck(t, db.Model(&Log{}).Where("user_id = ?", f.input.UserID).Count(&logs).Error, "count final log")
		require.EqualValues(t, 2200, user.Quota)
		require.Equal(t, int64(2200), token.RemainQuota)
		require.EqualValues(t, 1, logs)
	})
	t.Run("applied_replay_requires_its_original_receipt", func(t *testing.T) {
		f := fixture(t, 1)
		taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage applied replay candidate")
		installTaskPricingReceiptTransactionGuard(t, db, f.input)
		ProcessPendingBillingSettlementsOnce()
		require.Equal(t, BillingSettlementStatusApplied, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
		taskBillingTestCheck(t, db.Model(&taskPricingReceiptCandidate{}).Where("operation_key = ?", f.input.OperationKey).
			Update("task_pricing_receipt", `{"version":999,"digest":"retain-after-applied"}`).Error, "replace applied receipt with unknown version")
		_, _, err := replayTaskPricingReceiptCandidate(db, f.input)
		require.ErrorIs(t, err, taskpricing.ErrTaskPricingReceiptConflict)
		// Negative control: the unmodified production fast return still ignores
		// this candidate column. Do not claim the fixture upgraded that consumer.
		amount, replay, err := ApplyBillingSettlementOnce(f.input)
		taskBillingTestCheck(t, err, "observe existing ungated replay boundary")
		require.True(t, replay)
		require.EqualValues(t, 1500, amount)
		var user User
		var row taskPricingReceiptCandidate
		taskBillingTestCheck(t, db.First(&user, f.input.UserID).Error, "read once-debited replay wallet")
		taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&row).Error, "read retained applied evidence")
		require.EqualValues(t, 1500, user.Quota)
		require.Equal(t, `{"version":999,"digest":"retain-after-applied"}`, *row.TaskPricingReceipt)
	})
	t.Run("applied_replay_requires_its_original_provenance", func(t *testing.T) {
		f := fixture(t, 1)
		taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage provenance replay candidate")
		installTaskPricingReceiptTransactionGuard(t, db, f.input)
		ProcessPendingBillingSettlementsOnce()
		require.Equal(t, BillingSettlementStatusApplied, taskBillingContractRecord(t, db, f.input.OperationKey).Status)
		taskBillingTestCheck(t, db.Model(&taskPricingReceiptCandidate{}).Where("operation_key = ?", f.input.OperationKey).
			Update("task_pricing_provenance", nil).Error, "remove already-applied provenance")
		_, _, err := replayTaskPricingReceiptCandidate(db, f.input)
		require.ErrorIs(t, err, taskpricing.ErrTaskPricingReceiptConflict)
		// The actual production fast return is deliberately unchanged. Its
		// successful negative control does not prove any receipt protection.
		amount, replay, err := ApplyBillingSettlementOnce(f.input)
		taskBillingTestCheck(t, err, "observe ungated production provenance replay")
		require.True(t, replay)
		require.EqualValues(t, 1500, amount)
		var row taskPricingReceiptCandidate
		var user User
		taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&row).Error, "read unrepaired applied evidence")
		taskBillingTestCheck(t, db.First(&user, f.input.UserID).Error, "read once-debited wallet after provenance replay")
		require.Nil(t, row.TaskPricingProvenance)
		require.EqualValues(t, 1500, user.Quota)
	})
	t.Run("worker_replay_reconciliation_reject_and_retain", func(t *testing.T) {
		for _, change := range []string{"missing", "missing-zero", "unknown-version", "same-amount-plan", "query-failure"} {
			t.Run(change, func(t *testing.T) {
				ratio := 1.0
				if change == "missing-zero" {
					ratio = 0
				}
				f := fixture(t, ratio)
				if change == "query-failure" {
					f.input.ManualOnFailure = false
				}
				taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage rejection candidate")
				if strings.HasPrefix(change, "missing") {
					taskBillingTestCheck(t, db.Model(&taskPricingReceiptCandidate{}).Where("operation_key = ?", f.input.OperationKey).Update("task_pricing_receipt", nil).Error, "remove fixture receipt")
				}
				if change == "unknown-version" {
					taskBillingTestCheck(t, db.Model(&taskPricingReceiptCandidate{}).Where("operation_key = ?", f.input.OperationKey).Update("task_pricing_receipt", `{"version":999,"digest":"retained-unknown"}`).Error, "set unknown receipt version")
				}
				if change == "same-amount-plan" {
					plan := f.task.PrivateData.BillingContext.TaskBillingPlan
					plan.ExpressionPlan.SubmitFacts["seconds"], plan.EstimateQuota = 7, 700
					taskBillingTestCheck(t, taskpricing.ValidateSnapshot(plan), "verify self-consistent 7-second replacement")
					taskBillingTestCheck(t, f.task.Update(), "replace same-reservation plan")
				}
				var before taskPricingReceiptCandidate
				taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&before).Error, "read receipt before guarded consumers")
				if change == "query-failure" {
					taskBillingTestCheck(t, db.Callback().Query().Before("gorm:query").Register("test:receipt-read-fault", func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "taskPricingReceiptCandidate" {
							tx.AddError(errors.New("isolated receipt read unavailable"))
						}
					}), "install receipt query failure")
				}
				guard := installTaskPricingReceiptTransactionGuard(t, db, f.input)
				ProcessPendingBillingSettlementsOnce()
				require.Positive(t, guard.Load())
				_, _, err := replayTaskPricingReceiptCandidate(db, f.input)
				require.Error(t, err, "unverified reservation cannot apply while evidence is unavailable")
				require.Error(t, validateTaskPricingReceiptCandidate(db, f.input), "read-only reconciliation cannot approve or regenerate receipt")
				if change == "query-failure" {
					taskBillingTestCheck(t, db.Callback().Query().Remove("test:receipt-read-fault"), "remove receipt query failure")
				}
				var after taskPricingReceiptCandidate
				var user User
				var token Token
				var logs int64
				taskBillingTestCheck(t, db.Where("operation_key = ?", f.input.OperationKey).First(&after).Error, "read preserved candidate")
				require.Equal(t, before.TaskPricingReceipt, after.TaskPricingReceipt)
				require.Equal(t, before.TaskPricingProvenance, after.TaskPricingProvenance)
				wantStatus := BillingSettlementStatusManual
				if change == "query-failure" {
					wantStatus = BillingSettlementStatusPending
				}
				require.Equal(t, wantStatus, after.Status)
				taskBillingTestCheck(t, db.First(&user, f.input.UserID).Error, "read untouched wallet")
				taskBillingTestCheck(t, db.First(&token, f.input.TokenID).Error, "read untouched token")
				taskBillingTestCheck(t, db.Model(&Log{}).Where("user_id = ?", f.input.UserID).Count(&logs).Error, "count unpaid logs")
				require.EqualValues(t, 3000, user.Quota)
				require.Equal(t, int64(3000), token.RemainQuota)
				require.Zero(t, logs)
				if change == "query-failure" {
					require.Positive(t, after.Attempts)
					require.Positive(t, after.NextAttempt)
					// Wait for the existing recorded schedule once, rather than
					// clearing its deadline or rerunning until the balance changes.
					delay := time.Until(time.Unix(after.NextAttempt, 0))
					require.LessOrEqual(t, delay, 8*time.Second)
					if delay > 0 {
						timer := time.NewTimer(delay)
						<-timer.C
					}
					ProcessPendingBillingSettlementsOnce()
					recovered := taskBillingContractRecord(t, db, f.input.OperationKey)
					require.Equal(t, BillingSettlementStatusApplied, recovered.Status)
					require.EqualValues(t, 1500, recovered.AppliedFundingDelta)
				}
			})
		}
	})
	t.Run("applied_zero_is_a_lifecycle", func(t *testing.T) {
		f := fixture(t, 0)
		taskBillingTestCheck(t, stageTaskPricingReceiptCandidate(db, f, ""), "stage explicit zero")
		before := taskBillingContractRecord(t, db, f.input.OperationKey)
		require.Equal(t, BillingSettlementStatusPending, before.Status)
		guard := installTaskPricingReceiptTransactionGuard(t, db, f.input)
		ProcessPendingBillingSettlementsOnce()
		require.Positive(t, guard.Load(), "a zero delta still requires receipt validation at applied transition")
		after := taskBillingContractRecord(t, db, f.input.OperationKey)
		require.Equal(t, BillingSettlementStatusApplied, after.Status)
		require.Zero(t, after.AppliedFundingDelta)
		taskBillingTestCheck(t, validateTaskPricingReceiptCandidate(db, f.input), "read applied zero receipt")
	})
}

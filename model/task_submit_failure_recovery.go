// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	TaskSubmitFailureRecoveryPending  = "pending"
	TaskSubmitFailureRecoveryRecorded = "intent_recorded"
	TaskSubmitFailureRecoveryManual   = "manual"
	TaskSubmitFailureStatusRecorded   = "failure_recorded"
)

var ErrTaskSubmitFailureRecoveryRequiresReview = errors.New("task submission rejection requires manual review")

// This journal preserves the original refund parameters, never a token key or
// provider payload. Recorded means the original settlement intent is durable;
// only BillingSettlement's applied fields prove that money has been refunded.
type TaskSubmitFailureRecovery struct {
	TaskID                          int64  `gorm:"primaryKey;autoIncrement:false"`
	OperationKey                    string `gorm:"type:varchar(191);not null;index"`
	Source                          string `gorm:"type:varchar(32);not null"`
	UserID                          int    `gorm:"not null;index"`
	SubscriptionID                  int    `gorm:"not null;default:0"`
	TokenID                         int    `gorm:"not null;default:0"`
	FundingDelta                    int64  `gorm:"not null"`
	TokenDelta                      int64  `gorm:"not null"`
	FailedReserveRevision           int64  `gorm:"column:failed_wallet_reserve_revision;not null;default:0"`
	SubscriptionPreConsumeRequestID string `gorm:"type:varchar(64)"`
	FinalizeSubscriptionPreConsume  bool   `gorm:"not null;default:false"`
	AllowMissingToken               bool   `gorm:"not null;default:false"`
	Reason                          string `gorm:"type:text"`
	Status                          string `gorm:"type:varchar(16);not null;index"`
	Attempts                        int    `gorm:"not null;default:0"`
	NextAttempt                     int64  `gorm:"not null;default:0;index"`
	LastError                       string `gorm:"type:text"`
	CreatedAt                       int64  `gorm:"not null"`
	UpdatedAt                       int64  `gorm:"not null"`
}

func (record TaskSubmitFailureRecovery) refundInput() BillingSettlementInput {
	return BillingSettlementInput{
		OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID,
		SubscriptionID: record.SubscriptionID, TokenID: record.TokenID,
		FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta,
		SubscriptionPreConsumeRequestID: record.SubscriptionPreConsumeRequestID,
		FinalizeSubscriptionPreConsume:  record.FinalizeSubscriptionPreConsume, AllowMissingToken: record.AllowMissingToken,
	}
}

func taskSubmitFailureRecoveryRecord(taskID int64, reason string, input BillingSettlementInput, failedReserve int64) (TaskSubmitFailureRecovery, error) {
	input.TokenKey = "" // Existing settlement replay resolves cache identity by ID.
	if taskID <= 0 {
		return TaskSubmitFailureRecovery{}, ErrBillingSettlementOperationConflict
	}
	if err := validateTaskSubmitFailureRefundShape(input, failedReserve); err != nil {
		return TaskSubmitFailureRecovery{}, err
	}
	reasonRunes := []rune(common.SanitizePersistedLogContent(reason))
	record := TaskSubmitFailureRecovery{
		TaskID: taskID, OperationKey: input.OperationKey, Source: input.Source, UserID: input.UserID,
		SubscriptionID: input.SubscriptionID, TokenID: input.TokenID, FundingDelta: input.FundingDelta, TokenDelta: input.TokenDelta,
		FailedReserveRevision:           failedReserve,
		SubscriptionPreConsumeRequestID: input.SubscriptionPreConsumeRequestID,
		FinalizeSubscriptionPreConsume:  input.FinalizeSubscriptionPreConsume, AllowMissingToken: input.AllowMissingToken,
		Reason: string(reasonRunes[:min(len(reasonRunes), 512)]), Status: TaskSubmitFailureRecoveryPending,
	}
	// Refuse any future input field this journal cannot preserve exactly.
	if record.refundInput() != input {
		return TaskSubmitFailureRecovery{}, ErrBillingSettlementOperationConflict
	}
	return record, nil
}

func validateTaskSubmitFailureRefundShape(input BillingSettlementInput, failedReserve int64) error {
	if failedReserve < 0 || failedReserve > 0 && input.Source != BillingSettlementSourceWallet && input.Source != BillingSettlementSourceSubscription ||
		input.FundingDelta > 0 || input.FundingDelta == 0 && (failedReserve == 0 || input.Source != BillingSettlementSourceWallet || input.TokenDelta != 0) || input.TokenDelta > 0 ||
		!strings.HasPrefix(input.OperationKey, billingRequestOperationPrefix) || !strings.HasSuffix(input.OperationKey, billingRequestFinalizeSuffix) {
		return ErrBillingSettlementOperationConflict
	}
	return validateBillingSettlementInput(input)
}

// A timeout can win before the first paid retry projects its reservation onto
// a free placeholder. Only the original applied debit identifies that amount.
func verifyTaskSubmitRefund(tx *gorm.DB, task *Task, input *BillingSettlementInput, failedReserve int64) error {
	requestID := task.PrivateData.BillingRequestId
	if input.Source == BillingSettlementSourceSubscription {
		return verifyTaskSubmitSubscriptionRefund(tx, requestID, input, failedReserve)
	}
	var selection BillingPreConsumeSelection
	if err := withRowLock(tx).Where("request_id = ?", requestID).Take(&selection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Older wallet tasks can have a projected balance without a
			// request receipt. It is the only amount that can be proved.
			if input.Source == BillingSettlementSourceWallet && failedReserve == 0 && task.Quota > 0 &&
				input.FundingDelta == -int64(task.Quota) && (input.TokenDelta == 0 || input.TokenDelta == input.FundingDelta) {
				var receipt BillingSettlement
				prefix := billingRequestOperationPrefix + requestID
				proofErr := withRowLock(tx).Select("id").Where("operation_key = ? OR operation_key LIKE ? ESCAPE '!'", prefix+":pre-consume",
					taskSubmitWalletReservePattern(prefix+":reserve:")).Take(&receipt).Error
				if errors.Is(proofErr, gorm.ErrRecordNotFound) {
					return nil
				}
				if proofErr != nil {
					return proofErr
				}
			}
			return ErrTaskSubmitRefundIdentityMismatch
		}
		return err
	}
	if selection.RequestID != requestID || selection.UserID != input.UserID || selection.TokenID != input.TokenID ||
		selection.Source != input.Source || selection.EffectiveQuota < 0 {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	if input.Source != BillingSettlementSourceWallet {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	return reconcileTaskSubmitWalletRefund(tx, selection, input, failedReserve)
}

func verifyTaskSubmitSubscriptionRefund(tx *gorm.DB, requestID string, input *BillingSettlementInput, failedReserve int64) error {
	var debit SubscriptionPreConsumeRecord
	if err := withRowLock(tx).Where("request_id = ?", requestID).Take(&debit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTaskSubmitRefundIdentityMismatch
		}
		return err
	}
	if debit.RequestId != requestID || debit.Status != "consumed" || debit.UserId != input.UserID ||
		debit.UserSubscriptionId != input.SubscriptionID || debit.PreConsumed <= 0 ||
		input.SubscriptionPreConsumeRequestID != requestID || !input.FinalizeSubscriptionPreConsume ||
		debit.TokenId != 0 && debit.TokenId != input.TokenID {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	funding := debit.PreConsumed
	token := int64(0)
	if debit.TokenId > 0 {
		token = debit.PreConsumed
	}
	prefix := billingRequestOperationPrefix + requestID + ":reserve:"
	var reserves []BillingSettlement
	if err := withRowLock(tx).Where("subscription_pre_consume_request_id = ? AND operation_key LIKE ? ESCAPE '!'", requestID,
		taskSubmitWalletReservePattern(prefix)).Find(&reserves).Error; err != nil {
		return err
	}
	return reconcileTaskSubmitReserveChain(tx, input, prefix, reserves, failedReserve, funding, token)
}

func reconcileTaskSubmitWalletRefund(tx *gorm.DB, selection BillingPreConsumeSelection, input *BillingSettlementInput, failedReserve int64) error {
	if input.SubscriptionID != 0 || input.SubscriptionPreConsumeRequestID != "" || input.FinalizeSubscriptionPreConsume {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	prefix := billingRequestOperationPrefix + selection.RequestID
	var initial BillingSettlement
	if err := withRowLock(tx).Where("operation_key = ?", prefix+":pre-consume").Take(&initial).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTaskSubmitRefundIdentityMismatch
		}
		return err
	}
	if initial.OperationKey != prefix+":pre-consume" || initial.PreConsumeRequestID != selection.RequestID ||
		initial.AppliedFundingDelta != selection.EffectiveQuota || !isAppliedTaskSubmitDebit(initial, *input) {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	funding, token := initial.AppliedFundingDelta, initial.AppliedTokenDelta
	prefix += ":reserve:"
	var reserves []BillingSettlement
	if err := withRowLock(tx).Where("operation_key LIKE ? ESCAPE '!'", taskSubmitWalletReservePattern(prefix)).Find(&reserves).Error; err != nil {
		return err
	}
	return reconcileTaskSubmitReserveChain(tx, input, prefix, reserves, failedReserve, funding, token)
}

func reconcileTaskSubmitReserveChain(tx *gorm.DB, input *BillingSettlementInput, prefix string, reserves []BillingSettlement, failedReserve, funding, token int64) error {
	revisions := make([]*BillingSettlement, len(reserves))
	for _, reserve := range reserves {
		suffix, sameRequest := strings.CutPrefix(reserve.OperationKey, prefix)
		revision, parseErr := strconv.ParseUint(suffix, 10, 64)
		if !sameRequest || parseErr != nil || revision == 0 || strconv.FormatUint(revision, 10) != suffix ||
			revision > uint64(len(reserves)) || revisions[revision-1] != nil || reserve.PreConsumeRequestID != "" ||
			reserve.Source != input.Source || reserve.UserID != input.UserID || reserve.TokenID != input.TokenID ||
			reserve.TaskID != 0 || reserve.SubscriptionID != input.SubscriptionID || reserve.EffectPayload != "" ||
			reserve.SubscriptionPreConsumeRequestID != input.SubscriptionPreConsumeRequestID || reserve.FinalizeSubscriptionPreConsume ||
			reserve.FundingDelta <= 0 || reserve.TokenDelta != 0 && reserve.TokenDelta != reserve.FundingDelta {
			return ErrTaskSubmitRefundIdentityMismatch
		}
		revisions[revision-1] = &reserve
	}
	if failedReserve != 0 && (failedReserve < int64(len(revisions)) || failedReserve > int64(len(revisions))+1) {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	for i, reserve := range revisions {
		if int64(i)+1 == failedReserve {
			if funding != -input.FundingDelta || token != -input.TokenDelta {
				return ErrTaskSubmitRefundIdentityMismatch
			}
			if reserve.Status != BillingSettlementStatusApplied {
				return cancelRejectedTaskReserve(tx, *reserve)
			}
		}
		if !isAppliedTaskSubmitDebit(*reserve, *input) ||
			reserve.AppliedFundingDelta > math.MaxInt64-funding || reserve.AppliedTokenDelta > math.MaxInt64-token {
			return ErrTaskSubmitRefundIdentityMismatch
		}
		funding += reserve.AppliedFundingDelta
		token += reserve.AppliedTokenDelta
	}
	if failedReserve != 0 && failedReserve == int64(len(revisions)) {
		// The reserve finished before the rejection obtained its row lock.
		input.FundingDelta, input.TokenDelta = -funding, -token
		return nil
	}
	if funding != -input.FundingDelta || token != -input.TokenDelta {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	return nil
}

func taskSubmitWalletReservePattern(prefix string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(prefix) + "%"
}

// Keep index creation on a populated PostgreSQL ledger out of service startup.
func checkBillingSettlementReserveLookup(db *gorm.DB, newTable bool) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	const index = "idx_billing_settlements_operation_key_pattern"
	ctx, cancel := context.WithTimeout(db.Statement.Context, 5*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&BillingSettlement{}); err != nil {
		return err
	}
	table := statement.Quote(clause.Table{Name: statement.Schema.Table})
	if newTable {
		if err := db.Exec("CREATE INDEX IF NOT EXISTS " + index + " ON " + table + " (operation_key varchar_pattern_ops)").Error; err != nil {
			return err
		}
	}
	var valid bool
	if err := db.Raw("SELECT COALESCE((SELECT indisvalid FROM pg_index WHERE indexrelid = to_regclass(?) AND indrelid = to_regclass(?)), false)", index, table).Scan(&valid).Error; err != nil {
		return err
	}
	if !valid {
		return errors.New("PostgreSQL reserve lookup index is missing or invalid: follow the PostgreSQL deployment step in pkg/taskplugin/management.md")
	}
	return nil
}

func cancelRejectedTaskReserve(tx *gorm.DB, reserve BillingSettlement) error {
	if reserve.AppliedFundingDelta != 0 || reserve.AppliedTokenDelta != 0 {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	if reserve.Status == billingSettlementStatusCancelled {
		return nil
	}
	if reserve.Status != BillingSettlementStatusPending && reserve.Status != BillingSettlementStatusManual {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	result := tx.Model(&BillingSettlement{}).Where("id = ? AND revision = ? AND status = ?", reserve.ID, reserve.Revision, reserve.Status).
		Updates(billingSettlementFailureUpdates(reserve.Attempts, "task submission rejected before additional debit", billingSettlementStatusCancelled, 0, time.Now().Unix(), false))
	if result.Error == nil && result.RowsAffected != 1 {
		return ErrBillingSettlementOperationConflict
	}
	return result.Error
}

func isAppliedTaskSubmitDebit(debit BillingSettlement, input BillingSettlementInput) bool {
	return debit.Status == BillingSettlementStatusApplied && debit.Source == input.Source &&
		debit.UserID == input.UserID && debit.TokenID == input.TokenID && debit.TaskID == 0 && debit.SubscriptionID == input.SubscriptionID &&
		debit.AppliedFundingDelta >= 0 && debit.AppliedFundingDelta == debit.FundingDelta &&
		debit.AppliedTokenDelta >= 0 && debit.AppliedTokenDelta == debit.TokenDelta
}

// RecordTaskSubmitFailureRecovery is a trusted-host seam. It records a definite
// rejection for later ownership checks; it does not authorize or apply money.
func RecordTaskSubmitFailureRecovery(taskID int64, reason string, input BillingSettlementInput) error {
	_, err := RecordTaskSubmitFailureRecoveryWithStatus(taskID, reason, input)
	return err
}

// RecordTaskSubmitFailureRecoveryWithStatus also reports the current durable
// journal status, including an existing record that still requires review.
func RecordTaskSubmitFailureRecoveryWithStatus(taskID int64, reason string, input BillingSettlementInput) (string, error) {
	return RecordTaskSubmitFailureWithReserveRecovery(taskID, reason, input, 0)
}

func RecordTaskSubmitFailureWithReserveRecovery(taskID int64, reason string, input BillingSettlementInput, failedReserve int64) (string, error) {
	record, err := taskSubmitFailureRecoveryRecord(taskID, reason, input, failedReserve)
	if err != nil {
		return "", err
	}
	return recordTaskSubmitFailureRecovery(record)
}

// RecordTaskSubmitFailureStatusRecovery records a definite no-charge rejection
// without granting refund authority. Replay verifies the persisted placeholder.
func RecordTaskSubmitFailureStatusRecovery(taskID int64, requestID, reason string, userID, tokenID int) (string, error) {
	record, err := taskSubmitFailureStatusRecoveryRecord(taskID, requestID, reason, userID, tokenID)
	if err != nil {
		return "", err
	}
	return recordTaskSubmitFailureRecovery(record)
}

func taskSubmitFailureStatusRecoveryRecord(taskID int64, requestID, reason string, userID, tokenID int) (TaskSubmitFailureRecovery, error) {
	if strings.TrimSpace(requestID) == "" {
		return TaskSubmitFailureRecovery{}, ErrTaskSubmitRefundIdentityMismatch
	}
	reasonRunes := []rune(common.SanitizePersistedLogContent(reason))
	record := TaskSubmitFailureRecovery{TaskID: taskID, OperationKey: BillingRequestFinalizeOperationKey(requestID),
		UserID: userID, TokenID: tokenID, Reason: string(reasonRunes[:min(len(reasonRunes), 512)]), Status: TaskSubmitFailureRecoveryPending}
	if _, err := record.statusOnlyRequestID(); err != nil {
		return TaskSubmitFailureRecovery{}, err
	}
	return record, nil
}

func (record TaskSubmitFailureRecovery) statusOnlyRequestID() (string, error) {
	requestKey, prefixed := strings.CutPrefix(record.OperationKey, billingRequestOperationPrefix)
	requestID, suffixed := strings.CutSuffix(requestKey, billingRequestFinalizeSuffix)
	if !prefixed || !suffixed || strings.TrimSpace(requestID) == "" || len(record.OperationKey) > 191 || record.TaskID <= 0 || record.UserID <= 0 || record.TokenID < 0 ||
		record.refundInput() != (BillingSettlementInput{OperationKey: record.OperationKey, UserID: record.UserID, TokenID: record.TokenID}) {
		return "", ErrTaskSubmitRefundIdentityMismatch
	}
	return requestID, nil
}

func verifyTaskSubmitFailureStatusOnly(tx *gorm.DB, task *Task, record TaskSubmitFailureRecovery) error {
	requestID, err := record.statusOnlyRequestID()
	if err != nil {
		return err
	}
	private := task.PrivateData
	if task.UserId != record.UserID || private.TokenId != record.TokenID || private.BillingRequestId != requestID ||
		task.Quota != 0 || private.BillingSource != "" && private.BillingSource != BillingSettlementSourceWallet ||
		private.SubscriptionId != 0 || private.UpstreamTaskID != "" || !private.AwaitingUpstreamID {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	var subscription SubscriptionPreConsumeRecord
	if err := withRowLock(tx).Select("request_id").Where("request_id = ?", requestID).Take(&subscription).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		if err != nil {
			return err
		}
		return ErrTaskSubmitRefundIdentityMismatch
	}
	var settlement BillingSettlement
	// A supported wallet reserve sequence starts at revision one. Any
	// finalize or attempted reserve has its own ownership and must reconcile.
	operationKeys := []string{billingRequestOperationPrefix + requestID + ":reserve:1", record.OperationKey, BillingTaskFinalizeOperationKey(task.ID)}
	if err := withRowLock(tx).Select("id").Where("operation_key IN ?", operationKeys).Take(&settlement).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		if err != nil {
			return err
		}
		return ErrTaskSubmitRefundIdentityMismatch
	}
	return verifyTaskSubmitFailureZeroDebit(tx, task, record, requestID)
}

func verifyTaskSubmitFailureZeroDebit(tx *gorm.DB, task *Task, record TaskSubmitFailureRecovery, requestID string) error {
	var selection BillingPreConsumeSelection
	selectionErr := withRowLock(tx).Where("request_id = ?", requestID).Take(&selection).Error
	if selectionErr != nil && !errors.Is(selectionErr, gorm.ErrRecordNotFound) {
		return selectionErr
	}
	var debit BillingSettlement
	debitErr := withRowLock(tx).Where("operation_key = ?", billingRequestOperationPrefix+requestID+":pre-consume").Take(&debit).Error
	if debitErr != nil && !errors.Is(debitErr, gorm.ErrRecordNotFound) {
		return debitErr
	}
	if selectionErr != nil && debitErr != nil && task.PrivateData.BillingSource == "" {
		return nil // The request used no billing session.
	}
	if selectionErr != nil || debitErr != nil || selection.Source != BillingSettlementSourceWallet ||
		selection.UserID != record.UserID || selection.TokenID != record.TokenID || selection.EffectiveQuota != 0 ||
		debit.Status != BillingSettlementStatusApplied || debit.Source != selection.Source ||
		debit.UserID != selection.UserID || debit.TokenID != selection.TokenID || debit.PreConsumeRequestID != requestID ||
		debit.PreConsumeEffectiveQuota != 0 || debit.PreConsumeRequestedQuota != selection.RequestedQuota ||
		debit.FundingDelta != 0 || debit.TokenDelta != 0 || debit.AppliedFundingDelta != 0 || debit.AppliedTokenDelta != 0 ||
		debit.TaskID != 0 || debit.SubscriptionID != 0 {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	return nil
}

func recordTaskSubmitFailureRecovery(record TaskSubmitFailureRecovery) (string, error) {
	if DB == nil {
		return "", errors.New("database is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	record.NextAttempt = time.Now().Unix()
	status := ""
	save := func(tx *gorm.DB) error {
		// Persist the rejection even if the task table cannot currently be
		// updated. Replay independently verifies task and funding ownership.
		candidate := record
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}}, DoNothing: true}).Create(&candidate).Error; err != nil {
			return err
		}
		var existing TaskSubmitFailureRecovery
		if err := withRowLock(tx).First(&existing, "task_id = ?", record.TaskID).Error; err != nil {
			return err
		}
		if existing.refundInput() != record.refundInput() || existing.FailedReserveRevision != record.FailedReserveRevision {
			return ErrBillingSettlementOperationConflict
		}
		status = existing.Status
		return nil
	}
	db := DB.WithContext(ctx)
	if err := retryTaskSubmitFailureTransaction(db, save); err != nil {
		return "", err
	}
	return status, nil
}

func retryTaskSubmitFailureTransaction(db *gorm.DB, write func(*gorm.DB) error) error {
	if db.Dialector.Name() != "sqlite" {
		return db.Transaction(write)
	}
	ctx := db.Statement.Context
	for {
		retry, err := func() (bool, error) {
			if err := billingSettlementSQLiteWriteMu.lockContext(ctx); err != nil {
				return false, err
			}
			defer billingSettlementSQLiteWriteMu.Unlock()
			return taskSubmitFailureTransaction(db, write)
		}()
		if !retry {
			return err
		}
		// The failed transaction has rolled back. Release the process gate
		// before retrying so unrelated local writers can use their own budget.
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func taskSubmitFailureTransaction(db *gorm.DB, write func(*gorm.DB) error) (retryable bool, err error) {
	err = withTaskSubmitFailureWriteBudget(db, func(conn *gorm.DB) error {
		prepared := false
		err := conn.Transaction(func(tx *gorm.DB) error {
			err := write(tx)
			prepared = err == nil
			return err
		})
		var code interface{ Code() int }
		if conn.Dialector.Name() != "sqlite" || !errors.As(err, &code) || code.Code()&0xff != sqlite3.SQLITE_BUSY {
			return err
		}
		if prepared {
			// SQLite keeps the write open after a busy COMMIT. database/sql
			// has closed its Tx handle, so roll back on the pinned connection.
			pinned := conn.Statement.ConnPool.(*sql.Conn)
			ctx, cancel := context.WithTimeout(context.WithoutCancel(conn.Statement.Context), time.Second)
			defer cancel()
			if _, rollbackErr := pinned.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
				_ = pinned.Raw(func(any) error { return driver.ErrBadConn })
				return errors.Join(err, rollbackErr)
			}
		}
		retryable = true
		return err
	})
	return retryable, err
}

func withoutPendingTaskSubmitFailure(query *gorm.DB) *gorm.DB {
	pending := query.Session(&gorm.Session{NewDB: true}).Model(&TaskSubmitFailureRecovery{}).Select("1").
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: clause.Column{Table: query.NamingStrategy.TableName("Task"), Name: "id"}}).
		Where("status = ?", TaskSubmitFailureRecoveryPending)
	return query.Where("NOT EXISTS (?)", pending)
}

func withTaskSubmitFailureWriteBudget(db *gorm.DB, write func(*gorm.DB) error) error {
	ctx := db.Statement.Context
	if db.Dialector.Name() != "sqlite" {
		return write(db)
	}
	// Driver cancellation cannot interrupt SQLite's busy handler. Change
	// only this pinned connection, then restore its configured handler.
	return db.Connection(func(conn *gorm.DB) error {
		if ctx.Done() == nil {
			return write(conn)
		}
		pinned := conn.Statement.ConnPool.(*sql.Conn)
		var original int64
		if err := pinned.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&original); err != nil {
			return err
		}
		wait := min(original, int64(50))
		if deadline, ok := ctx.Deadline(); ok {
			wait = max(0, min(wait, time.Until(deadline).Milliseconds()))
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			if _, err := pinned.ExecContext(cleanupCtx, fmt.Sprintf("PRAGMA busy_timeout = %d", original)); err != nil {
				_ = pinned.Raw(func(any) error { return driver.ErrBadConn })
				common.SysError("failed to restore SQLite timeout after task submission recovery")
			}
		}()
		if _, err := pinned.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", wait)); err != nil {
			return err
		}
		return write(conn)
	})
}

func processPendingTaskSubmitFailures() {
	if DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db := DB.WithContext(ctx)
	if !db.Migrator().HasTable(&TaskSubmitFailureRecovery{}) {
		return
	}
	var records []TaskSubmitFailureRecovery
	if err := db.Where("status = ? AND next_attempt <= ?", TaskSubmitFailureRecoveryPending, time.Now().Unix()).
		Order("CASE WHEN next_attempt = 0 THEN created_at ELSE next_attempt END, task_id").Limit(25).Find(&records).Error; err != nil {
		common.SysError("failed to read task submit failure recovery journal")
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			break
		}
		var input *BillingSettlementInput
		if record.FundingDelta == 0 && record.FailedReserveRevision == 0 {
			if _, err := record.statusOnlyRequestID(); err != nil {
				updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryManual, err)
				continue
			}
		} else {
			original := record.refundInput()
			if _, err := taskSubmitFailureRecoveryRecord(record.TaskID, record.Reason, original, record.FailedReserveRevision); err != nil {
				updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryManual, err)
				continue
			}
			input = &original
		}
		claimCtx, cancelClaim := context.WithTimeout(ctx, 2*time.Second)
		claimDB := DB.WithContext(claimCtx)
		won, err := claimTaskSubmitFailureWithSettlementDB(claimDB, record.TaskID, record.Reason, input, &record, true)
		cancelClaim()
		if err != nil {
			status := TaskSubmitFailureRecoveryPending
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrTaskSubmitRefundIdentityMismatch) ||
				errors.Is(err, ErrBillingSettlementOperationConflict) || errors.Is(err, ErrTaskFinalizeOwned) {
				status = TaskSubmitFailureRecoveryManual
			}
			updateTaskSubmitFailureRecovery(record, status, err)
			continue
		}
		if !won {
			updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryManual, ErrTaskFinalizeOwned)
		}
	}
}

func updateTaskSubmitFailureRecovery(record TaskSubmitFailureRecovery, status string, cause error) {
	// A claim may use up its deadline while the journal is still writable.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	updated := false
	err := retryTaskSubmitFailureTransaction(DB.WithContext(ctx), func(conn *gorm.DB) error {
		result := writePendingTaskSubmitFailureDisposition(conn, record, status, cause)
		updated = result.RowsAffected == 1
		return result.Error
	})
	if err != nil {
		common.SysError("failed to update task submit failure recovery journal")
	} else if updated && status == TaskSubmitFailureRecoveryManual {
		common.SysError(fmt.Sprintf("task submit failure requires manual review: task_id=%d", record.TaskID))
	}
}

type taskSubmitFailureReviewItem struct {
	TaskID    string `json:"task_id"`
	LastError string `json:"last_error"`
	UpdatedAt int64  `json:"updated_at"`
}

func loadTaskSubmitFailureReview(tx *gorm.DB, limit int, data *BillingSettlementReconciliationData) error {
	if !tx.Migrator().HasTable(&TaskSubmitFailureRecovery{}) {
		return nil
	}
	query := tx.Model(&TaskSubmitFailureRecovery{}).Where("status = ?", TaskSubmitFailureRecoveryManual)
	if err := query.Count(&data.TaskSubmitFailureCount).Error; err != nil {
		return err
	}
	if data.TaskSubmitFailureCount == 0 {
		return nil
	}
	var rows []TaskSubmitFailureRecovery
	if err := query.Select("task_id", "last_error", "updated_at").Order("updated_at DESC, task_id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	data.TaskSubmitFailures = make([]taskSubmitFailureReviewItem, 0, len(rows))
	for _, row := range rows {
		data.TaskSubmitFailures = append(data.TaskSubmitFailures, taskSubmitFailureReview(row))
	}
	return nil
}

func taskSubmitFailureReview(row TaskSubmitFailureRecovery) taskSubmitFailureReviewItem {
	message := []rune(common.SanitizePersistedLogContent(common.MaskSensitiveInfo(row.LastError)))
	return taskSubmitFailureReviewItem{
		TaskID: strconv.FormatInt(row.TaskID, 10), LastError: string(message[:min(len(message), 512)]), UpdatedAt: row.UpdatedAt,
	}
}

// LookupTaskSubmitFailureReview retrieves one manual journal entry by its
// durable database ID. It never reads or changes a monetary intent.
func LookupTaskSubmitFailureReview(ctx context.Context, taskID int64) ([]taskSubmitFailureReviewItem, error) {
	var row TaskSubmitFailureRecovery
	err := DB.WithContext(ctx).Select("task_id", "last_error", "updated_at").
		Where("task_id = ? AND status = ?", taskID, TaskSubmitFailureRecoveryManual).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []taskSubmitFailureReviewItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []taskSubmitFailureReviewItem{taskSubmitFailureReview(row)}, nil
}

func writePendingTaskSubmitFailureDisposition(db *gorm.DB, record TaskSubmitFailureRecovery, status string, cause error) *gorm.DB {
	next, message := int64(0), ""
	if cause != nil {
		message = common.SanitizePersistedLogContent(cause.Error())
	}
	if status == TaskSubmitFailureRecoveryPending {
		next = time.Now().Add(time.Duration(1<<min(record.Attempts+1, 6)) * time.Second).Unix()
	}
	return db.Model(&TaskSubmitFailureRecovery{}).Where("task_id = ? AND status = ? AND attempts = ?", record.TaskID, TaskSubmitFailureRecoveryPending, record.Attempts).
		Updates(map[string]any{"status": status, "attempts": gorm.Expr("attempts + 1"), "last_error": message, "next_attempt": next, "updated_at": time.Now().Unix()})
}

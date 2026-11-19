// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TaskSubmitFailureRecoveryPending  = "pending"
	TaskSubmitFailureRecoveryRecorded = "intent_recorded"
	TaskSubmitFailureRecoveryManual   = "manual"
)

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

func taskSubmitFailureRecoveryRecord(taskID int64, reason string, input BillingSettlementInput) (TaskSubmitFailureRecovery, error) {
	input.TokenKey = "" // Existing settlement replay resolves cache identity by ID.
	if taskID <= 0 || input.FundingDelta >= 0 || input.TokenDelta > 0 ||
		!strings.HasPrefix(input.OperationKey, billingRequestOperationPrefix) || !strings.HasSuffix(input.OperationKey, billingRequestFinalizeSuffix) {
		return TaskSubmitFailureRecovery{}, ErrBillingSettlementOperationConflict
	}
	if err := validateBillingSettlementInput(input); err != nil {
		return TaskSubmitFailureRecovery{}, err
	}
	reasonRunes := []rune(common.SanitizePersistedLogContent(reason))
	record := TaskSubmitFailureRecovery{
		TaskID: taskID, OperationKey: input.OperationKey, Source: input.Source, UserID: input.UserID,
		SubscriptionID: input.SubscriptionID, TokenID: input.TokenID, FundingDelta: input.FundingDelta, TokenDelta: input.TokenDelta,
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

// A timeout can win before the first paid retry projects its reservation onto
// a free placeholder. Only the original applied debit identifies that amount.
func verifyUnprojectedTaskRetryRefund(tx *gorm.DB, requestID string, input BillingSettlementInput) error {
	var selection BillingPreConsumeSelection
	if err := tx.Where("request_id = ?", requestID).Take(&selection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTaskSubmitRefundIdentityMismatch
		}
		return err
	}
	if selection.UserID != input.UserID || selection.TokenID != input.TokenID ||
		selection.Source != input.Source || selection.EffectiveQuota <= 0 || selection.EffectiveQuota != -input.FundingDelta {
		return ErrTaskSubmitRefundIdentityMismatch
	}
	switch input.Source {
	case BillingSettlementSourceWallet:
		var debit BillingSettlement
		if err := tx.Where("operation_key = ?", billingRequestOperationPrefix+requestID+":pre-consume").Take(&debit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTaskSubmitRefundIdentityMismatch
			}
			return err
		}
		if debit.Status == BillingSettlementStatusApplied && debit.Source == input.Source &&
			debit.PreConsumeRequestID == requestID && debit.UserID == input.UserID && debit.TokenID == input.TokenID &&
			debit.AppliedFundingDelta == selection.EffectiveQuota && debit.AppliedTokenDelta == -input.TokenDelta &&
			input.SubscriptionID == 0 && input.SubscriptionPreConsumeRequestID == "" && !input.FinalizeSubscriptionPreConsume {
			return nil
		}
	case BillingSettlementSourceSubscription:
		var debit SubscriptionPreConsumeRecord
		if err := tx.Where("request_id = ?", requestID).Take(&debit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTaskSubmitRefundIdentityMismatch
			}
			return err
		}
		if debit.Status == "consumed" && debit.UserId == input.UserID && debit.TokenId == input.TokenID &&
			debit.UserSubscriptionId == input.SubscriptionID && debit.PreConsumed == selection.EffectiveQuota &&
			input.SubscriptionPreConsumeRequestID == requestID && input.FinalizeSubscriptionPreConsume &&
			input.TokenID > 0 && input.TokenDelta == input.FundingDelta {
			return nil
		}
	}
	return ErrTaskSubmitRefundIdentityMismatch
}

// RecordTaskSubmitFailureRecovery is a trusted-host seam. It records a definite
// rejection for later ownership checks; it does not authorize or apply money.
func RecordTaskSubmitFailureRecovery(taskID int64, reason string, input BillingSettlementInput) error {
	if DB == nil {
		return errors.New("database is not initialized")
	}
	record, err := taskSubmitFailureRecoveryRecord(taskID, reason, input)
	if err != nil {
		return err
	}
	if common.UsingSQLite {
		billingSettlementSQLiteWriteMu.Lock()
		defer billingSettlementSQLiteWriteMu.Unlock()
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// Persist the rejection even if the task table cannot currently be
		// updated. Replay independently verifies task and funding ownership.
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}}, DoNothing: true}).Create(&record).Error; err != nil {
			return err
		}
		var existing TaskSubmitFailureRecovery
		if err := withRowLock(tx).First(&existing, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if existing.refundInput() != record.refundInput() {
			return ErrBillingSettlementOperationConflict
		}
		return nil
	})
}

func withoutPendingTaskSubmitFailure(query *gorm.DB) *gorm.DB {
	pending := query.Session(&gorm.Session{NewDB: true}).Model(&TaskSubmitFailureRecovery{}).Select("1").
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: clause.Column{Table: query.NamingStrategy.TableName("Task"), Name: "id"}}).
		Where("status = ?", TaskSubmitFailureRecoveryPending)
	return query.Where("NOT EXISTS (?)", pending)
}

func withTaskSubmitFailureWriteBudget(db *gorm.DB, write func(*gorm.DB) error) error {
	ctx := db.Statement.Context
	if db.Dialector.Name() != "sqlite" || ctx.Done() == nil {
		return write(db)
	}
	// Driver cancellation cannot interrupt SQLite's busy handler. Change
	// only this pinned connection, then restore its configured handler.
	return db.Connection(func(conn *gorm.DB) error {
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
			if _, err := pinned.ExecContext(context.Background(), fmt.Sprintf("PRAGMA busy_timeout = %d", original)); err != nil {
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
	if DB == nil || !DB.Migrator().HasTable(&TaskSubmitFailureRecovery{}) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db := DB.WithContext(ctx)
	var records []TaskSubmitFailureRecovery
	if err := db.Where("status = ? AND next_attempt <= ?", TaskSubmitFailureRecoveryPending, time.Now().Unix()).Order("next_attempt, task_id").Limit(25).Find(&records).Error; err != nil {
		common.SysError("failed to read task submit failure recovery journal")
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			break
		}
		input := record.refundInput()
		if _, err := taskSubmitFailureRecoveryRecord(record.TaskID, record.Reason, input); err != nil {
			updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryManual, err)
			continue
		}
		claimCtx, cancelClaim := context.WithTimeout(ctx, 2*time.Second)
		claimDB := DB.WithContext(claimCtx)
		won, err := claimTaskSubmitFailureWithSettlementDB(claimDB, record.TaskID, record.Reason, &input)
		cancelClaim()
		if err != nil {
			status := TaskSubmitFailureRecoveryPending
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrTaskSubmitRefundIdentityMismatch) || errors.Is(err, ErrBillingSettlementOperationConflict) {
				status = TaskSubmitFailureRecoveryManual
			}
			updateTaskSubmitFailureRecovery(record, status, err)
			continue
		}
		if !won {
			var settlement BillingSettlement
			err := db.Where("operation_key = ?", input.OperationKey).First(&settlement).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryPending, err)
				continue
			}
			if err != nil || validateBillingSettlement(settlement, input) != nil {
				updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryManual, ErrTaskFinalizeOwned)
				continue
			}
		}
		updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryRecorded, nil)
	}
}

func updateTaskSubmitFailureRecovery(record TaskSubmitFailureRecovery, status string, cause error) {
	// A claim may use up its deadline while the journal is still writable.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	next, message := int64(0), ""
	if cause != nil {
		message = common.SanitizePersistedLogContent(cause.Error())
	}
	if status == TaskSubmitFailureRecoveryPending {
		next = time.Now().Add(time.Duration(1<<min(record.Attempts+1, 6)) * time.Second).Unix()
	}
	err := withTaskSubmitFailureWriteBudget(DB.WithContext(ctx), func(conn *gorm.DB) error {
		return conn.Model(&TaskSubmitFailureRecovery{}).Where("task_id = ? AND status = ?", record.TaskID, TaskSubmitFailureRecoveryPending).
			Updates(map[string]any{"status": status, "attempts": gorm.Expr("attempts + 1"), "last_error": message, "next_attempt": next, "updated_at": time.Now().Unix()}).Error
	})
	if err != nil {
		common.SysError("failed to update task submit failure recovery journal")
	}
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"errors"
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
		now := time.Now().Unix()
		// Task first, then journal: match timeout/retry lock order and invalidate
		// a scan that was read before this recovery fact was committed.
		version := gorm.Expr("CASE WHEN updated_at IS NULL OR updated_at < ? THEN ? WHEN updated_at < ? THEN updated_at + 1 ELSE updated_at END", now, now, int64(1<<63-1))
		if err := tx.Model(&Task{}).Where("id = ? AND status NOT IN ?", taskID, []string{TaskStatusFailure, TaskStatusSuccess}).UpdateColumn("updated_at", version).Error; err != nil {
			return err
		}
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
			updateTaskSubmitFailureRecovery(db, record, TaskSubmitFailureRecoveryManual, err)
			continue
		}
		won, err := claimTaskSubmitFailureWithSettlementDB(db, record.TaskID, record.Reason, &input)
		if err != nil {
			status := TaskSubmitFailureRecoveryPending
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrTaskSubmitRefundIdentityMismatch) || errors.Is(err, ErrBillingSettlementOperationConflict) {
				status = TaskSubmitFailureRecoveryManual
			}
			updateTaskSubmitFailureRecovery(db, record, status, err)
			continue
		}
		if !won {
			var settlement BillingSettlement
			err := db.Where("operation_key = ?", input.OperationKey).First(&settlement).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				updateTaskSubmitFailureRecovery(db, record, TaskSubmitFailureRecoveryPending, err)
				continue
			}
			if err != nil || validateBillingSettlement(settlement, input) != nil {
				updateTaskSubmitFailureRecovery(db, record, TaskSubmitFailureRecoveryManual, ErrTaskFinalizeOwned)
				continue
			}
		}
		updateTaskSubmitFailureRecovery(db, record, TaskSubmitFailureRecoveryRecorded, nil)
	}
}

func updateTaskSubmitFailureRecovery(db *gorm.DB, record TaskSubmitFailureRecovery, status string, cause error) {
	next, message := int64(0), ""
	if cause != nil {
		message = common.SanitizePersistedLogContent(cause.Error())
	}
	if status == TaskSubmitFailureRecoveryPending {
		next = time.Now().Add(time.Duration(1<<min(record.Attempts+1, 6)) * time.Second).Unix()
	}
	if err := db.Model(&TaskSubmitFailureRecovery{}).Where("task_id = ? AND status = ?", record.TaskID, TaskSubmitFailureRecoveryPending).
		Updates(map[string]any{"status": status, "attempts": gorm.Expr("attempts + 1"), "last_error": message, "next_attempt": next, "updated_at": time.Now().Unix()}).Error; err != nil {
		common.SysError("failed to update task submit failure recovery journal")
	}
}

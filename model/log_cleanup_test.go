package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeleteOldLogBatchDeletesOnlyLimitedRows(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 1, CreatedAt: 10, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 20, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 30, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 1000, Type: LogTypeConsume},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	rows, err := DeleteOldLogBatch(context.Background(), 100, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 2, rows)

	remainingOld, err := CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, remainingOld)

	var total int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&total).Error)
	assert.EqualValues(t, 2, total)

	rows, err = DeleteOldLogBatch(context.Background(), 100, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)

	remainingOld, err = CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.EqualValues(t, 0, remainingOld)
}

func TestDeleteOldLogBatchPreservesManagementAuditLogs(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 1, CreatedAt: 10, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 20, Type: LogTypeManage},
		{UserId: 1, CreatedAt: 1000, Type: LogTypeConsume},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	ctx := context.Background()
	remainingOld, err := CountOldLog(ctx, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, remainingOld)

	rows, err := DeleteOldLogBatch(ctx, 100, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)

	remainingOld, err = CountOldLog(ctx, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 0, remainingOld)

	var manageCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("type = ?", LogTypeManage).Count(&manageCount).Error)
	assert.EqualValues(t, 1, manageCount)
}

func TestDeleteOldLogBatchDeletesLegacyNullTypeLogs(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Exec("INSERT INTO logs (user_id, created_at, type) VALUES (?, ?, NULL)", 1, 10).Error)
	remainingOld, err := CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, remainingOld)

	rows, err := DeleteOldLogBatch(context.Background(), 100, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)
}

func TestDeleteOldLogBatchRechecksManagementTypeBeforeDelete(t *testing.T) {
	truncateTables(t)

	logEntry := Log{UserId: 1, CreatedAt: 10, Type: LogTypeConsume}
	require.NoError(t, LOG_DB.Create(&logEntry).Error)
	callbackName := "test:promote_log_to_manage_before_cleanup_delete"
	mutated := false
	require.NoError(t, LOG_DB.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if mutated || tx.Statement == nil || tx.Statement.Table != "logs" {
			return
		}
		mutated = true
		require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Model(&Log{}).Where("id = ?", logEntry.Id).Update("type", LogTypeManage).Error)
	}))
	t.Cleanup(func() { _ = LOG_DB.Callback().Delete().Remove(callbackName) })

	rows, err := DeleteOldLogBatch(context.Background(), 100, 10)
	require.NoError(t, err)
	assert.Zero(t, rows)

	var stored Log
	require.NoError(t, LOG_DB.First(&stored, logEntry.Id).Error)
	assert.Equal(t, LogTypeManage, stored.Type)
}

func TestDeleteOldLogBatchStopsWhenContextCancelledAfterSelect(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 1, CreatedAt: 10, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 20, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 30, Type: LogTypeConsume},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	ctx, cancel := context.WithCancel(context.Background())
	queryCallback := "test:cancel_log_cleanup_after_select"
	deleteCallback := "test:detect_log_cleanup_delete_after_cancel"
	var deleteAttemptedAfterCancel bool

	require.NoError(t, LOG_DB.Callback().Query().After("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "logs" {
			cancel()
		}
	}))
	require.NoError(t, LOG_DB.Callback().Delete().Before("gorm:delete").Register(deleteCallback, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "logs" && tx.Statement.Context != nil && tx.Statement.Context.Err() != nil {
			deleteAttemptedAfterCancel = true
		}
	}))
	t.Cleanup(func() {
		_ = LOG_DB.Callback().Query().Remove(queryCallback)
		_ = LOG_DB.Callback().Delete().Remove(deleteCallback)
	})

	rows, err := DeleteOldLogBatch(ctx, 100, 2)
	require.ErrorIs(t, err, context.Canceled)
	assert.EqualValues(t, 0, rows)
	assert.False(t, deleteAttemptedAfterCancel)

	remainingOld, err := CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.EqualValues(t, 3, remainingOld)
}

func TestDeleteOldLogDeletesAllMatchingRows(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 1, CreatedAt: 10, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 20, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 30, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 1000, Type: LogTypeConsume},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	rows, err := DeleteOldLog(context.Background(), 100, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 3, rows)

	remainingOld, err := CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.EqualValues(t, 0, remainingOld)

	var total int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&total).Error)
	assert.EqualValues(t, 1, total)
}

func TestDeleteOldLogBatchDeletesOldBillingLogReceipts(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 1, CreatedAt: 10, Type: LogTypeConsume},
		{UserId: 1, CreatedAt: 1000, Type: LogTypeConsume},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	receipts := []BillingLogReceipt{
		{OperationKey: "old-receipt", ClaimToken: "old", CreatedAt: 10},
		{OperationKey: "new-receipt", ClaimToken: "new", CreatedAt: 1000},
	}
	require.NoError(t, LOG_DB.Create(&receipts).Error)

	rows, err := DeleteOldLogBatch(context.Background(), 100, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)

	var oldReceiptCount int64
	require.NoError(t, LOG_DB.Model(&BillingLogReceipt{}).Where("operation_key = ?", "old-receipt").Count(&oldReceiptCount).Error)
	assert.Zero(t, oldReceiptCount)
	var newReceiptCount int64
	require.NoError(t, LOG_DB.Model(&BillingLogReceipt{}).Where("operation_key = ?", "new-receipt").Count(&newReceiptCount).Error)
	assert.EqualValues(t, 1, newReceiptCount)
}

func TestDeleteOldLogBatchDeletesOldBillingLogReceiptsWithoutOldLogs(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create(&BillingLogReceipt{OperationKey: "old-receipt-only", ClaimToken: "old", CreatedAt: 10}).Error)

	rows, err := DeleteOldLogBatch(context.Background(), 100, 10)
	require.NoError(t, err)
	assert.Zero(t, rows)

	var count int64
	require.NoError(t, LOG_DB.Model(&BillingLogReceipt{}).Where("operation_key = ?", "old-receipt-only").Count(&count).Error)
	assert.Zero(t, count)
}

func TestDeleteOldLogDeletesAllOldBillingLogReceipts(t *testing.T) {
	truncateTables(t)

	receipts := []BillingLogReceipt{
		{OperationKey: "old-receipt-1", ClaimToken: "old-1", CreatedAt: 10},
		{OperationKey: "old-receipt-2", ClaimToken: "old-2", CreatedAt: 11},
		{OperationKey: "old-receipt-3", ClaimToken: "old-3", CreatedAt: 12},
		{OperationKey: "new-receipt", ClaimToken: "new", CreatedAt: 1000},
	}
	require.NoError(t, LOG_DB.Create(&receipts).Error)

	rows, err := DeleteOldLog(context.Background(), 100, 2)
	require.NoError(t, err)
	assert.Zero(t, rows)

	var oldReceiptCount int64
	require.NoError(t, LOG_DB.Model(&BillingLogReceipt{}).Where("created_at < ?", 100).Count(&oldReceiptCount).Error)
	assert.Zero(t, oldReceiptCount)
	var newReceiptCount int64
	require.NoError(t, LOG_DB.Model(&BillingLogReceipt{}).Where("operation_key = ?", "new-receipt").Count(&newReceiptCount).Error)
	assert.EqualValues(t, 1, newReceiptCount)
}

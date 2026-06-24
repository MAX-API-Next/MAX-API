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

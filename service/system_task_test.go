package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLogCleanupTaskRenewsLockBetweenBillingReceiptBatches(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SystemTask{}))
	require.NoError(t, model.DB.Exec("DELETE FROM system_tasks").Error)

	task, err := model.CreateSystemTask(
		model.SystemTaskTypeLogCleanup,
		model.SystemTaskTypeLogCleanup,
		LogCleanupPayload{TargetTimestamp: 100, BatchSize: 1},
		LogCleanupState{},
	)
	require.NoError(t, err)
	const runnerID = "receipt-cleanup-runner"
	claimedTask, claimed, err := model.ClaimSystemTask(task.ID, model.SystemTaskTypeLogCleanup, runnerID, systemTaskLockUntil())
	require.NoError(t, err)
	require.True(t, claimed)

	var renewals atomic.Int64
	const callbackName = "test:system_task_state_renewal_count"
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "system_tasks" || tx.RowsAffected <= 0 {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		if _, hasState := updates["state"]; !hasState {
			return
		}
		if _, hasLockUntil := updates["locked_until"]; !hasLockUntil {
			return
		}
		renewals.Add(tx.RowsAffected)
	}))
	t.Cleanup(func() {
		_ = model.DB.Callback().Update().Remove(callbackName)
		_ = model.DB.Exec("DELETE FROM system_tasks").Error
	})

	require.NoError(t, model.DB.Create(&[]model.BillingLogReceipt{
		{OperationKey: "old-receipt-1", ClaimToken: "1", CreatedAt: 10},
		{OperationKey: "old-receipt-2", ClaimToken: "2", CreatedAt: 20},
		{OperationKey: "old-receipt-3", ClaimToken: "3", CreatedAt: 30},
	}).Error)

	runLogCleanupTask(context.Background(), claimedTask, runnerID)

	var remaining int64
	require.NoError(t, model.DB.Model(&model.BillingLogReceipt{}).Where("created_at < ?", 100).Count(&remaining).Error)
	assert.Zero(t, remaining)

	assert.GreaterOrEqual(t, renewals.Load(), int64(4))

	var reloaded model.SystemTask
	require.NoError(t, model.DB.First(&reloaded, claimedTask.ID).Error)
	assert.Equal(t, model.SystemTaskStatusSucceeded, reloaded.Status)
}

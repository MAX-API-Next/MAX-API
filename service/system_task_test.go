package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	oldUpdateSystemTaskState := updateSystemTaskState
	updateSystemTaskState = func(taskID string, lockedBy string, state any, lockUntil int64) error {
		err := oldUpdateSystemTaskState(taskID, lockedBy, state, lockUntil)
		if err == nil {
			renewals.Add(1)
		}
		return err
	}
	t.Cleanup(func() {
		updateSystemTaskState = oldUpdateSystemTaskState
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

	assert.EqualValues(t, 5, renewals.Load())

	var reloaded model.SystemTask
	require.NoError(t, model.DB.First(&reloaded, claimedTask.ID).Error)
	assert.Equal(t, model.SystemTaskStatusSucceeded, reloaded.Status)
}

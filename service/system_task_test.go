package service

import (
	"context"
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

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS system_task_update_audit_trigger").Error)
	require.NoError(t, model.DB.Exec("DROP TABLE IF EXISTS system_task_update_audit").Error)
	require.NoError(t, model.DB.Exec("CREATE TABLE system_task_update_audit (id INTEGER PRIMARY KEY AUTOINCREMENT)").Error)
	require.NoError(t, model.DB.Exec(`
CREATE TRIGGER system_task_update_audit_trigger
AFTER UPDATE ON system_tasks
WHEN NEW.status = 'running'
BEGIN
	INSERT INTO system_task_update_audit (id) VALUES (NULL);
END`).Error)
	t.Cleanup(func() {
		_ = model.DB.Exec("DROP TRIGGER IF EXISTS system_task_update_audit_trigger").Error
		_ = model.DB.Exec("DROP TABLE IF EXISTS system_task_update_audit").Error
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

	var renewals int64
	require.NoError(t, model.DB.Table("system_task_update_audit").Count(&renewals).Error)
	assert.GreaterOrEqual(t, renewals, int64(4))

	var reloaded model.SystemTask
	require.NoError(t, model.DB.First(&reloaded, claimedTask.ID).Error)
	assert.Equal(t, model.SystemTaskStatusSucceeded, reloaded.Status)
}

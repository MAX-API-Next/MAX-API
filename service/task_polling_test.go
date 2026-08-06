package service

import (
	"context"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateVideoTasksLeavesChargedTasksPendingWhenChannelCacheFails(t *testing.T) {
	truncate(t)

	task := makeTask(7101, 999999, 123, 0, BillingSourceWallet, 0)
	task.TaskID = "video-cache-failure-upstream"
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)

	err := updateVideoTasks(context.Background(), constant.TaskPlatform("video"), 999999, []string{task.TaskID}, map[string]*model.Task{
		task.TaskID: task,
	})
	require.Error(t, err)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, reloaded.Status)
	assert.Equal(t, 123, reloaded.Quota)
	assert.Empty(t, reloaded.FailReason)
}

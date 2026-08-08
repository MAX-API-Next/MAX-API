package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sunoPollingResponseAdaptor struct {
	responseBody string
}

func (a *sunoPollingResponseAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *sunoPollingResponseAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(a.responseBody)),
	}, nil
}

func (a *sunoPollingResponseAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *sunoPollingResponseAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

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

func TestUpdateSunoTasksReturnsErrorForUnsuccessfulResponse(t *testing.T) {
	truncate(t)
	seedChannel(t, 7201)
	baseURL := "https://suno.example.com"
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 7201).Update("base_url", baseURL).Error)

	originalGetTaskAdaptorFunc := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		require.Equal(t, constant.TaskPlatformSuno, platform)
		return &sunoPollingResponseAdaptor{
			responseBody: `{"code":"failure","message":"poll https://api.suno.example.com/v1/tasks?api_key=url-secret api_key:header-secret","data":[]}`,
		}
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = originalGetTaskAdaptorFunc
	})

	err := updateSunoTasks(context.Background(), 7201, []string{"suno-task"}, map[string]*model.Task{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "https://***.com/")
	require.Contains(t, err.Error(), "?api_key=***")
	require.Contains(t, err.Error(), "api_key:***")
	require.NotContains(t, err.Error(), "suno.example.com")
	require.NotContains(t, err.Error(), "url-secret")
	require.NotContains(t, err.Error(), "header-secret")
}

func TestTaskNeedsUpdateComparesPersistedFailReasonNormalization(t *testing.T) {
	t.Run("sanitized equivalent", func(t *testing.T) {
		rawFailReason := "upstream\r\nfailed\x00" + strings.Repeat("界", common.PersistedLogContentLimit+1)
		oldTask := &model.Task{
			Status:     model.TaskStatusFailure,
			Progress:   "100%",
			FailReason: common.SanitizePersistedLogContent(rawFailReason),
		}

		require.False(t, taskNeedsUpdate(oldTask, dto.SunoDataResponse{
			Status:     string(model.TaskStatusFailure),
			FailReason: rawFailReason,
		}))
	})

	t.Run("empty incoming reason keeps persisted value", func(t *testing.T) {
		oldTask := &model.Task{
			Status:     model.TaskStatusInProgress,
			FailReason: "persisted failure reason",
		}

		require.False(t, taskNeedsUpdate(oldTask, dto.SunoDataResponse{
			Status: string(model.TaskStatusInProgress),
		}))
	})
}

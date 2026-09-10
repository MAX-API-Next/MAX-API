package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

type usagePollingResponseAdaptor struct {
	usage    *types.TaskUsage
	usageErr error
}

type configuredWrapperPollingAdaptor struct{}

func (a *configuredWrapperPollingAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *configuredWrapperPollingAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"code":"success",
			"data":{
				"id":10,
				"status":"IN_PROGRESS",
				"data":{
					"id":"439499419230570",
					"object":"video.generation",
					"status":"completed",
					"data":[{"url":"https://cdn.example.com/polled.mp4"}]
				}
			}
		}`)),
	}, nil
}

func (a *configuredWrapperPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, errors.New("configured parser should handle wrapper response")
}

func (a *configuredWrapperPollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func (a *usagePollingResponseAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *usagePollingResponseAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}

func (a *usagePollingResponseAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess)}, nil
}

func (a *usagePollingResponseAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func (a *usagePollingResponseAdaptor) ExtractTaskUsage([]byte) (*types.TaskUsage, error) {
	return a.usage, a.usageErr
}

func TestApplyTaskUsageFactsAttachesProviderEvidence(t *testing.T) {
	taskResult := &relaycommon.TaskInfo{}
	usage := &types.TaskUsage{
		Source:       types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessComplete,
	}

	require.NoError(t, applyTaskUsageFacts(&usagePollingResponseAdaptor{usage: usage}, []byte(`{}`), taskResult))
	assert.Same(t, usage, taskResult.Usage)
}

func TestApplyTaskUsageFactsFailsClosedOnProviderError(t *testing.T) {
	taskResult := &relaycommon.TaskInfo{}
	providerErr := errors.New("ambiguous usage response")

	err := applyTaskUsageFacts(&usagePollingResponseAdaptor{usageErr: providerErr}, []byte(`{}`), taskResult)

	require.ErrorIs(t, err, providerErr)
	assert.Nil(t, taskResult.Usage)
}

func TestUpdateVideoSingleTaskUsesConfiguredParserForWrappedProviderResult(t *testing.T) {
	truncate(t)
	baseURL := "https://upstream.example.com"
	task := &model.Task{
		TaskID:      "task_wrapped_polling",
		Status:      model.TaskStatusInProgress,
		Progress:    "50%",
		SubmitTime:  time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
		PrivateData: model.TaskPrivateData{},
	}
	require.NoError(t, model.DB.Create(task).Error)
	channel := &model.Channel{
		Id:            8101,
		Key:           "sk-test",
		BaseURL:       &baseURL,
		OtherSettings: `{"task_protocol":"generic_video_task","task_protocol_config":{"task_id_path":"data.id","status_path":"data.status","result_url_paths":["data.data.0.url"]}}`,
	}
	task.ChannelId = channel.Id
	require.NoError(t, model.DB.Create(channel).Error)

	err := updateVideoSingleTask(context.Background(), &configuredWrapperPollingAdaptor{}, channel, task.TaskID, map[string]*model.Task{task.TaskID: task})

	require.NoError(t, err)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "https://cdn.example.com/polled.mp4", reloaded.PrivateData.ResultURL)
}

func TestAppliedTaskRecoveryPreservesSnapshotOnCASLoss(t *testing.T) {
	truncate(t)
	task := &model.Task{
		TaskID:   "task_recovery_cas_loss",
		Status:   model.TaskStatusInProgress,
		Progress: "50%",
		PrivateData: model.TaskPrivateData{
			PendingTerminalStatus:    model.TaskStatusSuccess,
			PendingTerminalProgress:  "100%",
			PendingTerminalResultURL: "https://cdn.example.com/cas-loss.mp4",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusSuccess).Error)

	outcome, err := recoverAppliedTaskTerminalEvidence(task)
	require.NoError(t, err)
	assert.Equal(t, appliedTaskRecoveryCASLost, outcome)
	assert.EqualValues(t, model.TaskStatusInProgress, task.Status)
	assert.EqualValues(t, model.TaskStatusSuccess, task.PrivateData.PendingTerminalStatus)
	assert.Equal(t, "https://cdn.example.com/cas-loss.mp4", task.PrivateData.PendingTerminalResultURL)

	task = &model.Task{
		TaskID: "task_missing_evidence_cas_loss",
		Status: model.TaskStatusInProgress,
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusSuccess).Error)

	outcome, err = markAppliedTaskEvidenceMissing(task, time.Now().Unix())
	require.NoError(t, err)
	assert.Equal(t, appliedTaskRecoveryCASLost, outcome)
	assert.EqualValues(t, model.TaskStatusInProgress, task.Status)
}

func TestAppliedTaskRecoveryPropagatesUpdateError(t *testing.T) {
	truncate(t)
	task := &model.Task{
		TaskID: "task_recovery_update_error",
		Status: model.TaskStatusInProgress,
		PrivateData: model.TaskPrivateData{
			PendingTerminalStatus: model.TaskStatusSuccess,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	callbackName := "test:task-recovery-update-error"
	forcedErr := errors.New("forced task recovery update error")
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		_ = tx.AddError(forcedErr)
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })

	outcome, err := recoverAppliedTaskTerminalEvidence(task)
	assert.ErrorIs(t, err, forcedErr)
	assert.Equal(t, appliedTaskRecoveryNoEvidence, outcome)
	assert.EqualValues(t, model.TaskStatusInProgress, task.Status)
	assert.EqualValues(t, model.TaskStatusSuccess, task.PrivateData.PendingTerminalStatus)

	missingEvidenceTask := &model.Task{
		TaskID: "task_missing_evidence_update_error",
		Status: model.TaskStatusInProgress,
	}
	require.NoError(t, model.DB.Create(missingEvidenceTask).Error)
	outcome, err = markAppliedTaskEvidenceMissing(missingEvidenceTask, time.Now().Unix())
	assert.ErrorIs(t, err, forcedErr)
	assert.Equal(t, appliedTaskRecoveryNoEvidence, outcome)
	assert.EqualValues(t, model.TaskStatusInProgress, missingEvidenceTask.Status)
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

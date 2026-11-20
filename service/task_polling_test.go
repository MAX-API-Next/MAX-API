package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
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

type usageFactPollingResponseAdaptor struct {
	contract     types.TaskUsageContract
	envelope     *types.TaskUsageEnvelope
	err          error
	calls        int
	producerKind string
}

type configuredWrapperPollingAdaptor struct {
	envelope                 *types.TaskUsageEnvelope
	adjustedPromptTokens     int
	adjustedCompletionTokens int
	adjustedTotalTokens      int
}

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
					"data":[{"url":"https://cdn.example.com/polled.mp4"}],
					"usage":{"prompt_tokens":8,"completion_tokens":12,"total_tokens":20}
				}
			}
		}`)),
	}, nil
}

func (a *configuredWrapperPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, errors.New("configured parser should handle wrapper response")
}

func (a *configuredWrapperPollingAdaptor) AdjustBillingOnComplete(_ *model.Task, taskResult *relaycommon.TaskInfo) int {
	a.adjustedPromptTokens = taskResult.PromptTokens
	a.adjustedCompletionTokens = taskResult.CompletionTokens
	a.adjustedTotalTokens = taskResult.TotalTokens
	return 0
}

func (a *configuredWrapperPollingAdaptor) UsageContract() types.TaskUsageContract {
	return taskusage.DoubaoVideoContract()
}

func (a *configuredWrapperPollingAdaptor) UsageProducerKind() string {
	return types.TaskUsageProducerKindGoAdapter
}

func (a *configuredWrapperPollingAdaptor) ProduceUsage(types.TaskUsageContext) (*types.TaskUsageEnvelope, error) {
	return a.envelope, nil
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

func (a *usageFactPollingResponseAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *usageFactPollingResponseAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}

func (a *usageFactPollingResponseAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess)}, nil
}

func (a *usageFactPollingResponseAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func (a *usageFactPollingResponseAdaptor) UsageContract() types.TaskUsageContract {
	return a.contract
}

func (a *usageFactPollingResponseAdaptor) UsageProducerKind() string {
	if a.producerKind == "" {
		return types.TaskUsageProducerKindGoAdapter
	}
	return a.producerKind
}

func (a *usageFactPollingResponseAdaptor) ProduceUsage(types.TaskUsageContext) (*types.TaskUsageEnvelope, error) {
	a.calls++
	return a.envelope, a.err
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

func TestApplyTaskUsageFactsPrefersValidatedEnvelopeAndProducesOnce(t *testing.T) {
	zero := int64(0)
	contract := taskusage.DoubaoVideoContract()
	envelope, err := taskusage.BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceProviderResponse, &types.TaskUsage{
		CompletionTokens: &zero, TotalTokens: &zero,
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	adaptor := &usageFactPollingResponseAdaptor{contract: contract, envelope: envelope}
	taskResult := &relaycommon.TaskInfo{}

	require.NoError(t, applyTaskUsageFacts(adaptor, []byte(`{"usage":{}}`), taskResult))
	require.Equal(t, 1, adaptor.calls)
	require.NotNil(t, taskResult.UsageEnvelope)
	require.Equal(t, types.TaskUsagePresencePresentZero, taskResult.UsageEnvelope.Presence)
	require.NotNil(t, taskResult.Usage)

	require.NoError(t, applyTaskUsageFacts(adaptor, []byte(`{"usage":{}}`), taskResult))
	require.Equal(t, 1, adaptor.calls, "an already validated envelope must not be produced twice")
}

func TestApplyTaskUsageFactsRejectsTamperedEnvelope(t *testing.T) {
	zero := int64(0)
	contract := taskusage.DoubaoVideoContract()
	envelope, err := taskusage.BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceProviderResponse, &types.TaskUsage{
		CompletionTokens: &zero, TotalTokens: &zero,
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	envelope.EvidenceDigest = "tampered"
	adaptor := &usageFactPollingResponseAdaptor{contract: contract, envelope: envelope}
	taskResult := &relaycommon.TaskInfo{}

	err = applyTaskUsageFacts(adaptor, []byte(`{}`), taskResult)
	require.Error(t, err)
	require.Nil(t, taskResult.Usage)
	require.Nil(t, taskResult.UsageEnvelope)
}

func TestApplyTaskUsageFactsRejectsNonProviderResponseEnvelope(t *testing.T) {
	zero := int64(0)
	contract := taskusage.DoubaoVideoContract()
	envelope, err := taskusage.BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceRequestEstimate, &types.TaskUsage{
		CompletionTokens: &zero, TotalTokens: &zero,
		Source: types.TaskUsageSourceRequestEstimate, Completeness: types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	adaptor := &usageFactPollingResponseAdaptor{contract: contract, envelope: envelope}
	taskResult := &relaycommon.TaskInfo{}

	err = applyTaskUsageFacts(adaptor, []byte(`{}`), taskResult)
	require.EqualError(t, err, `task usage envelope stage "request_estimate" is not supported for polling`)
	require.Nil(t, taskResult.Usage)
	require.Nil(t, taskResult.UsageEnvelope)
}

func TestApplyTaskUsageFactsDoesNotCopyPartialDoubaoUsageToLegacyFields(t *testing.T) {
	completion := int64(12)
	contract := taskusage.DoubaoVideoContract()
	envelope, err := taskusage.BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceProviderResponse, &types.TaskUsage{
		CompletionTokens: &completion,
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessPartial,
	})
	require.NoError(t, err)
	taskResult := &relaycommon.TaskInfo{}
	adaptor := &usageFactPollingResponseAdaptor{contract: contract, envelope: envelope}

	require.NoError(t, applyTaskUsageFacts(adaptor, []byte(`{}`), taskResult))
	assert.Zero(t, taskResult.CompletionTokens)
	assert.Zero(t, taskResult.TotalTokens)
	require.NotNil(t, taskResult.UsageEnvelope)
	assert.Equal(t, types.TaskUsageCompletenessPartial, taskResult.UsageEnvelope.Completeness)
}

func TestApplyTaskUsageFactsAcceptsTrustedTaskPluginEnvelope(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "..", "pkg", "taskusage", "testdata", "task-plugin-minimax-h3-v1.json"))
	require.NoError(t, err)
	var fixture struct {
		ProducerKind  string           `json:"producer_kind"`
		SourceID      string           `json:"source_id"`
		SchemaVersion int              `json:"schema_version"`
		Stage         string           `json:"stage"`
		Usage         *types.TaskUsage `json:"usage"`
	}
	require.NoError(t, common.Unmarshal(data, &fixture))
	require.Equal(t, types.TaskUsageProducerKindTaskPlugin, fixture.ProducerKind)
	contract := taskusage.MiniMaxH3Contract()
	require.Equal(t, contract.SourceID, fixture.SourceID)
	require.Equal(t, contract.SchemaVersion, fixture.SchemaVersion)
	envelope, err := taskusage.BuildEnvelope(fixture.ProducerKind, contract, fixture.Stage, fixture.Usage)
	require.NoError(t, err)
	taskResult := &relaycommon.TaskInfo{}
	adaptor := &usageFactPollingResponseAdaptor{
		contract: contract, envelope: envelope, producerKind: fixture.ProducerKind,
	}

	require.NoError(t, applyTaskUsageFacts(adaptor, []byte(`{}`), taskResult))
	require.NotNil(t, taskResult.UsageEnvelope)
	require.Equal(t, fixture.ProducerKind, taskResult.UsageEnvelope.ProducerKind)
	require.EqualValues(t, 5_000, *taskResult.Usage.OutputDurationMs)
}

func TestUpdateVideoSingleTaskUsesConfiguredParserForWrappedProviderResult(t *testing.T) {
	truncate(t)
	baseURL := "https://upstream.example.com"
	completionTokens := int64(12)
	totalTokens := int64(20)
	envelope, err := taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.DoubaoVideoContract(),
		types.TaskUsageSourceProviderResponse,
		&types.TaskUsage{
			CompletionTokens: &completionTokens,
			TotalTokens:      &totalTokens,
			Source:           types.TaskUsageSourceProviderResponse,
			Completeness:     types.TaskUsageCompletenessComplete,
		},
	)
	require.NoError(t, err)
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

	adaptor := &configuredWrapperPollingAdaptor{envelope: envelope}
	err = updateVideoSingleTask(context.Background(), adaptor, channel, task.TaskID, map[string]*model.Task{task.TaskID: task})

	require.NoError(t, err)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "https://cdn.example.com/polled.mp4", reloaded.PrivateData.ResultURL)
	assert.Equal(t, 8, adaptor.adjustedPromptTokens)
	assert.Equal(t, 12, adaptor.adjustedCompletionTokens)
	assert.Equal(t, 20, adaptor.adjustedTotalTokens)
}

func TestVideoTerminalPollRejectsNewerSameStatusProgress(t *testing.T) {
	for _, withRefund := range []bool{false, true} {
		name := "status-only"
		if withRefund {
			name = "with-refund"
		}
		t.Run(name, func(t *testing.T) {
			truncate(t)
			row := &model.Task{TaskID: name, UserId: 71, Status: model.TaskStatusInProgress, Progress: "10%",
				PrivateData: model.TaskPrivateData{BillingSource: model.BillingSettlementSourceWallet, TokenId: 72}}
			if withRefund {
				row.Quota = 10
			}
			require.NoError(t, model.DB.Create(row).Error)
			stale := *row
			row.Progress = "60%"
			row.Data = []byte(`{"provider":"newer"}`)
			won, err := row.UpdatePollingProgress(model.TaskStatusInProgress)
			require.NoError(t, err)
			require.True(t, won)
			attempted := false
			require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("observe-stale-video-terminal-write", func(tx *gorm.DB) {
				if candidate, ok := tx.Statement.Model.(*model.Task); ok && candidate.ID == row.ID {
					if values, ok := tx.Statement.Dest.(map[string]any); ok && values["status"] == model.TaskStatus(model.TaskStatusFailure) {
						attempted = true
					}
				}
			}))
			t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("observe-stale-video-terminal-write") })
			adaptor := &sunoPollingResponseAdaptor{responseBody: `{"code":"success","data":{"status":"FAILURE","fail_reason":"old response","data":{"provider":"older"}}}`}
			require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, &model.Channel{Type: constant.ChannelTypeDoubaoVideo}, row.TaskID, map[string]*model.Task{row.TaskID: &stale}))
			require.True(t, attempted)
			stored, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.Equal(t, row.Status, stored.Status)
			require.Equal(t, row.Progress, stored.Progress)
			require.Equal(t, row.Quota, stored.Quota)
			require.JSONEq(t, string(row.Data), string(stored.Data))
			var intents int64
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("task_id = ?", row.ID).Count(&intents).Error)
			require.Zero(t, intents)
		})
	}
}

func TestVideoPollingRecoversAppliedEvidenceWithoutChannelOrTimeout(t *testing.T) {
	for _, test := range []struct {
		name, state, rawURL string
		wantProxy           bool
		isVertex            bool
	}{
		{name: "applied", state: model.BillingSettlementStatusApplied, rawURL: "https://fixture.invalid/stored"},
		{name: "empty URL", state: model.BillingSettlementStatusApplied, wantProxy: true},
		{name: "data URL", state: model.BillingSettlementStatusApplied, rawURL: "data:video/mp4;base64,AA==", wantProxy: true},
		{name: "Vertex data URL", state: model.BillingSettlementStatusApplied, rawURL: "data:video/mp4;base64,AA==", wantProxy: true, isVertex: true},
		{name: "manual", state: model.BillingSettlementStatusManual, rawURL: "https://fixture.invalid/stored"},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			timeout := constant.TaskTimeoutMinutes
			constant.TaskTimeoutMinutes = 0
			t.Cleanup(func() { constant.TaskTimeoutMinutes = timeout })
			row := &model.Task{TaskID: "restart-" + test.state, UserId: 71, Quota: 80, ChannelId: 9899, Status: model.TaskStatusInProgress,
				Data: []byte(`{"provider":"stored"}`), PrivateData: model.TaskPrivateData{PendingTerminalStatus: model.TaskStatusSuccess,
					PendingTerminalFinishTime: 99, PendingTerminalResultURL: test.rawURL}}
			if test.isVertex {
				row.Platform = constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVertexAi))
			}
			require.NoError(t, model.DB.Create(row).Error)
			key := model.BillingTaskFinalizeOperationKey(row.ID)
			require.NoError(t, model.DB.Create(&model.BillingSettlement{OperationKey: key, Status: test.state, TaskID: row.ID,
				UserID: row.UserId, Source: model.BillingSettlementSourceWallet, TaskQuota: 80, TaskQuotaTarget: 80}).Error)
			err := updateVideoTasks(t.Context(), "fixture", row.ChannelId, []string{row.TaskID}, map[string]*model.Task{row.TaskID: row})
			stored, readErr := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, readErr)
			if test.state == model.BillingSettlementStatusManual {
				require.ErrorContains(t, err, "CacheGetChannel failed")
				require.EqualValues(t, model.TaskStatusInProgress, stored.Status)
				require.EqualValues(t, model.TaskStatusSuccess, stored.PrivateData.PendingTerminalStatus)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, model.TaskStatusSuccess, stored.Status)
				require.EqualValues(t, 99, stored.FinishTime)
				wantURL := test.rawURL
				if test.wantProxy {
					wantURL = taskcommon.BuildProxyURL(row.TaskID)
				}
				require.Equal(t, wantURL, stored.PrivateData.ResultURL)
				if !test.isVertex && strings.HasPrefix(test.rawURL, "data:") {
					require.Equal(t, test.rawURL, stored.PrivateData.InlineVideoDataURL)
				} else {
					require.Empty(t, stored.PrivateData.InlineVideoDataURL)
				}
				require.Empty(t, stored.PrivateData.PendingTerminalStatus)
			}
			require.JSONEq(t, `{"provider":"stored"}`, string(stored.Data))
			require.Equal(t, 80, stored.Quota)
			var records int64
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("task_id = ?", row.ID).Count(&records).Error)
			require.EqualValues(t, 1, records)
		})
	}
}

func TestVideoProgressPreservesPendingTerminalPayload(t *testing.T) {
	truncate(t)
	task := &model.Task{
		TaskID: "pending-terminal-payload", UserId: 71, Quota: 80,
		Status: model.TaskStatusInProgress, Progress: "10%",
		Data: []byte(`{"result":"stored terminal body"}`),
		PrivateData: model.TaskPrivateData{PendingTerminalStatus: model.TaskStatusSuccess,
			PendingTerminalResultURL: "https://fixture.invalid/stored"},
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.BillingSettlement{OperationKey: model.BillingTaskFinalizeOperationKey(task.ID),
		Status: model.BillingSettlementStatusPending, TaskID: task.ID, UserID: task.UserId,
		Source: model.BillingSettlementSourceWallet, TaskQuota: 80, TaskQuotaTarget: 80}).Error)
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: "50%"}}
	require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, &model.Channel{Type: constant.ChannelTypeMiniMax}, task.TaskID,
		map[string]*model.Task{task.TaskID: task}))
	stored, err := model.GetTaskPlaceholderByID(task.ID)
	require.NoError(t, err)
	require.Equal(t, "50%", stored.Progress)
	require.JSONEq(t, `{"result":"stored terminal body"}`, string(stored.Data))
	require.EqualValues(t, model.TaskStatusSuccess, stored.PrivateData.PendingTerminalStatus)
	require.Equal(t, "https://fixture.invalid/stored", stored.PrivateData.PendingTerminalResultURL)
}

func TestVertexCompletionKeepsInlinePayloadOutOfTaskRow(t *testing.T) {
	inline := "data:video/mp4;base64," + strings.Repeat("AAAA", 4096)
	for _, test := range []struct {
		name, statusPath, URLPath, expected string
		payload                             any
	}{
		{name: "object", statusPath: "status", URLPath: "nested.0.video", expected: `{"nested":[{"video":""}],"status":"completed"}`,
			payload: map[string]any{"nested": []any{map[string]any{"video": inline}}, "status": "completed"}},
		{name: "array", statusPath: "0.status", URLPath: "0.video", expected: `[{"video":"","status":"completed","asset_id":9007199254740993},{"video":""},{"video":"https://fixture.invalid/video"}]`,
			payload: []any{map[string]any{"video": " " + inline + "\n", "status": "completed", "asset_id": int64(9007199254740993)},
				map[string]any{"video": "data:video/webm;base64,QQ=="}, map[string]any{"video": "https://fixture.invalid/video"}}},
		{name: "JSON descriptor", statusPath: "status", URLPath: "descriptor", expected: `{"descriptor":"{\"asset_id\":9007199254740993,\"url\":\"\"}","status":"completed"}`,
			payload: map[string]any{"descriptor": `{"url":"\u0064` + inline[1:] + `","asset_id":9007199254740993}`, "status": "completed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			task := &model.Task{TaskID: "vertex-retrievable", UserId: 71, Status: model.TaskStatusInProgress,
				Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVertexAi))}
			require.NoError(t, model.DB.Create(task).Error)
			body, err := common.Marshal(test.payload)
			require.NoError(t, err)
			channel := &model.Channel{Type: constant.ChannelTypeVertexAi}
			channel.SetOtherSettings(dto.ChannelOtherSettings{TaskProtocol: taskcommon.TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{StatusPath: test.statusPath, ResultURLPaths: []string{test.URLPath}}})
			adaptor := &sunoPollingResponseAdaptor{responseBody: string(body)}
			require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, task.TaskID,
				map[string]*model.Task{task.TaskID: task}))
			stored, err := model.GetTaskPlaceholderByID(task.ID)
			require.NoError(t, err)
			require.EqualValues(t, model.TaskStatusSuccess, stored.Status)
			require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), stored.GetResultURL())
			require.Empty(t, stored.PrivateData.InlineVideoDataURL)
			require.Empty(t, stored.PrivateData.PendingTerminalResultURL)
			require.JSONEq(t, test.expected, string(stored.Data))
			if test.name != "object" {
				require.Contains(t, string(stored.Data), "9007199254740993")
			}
		})
	}
}

func TestVideoPollingRetainsOneBoundedInlineCopyForOtherProviders(t *testing.T) {
	truncate(t)
	task := &model.Task{TaskID: "bounded-inline-video", UserId: 71, Status: model.TaskStatusInProgress}
	require.NoError(t, model.DB.Create(task).Error)
	channel := &model.Channel{Type: constant.ChannelTypeMiniMax}
	channel.SetOtherSettings(dto.ChannelOtherSettings{TaskProtocol: taskcommon.TaskProtocolGenericVideo,
		TaskProtocolConfig: &dto.TaskProtocolConfig{ResultURLPaths: []string{"video"}}})
	inline := "data:video/webm;base64,AA=="
	adaptor := &sunoPollingResponseAdaptor{responseBody: `{"status":"completed","video":"` + inline + `"}`}
	require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, task.TaskID, map[string]*model.Task{task.TaskID: task}))
	stored, err := model.GetTaskPlaceholderByID(task.ID)
	require.NoError(t, err)
	require.Equal(t, inline, stored.PrivateData.InlineVideoDataURL)
	require.JSONEq(t, `{"status":"completed","video":""}`, string(stored.Data))
	require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), stored.GetResultURL())
	setVideoTaskResult(stored, "data:video/webm;base64,"+strings.Repeat("A", maxRetainedInlineVideoURLBytes))
	require.Empty(t, stored.PrivateData.InlineVideoDataURL, "historical pending evidence must also honor the stored size bound")
	require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), stored.GetResultURL())
}

type onFetchTaskPollingAdaptor struct {
	TaskPollingAdaptor
	onFetch func()
}

func (a *onFetchTaskPollingAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	a.onFetch()
	return a.TaskPollingAdaptor.FetchTask(baseURL, key, body, proxy)
}

func TestVideoPollingUsesEvidenceAppliedDuringFetch(t *testing.T) {
	truncate(t)
	const userID, tokenID = 971, 972
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "synthetic-video-delay", 900)
	task := &model.Task{TaskID: "video-delayed-poll", UserId: userID, Quota: 80, Status: model.TaskStatusInProgress,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "delayed-provider", TokenId: tokenID}}
	persistTask(t, task)
	input := model.BillingSettlementInput{
		OperationKey: model.BillingTaskFinalizeOperationKey(task.ID), Source: model.BillingSettlementSourceWallet,
		UserID: userID, TaskID: task.ID, TaskQuota: 80, TaskQuotaTarget: 80,
	}
	const originalURL = "https://fixture.invalid/settled"
	fetched := false
	adaptor := &onFetchTaskPollingAdaptor{
		TaskPollingAdaptor: &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
			Status: string(model.TaskStatusFailure), Reason: "superseded response",
		}},
		onFetch: func() {
			fetched = true
			latest, err := model.GetTaskPlaceholderByID(task.ID)
			require.NoError(t, err)
			latest.Data = []byte(`{"source":"durable"}`)
			persistPendingTaskTerminalEvidence(latest, &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), Url: originalURL}, 99)
			won, err := latest.UpdateWithStatusAndSettlementIntent(latest.Status, latest.UpdatedAt, input)
			require.NoError(t, err)
			require.True(t, won)
			delta, applied, err := model.ApplyBillingSettlementOnce(input)
			require.NoError(t, err)
			require.Zero(t, delta)
			require.False(t, applied)
		},
	}
	require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, &model.Channel{Type: constant.ChannelTypeMiniMax}, task.TaskID,
		map[string]*model.Task{task.TaskID: task}))
	require.True(t, fetched)
	stored, err := model.GetTaskPlaceholderByID(task.ID)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.Equal(t, originalURL, stored.PrivateData.ResultURL)
	require.Equal(t, int64(99), stored.FinishTime)
	require.JSONEq(t, `{"source":"durable"}`, string(stored.Data))
	require.Equal(t, 80, stored.Quota)
	require.EqualValues(t, 900, getUserQuota(t, userID))
	require.EqualValues(t, 900, getTokenRemainQuota(t, tokenID))
}

func TestVideoPollingPublishesAppliedEvidenceAfterTerminalCASLoss(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 961, 962, 963
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "synthetic-video-cas-loss", 900)
	seedChannel(t, channelID)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId, task.ChannelId, task.PrivateData.TokenId = userID, channelID, tokenID
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)
	interleaved := false
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("interleave-applied-video-terminal-update", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if interleaved || tx.Statement.Table != "tasks" || !ok || values["status"] != model.TaskStatus(model.TaskStatusSuccess) || values["updated_at"] == nil {
			return
		}
		interleaved = true
		tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE tasks SET updated_at = updated_at + 1, data = ? WHERE id = ?", []byte(`{"progress":"newer"}`), task.ID).Error)
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("interleave-applied-video-terminal-update") })
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), Usage: &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputVideoDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}}}
	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeMiniMax, Key: "fixture"}
	require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
	require.True(t, interleaved)
	stored, err := model.GetTaskPlaceholderByID(task.ID)
	require.NoError(t, err)
	require.EqualValues(t, model.TaskStatusSuccess, stored.Status)
	require.Empty(t, stored.PrivateData.PendingTerminalStatus)
	require.JSONEq(t, `{"progress":"newer"}`, string(stored.Data))
	require.Equal(t, 800, stored.Quota)
	require.EqualValues(t, 1_700, getUserQuota(t, userID))
	require.Equal(t, 1_700, getTokenRemainQuota(t, tokenID))
	var records int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("task_id = ?", task.ID).Count(&records).Error)
	require.EqualValues(t, 1, records)
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

func TestSunoBatchSkipsDuplicateResponsesAfterStaleProgress(t *testing.T) {
	truncate(t)
	seedChannel(t, 7201)
	seedUser(t, 7220, 80)
	seedToken(t, 7221, 7220, "synthetic-suno-stale", 80)
	row := &model.Task{TaskID: "stale-suno", ChannelId: 7201, Status: model.TaskStatusSubmitted, UpdatedAt: 100, Progress: "10%"}
	staleSuccess := &model.Task{TaskID: "stale-success", ChannelId: 7201, Status: model.TaskStatusSubmitted, UpdatedAt: 100}
	staleFailure := makeTask(7220, 7201, 20, 7221, BillingSourceWallet, 0)
	staleFailure.TaskID, staleFailure.Status, staleFailure.UpdatedAt = "stale-failure", model.TaskStatusSubmitted, 100
	neighbor := &model.Task{TaskID: "current-suno", ChannelId: 7201, Status: model.TaskStatusSubmitted, UpdatedAt: 100}
	require.NoError(t, model.DB.Create(neighbor).Error)
	for _, stale := range []*model.Task{row, staleSuccess, staleFailure} {
		require.NoError(t, model.DB.Create(stale).Error)
		require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", stale.ID).UpdateColumn("updated_at", 200).Error)
		require.EqualValues(t, 100, stale.UpdatedAt)
	}
	original := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return &sunoPollingResponseAdaptor{responseBody: `{"code":"success","data":[
			{"task_id":"stale-suno","status":"IN_PROGRESS","data":{"progress":50}},
			{"task_id":"stale-suno","status":"SUCCESS","data":{"progress":100}},
			{"task_id":"stale-success","status":"SUCCESS","data":{"progress":100}},
			{"task_id":"stale-failure","status":"FAILURE","fail_reason":"rejected"},
			{"task_id":"current-suno","status":"SUCCESS","data":{"progress":100}}
		]}`}
	}
	t.Cleanup(func() { GetTaskAdaptorFunc = original })

	taskIDs := []string{row.TaskID, staleSuccess.TaskID, staleFailure.TaskID, neighbor.TaskID}
	tasks := map[string]*model.Task{row.TaskID: row, staleSuccess.TaskID: staleSuccess, staleFailure.TaskID: staleFailure, neighbor.TaskID: neighbor}
	staleUpdates := 0
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("count-stale-suno-update", func(tx *gorm.DB) {
		if candidate, ok := tx.Statement.Model.(*model.Task); ok && candidate.ID == row.ID {
			staleUpdates++
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("count-stale-suno-update") })
	require.NoError(t, updateSunoTasks(t.Context(), 7201, taskIDs, tasks))
	require.Equal(t, 1, staleUpdates)
	require.Equal(t, "10%", row.Progress)
	require.EqualValues(t, model.TaskStatusSuccess, neighbor.Status)
	for _, stale := range []*model.Task{row, staleSuccess, staleFailure} {
		require.EqualValues(t, model.TaskStatusSubmitted, stale.Status)
		stored, err := model.GetTaskPlaceholderByID(stale.ID)
		require.NoError(t, err)
		require.EqualValues(t, model.TaskStatusSubmitted, stored.Status)
		require.EqualValues(t, 200, stored.UpdatedAt)
	}
	require.EqualValues(t, 80, getUserQuota(t, 7220))
	require.EqualValues(t, 80, getTokenRemainQuota(t, 7221))
	var receipts int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Count(&receipts).Error)
	require.Zero(t, receipts)
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

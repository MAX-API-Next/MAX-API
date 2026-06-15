package taskcommon

import (
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildTaskSubmitURLUsesConfiguredPath(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://upstream.example.com/root/",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					SubmitPath: "/v1/videos/create?model={model}",
				},
			},
			UpstreamModelName: "video-model",
		},
	}

	got := BuildTaskSubmitURL(info, "https://fallback.example.com/create")
	assert.Equal(t, "https://upstream.example.com/root/v1/videos/create?model=video-model", got)
}

func TestBuildTaskQueryURLUsesConfiguredPath(t *testing.T) {
	body := WithTaskProtocolConfig(map[string]any{
		"task_id": "task/abc",
	}, dto.ChannelOtherSettings{
		TaskProtocolConfig: &dto.TaskProtocolConfig{
			QueryPath: "/v1/videos/{task_id}",
		},
	})

	got := BuildTaskQueryURL("https://upstream.example.com", body, "https://fallback.example.com/task")
	assert.Equal(t, "https://upstream.example.com/v1/videos/task%2Fabc", got)
}

func TestBuildConfiguredTaskURLKeepsOperationNamePathSegments(t *testing.T) {
	got := BuildConfiguredTaskURL("https://upstream.example.com", "/v1/{operation_name}", map[string]string{
		"operation_name": "models/veo-3/operations/task abc",
	})

	assert.Equal(t, "https://upstream.example.com/v1/models/veo-3/operations/task%20abc", got)
}

func TestBuildConfiguredTaskURLUsesQueryEscaping(t *testing.T) {
	got := BuildConfiguredTaskURL("https://upstream.example.com", "/v1/videos?operation={operation_name}&model={model}&id={task_id}", map[string]string{
		"operation_name": "models/veo-3/operations/a&b",
		"model":          "video model",
		"task_id":        "task/abc&x=1",
	})

	assert.Equal(t, "https://upstream.example.com/v1/videos?operation=models%2Fveo-3%2Foperations%2Fa%26b&model=video+model&id=task%2Fabc%26x%3D1", got)
}

func TestBuildTaskQueryURLKeepsFallbackWithoutConfig(t *testing.T) {
	got := BuildTaskQueryURL("https://upstream.example.com", map[string]any{
		"task_id": "task_abc",
	}, "https://fallback.example.com/task_abc")

	assert.Equal(t, "https://fallback.example.com/task_abc", got)
}

func TestStripTaskProtocolConfig(t *testing.T) {
	body := WithTaskProtocolConfig(map[string]any{
		"task_id": "task_abc",
	}, dto.ChannelOtherSettings{
		TaskProtocolConfig: &dto.TaskProtocolConfig{QueryPath: "/v1/videos/{task_id}"},
	})

	clean := StripTaskProtocolConfig(body)
	_, exists := clean[taskProtocolConfigBodyKey]
	assert.False(t, exists)
	assert.Equal(t, "task_abc", clean["task_id"])
}

func TestParseConfiguredTaskResultUsesGenericProtocol(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		TaskProtocol: TaskProtocolGenericVideo,
		TaskProtocolConfig: &dto.TaskProtocolConfig{
			TaskIDPath:       "data.id",
			StatusPath:       "data.state",
			ProgressPath:     "data.percent",
			ResultURLPaths:   []string{"data.output.videos.0.url"},
			ErrorMessagePath: "data.error.message",
			StatusMap: map[string]string{
				"done": "SUCCESS",
			},
		},
	}
	body := []byte(`{
		"data": {
			"id": "upstream_task_1",
			"state": "done",
			"percent": 100,
			"output": {
				"videos": [
					{"url": "https://example.com/video.mp4"}
				]
			}
		}
	}`)

	result, parsed, err := ParseConfiguredTaskResult(body, settings)

	require.NoError(t, err)
	require.True(t, parsed)
	require.NotNil(t, result)
	assert.Equal(t, "upstream_task_1", result.TaskID)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, "https://example.com/video.mp4", result.Url)
}

func TestParseConfiguredTaskResultRequiresProtocol(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		TaskProtocolConfig: &dto.TaskProtocolConfig{
			StatusPath:     "status",
			ResultURLPaths: []string{"url"},
		},
	}
	body := []byte(`{"status":"succeeded","url":"https://example.com/video.mp4"}`)

	result, parsed, err := ParseConfiguredTaskResult(body, settings)

	require.NoError(t, err)
	assert.False(t, parsed)
	assert.Nil(t, result)
}

func TestValidateTaskProtocolSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		wantErr  bool
	}{
		{name: "empty settings", settings: "", wantErr: false},
		{name: "no task protocol config", settings: `{"azure_responses_version":"v1"}`, wantErr: false},
		{name: "empty query path uses default", settings: `{"task_protocol_config":{"submit_path":"/v1/videos/create"}}`, wantErr: false},
		{name: "task_id placeholder", settings: `{"task_protocol_config":{"query_path":"/v1/videos/{task_id}"}}`, wantErr: false},
		{name: "operation_name placeholder", settings: `{"task_protocol_config":{"query_path":"/v1beta/{operation_name}"}}`, wantErr: false},
		{name: "upstream_task_id placeholder", settings: `{"task_protocol_config":{"query_path":"/v1/tasks?id={upstream_task_id}"}}`, wantErr: false},
		{name: "static query path rejected", settings: `{"task_protocol_config":{"query_path":"/v1/videos/latest"}}`, wantErr: true},
		{name: "invalid json ignored", settings: `{not json`, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTaskProtocolSettings(tc.settings)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTryHandleConfiguredSubmitResponseKlingRouteUsesKlingFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("kling_official_route", true)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					TaskIDPath: "data.id",
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public_1",
		},
	}
	body := []byte(`{"data":{"id":"upstream_task_1"}}`)

	taskID, handled, taskErr := TryHandleConfiguredSubmitResponse(c, body, info)

	require.Nil(t, taskErr)
	require.True(t, handled)
	assert.Equal(t, "upstream_task_1", taskID)

	resp := recorder.Body.Bytes()
	assert.Equal(t, int64(0), gjson.GetBytes(resp, "code").Int())
	assert.Equal(t, "task_public_1", gjson.GetBytes(resp, "task_id").String())
	assert.Equal(t, "task_public_1", gjson.GetBytes(resp, "data.task_id").String())
	assert.Equal(t, "submitted", gjson.GetBytes(resp, "data.task_status").String())
}

func TestTryHandleConfiguredSubmitResponseKlingRouteFallsBackWithoutTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("kling_official_route", true)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					TaskIDPath: "data.id",
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_public_1",
		},
	}
	body := []byte(`{"data":{"task_status":"failed"}}`)

	taskID, handled, taskErr := TryHandleConfiguredSubmitResponse(c, body, info)

	require.Nil(t, taskErr)
	assert.False(t, handled)
	assert.Empty(t, taskID)
	assert.Zero(t, recorder.Body.Len())
}

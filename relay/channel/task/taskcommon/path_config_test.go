package taskcommon

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
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

func TestBuildConfiguredTaskURLDeduplicatesBasePath(t *testing.T) {
	got := BuildConfiguredTaskURL("https://upstream.example.com/v1", "/v1/videos/create", nil)

	assert.Equal(t, "https://upstream.example.com/v1/videos/create", got)
}

func TestBuildTaskQueryURLKeepsFallbackWithoutConfig(t *testing.T) {
	got := BuildTaskQueryURL("https://upstream.example.com", map[string]any{
		"task_id": "task_abc",
	}, "https://fallback.example.com/task_abc")

	assert.Equal(t, "https://fallback.example.com/task_abc", got)
}

func TestBuildConfiguredTaskRequestBodyPassThrough(t *testing.T) {
	c := newJSONTaskContext(`{
		"model": "local-model",
		"prompt": "test",
		"duration_seconds": 0,
		"with_audio": false
	}`)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "upstream-model",
			IsModelMapped:     true,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModePassThrough,
				},
			},
		},
	}
	req := relaycommon.TaskSubmitReq{Model: "local-model", Prompt: "test"}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, "upstream-model", gjson.GetBytes(body, "model").String())
	assert.Equal(t, "test", gjson.GetBytes(body, "prompt").String())
	assert.Equal(t, int64(0), gjson.GetBytes(body, "duration_seconds").Int())
	assert.False(t, gjson.GetBytes(body, "with_audio").Bool())
}

func TestBuildConfiguredTaskRequestBodyMediaGeneration(t *testing.T) {
	c := newJSONTaskContext(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 5,
		"model": "doubao-seedance-1-5-pro-251215",
		"prompt": "dance",
		"resolution": "720p",
		"with_audio": false
	}`)
	req := relaycommon.TaskSubmitReq{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 5,
		"model": "doubao-seedance-1-5-pro-251215",
		"prompt": "dance",
		"resolution": "720p",
		"with_audio": false
	}`), &req))
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeMediaGeneration,
				},
			},
		},
	}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, "doubao-seedance-1-5-pro-251215", gjson.GetBytes(body, "model").String())
	assert.Equal(t, "dance", gjson.GetBytes(body, "prompt").String())
	assert.Equal(t, "16:9", gjson.GetBytes(body, "aspect_ratio").String())
	assert.Equal(t, "video_generation", gjson.GetBytes(body, "capability").String())
	assert.Equal(t, "none", gjson.GetBytes(body, "control_mode").String())
	assert.Equal(t, "text", gjson.GetBytes(body, "input_mode").String())
	assert.Equal(t, "720p", gjson.GetBytes(body, "resolution").String())
	assert.Equal(t, int64(5), gjson.GetBytes(body, "duration_seconds").Int())
	assert.False(t, gjson.GetBytes(body, "with_audio").Bool())
}

func TestBuildConfiguredTaskRequestBodyLegacySeedanceMediaProtocol(t *testing.T) {
	c := newJSONTaskContext(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 5,
		"model": "doubao-seedance-1-5-pro-251215",
		"prompt": "dance",
		"resolution": "720p",
		"with_audio": false
	}`)
	req := relaycommon.TaskSubmitReq{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 5,
		"model": "doubao-seedance-1-5-pro-251215",
		"prompt": "dance",
		"resolution": "720p",
		"with_audio": false
	}`), &req))
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://upstream.example.com/v1",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolLegacySeedanceMedia,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					SubmitPath: "/v1/media/generations",
					QueryPath:  "/v1/media/tasks/{task_id}",
				},
			},
		},
	}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, "doubao-seedance-1-5-pro-251215", gjson.GetBytes(body, "model").String())
	assert.Equal(t, "dance", gjson.GetBytes(body, "prompt").String())
	assert.Equal(t, "video_generation", gjson.GetBytes(body, "capability").String())
	assert.Equal(t, "text", gjson.GetBytes(body, "input_mode").String())
	assert.Equal(t, "none", gjson.GetBytes(body, "control_mode").String())
	assert.Equal(t, int64(5), gjson.GetBytes(body, "duration_seconds").Int())
	assert.False(t, gjson.GetBytes(body, "with_audio").Bool())
	assert.Equal(t, "https://upstream.example.com/v1/media/generations", BuildTaskSubmitURL(info, "https://fallback.example.com/create"))

	result, parsed, err := ParseConfiguredTaskResult([]byte(`{
		"task_id": "task_upstream_1",
		"status": "succeeded",
		"result": {"video_url": "https://example.com/video.mp4"}
	}`), info.ChannelOtherSettings)
	require.NoError(t, err)
	require.True(t, parsed)
	require.NotNil(t, result)
	assert.Equal(t, "task_upstream_1", result.TaskID)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "https://example.com/video.mp4", result.Url)
}

func TestBuildConfiguredTaskRequestBodyMediaGenerationPreservesInvalidRawFields(t *testing.T) {
	c := newJSONTaskContext(`{
		"duration_seconds": "abc",
		"model": "video-model",
		"prompt": "dance",
		"reference_images": {"bad": true},
		"with_audio": "not-bool"
	}`)
	req := relaycommon.TaskSubmitReq{Model: "video-model", Prompt: "dance"}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeMediaGeneration,
				},
			},
		},
	}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, "abc", gjson.GetBytes(body, "duration_seconds").String())
	assert.True(t, gjson.GetBytes(body, "reference_images.bad").Bool())
	assert.Equal(t, "not-bool", gjson.GetBytes(body, "with_audio").String())
}

func TestBuildConfiguredTaskRequestBodyMediaGenerationPreservesInvalidMetadataFields(t *testing.T) {
	c := newJSONTaskContext(`{"model":"video-model","prompt":"dance"}`)
	req := relaycommon.TaskSubmitReq{
		Model:  "video-model",
		Prompt: "dance",
		Metadata: map[string]any{
			"duration_seconds": "abc",
			"reference_images": map[string]any{"bad": true},
			"with_audio":       "not-bool",
		},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeMediaGeneration,
				},
			},
		},
	}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, "abc", gjson.GetBytes(body, "duration_seconds").String())
	assert.True(t, gjson.GetBytes(body, "reference_images.bad").Bool())
	assert.Equal(t, "not-bool", gjson.GetBytes(body, "with_audio").String())
}

func TestBuildConfiguredTaskRequestBodyMediaGenerationKeepsExistingUpstreamModelOnEmptyPayloadModel(t *testing.T) {
	c := newJSONTaskContext(`{"prompt":"dance"}`)
	req := relaycommon.TaskSubmitReq{Prompt: "dance"}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "resolved-upstream-model",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeMediaGeneration,
				},
			},
		},
	}

	_, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	assert.Equal(t, "resolved-upstream-model", info.UpstreamModelName)
}

func TestBuildConfiguredTaskRequestBodyFieldMapping(t *testing.T) {
	c := newJSONTaskContext(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 0,
		"metadata": {
			"control_mode": "reference"
		},
		"model": "local-model",
		"prompt": "dance",
		"resolution": "720p",
		"with_audio": false
	}`)
	req := relaycommon.TaskSubmitReq{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 0,
		"metadata": {
			"control_mode": "reference"
		},
		"model": "local-model",
		"prompt": "dance",
		"resolution": "720p",
		"with_audio": false
	}`), &req))
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "upstream-model",
			IsModelMapped:     true,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeFieldMapping,
					RequestBodyDefaults: map[string]any{
						"capability":   "video_generation",
						"control_mode": "none",
						"input_mode":   "text",
						"with_audio":   true,
					},
					RequestBodyMapping: map[string]string{
						"aspect_ratio":     "aspect_ratio",
						"control_mode":     "metadata.control_mode",
						"duration_seconds": "duration_seconds,seconds,duration",
						"model":            "model",
						"prompt":           "prompt",
						"settings.size":    "resolution",
						"with_audio":       "with_audio",
					},
				},
			},
		},
	}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, "upstream-model", gjson.GetBytes(body, "model").String())
	assert.Equal(t, "dance", gjson.GetBytes(body, "prompt").String())
	assert.Equal(t, "16:9", gjson.GetBytes(body, "aspect_ratio").String())
	assert.Equal(t, "video_generation", gjson.GetBytes(body, "capability").String())
	assert.Equal(t, "reference", gjson.GetBytes(body, "control_mode").String())
	assert.Equal(t, "text", gjson.GetBytes(body, "input_mode").String())
	assert.Equal(t, "720p", gjson.GetBytes(body, "settings.size").String())
	assert.Equal(t, int64(0), gjson.GetBytes(body, "duration_seconds").Int())
	assert.False(t, gjson.GetBytes(body, "with_audio").Bool())
}

func TestBuildConfiguredTaskRequestBodyFieldMappingDoesNotInjectZeroDuration(t *testing.T) {
	c := newJSONTaskContext(`{
		"model": "local-model",
		"prompt": "dance"
	}`)
	req := relaycommon.TaskSubmitReq{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"model": "local-model",
		"prompt": "dance"
	}`), &req))
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeFieldMapping,
					RequestBodyDefaults: map[string]any{
						"duration_seconds": 9,
					},
					RequestBodyMapping: map[string]string{
						"duration_seconds": "duration_seconds,seconds,duration",
						"model":            "model",
						"prompt":           "prompt",
					},
				},
			},
		},
	}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	require.True(t, handled)
	body := readJSONBody(t, reader)
	assert.Equal(t, int64(9), gjson.GetBytes(body, "duration_seconds").Int())
}

func TestBuildConfiguredTaskRequestBodyAdapterFallsBack(t *testing.T) {
	c := newJSONTaskContext(`{"model":"video-model","prompt":"test"}`)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: TaskRequestBodyModeAdapter,
				},
			},
		},
	}
	req := relaycommon.TaskSubmitReq{Model: "video-model", Prompt: "test"}

	reader, handled, err := BuildConfiguredTaskRequestBody(c, info, &req)

	require.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, reader)
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
		{name: "valid request body mode", settings: `{"task_protocol":"generic_video_task","task_protocol_config":{"request_body_mode":"pass_through","query_path":"/v1/videos/{task_id}"}}`, wantErr: false},
		{name: "valid field mapping mode", settings: `{"task_protocol":"generic_video_task","task_protocol_config":{"request_body_mode":"field_mapping","request_body_mapping":{"model":"model"},"query_path":"/v1/videos/{task_id}"}}`, wantErr: false},
		{name: "field mapping without explicit mode", settings: `{"task_protocol":"generic_video_task","task_protocol_config":{"request_body_mapping":{"model":"model"},"query_path":"/v1/videos/{task_id}"}}`, wantErr: false},
		{name: "request body mode requires protocol", settings: `{"task_protocol_config":{"request_body_mode":"pass_through","query_path":"/v1/videos/{task_id}"}}`, wantErr: true},
		{name: "request body mapping requires protocol", settings: `{"task_protocol_config":{"request_body_mapping":{"model":"model"},"query_path":"/v1/videos/{task_id}"}}`, wantErr: true},
		{name: "field mapping mode requires mapping or defaults", settings: `{"task_protocol":"generic_video_task","task_protocol_config":{"request_body_mode":"field_mapping","query_path":"/v1/videos/{task_id}"}}`, wantErr: true},
		{name: "invalid request body mode", settings: `{"task_protocol":"generic_video_task","task_protocol_config":{"request_body_mode":"unsupported","query_path":"/v1/videos/{task_id}"}}`, wantErr: true},
		{name: "unknown task protocol rejected", settings: `{"task_protocol":"unsupported_video_task","task_protocol_config":{"query_path":"/v1/videos/{task_id}"}}`, wantErr: true},
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

func newJSONTaskContext(body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func readJSONBody(t *testing.T, reader io.Reader) []byte {
	t.Helper()
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.True(t, common.IsJsonObject(string(body)))
	return body
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

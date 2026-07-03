package doubao

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetVideoInputRatioUsesResolutionAndVideoInput(t *testing.T) {
	ratio, ok := GetVideoInputRatio("doubao-seedance-2-0-260128", "", false)
	require.True(t, ok)
	assert.InDelta(t, 1.0, ratio, 1e-9)

	ratio, ok = GetVideoInputRatio("doubao-seedance-2-0-260128", "1080p", false)
	require.True(t, ok)
	assert.InDelta(t, 51.0/46.0, ratio, 1e-9)

	ratio, ok = GetVideoInputRatio("doubao-seedance-2-0-260128", "4k", true)
	require.True(t, ok)
	assert.InDelta(t, 16.0/46.0, ratio, 1e-9)

	ratio, ok = GetVideoInputRatio("doubao-seedance-2-0-fast-260128", "4k", false)
	require.True(t, ok)
	assert.InDelta(t, 1.0, ratio, 1e-9)

	ratio, ok = GetVideoInputRatio("doubao-seedance-2-0-fast-260128", "4k", true)
	require.True(t, ok)
	assert.InDelta(t, 22.0/37.0, ratio, 1e-9)
}

func TestEstimateBillingUsesResolutionAndVideoInput(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model: "doubao-seedance-2-0-260128",
		Metadata: map[string]any{
			"resolution": "1080p",
			"content": []any{
				map[string]any{
					"type": "video_url",
					"video_url": map[string]any{
						"url": "https://example.com/input.mp4",
					},
				},
			},
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-9)
}

func TestEstimateBillingIgnoresUnusableVideoInput(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		want     float64
	}{
		{
			name: "empty top-level video_url",
			metadata: map[string]any{
				"resolution": "1080p",
				"video_url":  " ",
			},
			want: 51.0 / 46.0,
		},
		{
			name: "nil top-level video",
			metadata: map[string]any{
				"resolution": "1080p",
				"video":      nil,
			},
			want: 51.0 / 46.0,
		},
		{
			name: "false top-level video",
			metadata: map[string]any{
				"resolution": "1080p",
				"video":      false,
			},
			want: 51.0 / 46.0,
		},
		{
			name: "empty content video_url object",
			metadata: map[string]any{
				"resolution": "1080p",
				"content": []any{
					map[string]any{
						"type":      "video_url",
						"video_url": map[string]any{"url": ""},
					},
				},
			},
			want: 51.0 / 46.0,
		},
		{
			name: "usable content video_url object",
			metadata: map[string]any{
				"resolution": "1080p",
				"content": []any{
					map[string]any{
						"type":      "video_url",
						"video_url": map[string]any{"url": "https://example.com/input.mp4"},
					},
				},
			},
			want: 31.0 / 46.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: "doubao-seedance-2-0-260128",
			}
			c.Set("task_request", relaycommon.TaskSubmitReq{
				Model:    "doubao-seedance-2-0-260128",
				Metadata: tc.metadata,
			})

			ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
			require.NotNil(t, ratios)
			assert.InDelta(t, tc.want, ratios["video_input"], 1e-9)
		})
	}
}

func TestEstimateBillingUsesTopLevelVideoResolution(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "doubao-seedance-2-0-260128",
		Prompt:     "test",
		Resolution: "1080p",
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 51.0/46.0, ratios["video_input"], 1e-9)
}

func TestEstimateBillingUsesTopLevelRawVideoInput(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "test",
		"resolution": "1080p",
		"video_url": "https://example.com/input.mp4"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-9)
}

func TestEstimateBillingUsesLegacyRequestPayloadResolution(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "doubao-seedance-2-0-260128",
		Prompt:     "test",
		Resolution: "1080p",
		Metadata: map[string]any{
			"resolution": "720p",
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.Nil(t, ratios)
}

func TestConvertToRequestPayloadPreservesDoubaoVideoFields(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "test",
		"metadata": {
			"safety_identifier": "safety-123",
			"priority": 0,
			"resolution": "4k",
			"content": [
				{
					"type": "video_url",
					"video_url": { "url": "https://example.com/input.mp4" }
				}
			]
		}
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.NotNil(t, payload.SafetyIdentifier)
	assert.Equal(t, "safety-123", *payload.SafetyIdentifier)
	require.NotNil(t, payload.Priority)
	assert.Equal(t, 0, int(*payload.Priority))
	require.NotNil(t, payload.Resolution)
	assert.Equal(t, "4k", *payload.Resolution)
}

func TestConvertToRequestPayloadPreservesExplicitEmptyOptionalStrings(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "test",
		"metadata": {
			"resolution": "",
			"ratio": ""
		}
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.NotNil(t, payload.Resolution)
	assert.Equal(t, "", *payload.Resolution)
	require.NotNil(t, payload.Ratio)
	assert.Equal(t, "", *payload.Ratio)

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"resolution":""`)
	assert.Contains(t, string(data), `"ratio":""`)
}

func TestValidateConfiguredTaskProtocolAllowsPromptlessMediaRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model": "doubao-seedance-2-0-260128",
		"image": "https://example.com/frame.png"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: taskcommon.TaskProtocolGenericVideo,
				TaskProtocolConfig: &dto.TaskProtocolConfig{
					RequestBodyMode: taskcommon.TaskRequestBodyModeMediaGeneration,
				},
			},
		},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	require.Equal(t, "generate", info.Action)
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Equal(t, "doubao-seedance-2-0-260128", req.Model)
	require.Equal(t, []string{"https://example.com/frame.png"}, req.Images)
}

func TestParseSeedanceMediaTaskResultByShape(t *testing.T) {
	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"object": "media.task",
		"task_id": "task_123",
		"status": "succeeded",
		"progress": 100,
		"result": {
			"url": "https://example.com/result.mp4"
		}
	}`))

	require.NoError(t, err)
	require.NotNil(t, taskInfo)
	assert.Equal(t, "task_123", taskInfo.TaskID)
	assert.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
	assert.Equal(t, "100%", taskInfo.Progress)
	assert.Equal(t, "https://example.com/result.mp4", taskInfo.Url)
}

func TestConvertSeedanceMediaTaskToOpenAIVideoByShape(t *testing.T) {
	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(&model.Task{
		TaskID:    "task_123",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		Data:      []byte(`{"object":"media.task","task_id":"task_123","status":"succeeded","result":{"url":"https://example.com/result.mp4"}}`),
		CreatedAt: 1710000000,
		UpdatedAt: 1710000100,
		Properties: model.Properties{
			OriginModelName: "doubao-seedance-2-0-260128",
		},
	})

	require.NoError(t, err)
	assert.Contains(t, string(body), `"url":"https://example.com/result.mp4"`)
	assert.Contains(t, string(body), `"task_id":"task_123"`)
}

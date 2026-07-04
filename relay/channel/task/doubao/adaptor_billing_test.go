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
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withDoubaoTaskRateCards(t *testing.T, cards map[string]task_billing_setting.RateCard) {
	t.Helper()
	original := task_billing_setting.GetRateCardsCopy()
	originalData, err := common.Marshal(original)
	require.NoError(t, err)
	data, err := common.Marshal(cards)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"task_billing_setting.rate_cards": string(data),
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"task_billing_setting.rate_cards": string(originalData),
		}))
	})
}

func withTestQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	original := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() {
		common.QuotaPerUnit = original
	})
}

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

func TestEstimateTaskBillingUsesDoubaoRateCard(t *testing.T) {
	withTestQuotaPerUnit(t, 1000)
	withDoubaoTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"doubao-seedance-1-5-pro-251215": {
			Vendor:          "doubao",
			Unit:            "call",
			DefaultQuantity: 1,
			Strict:          true,
			Defaults: map[string]string{
				"capability":      "video_generation",
				"duration":        "5",
				"generate_audio":  "true",
				"has_video_input": "false",
				"ratio":           "adaptive",
				"resolution":      "720p",
			},
			Rows: []task_billing_setting.RateCardRow{
				{
					ID: "5s_720p_no_video_input",
					Match: map[string]string{
						"duration":        "5",
						"has_video_input": "false",
						"resolution":      "720p",
					},
					UnitPrice: 0.4,
				},
			},
		},
	})

	duration := 5
	withAudio := false
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-1-5-pro-251215",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "doubao-seedance-1-5-pro-251215",
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:           "doubao-seedance-1-5-pro-251215",
		Prompt:          "test",
		DurationSeconds: &duration,
		Resolution:      "720p",
		WithAudio:       &withAudio,
		Capability:      "video_generation",
	})

	got, err := taskcommon.EstimateGenericTaskBilling(c, info, ChannelName)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "doubao-seedance-1-5-pro-251215", got.RuleKey)
	assert.Equal(t, "5s_720p_no_video_input", got.RowID)
	assert.Equal(t, "call", got.Unit)
	assert.InDelta(t, 1.0, got.Quantity, 1e-9)
	assert.InDelta(t, 0.4, got.TotalPrice, 1e-9)
	assert.Equal(t, 400, got.Quota)
	assert.Equal(t, "5", got.Fields["duration"])
	assert.Equal(t, "720p", got.Fields["resolution"])
	assert.Equal(t, "false", got.Fields["has_video_input"])
	assert.Equal(t, "false", got.Fields["generate_audio"])
	assert.Equal(t, "false", got.Fields["has_audio"])
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

func TestConvertToRequestPayloadUsesTopLevelSeedanceFields(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model": "doubao-seedance-1-5-pro-251215",
		"content": [
			{ "type": "text", "text": "official prompt" },
			{ "type": "image_url", "role": "first_frame", "image_url": { "url": "https://example.com/first.png" } }
		],
		"callback_url": "https://example.com/callback",
		"return_last_frame": false,
		"service_tier": "default",
		"execution_expires_after": 3600,
		"ratio": "16:9",
		"duration": 5,
		"resolution": "720p",
		"generate_audio": false,
		"draft": false,
		"tools": [{ "type": "web_search" }],
		"safety_identifier": "user-123",
		"priority": 0,
		"frames": 29,
		"seed": 0,
		"camera_fixed": false,
		"watermark": false
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.Len(t, payload.Content, 2)
	assert.Equal(t, "text", payload.Content[0].Type)
	assert.Equal(t, "official prompt", payload.Content[0].Text)
	assert.Equal(t, "image_url", payload.Content[1].Type)
	require.NotNil(t, payload.Content[1].ImageURL)
	assert.Equal(t, "https://example.com/first.png", payload.Content[1].ImageURL.URL)
	assert.Equal(t, "first_frame", payload.Content[1].Role)
	require.NotNil(t, payload.CallbackURL)
	assert.Equal(t, "https://example.com/callback", *payload.CallbackURL)
	require.NotNil(t, payload.ReturnLastFrame)
	assert.False(t, bool(*payload.ReturnLastFrame))
	require.NotNil(t, payload.ServiceTier)
	assert.Equal(t, "default", *payload.ServiceTier)
	require.NotNil(t, payload.ExecutionExpiresAfter)
	assert.Equal(t, 3600, int(*payload.ExecutionExpiresAfter))
	require.NotNil(t, payload.Ratio)
	assert.Equal(t, "16:9", *payload.Ratio)
	require.NotNil(t, payload.Duration)
	assert.Equal(t, 5, int(*payload.Duration))
	require.NotNil(t, payload.Resolution)
	assert.Equal(t, "720p", *payload.Resolution)
	require.NotNil(t, payload.GenerateAudio)
	assert.False(t, bool(*payload.GenerateAudio))
	require.NotNil(t, payload.Draft)
	assert.False(t, bool(*payload.Draft))
	require.Len(t, payload.Tools, 1)
	assert.Equal(t, "web_search", payload.Tools[0].Type)
	require.NotNil(t, payload.SafetyIdentifier)
	assert.Equal(t, "user-123", *payload.SafetyIdentifier)
	require.NotNil(t, payload.Priority)
	assert.Equal(t, 0, int(*payload.Priority))
	require.NotNil(t, payload.Frames)
	assert.Equal(t, 29, int(*payload.Frames))
	require.NotNil(t, payload.Seed)
	assert.Equal(t, 0, int(*payload.Seed))
	require.NotNil(t, payload.CameraFixed)
	assert.False(t, bool(*payload.CameraFixed))
	require.NotNil(t, payload.Watermark)
	assert.False(t, bool(*payload.Watermark))

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"content":[`)
	assert.Contains(t, string(data), `"callback_url":"https://example.com/callback"`)
	assert.Contains(t, string(data), `"return_last_frame":false`)
	assert.Contains(t, string(data), `"execution_expires_after":3600`)
	assert.Contains(t, string(data), `"ratio":"16:9"`)
	assert.Contains(t, string(data), `"duration":5`)
	assert.Contains(t, string(data), `"resolution":"720p"`)
	assert.Contains(t, string(data), `"generate_audio":false`)
	assert.Contains(t, string(data), `"draft":false`)
	assert.Contains(t, string(data), `"priority":0`)
	assert.Contains(t, string(data), `"seed":0`)
	assert.Contains(t, string(data), `"camera_fixed":false`)
	assert.Contains(t, string(data), `"watermark":false`)
}

func TestConvertToRequestPayloadPreservesExplicitEmptyOptionalStrings(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "test",
		"ratio": "",
		"callback_url": "",
		"service_tier": "",
		"safety_identifier": "",
		"metadata": {
			"resolution": ""
		}
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.NotNil(t, payload.CallbackURL)
	assert.Equal(t, "", *payload.CallbackURL)
	require.NotNil(t, payload.ServiceTier)
	assert.Equal(t, "", *payload.ServiceTier)
	require.NotNil(t, payload.Resolution)
	assert.Equal(t, "", *payload.Resolution)
	require.NotNil(t, payload.Ratio)
	assert.Equal(t, "", *payload.Ratio)
	require.NotNil(t, payload.SafetyIdentifier)
	assert.Equal(t, "", *payload.SafetyIdentifier)

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"callback_url":""`)
	assert.Contains(t, string(data), `"service_tier":""`)
	assert.Contains(t, string(data), `"resolution":""`)
	assert.Contains(t, string(data), `"ratio":""`)
	assert.Contains(t, string(data), `"safety_identifier":""`)
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
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				TaskProtocol: taskcommon.TaskProtocolGenericVideo,
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

func TestValidateRequestAllowsOfficialContentWithoutTopLevelPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model": "doubao-seedance-2-0-260128",
		"content": [
			{ "type": "text", "text": "official prompt" }
		],
		"ratio": "16:9",
		"generate_audio": false
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	require.Equal(t, "generate", info.Action)
	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Len(t, req.Content, 1)
	assert.Equal(t, "official prompt", req.Content[0]["text"])
	require.NotNil(t, req.GenerateAudio)
	assert.False(t, *req.GenerateAudio)
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

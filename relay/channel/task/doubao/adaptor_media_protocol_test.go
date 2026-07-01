package doubao

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToGenericMediaRequestFromTopLevelFields(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"aspect_ratio": "16:9",
		"duration_seconds": 5,
		"end_image": "https://example.com/last-frame.png",
		"image": "https://example.com/first-frame.png",
		"input_mode": "single_image",
		"model": "doubao-seedance-2-0-260128",
		"prompt": "让人物自然转身并看向镜头",
		"resolution": "720p",
		"with_audio": true
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToGenericMediaRequest(&req)
	require.NoError(t, err)

	assert.Equal(t, "doubao-seedance-2-0-260128", payload.Model)
	assert.Equal(t, "让人物自然转身并看向镜头", payload.Prompt)
	assert.Equal(t, "16:9", payload.AspectRatio)
	assert.Equal(t, "video_generation", payload.Capability)
	assert.Equal(t, "end_frame", payload.ControlMode)
	assert.Equal(t, "single_image", payload.InputMode)
	assert.Equal(t, "https://example.com/first-frame.png", payload.Image)
	assert.Equal(t, "https://example.com/last-frame.png", payload.EndImage)
	assert.Equal(t, "720p", payload.Resolution)
	require.NotNil(t, payload.DurationSeconds)
	assert.Equal(t, 5, *payload.DurationSeconds)
	require.NotNil(t, payload.WithAudio)
	assert.True(t, *payload.WithAudio)
}

func TestConvertToGenericMediaRequestReferenceImages(t *testing.T) {
	var req relaycommon.TaskSubmitReq
	err := common.Unmarshal([]byte(`{
		"model": "doubao-seedance-2-0-260128",
		"prompt": "参考图片中的人物在海边回眸",
		"reference_images": ["asset://asset-a", "asset://asset-b"],
		"with_audio": false
	}`), &req)
	require.NoError(t, err)

	payload, err := (&TaskAdaptor{}).convertToGenericMediaRequest(&req)
	require.NoError(t, err)

	assert.Equal(t, "multi_image", payload.InputMode)
	assert.Equal(t, "reference", payload.ControlMode)
	assert.Equal(t, []string{"asset://asset-a", "asset://asset-b"}, payload.ReferenceImages)
	require.NotNil(t, payload.WithAudio)
	assert.False(t, *payload.WithAudio)
}

func TestParseSeedanceMediaTaskResult(t *testing.T) {
	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"object": "media.task",
		"task_id": "6c1a4eeaaee14736a78170ddbfff8361",
		"status": "succeeded",
		"progress": 100,
		"result": {
			"url": "https://example.com/result.mp4",
			"duration_seconds": 5
		}
	}`))
	require.NoError(t, err)

	assert.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
	assert.Equal(t, "100%", taskInfo.Progress)
	assert.Equal(t, "https://example.com/result.mp4", taskInfo.Url)
	assert.Equal(t, "6c1a4eeaaee14736a78170ddbfff8361", taskInfo.TaskID)
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

func TestEstimateBillingUsesTopLevelSeedanceMediaResolution(t *testing.T) {
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

func TestConvertToRequestPayloadPreservesSeedanceFields(t *testing.T) {
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
	assert.Equal(t, "4k", payload.Resolution)
}

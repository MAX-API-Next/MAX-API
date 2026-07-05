package taskcommon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withGenericTaskRateCards(t *testing.T, cards map[string]task_billing_setting.RateCard) {
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

func withGenericBillingQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	original := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() {
		common.QuotaPerUnit = original
	})
}

func TestEstimateGenericTaskBillingUsesConfiguredVideoRateCard(t *testing.T) {
	withGenericBillingQuotaPerUnit(t, 1000)
	withGenericTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"custom-video-model": {
			Vendor:          "custom",
			Unit:            "second",
			QuantityField:   "duration",
			DefaultQuantity: 5,
			Strict:          true,
			Defaults: map[string]string{
				"quality":         "std",
				"has_audio":       "false",
				"has_video_input": "false",
			},
			Rows: []task_billing_setting.RateCardRow{
				{
					ID: "std_720p_audio_video",
					Match: map[string]string{
						"quality":         "std",
						"resolution":      "720p",
						"has_audio":       "true",
						"has_video_input": "true",
					},
					UnitPrice: 0.8,
				},
			},
		},
	})

	duration := 5
	audio := true
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "custom-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "custom-video-model"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:           "custom-video-model",
		Mode:            "std",
		DurationSeconds: &duration,
		Resolution:      "720p",
		GenerateAudio:   &audio,
		Metadata: map[string]any{
			"video_url": "https://example.com/input.mp4",
		},
	})

	got, err := EstimateGenericTaskBilling(c, info, "custom-video")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "custom-video-model", got.RuleKey)
	assert.Equal(t, "std_720p_audio_video", got.RowID)
	assert.InDelta(t, 5.0, got.Quantity, 1e-9)
	assert.InDelta(t, 4.0, got.TotalPrice, 1e-9)
	assert.Equal(t, 4000, got.Quota)
	assert.Equal(t, "true", got.Fields["has_audio"])
	assert.Equal(t, "true", got.Fields["generate_audio"])
	assert.Equal(t, "true", got.Fields["has_video_input"])
}

func TestEstimateGenericTaskBillingIgnoresMissingRequestWithoutRateCard(t *testing.T) {
	withGenericTaskRateCards(t, map[string]task_billing_setting.RateCard{})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "unconfigured-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "unconfigured-video-model"},
	}

	got, err := EstimateGenericTaskBilling(c, info, "custom-video")

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestEstimateGenericTaskBillingUsesFinalRequestBody(t *testing.T) {
	withGenericBillingQuotaPerUnit(t, 1000)
	withGenericTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"mapped-video-model": {
			Vendor:          "custom",
			Unit:            "call",
			Strict:          true,
			DefaultQuantity: 1,
			Defaults: map[string]string{
				"duration":        "5",
				"resolution":      "720p",
				"has_video_input": "false",
			},
			Rows: []task_billing_setting.RateCardRow{
				{
					ID: "final_1080p_10s",
					Match: map[string]string{
						"duration":   "10",
						"resolution": "1080p",
					},
					UnitPrice: 2,
				},
			},
		},
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"mapped-video-model"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "mapped-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "mapped-video-model"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	storedDuration := 5
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:      "mapped-video-model",
		Duration:   &storedDuration,
		Resolution: "720p",
	})
	require.NoError(t, SyncTaskRequestContext(c, []byte(`{
		"model": "mapped-video-model",
		"duration_seconds": 10,
		"resolution": "1080p"
	}`)))

	got, err := EstimateGenericTaskBilling(c, info, "custom-video")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "final_1080p_10s", got.RowID)
	assert.Equal(t, "10", got.Fields["duration"])
	assert.Equal(t, "1080p", got.Fields["resolution"])
	assert.Equal(t, 2000, got.Quota)
}

func TestEstimateGenericTaskBillingVideoInputRequiresUsableMedia(t *testing.T) {
	withGenericBillingQuotaPerUnit(t, 1000)
	withGenericTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"content-video-model": {
			Vendor:          "custom",
			Unit:            "call",
			DefaultQuantity: 1,
			Strict:          true,
			Defaults: map[string]string{
				"has_video_input": "false",
			},
			Rows: []task_billing_setting.RateCardRow{
				{ID: "no_video", Match: map[string]string{"has_video_input": "false"}, UnitPrice: 1},
				{ID: "has_video", Match: map[string]string{"has_video_input": "true"}, UnitPrice: 2},
			},
		},
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"content-video-model"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "content-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "content-video-model"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model: "content-video-model",
		Metadata: map[string]any{
			"video_url": map[string]any{"url": ""},
		},
	})

	got, err := EstimateGenericTaskBilling(c, info, "custom-video")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "no_video", got.RowID)

	require.NoError(t, SyncTaskRequestContext(c, []byte(`{
		"model": "content-video-model",
		"content": [
			{ "type": "video_url", "video_url": "" },
			{ "type": "video_url", "video_url": { "url": "https://example.com/input.mp4" } }
		]
	}`)))

	got, err = EstimateGenericTaskBilling(c, info, "custom-video")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "has_video", got.RowID)
	assert.Equal(t, "true", got.Fields["has_video_input"])
	assert.Equal(t, "1", got.Fields["video_count"])
}

func TestEstimateGenericTaskBillingDoesNotDoubleCountRawImages(t *testing.T) {
	withGenericBillingQuotaPerUnit(t, 1000)
	withGenericTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"image-count-video-model": {
			Vendor:          "custom",
			Unit:            "call",
			DefaultQuantity: 1,
			Strict:          true,
			Defaults: map[string]string{
				"image_count": "0",
			},
			Rows: []task_billing_setting.RateCardRow{
				{ID: "three_images", Match: map[string]string{"image_count": "3"}, UnitPrice: 1},
			},
		},
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"image-count-video-model"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "image-count-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "image-count-video-model"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:           "image-count-video-model",
		Images:          []string{"https://example.com/a.png", "https://example.com/b.png"},
		ReferenceImages: []string{"https://example.com/ref.png"},
	})
	require.NoError(t, SyncTaskRequestContext(c, []byte(`{
		"model": "image-count-video-model",
		"images": ["https://example.com/a.png", "https://example.com/b.png"],
		"reference_images": ["https://example.com/ref.png"]
	}`)))

	got, err := EstimateGenericTaskBilling(c, info, "custom-video")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "three_images", got.RowID)
	assert.Equal(t, "3", got.Fields["image_count"])
}

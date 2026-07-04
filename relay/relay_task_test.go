package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/doubao"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withRelayTaskRateCards(t *testing.T, cards map[string]task_billing_setting.RateCard) {
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

func withRelayTaskQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	original := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() {
		common.QuotaPerUnit = original
	})
}

func TestPrepareTaskSubmitRequestBodyMakesParamOverrideVisibleToBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"test",
		"metadata":{"resolution":"720p"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"path":  "resolution",
						"mode":  "set",
						"value": "1080p",
					},
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"resolution": "720p",
		},
	})

	requestBody, taskErr := prepareTaskSubmitRequestBody(c, info, &doubao.TaskAdaptor{})

	require.Nil(t, taskErr)
	body, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"resolution":"1080p"`)

	ratios := (&doubao.TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 51.0/46.0, ratios["video_input"], 1e-9)
}

func TestPrepareTaskSubmitRequestBodyMakesMultipartParamOverrideVisibleToBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`--boundary--`))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"path":  "resolution",
						"mode":  "set",
						"value": "1080p",
					},
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"resolution": "720p",
		},
	})

	requestBody, taskErr := prepareTaskSubmitRequestBody(c, info, &doubao.TaskAdaptor{})

	require.Nil(t, taskErr)
	body, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"resolution":"1080p"`)

	finalBody, ok := relaycommon.GetTaskSubmitRequestBody(c)
	require.True(t, ok)
	assert.Contains(t, string(finalBody), `"resolution":"1080p"`)
	ratios := (&doubao.TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 51.0/46.0, ratios["video_input"], 1e-9)
}

func TestEstimateTaskBillingFallsBackToGenericRateCard(t *testing.T) {
	withRelayTaskQuotaPerUnit(t, 1000)
	withRelayTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"custom-video-model": {
			Vendor:          "custom",
			Unit:            "second",
			QuantityField:   "duration",
			DefaultQuantity: 5,
			Strict:          true,
			Defaults: map[string]string{
				"resolution":      "720p",
				"has_audio":       "false",
				"has_video_input": "false",
			},
			Rows: []task_billing_setting.RateCardRow{
				{
					ID: "720p_no_audio",
					Match: map[string]string{
						"resolution": "720p",
						"has_audio":  "false",
					},
					UnitPrice: 0.5,
				},
			},
		},
	})

	duration := 6
	audio := false
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
		DurationSeconds: &duration,
		Resolution:      "720p",
		GenerateAudio:   &audio,
	})

	got, err := estimateTaskBilling(c, info, &doubao.TaskAdaptor{}, constant.TaskPlatform("custom-video"))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "custom-video-model", got.RuleKey)
	assert.Equal(t, "720p_no_audio", got.RowID)
	assert.InDelta(t, 6.0, got.Quantity, 1e-9)
	assert.Equal(t, 3000, got.Quota)
}

func TestPrepareTaskSubmitRequestBodyParamOverrideReturnErrorIsLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"test"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode": "return_error",
						"value": map[string]interface{}{
							"message":     "forced bad request by param override",
							"status_code": 422,
							"code":        "forced_bad_request",
							"skip_retry":  true,
						},
					},
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
	})

	_, taskErr := prepareTaskSubmitRequestBody(c, info, &doubao.TaskAdaptor{})

	require.NotNil(t, taskErr)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, http.StatusUnprocessableEntity, taskErr.StatusCode)
	assert.Equal(t, "forced_bad_request", taskErr.Code)
	assert.Equal(t, "forced bad request by param override", taskErr.Message)
}

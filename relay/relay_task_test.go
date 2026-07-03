package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/doubao"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

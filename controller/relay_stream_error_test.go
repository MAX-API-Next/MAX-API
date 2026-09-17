package controller

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeChatErrorReachesControllerRetryPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldFlag, oldTimes, oldRanges := common.EmptyCompletionRetryEnabled, common.RetryTimes, operation_setting.AutomaticRetryStatusCodeRanges
	common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 2
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 502, End: 502}}
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled, common.RetryTimes, operation_setting.AutomaticRetryStatusCodeRanges = oldFlag, oldTimes, oldRanges
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
	_, apiErr := openai.OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"error\":{\"message\":\"overloaded\",\"type\":\"upstream_error\"}}\n\n"))})
	require.NotNil(t, apiErr)
	require.True(t, shouldRetry(c, apiErr, 1))
	require.False(t, shouldRetry(c, apiErr, 0))
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	require.False(t, shouldRetry(c, apiErr, 1))
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 502, End: 502}}
	c.Set("specific_channel_id", 1)
	require.False(t, shouldRetry(c, apiErr, 1))
}

func TestStartedChatStreamErrorUsesSSEFraming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(errors.New("overloaded"), types.ErrorCodeBadResponse, 502)
	require.False(t, writeStartedStreamError(c, types.RelayFormatOpenAI, apiErr))
	helper.SetEventStreamHeaders(c)
	require.NoError(t, helper.StringData(c, `{"choices":[{"delta":{"content":"hello"}}]}`))
	require.True(t, writeStartedStreamError(c, types.RelayFormatOpenAI, apiErr))
	require.Equal(t, http.StatusOK, recorder.Code)
	frames := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n\n")
	require.Len(t, frames, 3)
	require.True(t, strings.HasPrefix(frames[1], "data: "))
	require.Contains(t, frames[1], "overloaded")
	require.Equal(t, "data: [DONE]", frames[2])
}

func TestNativeChatUnstartedErrorUsesJSONStatusAndContentType(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(errors.New("overloaded"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	helper.SetEventStreamHeaders(c)
	require.False(t, writeStartedStreamError(c, types.RelayFormatOpenAI, apiErr))
	c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.Contains(t, recorder.Body.String(), "overloaded")
	require.NotContains(t, recorder.Body.String(), "data:")
}

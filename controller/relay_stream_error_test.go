package controller

import (
	"errors"
	"fmt"
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
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type partialNativeStreamWriter struct {
	*httptest.ResponseRecorder
	short  bool
	writes int
}

func (w *partialNativeStreamWriter) Write(data []byte) (int, error) {
	w.writes++
	if strings.Contains(string(data), "broken") {
		n, _ := w.ResponseRecorder.Write(data[:len(data)/2])
		if w.short {
			return n, nil
		}
		return n, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(data)
}

func TestNativeChatWriteFailureStaysLatchedThroughController(t *testing.T) {
	oldFlag, oldTimes := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 2
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldFlag, oldTimes })
	for _, partial := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			t.Run(fmt.Sprintf("partial=%t/short=%t", partial, short), func(t *testing.T) {
				writer := &partialNativeStreamWriter{ResponseRecorder: httptest.NewRecorder(), short: short}
				c, _ := gin.CreateTestContext(writer)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				info.SetEstimatePromptTokens(38)
				frames := ""
				if partial {
					frames = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"
				}
				frames += "data: {\"choices\":[{\"delta\":{\"content\":\"broken\"}}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":999,\"total_tokens\":1999}}\n\ndata: [DONE]\n\n"
				usage, apiErr := openai.OaiStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(frames))})
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
				require.False(t, shouldRetry(c, apiErr, 1))
				if partial {
					require.Equal(t, service.ResponseText2Usage(c, "hello", info.UpstreamModelName, 38), usage)
				} else {
					require.Nil(t, usage)
				}
				body, writes := writer.Body.String(), writer.writes
				require.NotEmpty(t, body)
				require.True(t, writeStartedStreamError(c, types.RelayFormatOpenAI, apiErr))
				require.Equal(t, writes, writer.writes, "controller must not append to a partially written SSE frame")
				require.Equal(t, body, writer.Body.String())
				n, err := c.Writer.WriteString("late write")
				require.Zero(t, n)
				if short {
					require.ErrorIs(t, err, io.ErrShortWrite)
				} else {
					require.ErrorIs(t, err, io.ErrClosedPipe)
				}
				require.Equal(t, writes, writer.writes)
			})
		}
	}
}

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

func TestNativeChatRequestErrorsRespectControllerRetryPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldFlag, oldTimes, oldRanges := common.EmptyCompletionRetryEnabled, common.RetryTimes, operation_setting.AutomaticRetryStatusCodeRanges
	common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 2
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 502, End: 502}}
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled, common.RetryTimes, operation_setting.AutomaticRetryStatusCodeRanges = oldFlag, oldTimes, oldRanges
	})
	for _, tc := range []struct {
		name      string
		errorType string
		code      any
		retry     bool
	}{
		{"invalid request without code", "invalid_request_error", nil, false},
		{"context limit", "upstream_error", "context_length_exceeded", false},
		{"invalid prompt", "upstream_error", "invalid_prompt", false},
		{"content policy", "invalid_request_error", "content_policy_violation", false},
		{"overload", "upstream_error", nil, true},
		{"server error", "server_error", "server_error", true},
		{"rate limit", "invalid_request_error", "rate_limit_exceeded", true},
		{"provider quota", "invalid_request_error", "insufficient_quota", true},
		{"provider credential", "invalid_request_error", "invalid_api_key", true},
		{"provider model", "invalid_request_error", "model_not_found", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			upstream := types.OpenAIError{Message: "synthetic request failure", Type: tc.errorType, Code: tc.code}
			body, err := common.Marshal(map[string]any{"error": upstream})
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			usage, apiErr := openai.OaiStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: " + string(body) + "\n\n"))})
			require.NotNil(t, apiErr)
			require.Nil(t, usage)
			require.False(t, c.Writer.Written())
			require.Equal(t, upstream.Message, apiErr.ToOpenAIError().Message)
			require.Equal(t, upstream.Type, apiErr.ToOpenAIError().Type)
			require.Equal(t, upstream.Code, apiErr.ToOpenAIError().Code)
			require.Equal(t, tc.retry, shouldRetry(c, apiErr, 1))
			require.False(t, shouldRetry(c, apiErr, 0))
		})
	}
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

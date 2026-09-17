package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const chatRoleFrame = `{"id":"resp_test","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
const chatOverloadFrame = `{"error":{"message":"Our servers are currently overloaded. Please try again later.","type":"upstream_error"}}`
const chatUsageFrame = `{"id":"","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":38,"completion_tokens":0,"total_tokens":38}}`

func newChatStreamTest(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldEnabled, oldRetries := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 2
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldEnabled, oldRetries })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI,
		DisablePing: true, IsStream: true, ShouldIncludeUsage: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	info.SetEstimatePromptTokens(38)
	return c, recorder, info
}

func replayChatStream(c *gin.Context, info *relaycommon.RelayInfo, frames ...string) (*dto.Usage, *types.MaxAPIError) {
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))}
	return OaiStreamHandler(c, info, resp)
}

func TestNativeChatStreamOverloadBeforeOutput(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, role := range []bool{false, true} {
			t.Run(fmt.Sprintf("force=%t/role=%t", force, role), func(t *testing.T) {
				c, recorder, info := newChatStreamTest(t)
				info.ChannelSetting.ForceFormat = force
				frames := []string{chatOverloadFrame, chatUsageFrame, "[DONE]"}
				if role {
					frames = append([]string{chatRoleFrame}, frames...)
				}
				usage, apiErr := replayChatStream(c, info, frames...)
				require.NotNil(t, apiErr)
				require.Nil(t, usage)
				require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				require.Equal(t, "upstream_error", apiErr.ToOpenAIError().Type)
				require.Contains(t, apiErr.Error(), "overloaded")
				require.False(t, types.IsSkipRetryError(apiErr))
				require.False(t, c.Writer.Written())
				require.Empty(t, recorder.Body.String())
			})
		}
	}
}

func TestNativeChatStreamEmptyRetriesOnceBeforeWriting(t *testing.T) {
	for _, done := range []bool{false, true} {
		t.Run(map[bool]string{false: "eof", true: "done"}[done], func(t *testing.T) {
			c, recorder, info := newChatStreamTest(t)
			frames := []string{chatRoleFrame, chatUsageFrame}
			if done {
				frames = append(frames, "[DONE]")
			}
			usage, apiErr := replayChatStream(c, info, frames...)
			require.NotNil(t, apiErr)
			require.Equal(t, types.ErrorCodeEmptyCompletion, apiErr.GetErrorCode())
			require.Nil(t, usage)
			require.Empty(t, recorder.Body.String())
			info.RetryIndex++
			usage, apiErr = replayChatStream(c, info, frames...)
			require.Nil(t, apiErr)
			require.Equal(t, 38, usage.PromptTokens)
			require.Equal(t, 1, emptyCompletionRetryCount(c))
			require.Equal(t, "empty_again", emptyCompletionInfo(c)["empty_retry_result"])
		})
	}
}

func TestNativeChatStreamPreservesNonTextAndZeroUsageOutput(t *testing.T) {
	for name, delta := range map[string]string{
		"text": `{"content":"hello"}`, "reasoning": `{"reasoning_content":"thinking"}`,
		"refusal": `{"refusal":"cannot comply"}`, "audio": `{"audio":{"data":"YWJj"}}`,
		"image":       `{"content":[{"type":"image_url","image_url":{"url":"test-image"}}]}`,
		"tool":        `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`,
		"legacy_tool": `{"function_call":{"name":"lookup","arguments":"{}"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, recorder, info := newChatStreamTest(t)
			frame := `{"choices":[{"index":0,"delta":` + delta + `,"finish_reason":null}]}`
			usage, apiErr := replayChatStream(c, info, chatRoleFrame, frame, chatUsageFrame, "[DONE]")
			require.Nil(t, apiErr)
			require.Equal(t, 38, usage.PromptTokens)
			require.Contains(t, recorder.Body.String(), frame)
			require.Equal(t, 0, emptyCompletionRetryCount(c))
		})
	}
}

func TestNativeChatStreamPartialFailureReturnsUsageWithoutRetry(t *testing.T) {
	c, recorder, info := newChatStreamTest(t)
	text := `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`
	usage, apiErr := replayChatStream(c, info, chatRoleFrame, text, chatUsageFrame, chatOverloadFrame, "[DONE]")
	require.NotNil(t, apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.NotNil(t, usage, "partial output must still reach the existing settlement path")
	require.Equal(t, 38, usage.PromptTokens)
	require.Contains(t, recorder.Body.String(), "hello")
	require.NotContains(t, recorder.Body.String(), "[DONE]", "outer error responder owns terminal framing")
}

func TestNativeChatStreamEmptyGuards(t *testing.T) {
	for _, guard := range []string{"disabled", "exhausted", "specific", "written"} {
		t.Run(guard, func(t *testing.T) {
			c, _, info := newChatStreamTest(t)
			switch guard {
			case "disabled":
				common.EmptyCompletionRetryEnabled = false
			case "exhausted":
				info.RetryIndex = common.RetryTimes
			case "specific":
				c.Set("specific_channel_id", 1)
			case "written":
				_, _ = c.Writer.WriteString(": ping\n\n")
			}
			_, apiErr := replayChatStream(c, info, chatRoleFrame, chatUsageFrame, "[DONE]")
			require.Nil(t, apiErr)
			require.Zero(t, emptyCompletionRetryCount(c))
		})
	}
}

func TestNativeChatStreamCancellationDoesNotTriggerEmptyRetry(t *testing.T) {
	c, _, info := newChatStreamTest(t)
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	_, apiErr := replayChatStream(c, info, chatRoleFrame)
	require.NotNil(t, apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Zero(t, emptyCompletionRetryCount(c))
}

func TestNativeChatStreamSuccessfulRetryRecordsWinningUsage(t *testing.T) {
	c, _, info := newChatStreamTest(t)
	_, apiErr := replayChatStream(c, info, chatRoleFrame, chatUsageFrame, "[DONE]")
	require.NotNil(t, apiErr)
	info.RetryIndex++
	usage, apiErr := replayChatStream(c, info, `{"choices":[{"delta":{"content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`, "[DONE]")
	require.Nil(t, apiErr)
	require.Equal(t, 12, usage.TotalTokens)
	require.Equal(t, "success", emptyCompletionInfo(c)["empty_retry_result"])
	require.False(t, info.StreamStatus.HasErrors(), "failed-attempt status must not contaminate successful attempt")
	_, ok := c.Get(string(constant.ContextKeyEmptyCompletionInfo))
	require.True(t, ok)
}

func TestNativeChatStreamBufferLimitsCloseRetryWindow(t *testing.T) {
	for _, limit := range []string{"events", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			c, recorder, info := newChatStreamTest(t)
			frames := make([]string, maxPendingChatStreamEvents+1)
			for i := range frames {
				frames[i] = chatRoleFrame
			}
			if limit == "bytes" {
				frames = []string{`{"padding":"` + strings.Repeat("x", maxPendingChatStreamBytes) + `","choices":[{"delta":{"role":"assistant"}}]}`}
			}
			allFrames := make([]string, len(frames)+1)
			copy(allFrames, frames)
			allFrames[len(frames)] = chatOverloadFrame
			_, apiErr := replayChatStream(c, info, allFrames...)
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotEmpty(t, recorder.Body.String())
		})
	}
}

type observedChatWriter struct {
	gin.ResponseWriter
	wrote chan struct{}
}

func (w *observedChatWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return n, err
}
func (w *observedChatWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func TestNativeChatStreamFlushesFirstPayloadWithoutWaitingForAnotherFrame(t *testing.T) {
	c, recorder, info := newChatStreamTest(t)
	wrote := make(chan struct{}, 1)
	c.Writer = &observedChatWriter{ResponseWriter: c.Writer, wrote: wrote}
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	producer := make(chan error, 1)
	go func() {
		_, err := io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		if err == nil {
			select {
			case <-wrote:
			case <-time.After(3 * time.Second):
				err = fmt.Errorf("first payload was not flushed")
			}
		}
		producer <- err
		_ = w.Close()
	}()
	_, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: r})
	require.NoError(t, <-producer)
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), "first")
}

func TestNativeChatStreamPingNeverAllowsRetryAfterCommit(t *testing.T) {
	c, _, info := newChatStreamTest(t)
	info.DisablePing = false
	settings := operation_setting.GetGeneralSetting()
	oldEnabled, oldSeconds := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 1
	t.Cleanup(func() { settings.PingIntervalEnabled, settings.PingIntervalSeconds = oldEnabled, oldSeconds })
	wrote := make(chan struct{}, 1)
	c.Writer = &observedChatWriter{ResponseWriter: c.Writer, wrote: wrote}
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	producer := make(chan error, 1)
	go func() {
		_, err := io.WriteString(w, "data: "+chatRoleFrame+"\n\n")
		if err == nil {
			select {
			case <-wrote:
			case <-time.After(3 * time.Second):
				err = fmt.Errorf("expected ping")
			}
		}
		if err == nil {
			_, err = io.WriteString(w, "data: "+chatOverloadFrame+"\n\n")
		}
		producer <- err
		_ = w.Close()
	}()
	_, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: r})
	require.NoError(t, <-producer)
	require.NotNil(t, apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestNativeChatStreamErrorSemanticsIndependentOfEmptySwitch(t *testing.T) {
	c, recorder, info := newChatStreamTest(t)
	common.EmptyCompletionRetryEnabled = false
	_, apiErr := replayChatStream(c, info, `{"error":{"message":"message-only failure","code":"server_error"}}`)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCode("server_error"), apiErr.GetErrorCode())
	require.Contains(t, apiErr.Error(), "message-only failure")
	require.Empty(t, recorder.Body.String())
}

func TestNativeChatStreamEmptyOrMalformedFrames(t *testing.T) {
	for name, frames := range map[string][]string{
		"no_frames":     {"[DONE]"},
		"role_only_eof": {chatRoleFrame},
		"usage_only":    {chatUsageFrame, "[DONE]"},
		"whitespace":    {`{"choices":[{"delta":{"content":" ","reasoning_content":"\n"}}]}`, "[DONE]"},
		"malformed":     {chatRoleFrame, `{"choices":`},
	} {
		t.Run(name, func(t *testing.T) {
			c, recorder, info := newChatStreamTest(t)
			usage, apiErr := replayChatStream(c, info, frames...)
			require.NotNil(t, apiErr)
			require.Nil(t, usage)
			require.Empty(t, recorder.Body.String())
			if name == "malformed" {
				require.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
			} else {
				require.Equal(t, types.ErrorCodeEmptyCompletion, apiErr.GetErrorCode())
			}
		})
	}
}

func TestNativeChatStreamContentFilterIsNotRetried(t *testing.T) {
	c, recorder, info := newChatStreamTest(t)
	_, apiErr := replayChatStream(c, info, `{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`, chatUsageFrame, "[DONE]")
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), "content_filter")
	require.Zero(t, emptyCompletionRetryCount(c))
}

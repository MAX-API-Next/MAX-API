package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Use the real adapters and keep the provider open after metadata. The close
// is a bounded fallback: a broken watchdog must fail rather than hang the suite.
func stalledFirstResultStream(t *testing.T, frames []string) *http.Response {
	t.Helper()
	r, w := io.Pipe()
	finished := make(chan struct{})
	t.Cleanup(func() { _ = r.Close(); _ = w.Close(); <-finished })
	go func() {
		defer close(finished)
		defer w.Close()
		for _, frame := range frames {
			if _, err := io.WriteString(w, "data: "+frame+"\n\n"); err != nil {
				return
			}
		}
		time.Sleep(1500 * time.Millisecond)
	}()
	return &http.Response{StatusCode: http.StatusOK, Body: r}
}

func TestAdaptersMetadataDoesNotEndFirstResultWait(t *testing.T) {
	created := `{"type":"response.created","response":{"id":"resp_test","status":"in_progress","output":[]}}`
	tests := []struct {
		name    string
		frames  []string
		buffer  bool
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{"chat_role", []string{chatRoleFrame}, false, OaiStreamHandler},
		{"chat_usage", []string{chatUsageFrame}, false, OaiStreamHandler},
		{"chat_buffer_limit", stringsToFrames(chatRoleFrame, maxPendingChatStreamEvents+1), true, OaiStreamHandler},
		{"responses_created", []string{created}, false, OaiResponsesStreamHandler},
		{"responses_buffer_limit", stringsToFrames(created, maxPendingResponsesStreamEvents), true, OaiResponsesStreamHandler},
		{"chat_to_responses_role", []string{chatRoleFrame}, false, OaiChatToResponsesStreamHandler},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oldSetting := *operation_setting.GetMonitorSetting()
			*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{StreamingFirstResultTimeoutSeconds: 1}
			t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldSetting })
			c, recorder, info := newChatStreamTest(t)
			info.InitChannelMeta(c)
			info.UpstreamModelName = "gpt-test"
			common.EmptyCompletionRetryEnabled = test.buffer
			usage, apiErr := test.handler(c, info, stalledFirstResultStream(t, test.frames))
			require.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason,
				"metadata must not disable the first-result deadline")
			require.NotNil(t, apiErr)
			require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, apiErr.GetErrorCode())
			require.True(t, types.IsSkipRetryError(apiErr), "forwarded metadata closes the replay window")
			require.Nil(t, usage, "metadata must not create partial output usage")
			require.NotEmpty(t, recorder.Body.String(), "metadata forwarding remains compatible")
		})
	}
}

func stringsToFrames(frame string, count int) []string {
	frames := make([]string, count)
	for i := range frames {
		frames[i] = frame
	}
	return frames
}

func TestNativeChatFirstResultAcceptsSupportedOutput(t *testing.T) {
	for name, delta := range map[string]string{
		"text": `{"content":"hello"}`, "reasoning": `{"reasoning_content":"thinking"}`,
		"refusal": `{"refusal":"cannot comply"}`, "audio": `{"audio":{"data":"YWJj"}}`,
		"image": `{"content":[{"type":"image_url","image_url":{"url":"test-image"}}]}`,
		"tool":  `{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"lookup","arguments":"{}"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, _, info := newChatStreamTest(t)
			_, apiErr := replayChatStream(c, info, chatRoleFrame, `{"choices":[{"delta":`+delta+`}]}`, chatUsageFrame, "[DONE]")
			require.Nil(t, apiErr)
			require.True(t, info.HasRecordedChannelFirstResult())
			require.False(t, info.FirstResultTime.IsZero())
		})
	}
}

func TestFirstResultWriteFailuresDoNotCountAsOutput(t *testing.T) {
	for name, handler := range map[string]func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError){
		"chat": OaiStreamHandler, "responses_to_chat": OaiResponsesToChatStreamHandler,
		"responses": OaiResponsesStreamHandler, "chat_to_responses": OaiChatToResponsesStreamHandler,
	} {
		t.Run(name, func(t *testing.T) {
			c, _, info := newChatStreamTest(t)
			common.EmptyCompletionRetryEnabled = false
			failed, _ := gin.CreateTestContext(&rejectedChatResponseWriter{header: make(http.Header)})
			c.Writer = failed.Writer
			frame := `{"choices":[{"delta":{"content":"never delivered"}}]}`
			if name == "responses" || name == "responses_to_chat" {
				frame = `{"type":"response.output_text.delta","delta":"never delivered"}`
			}
			_, apiErr := handler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\n"))})
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr), "committed write failures must not replay")
			require.False(t, info.HasRecordedChannelFirstResult())
			require.True(t, info.FirstResultTime.IsZero())
		})
	}
}

func TestChatFirstResultExcludesMetadataAndDroppedExtensions(t *testing.T) {
	for _, test := range []struct {
		name, delta string
		force       bool
	}{
		{"provider_metadata", `{"provider_stats":{"tokens":0}}`, false},
		{"tool_index", `{"tool_calls":[{"index":0}]}`, false},
		{"audio_id", `{"audio":{"id":"audio-1"}}`, false},
		{"formatted_audio", `{"audio":{"data":"YWJj"}}`, true},
		{"formatted_refusal", `{"refusal":"discarded by the existing typed format"}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _, info := newChatStreamTest(t)
			common.EmptyCompletionRetryEnabled = false
			info.ChannelSetting.ForceFormat = test.force
			_, _ = replayChatStream(c, info, `{"choices":[{"delta":`+test.delta+`}]}`, "[DONE]")
			require.True(t, info.HasRecordedChannelFirstResponse())
			require.False(t, info.HasRecordedChannelFirstResult(), "only delivered generation fields may close the output deadline")
		})
	}
}

package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesAdaptersWaitForGenerationOutput(t *testing.T) {
	for _, provider := range []struct {
		name             string
		metadata, output string
		forwardsMetadata bool
		handler          func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{"claude", `{"type":"message_start","message":{"id":"msg_test","model":"test-model","usage":{"input_tokens":10,"output_tokens":0}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`, true,
			func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
				return claude.ClaudeStreamHandler(c, resp, info)
			}},
		{"gemini", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":0,"totalTokenCount":10}}`,
			`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, true, gemini.GeminiResponsesStreamHandler},
		{"chat_to_responses", `{"choices":[{"delta":{"role":"assistant"}}]}`,
			`{"choices":[{"delta":{"content":"hello"}}]}`, true, openai.OaiChatToResponsesStreamHandler},
		{"responses", `{"type":"response.created","response":{"id":"resp_test","status":"in_progress","output":[]}}`,
			`{"type":"response.output_text.delta","delta":"hello"}`, true, openai.OaiResponsesStreamHandler},
		{"responses_to_chat", `{"type":"response.created","response":{"id":"resp_test","status":"in_progress","output":[]}}`,
			`{"type":"response.output_text.delta","delta":"hello"}`, false, openai.OaiResponsesToChatStreamHandler},
	} {
		for _, hasOutput := range []bool{false, true} {
			name := "metadata"
			if hasOutput {
				name = "output"
			}
			t.Run(provider.name+"/"+name, func(t *testing.T) {
				oldSetting := *operation_setting.GetMonitorSetting()
				*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{StreamingFirstResultTimeoutSeconds: 1}
				t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldSetting })
				oldRetry := common.EmptyCompletionRetryEnabled
				common.EmptyCompletionRetryEnabled = false
				t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldRetry })
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, StartTime: time.Now(), RelayFormat: types.RelayFormatOpenAIResponses}
				info.InitChannelMeta(c)
				info.UpstreamModelName = "test-model"
				info.SetEstimatePromptTokens(10)
				if provider.name == "responses_to_chat" {
					info.RelayFormat = types.RelayFormatOpenAI
				}
				r, w := io.Pipe()
				finished := make(chan struct{})
				t.Cleanup(func() { _ = r.Close(); _ = w.Close(); <-finished })
				go func() {
					defer close(finished)
					defer w.Close()
					if _, err := io.WriteString(w, "data: "+provider.metadata+"\n\n"); err != nil {
						return
					}
					if hasOutput {
						if _, err := io.WriteString(w, "data: "+provider.output+"\n\n"); err != nil {
							return
						}
					}
					time.Sleep(1500 * time.Millisecond)
				}()
				_, apiErr := provider.handler(c, info, &http.Response{StatusCode: 200, Body: r})
				require.Equal(t, hasOutput, info.HasRecordedChannelFirstResult())
				if hasOutput {
					require.NotEqual(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason, "real output disables the first-result watchdog")
				} else {
					require.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
					require.NotNil(t, apiErr)
					require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, apiErr.GetErrorCode())
					require.Equal(t, provider.forwardsMetadata, types.IsSkipRetryError(apiErr))
				}
			})
		}
	}
}

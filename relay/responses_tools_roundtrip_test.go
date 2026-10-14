package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesCustomToolRoundTripAcrossProviders(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		frames []string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
		stream func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{name: "openai", body: `{"model":"test","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_test","type":"function","function":{"name":"apply_patch","arguments":"{\"input\":\"patch body\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
			frames: []string{
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_test","type":"function","function":{"arguments":"{\"input\":"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"apply_patch","arguments":"\"patch body\"}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
			}, handle: openai.OaiChatToResponsesHandler, stream: openai.OaiChatToResponsesStreamHandler},
		{name: "claude", body: `{"type":"message","model":"test","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call_test","name":"apply_patch","input":{"input":"patch body"}}],"usage":{"input_tokens":4,"output_tokens":2}}`,
			frames: []string{
				`{"type":"message_start","message":{"model":"test","usage":{"input_tokens":4}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_test","name":"apply_patch","input":{}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"patch body\"}"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
			}, handle: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
				return claude.ClaudeHandler(c, resp, info)
			},
			stream: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
				return claude.ClaudeStreamHandler(c, resp, info)
			}},
		{name: "gemini", body: `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_test","name":"apply_patch","args":{"input":"patch body"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`,
			frames: []string{`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_test","name":"apply_patch","args":{"input":"patch body"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`},
			handle: gemini.GeminiResponsesHandler, stream: gemini.GeminiResponsesStreamHandler},
	}
	for _, tt := range tests {
		for _, stream := range []bool{false, true} {
			t.Run(tt.name+map[bool]string{false: "/nonstream", true: "/stream"}[stream], func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{IsStream: stream, RelayFormat: types.RelayFormatOpenAIResponses, DisablePing: true,
					OriginModelName: "test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"},
					Request: &dto.OpenAIResponsesRequest{Tools: []byte(`[{"type":"custom","name":"apply_patch"}]`)}}
				body := tt.body
				handler := tt.handle
				if stream {
					body = "data: " + strings.Join(tt.frames, "\n\ndata: ") + "\n\n"
					handler = tt.stream
				}
				usage, apiErr := handler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))})
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				var converted dto.OpenAIResponsesResponse
				if stream {
					require.Contains(t, recorder.Body.String(), "response.custom_tool_call_input.delta")
					require.Contains(t, recorder.Body.String(), "response.custom_tool_call_input.done")
					require.NotContains(t, recorder.Body.String(), "response.function_call_arguments")
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data: ") {
							continue
						}
						var event dto.ResponsesStreamResponse
						require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &event))
						if event.Type == "response.completed" {
							converted = *event.Response
						}
					}
				} else {
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &converted))
				}
				require.Len(t, converted.Output, 1)
				require.Equal(t, "custom_tool_call", converted.Output[0].Type)
				require.Equal(t, "apply_patch", converted.Output[0].Name)
				require.Equal(t, "patch body", converted.Output[0].Input)
				require.Empty(t, converted.Output[0].Arguments)
				require.Equal(t, "call_test", converted.Output[0].CallId)
			})
		}
	}
}

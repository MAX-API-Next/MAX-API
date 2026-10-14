package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

type responsesSelectiveFailureWriter struct {
	*httptest.ResponseRecorder
	failOn string
}

func (w *responsesSelectiveFailureWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.failOn) {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}

func TestResponsesStreamFailureRetainsOnlyDeliveredUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitTokenEncoders()
	for _, provider := range []struct {
		name   string
		frames []string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{name: "chat_bridge", frames: []string{
			`{"choices":[{"delta":{"content":"partial output"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			`{"choices":[{"delta":{"content":"undelivered"}}],"usage":{"prompt_tokens":1000,"completion_tokens":999,"total_tokens":1999}}`,
		}, handle: openai.OaiChatToResponsesStreamHandler},
		{name: "gemini", frames: []string{
			`{"candidates":[{"content":{"parts":[{"text":"partial output"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`,
			`{"candidates":[{"content":{"parts":[{"text":"undelivered"}]}}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":999,"totalTokenCount":1999}}`,
		}, handle: gemini.GeminiResponsesStreamHandler},
		{name: "claude", frames: []string{
			`{"type":"message_start","message":{"model":"gpt-test","usage":{"input_tokens":10,"output_tokens":2}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"undelivered"}}`,
		}, handle: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
			return claude.ClaudeStreamHandler(c, resp, info)
		}},
		{name: "responses_native", frames: []string{
			`{"type":"response.output_text.delta","delta":"partial output"}`,
			`{"type":"response.output_text.delta","delta":"undelivered"}`,
		}, handle: openai.OaiResponsesStreamHandler},
	} {
		for _, failure := range []string{"first_write", "later_write", "malformed", "done_without_terminal"} {
			t.Run(provider.name+"/"+failure, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				var writer http.ResponseWriter = recorder
				frames := append([]string(nil), provider.frames...)
				if failure == "first_write" {
					writer = &responsesSelectiveFailureWriter{recorder, "event:"}
				}
				if failure == "later_write" {
					writer = &responsesSelectiveFailureWriter{recorder, "undelivered"}
				}
				if failure == "malformed" {
					frames = append(frames[:len(frames)-1], `{broken`)
				}
				if failure == "done_without_terminal" {
					frames = append(frames[:len(frames)-1], `[DONE]`)
				}
				c, _ := gin.CreateTestContext(writer)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses, DisablePing: true,
					OriginModelName: "gpt-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				info.SetEstimatePromptTokens(10)
				usage, apiErr := provider.handle(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))})
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
				if failure == "first_write" {
					require.Nil(t, usage)
					return
				}
				require.NotNil(t, usage)
				require.Equal(t, 10, usage.PromptTokens)
				require.Positive(t, usage.CompletionTokens)
				require.Less(t, usage.CompletionTokens, 10, "failed chunk must not replace delivered usage")
				require.NotContains(t, recorder.Body.String(), "undelivered")
				require.NotContains(t, recorder.Body.String(), "event: response.completed")
			})
		}
	}
}

func TestResponsesProviderExplicitZeroSurvivesFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitTokenEncoders()
	for _, provider := range []struct {
		name   string
		frames []string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{name: "chat_bridge", frames: []string{`{"choices":[{"delta":{"content":"visible"}}]}`, `{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`}, handle: openai.OaiChatToResponsesStreamHandler},
		{name: "gemini", frames: []string{`{"candidates":[{"content":{"parts":[{"text":"visible"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}}`}, handle: gemini.GeminiResponsesStreamHandler},
		{name: "claude", frames: []string{`{"type":"message_start","message":{"model":"gpt-test","usage":{"input_tokens":0}}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"visible"}}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`}, handle: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
			return claude.ClaudeStreamHandler(c, resp, info)
		}},
		{name: "responses_native", frames: []string{`{"type":"response.output_text.delta","delta":"visible"}`, `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`}, handle: openai.OaiResponsesStreamHandler},
	} {
		for _, emptyUsage := range []bool{false, true} {
			t.Run(provider.name+map[bool]string{false: "/explicit_zero", true: "/empty_usage"}[emptyUsage], func(t *testing.T) {
				frames := append([]string(nil), provider.frames...)
				if emptyUsage {
					for i, frame := range frames {
						var path string
						switch provider.name {
						case "chat_bridge":
							if strings.Contains(frame, `"usage"`) {
								path = "usage"
							}
						case "gemini":
							path = "usageMetadata"
						case "claude":
							if strings.Contains(frame, `"message_start"`) {
								path = "message.usage"
							} else if strings.Contains(frame, `"message_delta"`) {
								path = "usage"
							}
						case "responses_native":
							if strings.Contains(frame, `"response.completed"`) {
								path = "response.usage"
							}
						}
						if path != "" {
							var err error
							frames[i], err = sjson.SetRaw(frame, path, `{}`)
							require.NoError(t, err)
						}
					}
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses, DisablePing: true, OriginModelName: "gpt-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				info.SetEstimatePromptTokens(10)
				usage, apiErr := provider.handle(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))})
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				if emptyUsage {
					require.Equal(t, 10, usage.PromptTokens)
					require.Positive(t, usage.CompletionTokens)
				} else {
					require.Zero(t, usage.PromptTokens)
					require.Zero(t, usage.CompletionTokens)
					require.Zero(t, usage.TotalTokens)
					require.NotNil(t, usage.BillingUsage)
					require.True(t, usage.BillingUsage.TokenCountsReported)
				}
			})
		}
	}
}

func TestResponsesTerminalBufferedToolInputKeepsDeliveredUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []struct {
		name   string
		frames []string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{"chat", []string{
			`{"choices":[{"delta":{"content":"visible"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_buffered","function":{"name":"patch","arguments":"{\"input\":\"undelivered buffered input\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1000,"completion_tokens":999,"total_tokens":1999}}`,
		}, openai.OaiChatToResponsesStreamHandler},
		{"gemini", []string{
			`{"candidates":[{"content":{"parts":[{"text":"visible"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_buffered","name":"patch","args":{"input":"undelivered buffered input"}}}]}}]}`,
			`{"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":999,"totalTokenCount":1999}}`,
		}, gemini.GeminiResponsesStreamHandler},
	} {
		t.Run(provider.name, func(t *testing.T) {
			writer := &responsesSelectiveFailureWriter{httptest.NewRecorder(), "event: response.custom_tool_call_input.delta\n"}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses, DisablePing: true,
				OriginModelName: "gpt-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
				Request: &dto.OpenAIResponsesRequest{Tools: []byte(`[{"type":"custom","name":"patch"}]`)}}
			info.SetEstimatePromptTokens(10)
			usage, apiErr := provider.handle(c, info, &http.Response{StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader("data: " + strings.Join(provider.frames, "\n\ndata: ") + "\n\n"))})
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotNil(t, usage)
			require.Equal(t, 10, usage.PromptTokens)
			require.Equal(t, 2, usage.CompletionTokens)
			require.NotContains(t, writer.Body.String(), "undelivered buffered input")
		})
	}
}

func TestResponsesTerminalUsageSurvivesToolAndReasoningMetadataFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []struct {
		name    string
		outputs []string
		final   string
		handle  func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError)
	}{
		{"chat", []string{
			`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_lookup","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_patch","function":{"name":"patch","arguments":"{\"input\":\"\"}"}}]}}]}`,
		}, `{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`, openai.OaiChatToResponsesStreamHandler},
		{"gemini", []string{
			`{"candidates":[{"content":{"parts":[{"thought":true,"text":"thinking"}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_lookup","name":"lookup","args":{"q":"x"}}}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_patch","name":"patch","args":{"input":""}}}]}}]}`,
		}, `{"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":%d,"candidatesTokenCount":%d,"totalTokenCount":%d}}`, gemini.GeminiResponsesStreamHandler},
	} {
		for outputIndex, event := range []string{"response.reasoning_summary_text.done", "response.function_call_arguments.done", "response.custom_tool_call_input.done"} {
			for _, zero := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/zero_%t", provider.name, event, zero), func(t *testing.T) {
					prompt, completion := 4, 2
					if zero {
						prompt, completion = 0, 0
					}
					writer := &responsesSelectiveFailureWriter{httptest.NewRecorder(), "event: " + event + "\n"}
					c, _ := gin.CreateTestContext(writer)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses, DisablePing: true,
						OriginModelName: "gpt-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
						Request: &dto.OpenAIResponsesRequest{Tools: []byte(`[{"type":"custom","name":"patch"}]`)}}
					info.SetEstimatePromptTokens(10)
					frames := []string{provider.outputs[outputIndex], fmt.Sprintf(provider.final, prompt, completion, prompt+completion)}
					usage, apiErr := provider.handle(c, info, &http.Response{StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))})
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
					require.NotNil(t, usage)
					require.Equal(t, prompt, usage.PromptTokens)
					require.Equal(t, completion, usage.CompletionTokens)
					require.NotNil(t, usage.BillingUsage)
					require.True(t, usage.BillingUsage.TokenCountsReported)
				})
			}
		}
	}
}

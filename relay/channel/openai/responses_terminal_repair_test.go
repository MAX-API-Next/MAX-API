package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesToChatLegalTerminalEvents(t *testing.T) {
	for _, terminal := range []struct {
		name, event, status, reason, finish string
	}{
		{"length", "response.incomplete", "incomplete", "max_output_tokens", "length"},
		{"filter", "response.incomplete", "incomplete", "content_filter", "content_filter"},
		{"omitted_status", "response.incomplete", "", "max_output_tokens", "length"},
		{"done", "response.done", "completed", "", "stop"},
		{"done_incomplete", "response.done", "incomplete", "max_output_tokens", "length"},
		{"completed_incomplete", "response.completed", "incomplete", "content_filter", "content_filter"},
	} {
		for _, zero := range []bool{false, true} {
			for _, terminalOnly := range []bool{false, true} {
				t.Run(terminal.name+map[bool]string{false: "/known", true: "/zero"}[zero]+map[bool]string{false: "/deltas", true: "/terminal_only"}[terminalOnly], func(t *testing.T) {
					recorder := httptest.NewRecorder()
					gin.SetMode(gin.TestMode)
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, OriginModelName: "gpt-test", DisablePing: true, ShouldIncludeUsage: true,
						ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
					info.SetEstimatePromptTokens(10)
					prompt, completion := 10, 2
					if zero {
						prompt, completion = 0, 0
					}
					response := map[string]any{"id": "resp_terminal", "model": "gpt-test",
						"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "partial output"}}}},
						"usage":  map[string]any{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}}
					if terminal.status != "" {
						response["status"] = terminal.status
					}
					if terminal.reason != "" {
						response["incomplete_details"] = map[string]any{"reason": terminal.reason}
					}
					data, err := common.Marshal(map[string]any{"type": terminal.event, "response": response})
					require.NoError(t, err)
					frames := []string{`{"type":"response.created","response":{"id":"resp_terminal","model":"gpt-test"}}`}
					if !terminalOnly {
						frames = append(frames, `{"type":"response.output_text.delta","delta":"partial output"}`)
					}
					frames = append(frames, string(data))
					usage, apiErr := OaiResponsesToChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))})
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					require.Equal(t, prompt, usage.PromptTokens)
					require.Equal(t, completion, usage.CompletionTokens)
					require.Equal(t, prompt+completion, usage.TotalTokens)
					require.Contains(t, recorder.Body.String(), `"finish_reason":"`+terminal.finish+`"`)
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "partial output"))
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
					require.NotContains(t, recorder.Body.String(), `"error":`)
				})
			}
		}
	}
}

func TestResponsesToChatTerminalOnlyToolCalls(t *testing.T) {
	for _, event := range []string{"response.completed", "response.done", "response.incomplete"} {
		for _, toolType := range []string{dto.BuildInCallFunctionCall, dto.BuildInCallCustomToolCall} {
			t.Run(event+"/"+toolType, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, OriginModelName: "gpt-test", DisablePing: true,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				item := map[string]any{"type": toolType, "id": "item_terminal", "call_id": "call_terminal", "name": "lookup"}
				if toolType == dto.BuildInCallCustomToolCall {
					item["input"] = "terminal payload"
				} else {
					item["arguments"] = "terminal payload"
				}
				status, finish := "completed", "tool_calls"
				if event == "response.incomplete" {
					status, finish = "incomplete", "length"
				}
				data, err := common.Marshal(map[string]any{"type": event, "response": map[string]any{"id": "resp_tool_terminal", "status": status, "output": []any{item},
					"usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}})
				require.NoError(t, err)
				usage, apiErr := OaiResponsesToChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + string(data) + "\n\n"))})
				require.Nil(t, apiErr)
				require.Equal(t, 12, usage.TotalTokens)
				require.Contains(t, recorder.Body.String(), `"arguments":"terminal payload"`)
				require.Contains(t, recorder.Body.String(), `"name":"lookup"`)
				require.Contains(t, recorder.Body.String(), `"id":"call_terminal"`)
				require.Contains(t, recorder.Body.String(), `"finish_reason":"`+finish+`"`)
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
			})
		}
	}
}

type responsesToChatToolFailureWriter struct {
	*httptest.ResponseRecorder
}

func (w *responsesToChatToolFailureWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "undelivered") {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}

func TestResponsesToChatPartialEstimateIncludesDeliveredTools(t *testing.T) {
	for _, toolType := range []string{dto.BuildInCallFunctionCall, dto.BuildInCallCustomToolCall} {
		for _, mixed := range []bool{false, true} {
			for _, ending := range []string{"eof", "malformed", "write_failure"} {
				t.Run(toolType+map[bool]string{false: "/tool_only/", true: "/mixed/"}[mixed]+ending, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					var writer http.ResponseWriter = recorder
					if ending == "write_failure" {
						writer = &responsesToChatToolFailureWriter{recorder}
					}
					c, _ := gin.CreateTestContext(writer)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, OriginModelName: "gpt-test", DisablePing: true,
						ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
					info.SetEstimatePromptTokens(10)
					frames, deliveredText := []string{}, ""
					if mixed {
						frames = append(frames, `{"type":"response.output_text.delta","delta":"visible text"}`)
						frames = append(frames, `{"type":"response.reasoning_summary_text.delta","delta":"reasoning"}`)
						deliveredText = "visible textreasoning"
					}
					item := map[string]any{"type": toolType, "id": "item_partial", "call_id": "call_partial", "name": "lookup"}
					itemData, err := common.Marshal(map[string]any{"type": "response.output_item.added", "item": item})
					require.NoError(t, err)
					frames = append(frames, string(itemData))
					argumentEvent := "response.function_call_arguments.delta"
					if toolType == dto.BuildInCallCustomToolCall {
						argumentEvent = "response.custom_tool_call_input.delta"
					}
					argumentData, err := common.Marshal(map[string]any{"type": argumentEvent, "item_id": "item_partial", "delta": "delivered tool arguments"})
					require.NoError(t, err)
					frames = append(frames, string(argumentData))
					deliveredText += "lookupdelivered tool arguments"
					if ending == "malformed" {
						frames = append(frames, `{broken`)
					}
					if ending == "write_failure" {
						failed, err := common.Marshal(map[string]any{"type": argumentEvent, "item_id": "item_partial", "delta": "undelivered"})
						require.NoError(t, err)
						frames = append(frames, string(failed))
					}
					usage, apiErr := OaiResponsesToChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))})
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
					require.NotNil(t, usage)
					require.Equal(t, 10, usage.PromptTokens)
					require.Equal(t, service.EstimateTokenByModel("gpt-test", deliveredText), usage.CompletionTokens)
					require.Contains(t, recorder.Body.String(), "delivered tool arguments")
					require.NotContains(t, recorder.Body.String(), "undelivered")
				})
			}
		}
	}
}

func TestResponsesToChatLegalTerminalsPreserveProviderFailure(t *testing.T) {
	for _, event := range []string{"response.completed", "response.done", "response.incomplete"} {
		t.Run(event, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, OriginModelName: "gpt-test", DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			data := `{"type":"response.output_text.delta","delta":"partial"}` + "\n\ndata: " + `{"type":"` + event + `","response":{"status":"failed","error":{"message":"provider failure"}}}`
			_, apiErr := OaiResponsesToChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + data + "\n\n"))})
			require.NotNil(t, apiErr)
			require.Equal(t, "provider failure", apiErr.Error())
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
		})
	}
}

func TestNativeResponsesLegalTerminalsPreserveReportedUsage(t *testing.T) {
	oldEmptyRetry := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmptyRetry })
	for _, event := range []string{"response.completed", "response.done", "response.incomplete"} {
		for _, zero := range []bool{false, true} {
			for _, delivered := range []bool{false, true} {
				name := event
				if zero {
					name += "/zero"
				} else {
					name += "/known"
				}
				if delivered {
					name += "/output"
				} else {
					name += "/metadata"
				}
				t.Run(name, func(t *testing.T) {
					c, info := newResponsesUsageTestContext()
					status := "completed"
					if event == "response.incomplete" {
						status = "incomplete"
					}
					prompt, completion := 4, 2
					if zero {
						prompt, completion = 0, 0
					}
					terminal, err := common.Marshal(map[string]any{"type": event, "response": map[string]any{
						"id": "resp_native_legal", "status": status, "output": []any{},
						"usage": map[string]int{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion},
					}})
					require.NoError(t, err)
					body := "data: " + string(terminal) + "\n\n"
					if delivered {
						body = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" + body
					}
					usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))})
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					require.Equal(t, prompt, usage.PromptTokens)
					require.Equal(t, completion, usage.CompletionTokens)
					require.Equal(t, prompt+completion, usage.TotalTokens)
					require.Equal(t, delivered, info.HasRecordedChannelFirstResult())
				})
			}
		}
	}
}

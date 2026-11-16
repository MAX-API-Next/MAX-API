package service_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay"
	"github.com/MAX-API-Next/MAX-API/relay/channel/cohere"
	"github.com/MAX-API-Next/MAX-API/relay/channel/dify"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type replayConvertedWriter struct {
	gin.ResponseWriter
	short  bool
	marker string
	failed bool
}

type replayCohereWriter struct {
	gin.ResponseWriter
	closed chan bool
}

func (w *replayCohereWriter) CloseNotify() <-chan bool { return w.closed }

func (w *replayConvertedWriter) Write(p []byte) (int, error) {
	marker := w.marker
	if marker == "" {
		marker = "undelivered"
	}
	if strings.Contains(string(p), marker) {
		w.failed = true
		if w.short {
			return w.ResponseWriter.Write(p[:len(p)-1])
		}
		return 0, fmt.Errorf("synthetic converted write failure")
	}
	return w.ResponseWriter.Write(p)
}

func (w *replayConvertedWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func TestConvertedWriteEstimateSettlement(t *testing.T) {
	service.InitTokenEncoders()
	for i, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		for j, partial := range []bool{false, true} {
			for k, facts := range []string{"missing", "rejected_last", "rejected_then_finish", "trailing", "usage_only"} {
				t.Run(fmt.Sprintf("%s/partial=%t/%s", format, partial, facts), func(t *testing.T) {
					usageReplayRatios(t)
					c, info := usageReplayInfo(t, 9700+i*10+j*5+k, "")
					c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer}
					info.IsStream, info.RelayFormat = true, format
					info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
					text, raw := "", ""
					if partial {
						text = "accepted"
						raw = "data: " + `{"choices":[{"index":0,"delta":{"content":"accepted"}}]}` + "\n\n"
					}
					if facts != "usage_only" {
						ownUsage := ""
						if facts != "missing" {
							ownUsage = `,"usage":{"prompt_tokens":999,"completion_tokens":999}`
						}
						raw += "data: " + `{"choices":[{"index":0,"delta":{"content":"` + strings.Repeat("undelivered ", 128) + `"}}]` + ownUsage + "}\n\n"
					}
					prompt, output := 38, service.EstimateTokenByModel(info.UpstreamModelName, text)
					if facts == "trailing" || facts == "usage_only" {
						raw += "data: " + `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7}}` + "\n\n"
						prompt, output = 100, 7
					}
					if facts != "rejected_last" {
						raw += "data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
					}
					raw += "data: [DONE]\n\n"
					u, apiErr := openai.OaiStreamHandler(c, info, usageReplayResponse(raw))
					require.Nil(t, apiErr, "converted renderer retains its legacy soft-error return")
					service.PostTextConsumeQuota(c, info, u, nil)
					service.PostTextConsumeQuota(c, info, u, nil)
					assertUsageReplayBalance(t, info, prompt+output*2)
					require.Equal(t, prompt, u.PromptTokens)
					require.Equal(t, output, u.CompletionTokens)
				})
			}
		}
	}
}

func TestConvertedRejectedToolSurchargeSettlement(t *testing.T) {
	service.InitTokenEncoders()
	oldUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldUnit })
	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	oldPrices := setting.Prices
	setting.Prices = make(map[string]float64, len(oldPrices)+1)
	for name, price := range oldPrices {
		setting.Prices[name] = price
	}
	setting.Prices["undelivered_tool"] = 1 // $1 / 1K calls = 500 quota for one call.
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() { setting.Prices = oldPrices; operation_setting.RebuildToolPriceIndex() })
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, mode := range []string{"error", "short", "success"} {
			for _, finish := range []string{"same_frame", "trailing_finish"} {
				t.Run(fmt.Sprintf("%s/%s/%s", format, mode, finish), func(t *testing.T) {
					usageReplayRatios(t)
					c, info := usageReplayInfo(t, 9797, "")
					info.IsStream, info.RelayFormat = true, format
					info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
					if mode != "success" {
						c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
					}
					raw := "data: " + `{"choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n"
					finishField := ""
					if finish == "same_frame" {
						finishField = `,"finish_reason":"tool_calls"`
					}
					raw += "data: " + `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"undelivered_tool","arguments":"{}"}}]}` + finishField + `}]}` + "\n\n"
					if finish == "trailing_finish" {
						raw += "data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"
					}
					raw += "data: " + `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}` + "\n\ndata: [DONE]\n\n"
					u, apiErr := openai.OaiStreamHandler(c, info, usageReplayResponse(raw))
					require.Nil(t, apiErr, "preserve converted legacy soft-error return")
					service.PostTextConsumeQuota(c, info, u, nil)
					service.PostTextConsumeQuota(c, info, u, nil)
					want := 14 // Reported 10 input + 2 output * ratio2.
					if mode == "success" {
						want += 500
					}
					assertUsageReplayBalance(t, info, want)
					var log model.Log
					require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).First(&log).Error)
					var other map[string]any
					require.NoError(t, common.Unmarshal([]byte(log.Other), &other))
					if mode == "success" {
						require.Len(t, info.ToolUsageSnapshot().Items, 1)
						require.Contains(t, other, "tool_calls")
					} else {
						require.Empty(t, info.ToolUsageSnapshot().Items)
						require.NotContains(t, other, "tool_calls")
					}
				})
			}
		}
	}
}

func TestChatContradictoryZeroTotalAudioSettlement(t *testing.T) {
	service.InitTokenEncoders()
	for _, failed := range []bool{false, true} {
		for _, tc := range []struct {
			name                          string
			prompt, completion, rawTotal  int
			textInput, audioInput, output int
			wantTotal, quota              int
			free                          bool
		}{
			{"contradictory_zero", 2, 1, 0, 1, 1, 1, 3, 17, false},
			{"reported_zero", 0, 0, 0, 0, 0, 0, 0, 0, false},
			{"coherent", 2, 1, 3, 1, 1, 1, 3, 17, false},
			{"positive_total", 2, 1, 99, 1, 1, 1, 99, 17, false},
			{"free_price", 2, 1, 0, 1, 1, 1, 3, 0, true},
		} {
			t.Run(fmt.Sprintf("failed=%t/%s", failed, tc.name), func(t *testing.T) {
				usageReplayRatios(t)
				expr := ""
				if tc.free {
					expr = `tier("free", 0)`
				}
				c, info := usageReplayInfo(t, 9799, expr)
				info.IsStream, info.ShouldIncludeUsage = true, true
				recorder := usageReplayRecorder(c)
				facts := fmt.Sprintf(`{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"prompt_tokens_details":{"text_tokens":%d,"audio_tokens":%d},"completion_tokens_details":{"audio_tokens":%d}}`, tc.prompt, tc.completion, tc.rawTotal, tc.textInput, tc.audioInput, tc.output)
				raw := "data: " + `{"choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\ndata: " + `{"choices":[],"usage":` + facts + `}` + "\n\n"
				if failed {
					raw += "data: " + `{"error":{"type":"server_error","message":"synthetic audio stream failure"}}` + "\n\n"
				} else {
					raw += "data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
				}
				u, apiErr := openai.OaiStreamHandler(c, info, usageReplayResponse(raw))
				if failed {
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
				} else {
					require.Nil(t, apiErr)
				}
				require.NotNil(t, u)
				settle := func() {
					if failed {
						service.PostPartialConsumeQuota(c, info, u)
					} else {
						service.PostAudioConsumeQuota(c, info, u, "")
					}
				}
				settle()
				settle()
				assertUsageReplayBalance(t, info, tc.quota) // 1 text + 1 audio*4 + 1 output audio*4*3 =17.
				require.Equal(t, tc.prompt, u.PromptTokens)
				require.Equal(t, tc.completion, u.CompletionTokens)
				require.Equal(t, tc.wantTotal, u.TotalTokens)
				if tc.free {
					var user model.User
					require.NoError(t, model.DB.First(&user, info.UserId).Error)
					require.Equal(t, 1, user.RequestCount, "known zero-price usage refunds reservation and records one request")
				}
				if failed {
					require.NotNil(t, u.BillingUsage)
					require.True(t, u.BillingUsage.TokenCountsReported)
					require.False(t, u.BillingUsage.Estimated)
				} else {
					var last *dto.Usage
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
						if !strings.HasPrefix(line, "data:") || data == "[DONE]" {
							continue
						}
						var event dto.ChatCompletionsStreamResponse
						require.NoError(t, common.UnmarshalJsonStr(data, &event))
						if event.Usage != nil {
							last = event.Usage
						}
					}
					require.NotNil(t, last)
					require.Equal(t, tc.wantTotal, last.TotalTokens)
				}
			})
		}
	}
}

func usageReplayPricedResponsesTools(t *testing.T) {
	t.Helper()
	service.InitTokenEncoders()
	oldUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldUnit })
	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	oldPrices := setting.Prices
	setting.Prices = make(map[string]float64, len(oldPrices)+3)
	for name, price := range oldPrices {
		setting.Prices[name] = price
	}
	for _, name := range []string{"undelivered_tool", "accepted_tool", dto.BuildInToolFileSearch} {
		setting.Prices[name] = 1
	}
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() { setting.Prices = oldPrices; operation_setting.RebuildToolPriceIndex() })
}

func TestResponsesRejectedToolSurchargeSettlement(t *testing.T) {
	usageReplayPricedResponsesTools(t)
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, types.RelayFormatClaude} {
		for _, kind := range []string{dto.BuildInCallFunctionCall, dto.BuildInCallCustomToolCall, dto.BuildInCallFileSearchCall, dto.BuildInCallToolUse, "idless"} {
			if kind == dto.BuildInCallFileSearchCall && format != types.RelayFormatOpenAIResponses || kind == "idless" && format == types.RelayFormatOpenAIResponses {
				continue
			}
			for _, frame := range []string{"item", "terminal"} {
				for _, mode := range []string{"error", "short", "success"} {
					skipped := kind == "idless" || kind == dto.BuildInCallToolUse && format != types.RelayFormatOpenAIResponses
					if skipped && mode != "success" {
						continue
					}
					t.Run(fmt.Sprintf("%s/%s/%s/%s", format, kind, frame, mode), func(t *testing.T) {
						usageReplayRatios(t)
						c, info := usageReplayInfo(t, 9800, "")
						info.IsStream, info.RelayFormat, info.FinalRequestRelayFormat = true, format, types.RelayFormatOpenAIResponses
						handler := openai.OaiResponsesToChatStreamHandler
						if format == types.RelayFormatOpenAIResponses {
							info.RelayMode, handler = relayconstant.RelayModeResponses, openai.OaiResponsesStreamHandler
						}
						info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
							dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
						}}
						recorder := usageReplayRecorder(c)
						if mode != "success" {
							c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
						}
						item := map[string]any{"type": kind, "id": "tool_1", "call_id": "call_1", "name": "undelivered_tool", "status": "completed"}
						if kind == dto.BuildInCallCustomToolCall {
							item["input"] = "{}"
						} else {
							item["arguments"] = "{}"
						}
						if kind == "idless" {
							item["type"], item["id"], item["call_id"] = dto.BuildInCallFunctionCall, "", ""
						}
						terminal := map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": []any{item}}}
						encodedTerminal, err := common.Marshal(terminal)
						require.NoError(t, err)
						raw := "data: " + `{"type":"response.output_text.delta","delta":"hi","response":{"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"
						if frame == "item" {
							encodedItem, err := common.Marshal(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
							require.NoError(t, err)
							raw += "data: " + string(encodedItem) + "\n\n"
						}
						raw += "data: " + string(encodedTerminal) + "\n\n"
						u, apiErr := handler(c, info, usageReplayResponse(raw))
						if mode == "success" {
							require.Nil(t, apiErr)
						} else {
							require.NotNil(t, apiErr)
							require.True(t, types.IsSkipRetryError(apiErr))
						}
						require.NotNil(t, u)
						settle := func() {
							if mode == "success" {
								service.PostTextConsumeQuota(c, info, u, nil)
							} else {
								service.PostPartialConsumeQuota(c, info, u)
							}
						}
						settle()
						settle()
						want := 14
						if mode == "success" && !skipped {
							want += 500 // One completed custom/file-search call at $1/1K; terminal duplicate must not add a second.
						}
						assertUsageReplayBalance(t, info, want)
						if kind == dto.BuildInCallFileSearchCall {
							calls := 0
							if mode == "success" {
								calls = 1
							}
							require.Equal(t, calls, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
						} else if want == 14 {
							require.Empty(t, info.ToolUsageSnapshot().Items)
						}
						if skipped {
							require.NotContains(t, recorder.Body.String(), "undelivered_tool", "skipped converted output cannot be billed")
						}
					})
				}
			}
		}
	}
}

func TestResponsesMixedTerminalToolDeliverySettlement(t *testing.T) {
	usageReplayPricedResponsesTools(t)
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, types.RelayFormatClaude} {
		for _, mode := range []string{"error", "short", "success"} {
			t.Run(fmt.Sprintf("%s/%s", format, mode), func(t *testing.T) {
				usageReplayRatios(t)
				c, info := usageReplayInfo(t, 9801, "")
				info.IsStream, info.RelayFormat, info.FinalRequestRelayFormat = true, format, types.RelayFormatOpenAIResponses
				handler := openai.OaiResponsesToChatStreamHandler
				if format == types.RelayFormatOpenAIResponses {
					info.RelayMode, handler = relayconstant.RelayModeResponses, openai.OaiResponsesStreamHandler
				}
				if mode != "success" {
					c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
				}
				raw := "data: " + `{"type":"response.output_text.delta","delta":"hi","response":{"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","id":"tool_1","call_id":"call_1","name":"accepted_tool","arguments":"{}"},{"type":"function_call","id":"tool_2","call_id":"call_2","name":"undelivered_tool","arguments":"{}"}]}}` + "\n\n"
				u, apiErr := handler(c, info, usageReplayResponse(raw))
				want := 14
				if mode == "success" {
					require.Nil(t, apiErr)
					want += 1000
				} else {
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
					if format != types.RelayFormatOpenAIResponses {
						want += 500 // First converted tool was fully written before the second failed.
					}
				}
				require.NotNil(t, u)
				service.PostPartialConsumeQuota(c, info, u)
				service.PostPartialConsumeQuota(c, info, u)
				assertUsageReplayBalance(t, info, want)
			})
		}
	}
}

func TestNativeResponsesRejectedImageSurchargeSettlement(t *testing.T) {
	usageReplayPricedResponsesTools(t)
	for _, mode := range []string{"error", "short", "success"} {
		t.Run(mode, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9802, "")
			info.IsStream, info.RelayMode = true, relayconstant.RelayModeResponses
			info.RelayFormat, info.FinalRequestRelayFormat = types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponses
			if mode != "success" {
				c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
			}
			raw := "data: " + `{"type":"response.output_text.delta","delta":"hi","response":{"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"image_1","status":"completed","quality":"low","size":"1024x1024","result":"undelivered"}]}}` + "\n\n"
			u, apiErr := openai.OaiResponsesStreamHandler(c, info, usageReplayResponse(raw))
			want := 14
			if mode == "success" {
				require.Nil(t, apiErr)
				want += 5500 // Fixed low1024 image price .011 * 500000 quota/USD.
			} else {
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
			}
			require.NotNil(t, u)
			service.PostPartialConsumeQuota(c, info, u)
			service.PostPartialConsumeQuota(c, info, u)
			assertUsageReplayBalance(t, info, want)
			require.Equal(t, mode == "success", c.GetBool("image_generation_call"))
		})
	}
}

func TestNativeResponsesBufferedSurchargeSettlement(t *testing.T) {
	usageReplayPricedResponsesTools(t)
	oldEnabled, oldRetries := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 3
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldEnabled, oldRetries })
	for _, kind := range []string{dto.BuildInCallFunctionCall, dto.BuildInCallFileSearchCall, dto.ResponsesOutputTypeImageGenerationCall} {
		for _, mode := range []string{"error", "short", "success"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				usageReplayRatios(t)
				c, info := usageReplayInfo(t, 9803, "")
				info.IsStream, info.RelayMode = true, relayconstant.RelayModeResponses
				info.RelayFormat, info.FinalRequestRelayFormat = types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponses
				info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
					dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
				}}
				if mode != "success" {
					c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
				}
				item := map[string]any{"type": kind, "id": "item_1", "call_id": "call_1", "name": "undelivered_tool", "arguments": "{}", "status": "completed", "quality": "low", "size": "1024x1024", "result": "undelivered"}
				visible := map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "accepted"}}}
				terminal, err := common.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": []any{visible, item}}})
				require.NoError(t, err)
				// Whitespace is delivered output, but stays buffered until the visible terminal arrives.
				raw := "data: " + `{"type":"response.output_text.delta","delta":" ","response":{"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\ndata: " + string(terminal) + "\n\n"
				u, apiErr := openai.OaiResponsesStreamHandler(c, info, usageReplayResponse(raw))
				want := 14
				if mode == "success" {
					require.Nil(t, apiErr)
					if kind == dto.ResponsesOutputTypeImageGenerationCall {
						want += 5500
					} else {
						want += 500
					}
				} else {
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
				}
				require.NotNil(t, u)
				service.PostPartialConsumeQuota(c, info, u)
				service.PostPartialConsumeQuota(c, info, u)
				assertUsageReplayBalance(t, info, want)
			})
		}
	}
}

func TestResponsesNonstreamToolEstimateSettlement(t *testing.T) {
	service.InitTokenEncoders()
	oldAudit, oldLimit := common.LogResponseContentEnabled, common.LogContentMaxCharacters
	common.LogResponseContentEnabled, common.LogContentMaxCharacters = true, 10000
	t.Cleanup(func() { common.LogResponseContentEnabled, common.LogContentMaxCharacters = oldAudit, oldLimit })
	args := `{"payload":"` + strings.Repeat("tool argument ", 32) + `"}`
	id := 9750
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, kind := range []string{dto.BuildInCallFunctionCall, dto.BuildInCallCustomToolCall} {
			for _, reported := range []int{-1, 0, 7} {
				for _, mixed := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/output=%d/mixed=%t", format, kind, reported, mixed), func(t *testing.T) {
						id++
						c, info := usageReplayInfo(t, id, "")
						info.RelayFormat, info.FinalRequestRelayFormat = format, types.RelayFormatOpenAIResponses
						recorder := usageReplayRecorder(c)
						tool := map[string]any{"type": kind, "id": "tool_1", "call_id": "call_1", "name": " lookup "}
						field := "arguments"
						if kind == dto.BuildInCallCustomToolCall {
							field = "input"
						}
						tool[field] = args
						outputs := []any{tool, map[string]any{"type": dto.BuildInCallFunctionCall, "name": " ", "arguments": "unforwarded tool data"}}
						audit := ""
						if mixed {
							audit = "visible"
							outputs = append(outputs, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": audit}}})
						}
						counts := map[string]any{"input_tokens": 100}
						output := service.EstimateTokenByModel(info.UpstreamModelName, audit+"lookup"+args)
						if reported >= 0 {
							counts["output_tokens"] = reported
							output = reported
						}
						raw, err := common.Marshal(map[string]any{"status": "completed", "output": outputs, "usage": counts})
						require.NoError(t, err)
						u, apiErr := openai.OaiResponsesToChatHandler(c, info, usageReplayResponse(string(raw)))
						require.Nil(t, apiErr)
						require.NotNil(t, u.BillingUsage, "settlement retains internal billing evidence")
						require.Equal(t, reported >= 0, u.BillingUsage.TokenCountsReported)
						require.NotEmpty(t, recorder.Body.String())
						if format == types.RelayFormatOpenAI {
							require.NotContains(t, recorder.Body.String(), `"billing_usage"`, "Chat client response excludes internal billing evidence")
							var wire struct{ Usage map[string]any }
							require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &wire))
							require.EqualValues(t, u.PromptTokens, wire.Usage["prompt_tokens"])
							require.EqualValues(t, u.CompletionTokens, wire.Usage["completion_tokens"])
							require.EqualValues(t, u.TotalTokens, wire.Usage["total_tokens"])
						}
						require.Equal(t, audit, info.AuditResponseContent, "tool estimation must not alter message-only audit text")
						require.NotContains(t, recorder.Body.String(), "unforwarded tool data")
						service.PostTextConsumeQuota(c, info, u, nil)
						service.PostTextConsumeQuota(c, info, u, nil)
						assertUsageReplayBalance(t, info, 100+2*output)
						require.Equal(t, output, u.CompletionTokens)
						require.Equal(t, 100+output, u.TotalTokens)
					})
				}
			}
		}
	}
}

func TestResponsesConvertedRejectedFrameSettlement(t *testing.T) {
	service.InitTokenEncoders()
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		for _, mode := range []string{"error", "short", "success", "success_zero"} {
			for _, prior := range []string{"missing", "zero", "reported"} {
				for _, kind := range []string{"text", "tool"} {
					t.Run(fmt.Sprintf("%s/%s/prior=%s/%s", format, mode, prior, kind), func(t *testing.T) {
						usageReplayRatios(t)
						c, info := usageReplayInfo(t, 9791, "")
						info.IsStream, info.RelayFormat = true, format
						info.FinalRequestRelayFormat = types.RelayFormatOpenAIResponses
						recorder := usageReplayRecorder(c)
						failed := mode == "error" || mode == "short"
						if failed {
							c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
						}
						raw, prompt, output := "", 38, 2 // A fully delivered tool name "hi" estimates two output tokens.
						if prior != "missing" {
							facts := `{"input_tokens":0,"output_tokens":0}`
							prompt, output = 0, 0
							if prior == "reported" {
								facts, prompt, output = `{"input_tokens":20,"output_tokens":3}`, 20, 3
							}
							raw += "data: " + `{"type":"response.created","response":{"usage":` + facts + `}}` + "\n\n"
						}
						raw += "data: " + `{"type":"response.output_item.added","item":{"type":"function_call","id":"tool_1","call_id":"call_1","name":"hi","arguments":""}}` + "\n\n"
						facts := `{"input_tokens":100,"output_tokens":999}`
						if mode == "success_zero" {
							facts = `{"input_tokens":0,"output_tokens":0}`
						}
						if failed {
							facts = `{"input_tokens":100,"output_tokens":999,"input_tokens_details":{"cached_tokens":90},"output_tokens_details":{"reasoning_tokens":900}}`
						} else if mode == "success_zero" {
							prompt, output = 0, 0
						} else {
							prompt, output = 100, 999
						}
						terminalOutput := `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"undelivered fallback"}]}]`
						if kind == "tool" {
							terminalOutput = `[{"type":"function_call","id":"tool_2","call_id":"call_2","name":"undelivered","arguments":""}]`
						}
						raw += "data: " + `{"type":"response.completed","response":{"status":"completed","output":` + terminalOutput + `,"usage":` + facts + `}}` + "\n\n"
						u, apiErr := openai.OaiResponsesToChatStreamHandler(c, info, usageReplayResponse(raw))
						if failed {
							require.NotNil(t, apiErr)
							require.True(t, types.IsSkipRetryError(apiErr))
							require.NotNil(t, u)
						} else {
							require.Nil(t, apiErr)
							require.Contains(t, recorder.Body.String(), "undelivered")
						}
						service.PostTextConsumeQuota(c, info, u, nil)
						service.PostTextConsumeQuota(c, info, u, nil)
						assertUsageReplayBalance(t, info, prompt+output*2)
						require.Equal(t, prompt, u.PromptTokens)
						require.Equal(t, output, u.CompletionTokens)
						require.Equal(t, !failed || prior != "missing", u.BillingUsage.TokenCountsReported)
						if failed {
							require.Zero(t, u.PromptTokensDetails.CachedTokens)
							require.Zero(t, u.CompletionTokenDetails.ReasoningTokens)
						}
					})
				}
			}
		}
	}
}

func TestNativeResponsesRejectedFrameSettlement(t *testing.T) {
	service.InitTokenEncoders()
	for _, mode := range []string{"error", "short", "success"} {
		for _, frame := range []string{"terminal", "delta", "metadata"} {
			for _, counts := range []string{"partial", "zero", "positive_details"} {
				t.Run(mode+"/"+frame+"/"+counts, func(t *testing.T) {
					usageReplayRatios(t)
					c, info := usageReplayInfo(t, 9793, "")
					info.IsStream, info.RelayMode = true, relayconstant.RelayModeResponses
					info.RelayFormat, info.FinalRequestRelayFormat = types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponses
					failed := mode != "success"
					if failed {
						c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
					}
					raw := "data: " + `{"type":"response.output_text.delta","delta":"hi","response":{"usage":{"input_tokens":20,"output_tokens":3}}}` + "\n\n"
					facts, prompt, output, cache, quota := `{"input_tokens":100}`, 100, 3, 0, 106
					if counts == "zero" {
						facts, prompt, output, quota = `{"input_tokens":0,"output_tokens":0}`, 0, 0, 0
					} else if counts == "positive_details" {
						facts, output, cache, quota = `{"input_tokens":100,"output_tokens":999,"input_tokens_details":{"cached_tokens":90},"output_tokens_details":{"reasoning_tokens":900}}`, 999, 90, 2017
					}
					response := `{"id":"undelivered","status":"completed","usage":` + facts + `}`
					var event string
					switch frame {
					case "terminal":
						event = `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"undelivered"}]}],"usage":` + facts + `}}`
					case "delta":
						event = `{"type":"response.output_text.delta","delta":"undelivered","response":` + response + `}`
					case "metadata":
						event = `{"type":"response.created","response":` + response + `}`
					}
					if failed && frame != "metadata" {
						prompt, output, cache, quota = 20, 3, 0, 26
					}
					raw += "data: " + event + "\n\n"
					if frame != "terminal" {
						raw += "data: " + `{"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n\n"
					}
					u, apiErr := openai.OaiResponsesStreamHandler(c, info, usageReplayResponse(raw))
					if failed {
						require.NotNil(t, apiErr)
						require.True(t, types.IsSkipRetryError(apiErr))
					} else {
						require.Nil(t, apiErr)
					}
					require.NotNil(t, u)
					service.PostTextConsumeQuota(c, info, u, nil)
					service.PostTextConsumeQuota(c, info, u, nil)
					assertUsageReplayBalance(t, info, quota)
					require.Equal(t, prompt, u.PromptTokens)
					require.Equal(t, output, u.CompletionTokens)
					require.Equal(t, cache, u.PromptTokensDetails.CachedTokens)
					require.True(t, u.BillingUsage.TokenCountsReported)
				})
			}
		}
	}
}

func TestNativeResponsesBufferedUsageSettlement(t *testing.T) {
	service.InitTokenEncoders()
	oldEnabled, oldRetries := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 3
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldEnabled, oldRetries })
	for _, mode := range []string{"error", "short", "success"} {
		t.Run(mode, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9794, "")
			info.IsStream, info.RelayMode = true, relayconstant.RelayModeResponses
			info.RelayFormat, info.FinalRequestRelayFormat = types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponses
			if mode != "success" {
				c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: mode == "short"}
			}
			frames := []string{
				`{"type":"response.created","response":{"usage":{"input_tokens":20,"output_tokens":3}}}`,
				`{"type":"response.output_text.delta","delta":" ","response":{"usage":{"input_tokens":50,"output_tokens":5}}}`,
				`{"type":"response.output_text.delta","delta":" ","response":{"id":"undelivered","usage":{"input_tokens":100,"output_tokens":999}}}`,
				`{"type":"response.created","response":{"usage":{"input_tokens":30}}}`,
				`{"type":"response.output_text.delta","delta":"hi"}`,
				`{"type":"response.completed","response":{"status":"completed","output":[]}}`,
			}
			u, apiErr := openai.OaiResponsesStreamHandler(c, info, usageReplayResponse("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"))
			output := 5
			if mode == "success" {
				require.Nil(t, apiErr)
				output = 999
			} else {
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
			}
			require.NotNil(t, u, "the first complete whitespace frame is delivered output")
			service.PostTextConsumeQuota(c, info, u, nil)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 30+output*2)
			require.Equal(t, 30, u.PromptTokens, "later independent metadata stays authoritative across buffered flush failure")
			require.Equal(t, output, u.CompletionTokens)
		})
	}
}

func TestUsageCorrectionInterruptedCallerLedger(t *testing.T) {
	service.InitTokenEncoders()
	service.InitHttpClient()
	oldPolicy := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy.Enabled = false
	t.Cleanup(func() { model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = oldPolicy })
	text := strings.Repeat("delivered output ", 32)
	for i, tc := range []struct {
		name             string
		format           types.RelayFormat
		chat, tool, zero bool
	}{
		{"claude_missing_counter", types.RelayFormatClaude, false, false, false},
		{"converted_missing_counter", types.RelayFormatOpenAI, false, false, false},
		{"converted_tool_input", types.RelayFormatOpenAI, false, true, false},
		{"chat_missing_terminal", types.RelayFormatOpenAI, true, false, false},
		{"chat_zero_missing_terminal", types.RelayFormatOpenAI, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9590+i, "")
			frames := []string{
				`{"type":"message_start","message":{"model":"audit-model","usage":{"input_tokens":100,"output_tokens":1}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}`,
				`{"type":"message_delta","usage":{"input_tokens":100}}`,
				`{"type":"error","error":{"type":"overloaded_error","message":"synthetic failure"}}`,
			}
			prompt, output := 100, service.EstimateTokenByModel("audit-model", text)
			require.Greater(t, output, 1)
			channelType := constant.ChannelTypeAnthropic
			if tc.tool {
				frames = []string{frames[0], `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_test","name":"lookup","input":{"value":"` + strings.Repeat("hidden argument ", 128) + `"}}}`, frames[3]}
				output = service.EstimateTokenByModel("audit-model", "lookup")
			}
			if tc.chat {
				channelType = constant.ChannelTypeOpenAI
				counts := `{"prompt_tokens":100,"completion_tokens":22}`
				prompt, output = 100, 22
				if tc.zero {
					counts = `{"prompt_tokens":0,"completion_tokens":0}`
					prompt, output = 0, 0
				}
				frames = []string{`{"choices":[{"delta":{"content":"partial output"}}]}`, `{"choices":[],"usage":` + counts + `}`}
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), channelType)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.RelayFormat, info.IsStream = tc.format, true
			var apiErr *types.MaxAPIError
			if tc.format == types.RelayFormatClaude {
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info.Request = &dto.ClaudeRequest{Model: "audit-model", Stream: common.GetPointer(true), MaxTokens: common.GetPointer(uint(100))}
				apiErr = relay.ClaudeHelper(c, info)
			} else {
				info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
				apiErr = relay.TextHelper(c, info)
			}
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertUsageReplayBalance(t, info, prompt+output*2)
			var log model.Log
			require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).First(&log).Error)
			require.Equal(t, prompt, log.PromptTokens)
			require.Equal(t, output, log.CompletionTokens)
			require.Contains(t, log.Other, `"status":"error"`)
		})
	}
}

func TestChatProviderCacheErrorSettlement(t *testing.T) {
	service.InitTokenEncoders()
	service.InitHttpClient()
	oldPolicy := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy.Enabled = false
	t.Cleanup(func() { model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = oldPolicy })
	for i, tc := range []struct{ channel, cached int }{
		{constant.ChannelTypeMoonshot, 90}, {constant.ChannelTypeOpenAI, 90},
		{constant.ChannelTypeZhipu_v4, 90}, {constant.ChannelTypeDeepSeek, 90}, {constant.ChannelTypeMoonshot, 0},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.channel, tc.cached), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9680+i, "")
			counts := map[string]any{"prompt_tokens": 100, "completion_tokens": 10}
			choice := map[string]any{"delta": map[string]any{"content": "partial output"}}
			frame := map[string]any{"usage": counts, "choices": []any{choice}}
			switch tc.channel {
			case constant.ChannelTypeMoonshot:
				choice["usage"] = map[string]any{"cached_tokens": tc.cached}
			case constant.ChannelTypeOpenAI:
				frame["timings"] = map[string]any{"cache_n": tc.cached}
			case constant.ChannelTypeZhipu_v4:
				counts["cached_tokens"] = tc.cached
			case constant.ChannelTypeDeepSeek:
				counts["prompt_cache_hit_tokens"] = tc.cached
			}
			raw, err := common.Marshal(frame)
			require.NoError(t, err)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+string(raw)+"\n\ndata: "+`{"error":{"type":"overloaded_error","message":"synthetic failure"}}`+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), tc.channel)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.IsStream = true
			info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
			apiErr := relay.TextHelper(c, info)
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			want := 39 // (100-90) + 90*0.1 + 10*2.
			if tc.cached == 0 {
				want = 120
			}
			assertUsageReplayBalance(t, info, want)
		})
	}
}

func TestUsageCorrectionResponsesExplicitZeroSettlement(t *testing.T) {
	c, info := usageReplayInfo(t, 9530, "")
	raw := `{"id":"resp_synthetic","object":"response","status":"completed","model":"synthetic-model","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Visible response with explicitly reported zero usage"}]}],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`
	usage, apiErr := openai.OaiResponsesToChatHandler(c, info, usageReplayResponse(raw))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	service.PostTextConsumeQuota(c, info, usage, nil)
	service.PostTextConsumeQuota(c, info, usage, nil)
	var user model.User
	var logs []model.Log
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).Find(&logs).Error)
	require.Equal(t, int64(10000), user.Quota, "reported zero must not become an estimated debit")
	require.Len(t, logs, 1, "the same settlement remains idempotent")
	require.Zero(t, logs[0].Quota)
}

func TestClaudeMissingInputCacheCallerSettlement(t *testing.T) {
	for i, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9600+i, "")
			service.InitHttpClient()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+`{"type":"message_start","message":{"model":"audit-model","usage":{"cache_read_input_tokens":10,"cache_creation_input_tokens":8}}}`+"\n\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"accepted"}}`+"\n\ndata: "+`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeAnthropic)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.IsStream, info.RelayFormat = true, format
			if format == types.RelayFormatClaude {
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info.Request = &dto.ClaudeRequest{Model: "audit-model", Stream: common.GetPointer(true), MaxTokens: common.GetPointer(uint(100))}
				require.Nil(t, relay.ClaudeHelper(c, info))
			} else {
				info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
				require.Nil(t, relay.TextHelper(c, info))
			}
			service.HandleFailedBilling(c, info, types.NewError(fmt.Errorf("duplicate cleanup"), types.ErrorCodeBadResponse))
			assertUsageReplayBalance(t, info, 31) // (38-10-8) + 10*0.1 + 8*1.25.
		})
	}
}

func TestResponsesMissingInputFloorSettlement(t *testing.T) {
	for i, mode := range []string{"nonstream", "converted", "native"} {
		t.Run(mode, func(t *testing.T) {
			c, info := usageReplayInfo(t, 9610+i, "")
			raw := `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"output_tokens":0,"input_tokens_details":{"cached_tokens":1000}}}`
			handler := openai.OaiResponsesToChatHandler
			if mode != "nonstream" {
				info.IsStream = true
				handler = openai.OaiResponsesToChatStreamHandler
				raw = "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","usage":{"output_tokens":0,"input_tokens_details":{"cached_tokens":1000}}}}` + "\n\n"
				if mode == "native" {
					handler = openai.OaiResponsesStreamHandler
				}
			}
			u, apiErr := handler(c, info, usageReplayResponse(raw))
			require.Nil(t, apiErr)
			service.PostTextConsumeQuota(c, info, u, nil)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 100) // Known cached subset: 1000*0.1; no uncached/output tokens.
		})
	}
}

func TestResponsesMissingOutputFloorSettlement(t *testing.T) {
	for i, mode := range []string{"nonstream", "converted", "native"} {
		for j, reported := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", mode, reported), func(t *testing.T) {
				c, info := usageReplayInfo(t, 9660+i*2+j, "")
				counts := `"input_tokens":100,"output_tokens_details":{"reasoning_tokens":1000}`
				output := 1000
				if reported {
					counts += `,"output_tokens":0`
					output = 0
				}
				raw := `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{` + counts + `}}`
				handler := openai.OaiResponsesToChatHandler
				if mode != "nonstream" {
					info.IsStream = true
					handler = openai.OaiResponsesToChatStreamHandler
					raw = "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","usage":{` + counts + `}}}` + "\n\n"
					if mode == "native" {
						handler = openai.OaiResponsesStreamHandler
					}
				}
				u, apiErr := handler(c, info, usageReplayResponse(raw))
				require.Nil(t, apiErr)
				service.PostTextConsumeQuota(c, info, u, nil)
				service.PostTextConsumeQuota(c, info, u, nil)
				assertUsageReplayBalance(t, info, 100+output*2)
				require.Equal(t, output, u.CompletionTokens)
			})
		}
	}
}

func TestResponsesToolImageDetailEstimateSettlement(t *testing.T) {
	oldCount, oldMedia := constant.CountToken, constant.GetMediaToken
	constant.CountToken, constant.GetMediaToken = true, false
	t.Cleanup(func() { constant.CountToken, constant.GetMediaToken = oldCount, oldMedia })
	service.InitTokenEncoders()
	for i, detail := range []string{"low", "high", "auto"} {
		t.Run(detail, func(t *testing.T) {
			c, info := usageReplayInfo(t, 9620+i, "")
			info.UpstreamModelName = "gpt-4o"
			c.Set(string(constant.ContextKeyOriginalModel), "gpt-4o")
			var request dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"input":[{"type":"function_call_output","output":[{"type":"input_image","image_url":"https://example.invalid/image.png","detail":"`+detail+`"}]}]}`, &request))
			estimate, err := service.EstimateRequestToken(c, request.GetTokenCountMeta(), info)
			require.NoError(t, err)
			want := 258
			if detail == "low" {
				want = 88
			}
			require.Equal(t, want, estimate) // Existing 3-token envelope + 85/255 image heuristic.
			info.SetEstimatePromptTokens(estimate)
			u, apiErr := openai.OaiResponsesToChatHandler(c, info, usageReplayResponse(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"output_tokens":0}}`))
			require.Nil(t, apiErr)
			service.PostTextConsumeQuota(c, info, u, nil)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, want)
		})
	}
}

func TestDifyReportedPartialTotalSettlement(t *testing.T) {
	service.InitTokenEncoders()
	c, info := usageReplayInfo(t, 9630, "")
	info.IsStream = true
	a := &dify.Adaptor{}
	a.Init(info)
	raw := "data: " + `{"event":"message","answer":"hi"}` + "\n\ndata: " + `{"event":"message_end","metadata":{"usage":{"total_tokens":50}}}` + "\n\n"
	u, apiErr := a.DoResponse(c, usageReplayResponse(raw), info)
	require.Nil(t, apiErr)
	usage := u.(*dto.Usage)
	service.PostTextConsumeQuota(c, info, usage, nil)
	service.PostTextConsumeQuota(c, info, usage, nil)
	assertUsageReplayBalance(t, info, 42) // 38 input + existing estimate 2 output at ratio 2.
	require.Equal(t, 50, usage.TotalTokens)
}

func TestCohereRejectedTextSettlement(t *testing.T) {
	service.InitTokenEncoders()
	for _, tc := range []struct {
		name, counts           string
		partial, short, reject bool
		prompt, completion     int
	}{
		{"error_first", `{}`, false, false, true, 38, 0},
		{"error_partial", `{}`, true, false, true, 38, 2},
		{"short_first", `{}`, false, true, true, 38, 0},
		{"short_partial", `{}`, true, true, true, 38, 2},
		{"missing_output", `{"input_tokens":100}`, true, false, false, 100, 2},
		{"reported_zero", `{"input_tokens":0,"output_tokens":0}`, true, false, false, 0, 0},
		{"reported_positive", `{"input_tokens":100,"output_tokens":7}`, true, false, false, 100, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9792, "")
			info.IsStream = true
			recorder := usageReplayRecorder(c)
			c.Writer = &replayCohereWriter{ResponseWriter: c.Writer, closed: make(chan bool)}
			if tc.reject {
				c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: tc.short}
			}
			a := &cohere.Adaptor{}
			a.Init(info)
			raw := ""
			if tc.partial {
				raw = `{"text":"hi"}` + "\n"
			}
			if tc.reject {
				raw += `{"text":"` + strings.Repeat("undelivered ", 128) + `"}` + "\n"
			}
			raw += `{"is_finished":true,"text":"undelivered terminal text","finish_reason":"COMPLETE","response":{"meta":{"billed_units":` + tc.counts + `}}}` + "\n"
			u, apiErr := a.DoResponse(c, usageReplayResponse(raw), info)
			require.Nil(t, apiErr, "preserve the existing Cohere soft-error return")
			usage := u.(*dto.Usage)
			service.PostTextConsumeQuota(c, info, usage, nil)
			service.PostTextConsumeQuota(c, info, usage, nil)
			assertUsageReplayBalance(t, info, tc.prompt+tc.completion*2)
			require.Equal(t, tc.prompt, usage.PromptTokens)
			require.Equal(t, tc.completion, usage.CompletionTokens)
			if !tc.reject {
				require.NotContains(t, recorder.Body.String(), "undelivered terminal text")
			}
		})
	}
}

func TestCohereMetadataFinishFailurePreservesReportedUsage(t *testing.T) {
	service.InitTokenEncoders()
	for _, short := range []bool{false, true} {
		for _, tc := range []struct {
			name, counts       string
			prompt, completion int
		}{
			{"reported", `{"input_tokens":1000,"output_tokens":999}`, 1000, 999},
			{"zero", `{"input_tokens":0,"output_tokens":0}`, 0, 0},
			{"missing_output", `{"input_tokens":1000}`, 1000, 2},
		} {
			t.Run(fmt.Sprintf("short=%t/%s", short, tc.name), func(t *testing.T) {
				usageReplayRatios(t)
				c, info := usageReplayInfo(t, 9798, "")
				info.IsStream = true
				c.Writer = &replayCohereWriter{ResponseWriter: c.Writer, closed: make(chan bool)}
				writer := &replayConvertedWriter{ResponseWriter: c.Writer, short: short, marker: `"finish_reason":"stop"`}
				c.Writer = writer
				a := &cohere.Adaptor{}
				a.Init(info)
				raw := `{"text":"hi"}` + "\n" + `{"is_finished":true,"finish_reason":"COMPLETE","response":{"meta":{"billed_units":` + tc.counts + `}}}` + "\n"
				u, apiErr := a.DoResponse(c, usageReplayResponse(raw), info)
				require.Nil(t, apiErr, "retain legacy soft-error return")
				require.True(t, writer.failed, "the metadata-only finish must actually fail its write")
				usage := u.(*dto.Usage)
				service.PostTextConsumeQuota(c, info, usage, nil)
				service.PostTextConsumeQuota(c, info, usage, nil)
				assertUsageReplayBalance(t, info, tc.prompt+tc.completion*2)
				require.Equal(t, tc.prompt, usage.PromptTokens)
				require.Equal(t, tc.completion, usage.CompletionTokens)
			})
		}
	}
}

func TestDifyRejectedTextSettlement(t *testing.T) {
	service.InitTokenEncoders()
	for _, short := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			for _, counts := range []string{"missing", "zero", "reported"} {
				t.Run(fmt.Sprintf("short=%t/partial=%t/%s", short, partial, counts), func(t *testing.T) {
					usageReplayRatios(t)
					c, info := usageReplayInfo(t, 9790, "")
					info.IsStream = true
					c.Writer = &replayConvertedWriter{ResponseWriter: c.Writer, short: short}
					a := &dify.Adaptor{}
					a.Init(info)
					raw, prompt, output := "", 38, 0
					if partial {
						raw = "data: " + `{"event":"message","answer":"hi"}` + "\n\n"
						output = 2
					}
					raw += "data: " + `{"event":"message","answer":"` + strings.Repeat("undelivered ", 128) + `"}` + "\n\n"
					raw += "data: " + `{"event":"message","answer":"ignored after failure"}` + "\n\n"
					facts := `{}`
					if counts == "zero" {
						facts, prompt, output = `{"prompt_tokens":100,"completion_tokens":0}`, 100, 0
					} else if counts == "reported" {
						facts, prompt, output = `{"prompt_tokens":100,"completion_tokens":7}`, 100, 7
					}
					raw += "data: " + `{"event":"message_end","metadata":{"usage":` + facts + `}}` + "\n\n"
					u, apiErr := a.DoResponse(c, usageReplayResponse(raw), info)
					require.Nil(t, apiErr, "preserve Dify's existing soft-error return")
					usage := u.(*dto.Usage)
					service.PostTextConsumeQuota(c, info, usage, nil)
					service.PostTextConsumeQuota(c, info, usage, nil)
					assertUsageReplayBalance(t, info, prompt+output*2)
					require.Equal(t, prompt, usage.PromptTokens)
					require.Equal(t, output, usage.CompletionTokens)
					require.True(t, info.StreamStatus.HasErrors())
				})
			}
		}
	}
}

func TestGeminiFileDataFallbackSettlement(t *testing.T) {
	oldCount, oldMedia := constant.CountToken, constant.GetMediaToken
	constant.CountToken, constant.GetMediaToken = true, false
	t.Cleanup(func() { constant.CountToken, constant.GetMediaToken = oldCount, oldMedia })
	service.InitTokenEncoders()
	for i, tool := range []bool{false, true} {
		t.Run(fmt.Sprint(tool), func(t *testing.T) {
			c, info := usageReplayInfo(t, 9640+i, "")
			info.RelayFormat, info.FinalRequestRelayFormat = types.RelayFormatGemini, types.RelayFormatGemini
			c.Set(string(constant.ContextKeyOriginalModel), "gemini-test")
			part := `{"fileData":{"mimeType":"image/png","fileUri":"https://example.invalid/media"}}`
			text := ""
			if tool {
				part = `{"functionResponse":{"name":"lookup","response":{},"parts":[` + part + `]}}`
				text = "lookup"
			}
			var request dto.GeminiChatRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[`+part+`]}]}`, &request))
			estimate, err := service.EstimateRequestToken(c, request.GetTokenCountMeta(), info)
			require.NoError(t, err)
			want := 520 + service.CountTextToken(text, "gemini-test")
			require.Equal(t, want, estimate)
			info.SetEstimatePromptTokens(estimate)
			u, apiErr := gemini.GeminiChatHandler(c, info, usageReplayResponse(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":0}}`))
			require.Nil(t, apiErr)
			service.PostTextConsumeQuota(c, info, u, nil)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, want)
		})
	}
}

func TestGeminiURLImageAliasEstimateAndSettlement(t *testing.T) {
	oldCount, oldMedia, oldNonStream := constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream
	oldFetch, oldWorker := *system_setting.GetFetchSetting(), system_setting.WorkerUrl
	constant.CountToken, constant.GetMediaToken = true, true
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{EnableSSRFProtection: false}
	system_setting.WorkerUrl = ""
	t.Cleanup(func() {
		constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = oldCount, oldMedia, oldNonStream
		*system_setting.GetFetchSetting(), system_setting.WorkerUrl = oldFetch, oldWorker
	})
	service.InitHttpClient()
	service.InitTokenEncoders()
	var downloads atomic.Int32
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(imageServer.Close)
	id := 9720
	for _, modelName := range []string{"gpt-4o", "gemini-test"} {
		for _, tool := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				for _, countNonStream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/tool=%t/stream=%t/local_nonstream=%t", modelName, tool, stream, countNonStream), func(t *testing.T) {
						id++
						constant.GetMediaTokenNotStream = countNonStream
						c, info := usageReplayInfo(t, id, "")
						info.RelayFormat, info.FinalRequestRelayFormat, info.IsStream = types.RelayFormatGemini, types.RelayFormatGemini, stream
						c.Set(string(constant.ContextKeyOriginalModel), modelName)
						part := `{"fileData":{"mimeType":"image/png","fileUri":"` + imageServer.URL + `/image"}}`
						text := ""
						if tool {
							part = `{"functionResponse":{"name":"lookup","response":{},"parts":[` + part + `]}}`
							text = "lookup"
						}
						var request dto.GeminiChatRequest
						require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[`+part+`]}]}`, &request))
						before := downloads.Load()
						estimate, err := service.EstimateRequestToken(c, request.GetTokenCountMeta(), info)
						require.Equal(t, before, downloads.Load(), "Gemini URL estimation must not fetch the image")
						require.NoError(t, err)
						want := 520 + service.CountTextToken(text, modelName)
						require.Equal(t, want, estimate)
						info.SetEstimatePromptTokens(estimate)
						raw := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":0}}`
						var usage *dto.Usage
						var apiErr *types.MaxAPIError
						if stream {
							usage, apiErr = gemini.GeminiChatStreamHandler(c, info, usageReplayResponse("data: "+raw+"\n\n"))
						} else {
							usage, apiErr = gemini.GeminiChatHandler(c, info, usageReplayResponse(raw))
						}
						require.Nil(t, apiErr)
						service.PostTextConsumeQuota(c, info, usage, nil)
						service.PostTextConsumeQuota(c, info, usage, nil)
						assertUsageReplayBalance(t, info, want)
					})
				}
			}
		}
	}
	t.Run("other_formats_keep_local_image_load", func(t *testing.T) {
		c, info := usageReplayInfo(t, 9740, "")
		info.IsStream = true
		c.Set(string(constant.ContextKeyOriginalModel), "gpt-4o")
		before := downloads.Load()
		_, err := service.EstimateRequestToken(c, &types.TokenCountMeta{Files: []*types.FileMeta{{FileType: types.FileTypeImage, Source: types.NewURLFileSource(imageServer.URL + "/image")}}}, info)
		require.Error(t, err)
		require.EqualValues(t, before+1, downloads.Load())
	})
}

func TestResponsesImageOnlyMissingInputSettlement(t *testing.T) {
	for i, reported := range []bool{false, true} {
		t.Run(fmt.Sprint(reported), func(t *testing.T) {
			c, info := usageReplayInfo(t, 9650+i, "")
			info.RelayMode, info.RelayFormat, info.IsStream = relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses, true
			counts := `"output_tokens":0`
			prompt := 38
			if reported {
				counts += `,"input_tokens":0`
				prompt = 0
			}
			raw := "data: " + `{"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","status":"completed","result":"synthetic-image","quality":"low","size":"1024x1024"}],"usage":{` + counts + `}}}` + "\n\n"
			u, apiErr := openai.OaiResponsesStreamHandler(c, info, usageReplayResponse(raw))
			require.Nil(t, apiErr)
			service.PostTextConsumeQuota(c, info, u, nil)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 5500+prompt) // Existing USD .011 image call + input at ratio 1.
			require.Equal(t, prompt, u.PromptTokens)
			require.Zero(t, u.CompletionTokens)
		})
	}
}

func TestUsageCorrectionClaudeCallerPartialSettlement(t *testing.T) {
	for i, zero := range []bool{false, true} {
		t.Run(fmt.Sprint(zero), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9540+i, "")
			service.InitHttpClient()
			input, output, want := 100, 22, 144 // 100*1 + 22*2, independent of the reserved 1000.
			if zero {
				input, output, want = 0, 0, 0
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"test\",\"model\":\"audit-model\",\"usage\":{\"input_tokens\":%d}}}\n\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"delivered\"}}\n\n"+
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":%d}}\n\n", input, output)
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close() // Abnormal HTTP EOF after complete SSE usage frames.
				}
			}))
			t.Cleanup(upstream.Close)
			c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeAnthropic)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.RelayMode, info.RelayFormat, info.IsStream = relayconstant.RelayModeUnknown, types.RelayFormatClaude, true
			info.Request = &dto.ClaudeRequest{Model: "audit-model", Stream: common.GetPointer(true), MaxTokens: common.GetPointer(uint(100))}
			apiErr := relay.ClaudeHelper(c, info)
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertUsageReplayBalance(t, info, want)
			var settlements []model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).Find(&settlements).Error)
			require.Len(t, settlements, 1, "partial settlement and failure refund must share one finalize operation")
		})
	}
}

func TestUsageCorrectionChatLateUsageSettlement(t *testing.T) {
	for i, zero := range []bool{false, true} {
		t.Run(fmt.Sprint(zero), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9550+i, "")
			service.InitHttpClient()
			input, output, want := 100, 22, 144
			if zero {
				input, output, want = 0, 0, 0
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"delivered\"}}]}\n\n"+
					"data: {\"error\":{\"message\":\"synthetic terminal failure\"}}\n\n"+
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", input, output, input+output)
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.IsStream = true
			info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
			apiErr := relay.TextHelper(c, info)
			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), "synthetic terminal failure")
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertUsageReplayBalance(t, info, want)
		})
	}
}

func TestClaudeIncompleteEOFCallerSettlesOnce(t *testing.T) {
	for i, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9560+i, "")
			service.InitHttpClient()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+`{"type":"message_start","message":{"model":"audit-model","usage":{"input_tokens":100}}}`+"\n\ndata: "+
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}`+"\n\ndata: "+
					`{"type":"message_delta","usage":{"output_tokens":22}}`+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeAnthropic)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.RelayFormat, info.IsStream = format, true
			var apiErr *types.MaxAPIError
			if format == types.RelayFormatClaude {
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info.Request = &dto.ClaudeRequest{Model: "audit-model", Stream: common.GetPointer(true), MaxTokens: common.GetPointer(uint(100))}
				apiErr = relay.ClaudeHelper(c, info)
			} else {
				info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
				apiErr = relay.TextHelper(c, info)
			}
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertUsageReplayBalance(t, info, 144) // 100*1 + 22*2, not the reserved 1000.
			var log model.Log
			require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).First(&log).Error)
			var projected struct {
				StreamStatus struct {
					Status    string `json:"status"`
					EndReason string `json:"end_reason"`
				} `json:"stream_status"`
			}
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &projected))
			require.Equal(t, "error", projected.StreamStatus.Status)
			require.Equal(t, "eof", projected.StreamStatus.EndReason)
		})
	}
}

func TestResponsesToolOutputInlineFileEstimateAndSettlement(t *testing.T) {
	oldCount, oldMedia := constant.CountToken, constant.GetMediaToken
	constant.CountToken, constant.GetMediaToken = true, false
	t.Cleanup(func() { constant.CountToken, constant.GetMediaToken = oldCount, oldMedia })
	service.InitTokenEncoders()
	for i, tc := range []struct {
		name, field, value string
		url                bool
	}{
		{"raw_base64", "file_data", "JVBERi0xLjQK", false},
		{"data_uri", "file_data", "data:application/pdf;base64,JVBERi0xLjQK", false},
		{"existing_url", "file_url", "https://example.invalid/document.pdf", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := usageReplayInfo(t, 9570+i, "")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			var request dto.OpenAIResponsesRequest
			raw := `{"input":[{"type":"function_call_output","call_id":"call_file","output":[{"type":"input_file","filename":"document.pdf","` + tc.field + `":"` + tc.value + `"}]}]}`
			require.NoError(t, common.UnmarshalJsonStr(raw, &request))
			meta := request.GetTokenCountMeta()
			require.Len(t, meta.Files, 1)
			require.Equal(t, types.FileTypeFile, meta.Files[0].FileType)
			require.Equal(t, tc.url, meta.Files[0].Source.IsURL())
			require.Equal(t, tc.value, meta.Files[0].Source.GetRawData())
			withoutFile := *meta
			withoutFile.Files = nil
			textOnly, err := service.EstimateRequestToken(c, &withoutFile, info)
			require.NoError(t, err)
			require.Equal(t, 3, textOnly) // Existing Chat-format envelope estimate.
			estimate, err := service.EstimateRequestToken(c, meta, info)
			require.NoError(t, err)
			require.Equal(t, 4099, estimate) // 3 envelope + existing 4096-per-file heuristic.
			info.SetEstimatePromptTokens(estimate)
			u, apiErr := openai.OaiResponsesToChatHandler(c, info, usageReplayResponse(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible"}]}],"usage":{"output_tokens":0}}`))
			require.Nil(t, apiErr)
			service.PostTextConsumeQuota(c, info, u, nil)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 4099)
		})
	}
}

func TestClaudeEmptyEOFRetryCallerSettlesOnlySuccess(t *testing.T) {
	for i, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9580+i, "")
			service.InitHttpClient()
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if calls.Add(1) == 1 {
					return // Empty EOF before headers or content reach the downstream writer.
				}
				_, _ = fmt.Fprint(w, "data: "+`{"type":"message_start","message":{"model":"audit-model","usage":{"input_tokens":100}}}`+"\n\ndata: "+
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"second attempt"}}`+"\n\ndata: "+
					`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":22}}`+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeAnthropic)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.RelayFormat, info.IsStream = format, true
			if format == types.RelayFormatClaude {
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info.Request = &dto.ClaudeRequest{Model: "audit-model", Stream: common.GetPointer(true), MaxTokens: common.GetPointer(uint(100))}
			} else {
				info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
			}
			call := func() *types.MaxAPIError {
				if format == types.RelayFormatClaude {
					return relay.ClaudeHelper(c, info)
				}
				return relay.TextHelper(c, info)
			}
			firstErr := call()
			require.NotNil(t, firstErr)
			require.False(t, types.IsSkipRetryError(firstErr))
			require.False(t, c.Writer.Written())
			info.RetryIndex++
			require.Nil(t, call())
			require.EqualValues(t, 2, calls.Load())
			service.HandleFailedBilling(c, info, firstErr)
			service.HandleFailedBilling(c, info, firstErr)
			assertUsageReplayBalance(t, info, 144)
		})
	}
}

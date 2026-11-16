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
	"github.com/MAX-API-Next/MAX-API/relay/channel/dify"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type replayConvertedWriter struct {
	gin.ResponseWriter
	short bool
}

func (w *replayConvertedWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "undelivered") {
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

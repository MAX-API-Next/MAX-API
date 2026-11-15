package openai

import (
	"errors"
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
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func correctionContext(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	service.InitTokenEncoders()
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.Path2RelayMode(path), RelayFormat: types.RelayFormatOpenAI, DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{SupportStreamOptions: true, UpstreamModelName: "gpt-test"}}
	info.SetEstimatePromptTokens(38)
	return c, w, info
}

func TestUsageCorrectionChatLateUsage(t *testing.T) {
	for _, counts := range []string{`{"prompt_tokens":100,"completion_tokens":22,"total_tokens":122}`, `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`} {
		t.Run(counts, func(t *testing.T) {
			c, w, info := correctionContext(t, "/v1/chat/completions")
			info.IsStream = true
			raw := "data: " + `{"choices":[{"delta":{"content":"delivered text"}}]}` + "\n\ndata: " +
				`{"error":{"type":"server_error","message":"synthetic terminal failure"}}` + "\n\ndata: " +
				`{"choices":[{"delta":{"content":"ignored after error"}}]}` + "\n\ndata: " +
				`{"choices":[],"usage":` + counts + `}` + "\n\ndata: [DONE]\n\n"
			u, apiErr := OaiStreamHandler(c, info, correctionResponse(raw))
			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), "synthetic terminal failure")
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotNil(t, u)
			if strings.Contains(counts, ":100") {
				require.Equal(t, 100, u.PromptTokens)
				require.Equal(t, 22, u.CompletionTokens)
			} else {
				require.Zero(t, u.PromptTokens)
				require.Zero(t, u.CompletionTokens)
			}
			require.NotContains(t, w.Body.String(), "ignored after error")
			require.NotContains(t, w.Body.String(), "[DONE]")
		})
	}
}

func TestUsageCorrectionChatErrorDrainIsBounded(t *testing.T) {
	c, _, info := correctionContext(t, "/v1/chat/completions")
	info.IsStream = true
	r, w := io.Pipe()
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	go func() {
		_, _ = io.WriteString(w, "data: "+`{"choices":[{"delta":{"content":"delivered text"}}]}`+"\n\ndata: "+`{"error":{"message":"synthetic terminal failure"}}`+"\n\n")
		// The upstream never sends usage or closes; the grace period must close it.
	}()
	started := time.Now()
	u, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: 200, Body: r})
	require.NotNil(t, apiErr)
	require.NotNil(t, u)
	require.Less(t, time.Since(started), 3*time.Second)
	require.Positive(t, u.CompletionTokens)
}

func correctionResponse(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
}

func TestUsageCorrectionChatRequiresCompletionEvidence(t *testing.T) {
	for _, ending := range []string{"missing", "finish_only", "done_only", "zero_missing"} {
		t.Run(ending, func(t *testing.T) {
			c, w, info := correctionContext(t, "/v1/chat/completions")
			info.IsStream = true
			raw := "data: " + `{"choices":[{"delta":{"content":"delivered text"}}]}` + "\n\n"
			counts := `{"prompt_tokens":100,"completion_tokens":22}`
			if ending == "zero_missing" {
				counts = `{"prompt_tokens":0,"completion_tokens":0}`
			}
			raw += "data: " + `{"choices":[],"usage":` + counts + `}` + "\n\n"
			if ending == "finish_only" {
				raw += "data: " + `{"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"
			}
			if ending == "done_only" {
				raw += "data: [DONE]\n\n"
			}
			u, apiErr := OaiStreamHandler(c, info, correctionResponse(raw))
			require.NotNil(t, u)
			if ending == "missing" || ending == "zero_missing" {
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
				require.True(t, info.StreamStatus.IsAbnormalEnd())
				require.NotContains(t, w.Body.String(), "[DONE]")
				require.NotNil(t, u.BillingUsage)
			} else {
				require.Nil(t, apiErr, "finish_reason without DONE remains compatible")
			}
			if ending == "zero_missing" {
				require.Zero(t, u.PromptTokens)
				require.Zero(t, u.CompletionTokens)
			} else {
				require.Equal(t, 100, u.PromptTokens)
				require.Equal(t, 22, u.CompletionTokens)
			}
		})
	}
}

func TestUsageCorrectionChatChoiceCompletion(t *testing.T) {
	for _, ending := range []string{"partial", "missing_choice", "duplicate", "complete", "done"} {
		t.Run(ending, func(t *testing.T) {
			c, _, info := correctionContext(t, "/v1/chat/completions")
			info.IsStream = true
			info.Request = &dto.GeneralOpenAIRequest{N: common.GetPointer(2)}
			raw := "data: " + `{"choices":[{"index":0,"delta":{"content":"first"}}]}` + "\n\n"
			if ending != "missing_choice" {
				raw += "data: " + `{"choices":[{"index":1,"delta":{"content":"second"}}]}` + "\n\n"
			}
			raw += "data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}` + "\n\n"
			if ending == "duplicate" {
				raw += "data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
			}
			if ending == "complete" {
				raw += "data: " + `{"choices":[{"index":1,"delta":{},"finish_reason":"length"}]}` + "\n\n"
			}
			if ending == "done" {
				raw += "data: [DONE]\n\n"
			}
			u, apiErr := OaiStreamHandler(c, info, correctionResponse(raw))
			require.NotNil(t, u)
			require.Equal(t, 4, u.PromptTokens)
			require.Equal(t, 2, u.CompletionTokens)
			if ending == "complete" || ending == "done" {
				require.Nil(t, apiErr)
			} else {
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
				require.True(t, info.StreamStatus.IsAbnormalEnd())
			}
		})
	}
}

func TestUsageCorrectionChatRetainsProviderCache(t *testing.T) {
	for _, channel := range []int{constant.ChannelTypeMoonshot, constant.ChannelTypeOpenAI, constant.ChannelTypeZhipu_v4, constant.ChannelTypeDeepSeek} {
		for _, ending := range []string{"error", "trailing", "trailing_cache_only", "updated", "vendor_zero", "canonical_zero", "parent_zero", "cache_only"} {
			t.Run(fmt.Sprintf("%d/%s", channel, ending), func(t *testing.T) {
				c, w, info := correctionContext(t, "/v1/chat/completions")
				info.IsStream, info.ChannelType = true, channel
				frame := func(cached int, canonicalZero, cacheOnly bool) string {
					counts := map[string]any{}
					if !cacheOnly {
						counts["prompt_tokens"], counts["completion_tokens"] = 100, 10
					}
					choice := map[string]any{"delta": map[string]any{"content": "partial output"}}
					body := map[string]any{"usage": counts, "choices": []any{choice}}
					switch channel {
					case constant.ChannelTypeMoonshot:
						choice["usage"] = map[string]any{"cached_tokens": cached}
					case constant.ChannelTypeOpenAI:
						body["timings"] = map[string]any{"cache_n": cached}
					case constant.ChannelTypeZhipu_v4:
						counts["cached_tokens"] = cached
					case constant.ChannelTypeDeepSeek:
						counts["prompt_cache_hit_tokens"] = cached
					}
					if canonicalZero {
						counts["prompt_tokens_details"] = map[string]any{"cached_tokens": 0}
					}
					if cacheOnly && (channel == constant.ChannelTypeMoonshot || channel == constant.ChannelTypeOpenAI) {
						delete(body, "usage")
					}
					raw, err := common.Marshal(body)
					require.NoError(t, err)
					return string(raw)
				}
				frames := []string{frame(90, false, ending == "cache_only")}
				cached, prompt, output := 90, 100, 10
				switch ending {
				case "updated":
					cached = 80
					frames = append(frames, frame(80, false, false))
				case "vendor_zero":
					cached = 0
					frames = append(frames, frame(0, false, false))
				case "canonical_zero":
					cached = 0
					frames = append(frames, frame(90, true, false))
				case "parent_zero":
					cached, prompt, output = 0, 0, 0
					frames = append(frames, `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`)
				case "cache_only":
					prompt, output = 90, service.EstimateTokenByModel(info.UpstreamModelName, "partial output")
				}
				frames = append(frames, `{"error":{"type":"overloaded_error","message":"synthetic failure"}}`)
				if ending == "trailing" {
					frames = append(frames, `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`)
				}
				if ending == "trailing_cache_only" {
					cached = 80
					frames = append(frames, frame(80, false, true))
				}
				u, apiErr := OaiStreamHandler(c, info, correctionResponse("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"))
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr))
				require.Equal(t, prompt, u.PromptTokens)
				require.Equal(t, output, u.CompletionTokens)
				require.Equal(t, cached, u.PromptTokensDetails.CachedTokens)
				require.Equal(t, cached, u.BillingUsage.OpenAIUsage.PromptTokensDetails.CachedTokens)
				require.NotContains(t, w.Body.String(), "synthetic failure")
			})
		}
	}
	for _, short := range []bool{false, true} {
		c, _, info := correctionContext(t, "/v1/chat/completions")
		failed, _ := gin.CreateTestContext(&rejectedChatResponseWriter{header: make(http.Header), short: short})
		c.Writer = failed.Writer
		info.IsStream, info.ChannelType = true, constant.ChannelTypeMoonshot
		u, apiErr := OaiStreamHandler(c, info, correctionResponse("data: "+`{"choices":[{"delta":{"content":"never delivered"},"usage":{"cached_tokens":90}}],"usage":{"prompt_tokens":100,"completion_tokens":10}}`+"\n\n"))
		require.NotNil(t, apiErr)
		require.Nil(t, u, "rejected output must not enter cache or billing evidence")
	}
}

func TestUsageCorrectionResponsesStreamCounterPresence(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, ending := range []string{"provider", "eof", "timeout", "complete"} {
			for _, tc := range []struct {
				name, counts       string
				prompt, completion int
			}{
				{"input_zero", `{"input_tokens":0}`, 0, 1},
				{"output_zero", `{"output_tokens":0}`, 38, 0},
				{"both_zero", `{"input_tokens":0,"output_tokens":0}`, 0, 0},
				{"late_input_zero", `{"input_tokens":0}`, 0, 2},
			} {
				t.Run(string(format)+"/"+ending+"/"+tc.name, func(t *testing.T) {
					c, _, info := correctionContext(t, "/v1/chat/completions")
					info.IsStream, info.RelayFormat = true, format
					raw := ""
					if tc.name == "late_input_zero" {
						raw = "data: " + `{"type":"response.created","response":{"usage":{"input_tokens":4,"output_tokens":2}}}` + "\n\n"
					}
					raw += "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.in_progress","response":{"usage":` + tc.counts + `}}` + "\n\n"
					if ending == "provider" {
						raw += "data: " + `{"type":"response.failed","response":{"status":"failed","error":{"message":"synthetic failure"}}}` + "\n\n"
					}
					if ending == "complete" {
						raw += "data: " + `{"type":"response.completed","response":{"status":"completed"}}` + "\n\n"
					}
					resp := correctionResponse(raw)
					if ending == "timeout" {
						constant.StreamingTimeout = 1
						r, w := io.Pipe()
						t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
						go func() { _, _ = io.WriteString(w, raw) }()
						resp.Body = r
					}
					handler := OaiResponsesToChatStreamHandler
					if format == types.RelayFormatOpenAIResponses {
						handler = OaiResponsesStreamHandler
					}
					u, apiErr := handler(c, info, resp)
					if ending == "complete" {
						require.Nil(t, apiErr)
					} else {
						require.NotNil(t, apiErr)
					}
					require.NotNil(t, u)
					require.Equal(t, tc.prompt, u.PromptTokens)
					wantOutput := tc.completion
					if tc.name == "input_zero" && format != types.RelayFormatOpenAIResponses {
						wantOutput = 2
					} // Preserve each handler's existing estimator.
					require.Equal(t, wantOutput, u.CompletionTokens)
					require.Equal(t, tc.prompt+wantOutput, u.TotalTokens)
					require.NotNil(t, u.BillingUsage)
					require.Equal(t, tc.name == "both_zero" || tc.name == "late_input_zero", u.BillingUsage.TokenCountsReported)
				})
			}
		}
	}
}

func TestUsageCorrectionResponsesStandardFieldsPrecedeChatAliases(t *testing.T) {
	for _, handler := range []func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.MaxAPIError){OaiResponsesStreamHandler, OaiResponsesToChatStreamHandler} {
		for _, zero := range []bool{false, true} {
			c, _, info := correctionContext(t, "/v1/chat/completions")
			info.IsStream = true
			counts := `{"input_tokens":100,"output_tokens":50,"prompt_tokens":0,"completion_tokens":0,"input_tokens_details":{"cached_tokens":1},"prompt_tokens_details":{"cached_tokens":0}}`
			if zero {
				counts = `{"input_tokens":0,"output_tokens":0,"prompt_tokens":100,"completion_tokens":50,"input_tokens_details":{"cached_tokens":0},"prompt_tokens_details":{"cached_tokens":1}}`
			}
			raw := "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","usage":` + counts + `}}` + "\n\n"
			u, apiErr := handler(c, info, correctionResponse(raw))
			require.Nil(t, apiErr)
			if zero {
				require.Zero(t, u.PromptTokens)
				require.Zero(t, u.CompletionTokens)
				require.Zero(t, u.PromptTokensDetails.CachedTokens)
			} else {
				require.Equal(t, 100, u.PromptTokens)
				require.Equal(t, 50, u.CompletionTokens)
				require.Equal(t, 1, u.PromptTokensDetails.CachedTokens)
			}
		}
	}
}

func TestUsageCorrectionResponsesEmptyDetailsKeepPresentAlias(t *testing.T) {
	c, _, info := correctionContext(t, "/v1/chat/completions")
	info.IsStream = true
	raw := "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":100,"output_tokens":50,"input_tokens_details":{},"prompt_tokens_details":{"cached_tokens":1}}}}` + "\n\n"
	u, apiErr := OaiResponsesToChatStreamHandler(c, info, correctionResponse(raw))
	require.Nil(t, apiErr)
	require.Equal(t, 1, u.PromptTokensDetails.CachedTokens, "an empty details object does not report cached_tokens=0")
}

func TestUsageCorrectionConvertedTerminalFreezesSnapshot(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		for _, tail := range []string{
			`{"type":"response.in_progress","response":{"usage":{"input_tokens":0}}}`,
			`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":999,"output_tokens":999}}}`,
			`{"type":"response.output_text.delta","delta":"late output"}`,
			`{"type":"response.failed","response":{"status":"failed","error":{"message":"late failure"}}}`,
			`{broken`,
		} {
			t.Run(string(format)+"/"+tail, func(t *testing.T) {
				c, w, info := correctionContext(t, "/v1/chat/completions")
				info.IsStream, info.RelayFormat = true, format
				info.IsFirstRequest = true
				raw := "data: " + `{"type":"response.output_text.delta","delta":"accepted output"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":100,"output_tokens":22}}}` + "\n\ndata: " + tail + "\n\n"
				u, apiErr := OaiResponsesToChatStreamHandler(c, info, correctionResponse(raw))
				require.Nil(t, apiErr)
				require.Equal(t, 100, u.PromptTokens)
				require.Equal(t, 22, u.CompletionTokens)
				require.NotNil(t, u.BillingUsage)
				require.Equal(t, 100, u.BillingUsage.OpenAIUsage.PromptTokens)
				require.NotContains(t, w.Body.String(), "late output")
			})
		}
	}
}

func TestUsageCorrectionResponsesPreservesTotalAndInputFloor(t *testing.T) {
	for _, mode := range []string{"nonstream", "converted", "native"} {
		for _, tc := range []struct {
			name, counts          string
			prompt, output, total int
		}{
			{"reported_total", `{"input_tokens":0,"total_tokens":100}`, 0, 2, 100},
			{"reported_zero_total", `{"input_tokens":0,"total_tokens":0}`, 0, 2, 0},
			{"nonzero_input_zero_total", `{"input_tokens":7,"total_tokens":0}`, 7, 2, 0},
			{"cached_subset", `{"output_tokens":0,"input_tokens_details":{"cached_tokens":1000}}`, 1000, 0, 1000},
			{"explicit_parent_zero", `{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":1000}}`, 0, 0, 0},
			{"output_subset", `{"input_tokens":100,"output_tokens_details":{"reasoning_tokens":1000}}`, 100, 1000, 1100},
			{"explicit_output_zero", `{"input_tokens":100,"output_tokens":0,"output_tokens_details":{"reasoning_tokens":1000}}`, 100, 0, 100},
			{"explicit_output_positive", `{"input_tokens":100,"output_tokens":7,"output_tokens_details":{"reasoning_tokens":1000}}`, 100, 7, 107},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				c, _, info := correctionContext(t, "/v1/chat/completions")
				raw := `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":` + tc.counts + `}`
				handler := OaiResponsesToChatHandler
				if mode != "nonstream" {
					info.IsStream = true
					raw = "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","usage":` + tc.counts + `}}` + "\n\n"
					handler = OaiResponsesToChatStreamHandler
					if mode == "native" {
						handler = OaiResponsesStreamHandler
					}
				}
				u, apiErr := handler(c, info, correctionResponse(raw))
				require.Nil(t, apiErr)
				output, total := tc.output, tc.total
				if mode == "native" && (tc.name == "reported_total" || tc.name == "reported_zero_total" || tc.name == "nonzero_input_zero_total") {
					output = 1
				}
				require.Equal(t, tc.prompt, u.PromptTokens)
				require.Equal(t, output, u.CompletionTokens)
				require.Equal(t, total, u.TotalTokens)
			})
		}
	}
}

func TestUsageCorrectionResponsesPresence(t *testing.T) {
	for _, tc := range []struct {
		name, usage        string
		prompt, completion int
		estimated          bool
	}{
		{"zero", `{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, 0, 0, false},
		{"reported", `{"input_tokens":100,"output_tokens":20,"total_tokens":120}`, 100, 20, false},
		{"input_zero", `{"input_tokens":0}`, 0, -1, true},
		{"output_zero", `{"output_tokens":0}`, 38, 0, true},
		{"empty", `{}`, 38, -1, true},
		{"null", `null`, 38, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, info := correctionContext(t, "/v1/chat/completions")
			raw := `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Visible text requiring nonzero output estimation"}]}],"usage":` + tc.usage + `}`
			usage, apiErr := OaiResponsesToChatHandler(c, info, correctionResponse(raw))
			require.Nil(t, apiErr)
			require.Equal(t, tc.prompt, usage.PromptTokens)
			if tc.completion < 0 {
				require.Positive(t, usage.CompletionTokens)
			} else {
				require.Equal(t, tc.completion, usage.CompletionTokens)
			}
			require.Equal(t, tc.estimated, usage.BillingUsage.Estimated)
			require.Equal(t, !tc.estimated, usage.BillingUsage.TokenCountsReported)
			require.Equal(t, dto.BillingUsageSourceOAIResponses, usage.BillingUsage.Source)
		})
	}
}

func TestUsageCorrectionConvertedChatFallback(t *testing.T) {
	for _, tc := range []struct {
		path   string
		format types.RelayFormat
	}{
		{"/v1/messages", types.RelayFormatClaude},
		{"/v1beta/models/synthetic:streamGenerateContent", types.RelayFormatGemini},
	} {
		t.Run(tc.path, func(t *testing.T) {
			c, w, info := correctionContext(t, tc.path)
			info.RelayFormat, info.IsStream = tc.format, true
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
			raw := "data: " + `{"choices":[{"index":0,"delta":{"content":"Visible converted content requiring nonzero output usage"}}]}` + "\n\ndata: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
			usage, apiErr := OaiStreamHandler(c, info, correctionResponse(raw))
			require.Nil(t, apiErr)
			require.Contains(t, w.Body.String(), "Visible converted content")
			require.Positive(t, usage.CompletionTokens)
		})
	}
}

type convertedUsageWriter struct{ gin.ResponseWriter }

func (w *convertedUsageWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "undelivered") {
		return 0, errors.New("synthetic converted write failure")
	}
	return w.ResponseWriter.Write(p)
}

func (w *convertedUsageWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func TestUsageCorrectionConvertedWritesGateEstimates(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, ending := range []string{"first_failure", "partial_failure", "last_payload"} {
			t.Run(string(format)+"/"+ending, func(t *testing.T) {
				c, w, info := correctionContext(t, "/v1/messages")
				c.Writer = &convertedUsageWriter{c.Writer}
				info.RelayFormat, info.IsStream = format, true
				info.EnableFirstResultTracking()
				info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
				frames := []string{}
				text := ""
				if ending != "first_failure" {
					text = "accepted"
					frames = append(frames, `{"choices":[{"index":0,"delta":{"content":"accepted"}}]}`)
				}
				if ending != "last_payload" {
					frames = append(frames, `{"choices":[{"index":0,"delta":{"content":"undelivered `+strings.Repeat("output ", 128)+`"}}]}`, `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
				}
				raw := "data: " + strings.Join(frames, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"
				u, apiErr := OaiStreamHandler(c, info, correctionResponse(raw))
				require.Nil(t, apiErr, "retain the legacy soft-error return contract")
				require.Equal(t, service.EstimateTokenByModel(info.UpstreamModelName, text), u.CompletionTokens)
				require.Equal(t, ending != "first_failure", info.HasRecordedChannelFirstResult())
				if ending != "last_payload" {
					require.True(t, info.StreamStatus.HasErrors())
				}
				require.Equal(t, strings.Count(text, "accepted"), strings.Count(w.Body.String(), "accepted"), "last payload must be forwarded exactly once")
			})
		}
	}
}

func TestUsageCorrectionTTSZeroSnapshot(t *testing.T) {
	c, _, info := correctionContext(t, "/v1/audio/speech")
	info.IsStream = true
	raw := "data: " + `{"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}` + "\n\ndata: " + `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}` + "\n\ndata: " + `{"usage":{}}` + "\n\ndata: [DONE]\n\n"
	usage := OpenaiTTSHandler(c, correctionResponse(raw), info)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.TotalTokens)
}

func TestUsageCorrectionTTSReportedTotal(t *testing.T) {
	for _, tc := range []struct {
		name, total, followup string
		prompt, want          int
	}{
		{"zero", `,"total_tokens":0`, `{}`, 5, 0},
		{"positive", `,"total_tokens":99`, `{}`, 5, 99},
		{"missing", "", `{}`, 5, 12},
		{"later_parent", `,"total_tokens":0`, `{"prompt_tokens":6}`, 6, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, info := correctionContext(t, "/v1/audio/speech")
			info.IsStream = true
			raw := "data: " + `{"usage":{"prompt_tokens":5,"completion_tokens":7` + tc.total + `}}` + "\n\ndata: " + `{"usage":` + tc.followup + `}` + "\n\ndata: [DONE]\n\n"
			u := OpenaiTTSHandler(c, correctionResponse(raw), info)
			require.Equal(t, tc.prompt, u.PromptTokens)
			require.Equal(t, 7, u.CompletionTokens)
			require.Equal(t, tc.want, u.TotalTokens)
		})
	}
}

func TestUsageCorrectionGeminiRequestsUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, supported := range []bool{false, true} {
			c, _, info := correctionContext(t, "/v1beta/models/synthetic:streamGenerateContent")
			info.IsStream, info.SupportStreamOptions = stream, supported
			info.ChannelType = constant.ChannelTypeOpenAI
			var request dto.GeminiChatRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, &request))
			adaptor := &Adaptor{}
			adaptor.Init(info)
			value, err := adaptor.ConvertGeminiRequest(c, info, &request)
			require.NoError(t, err)
			out, ok := value.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)
			if stream && supported {
				require.NotNil(t, out.StreamOptions)
				require.True(t, out.StreamOptions.IncludeUsage)
			} else {
				require.Nil(t, out.StreamOptions)
			}
		}
	}
}

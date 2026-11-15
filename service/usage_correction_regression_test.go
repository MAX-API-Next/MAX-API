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
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

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

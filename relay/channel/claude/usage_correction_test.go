package claude

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeMissingFinalCounterEstimatesDeliveredOutput(t *testing.T) {
	service.InitTokenEncoders()
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		for _, counter := range []string{"", `,"output_tokens":0`, `,"output_tokens":22`} {
			t.Run(string(format)+counter, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				text := strings.Repeat("delivered output ", 32)
				raw := "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":100,"output_tokens":1}}}` + "\n\ndata: " +
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}` + "\n\ndata: " +
					`{"type":"message_delta","usage":{"input_tokens":100` + counter + `}}` + "\n\ndata: " +
					`{"type":"error","error":{"type":"overloaded_error","message":"synthetic failure"}}` + "\n\n"
				u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
				require.NotNil(t, apiErr)
				require.NotNil(t, u)
				want := service.ResponseText2Usage(c, text, "claude-test", 100).CompletionTokens
				if counter == `,"output_tokens":0` {
					want = 0
				}
				if counter == `,"output_tokens":22` {
					want = 22
				}
				require.Greater(t, service.ResponseText2Usage(c, text, "claude-test", 100).CompletionTokens, 1)
				require.Equal(t, want, u.CompletionTokens)
			})
		}
	}
}

func TestClaudePartialToolEstimateMatchesClientPayload(t *testing.T) {
	service.InitTokenEncoders()
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			input := `{"value":"` + strings.Repeat("undelivered argument ", 128) + `"}`
			raw := "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":100}}}` + "\n\ndata: " +
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_test","name":"lookup","input":` + input + `}}` + "\n\ndata: " +
				`{"type":"error","error":{"type":"overloaded_error","message":"synthetic failure"}}` + "\n\n"
			u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
			require.NotNil(t, apiErr)
			require.NotNil(t, u)
			text := "lookup"
			if format == types.RelayFormatClaude {
				text += input
				require.Contains(t, w.Body.String(), "undelivered argument")
			} else {
				require.NotContains(t, w.Body.String(), "undelivered argument")
			}
			require.Equal(t, service.ResponseText2Usage(c, text, "claude-test", 100).CompletionTokens, u.CompletionTokens)
		})
	}
}

func TestClaudeOpaqueThinkingDelivery(t *testing.T) {
	service.InitTokenEncoders()
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		for _, block := range []string{"redacted", "signature", "thinking_start"} {
			for _, ending := range []string{"provider", "eof", "write", "empty"} {
				t.Run(fmt.Sprintf("%s/%s/%s", format, block, ending), func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(&correctionClaudeWriter{recorder})
					c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
					info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
					payload := "synthetic-opaque-ciphertext"
					if ending == "write" {
						payload = "undelivered-opaque-ciphertext"
					} else if ending == "empty" {
						payload = ""
					}
					frame := `{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"` + payload + `"}}`
					if block == "signature" {
						frame = `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"` + payload + `"}}`
					} else if block == "thinking_start" {
						frame = `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":"` + payload + `"}}`
					}
					raw := "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":100,"output_tokens":0}}}` + "\n\ndata: " + frame + "\n\n"
					if ending != "eof" {
						raw += "data: " + `{"type":"error","error":{"type":"overloaded_error","message":"synthetic failure"}}` + "\n\n"
					}
					u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
					if format != types.RelayFormatClaude || ending == "write" || ending == "empty" {
						require.Nil(t, u, "only native clients receive complete opaque output")
						return
					}
					require.Contains(t, recorder.Body.String(), payload)
					require.NotNil(t, u)
					require.Equal(t, 100, u.PromptTokens)
					require.Zero(t, u.CompletionTokens, "opaque ciphertext must not become estimated plaintext tokens")
					require.True(t, u.BillingUsage.InputTokensReported)
					require.False(t, u.BillingUsage.TokenCountsReported)
					require.True(t, dto.CloneBillingUsage(u.BillingUsage).InputTokensReported)
				})
			}
		}
	}
}

func TestClaudeInitialThinkingEstimate(t *testing.T) {
	service.InitTokenEncoders()
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		for _, signature := range []string{"", "synthetic-signature"} {
			t.Run(string(format)+"/"+signature, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				text := strings.Repeat("initial thinking ", 64)
				raw := "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":100}}}` + "\n\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"` + text + `","signature":"` + signature + `"}}` + "\n\ndata: " + `{"type":"error","error":{"type":"overloaded_error","message":"synthetic failure"}}` + "\n\n"
				u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
				require.NotNil(t, apiErr)
				if format != types.RelayFormatClaude {
					require.Nil(t, u)
					return
				}
				require.NotNil(t, u)
				require.Equal(t, service.EstimateTokenByModel(info.UpstreamModelName, text), u.CompletionTokens)
			})
		}
	}
}

type correctionClaudeWriter struct{ *httptest.ResponseRecorder }

func TestClaudeMissingInputEstimateExcludesReportedCache(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
		for _, input := range []string{"", `"input_tokens":0,`, `"input_tokens":7,`} {
			t.Run(string(format)+input, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				info.SetEstimatePromptTokens(38)
				raw := "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{` + input + `"cache_read_input_tokens":10,"cache_creation_input_tokens":8}}}` + "\n\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"accepted"}}` + "\n\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}` + "\n\n"
				u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
				require.Nil(t, apiErr)
				require.NotNil(t, u.BillingUsage)
				want := 20
				if input == `"input_tokens":0,` {
					want = 0
				}
				if input == `"input_tokens":7,` {
					want = 7
				}
				require.Equal(t, want, u.BillingUsage.ClaudeUsage.InputTokens)
				require.Equal(t, want+18, buildOpenAIStyleUsageFromClaudeUsage(u).PromptTokens)
			})
		}
	}
}

func (w *correctionClaudeWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "undelivered") {
		return len(p) / 2, errors.New("synthetic short write")
	}
	return w.ResponseRecorder.Write(p)
}

func TestUsageCorrectionClaudePartialError(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		for _, failure := range []string{"provider", "disconnect", "write", "metadata", "zero", "known"} {
			t.Run(string(format)+"/"+failure, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				var writer http.ResponseWriter = &correctionClaudeWriter{recorder}
				c, _ := gin.CreateTestContext(writer)
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				info.SetEstimatePromptTokens(38)
				raw := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"test\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":100}}}\n\n"
				if failure != "metadata" {
					raw += "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"some delivered output\"}}\n\n"
				}
				switch failure {
				case "known":
					raw += "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":22}}\n\n"
				case "zero":
					raw += "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0}}\n\n"
				case "write":
					raw += "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"undelivered\"}}\n\n"
				}
				var body io.ReadCloser
				if failure == "disconnect" {
					body = &claudeResponsesBrokenBody{strings.NewReader(raw)}
				} else {
					raw += "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"synthetic provider failure\"}}\n\n"
					body = io.NopCloser(strings.NewReader(raw))
				}
				u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: body}, info)
				require.NotNil(t, apiErr)
				require.True(t, types.IsSkipRetryError(apiErr), "written metadata also prevents replay")
				if failure == "metadata" {
					require.Nil(t, u, "metadata alone is not a billable partial response")
					return
				}
				require.NotNil(t, u)
				require.Equal(t, 100, u.PromptTokens)
				require.NotNil(t, u.BillingUsage)
				if failure == "known" {
					require.Equal(t, 22, u.CompletionTokens)
				} else if failure == "zero" {
					require.Zero(t, u.CompletionTokens)
				} else {
					require.Positive(t, u.CompletionTokens)
				}
				require.Equal(t, u.PromptTokens+u.CompletionTokens, u.TotalTokens)
				require.NotContains(t, recorder.Body.String(), "[DONE]")
			})
		}
	}
}

func TestClaudeEOFWithoutTerminalPreservesPartialUsage(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			raw := "data: " + `{"type":"message_start","message":{"id":"eof","model":"claude-test","usage":{"input_tokens":100}}}` + "\n\ndata: " +
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}` + "\n\ndata: " +
				`{"type":"message_delta","usage":{"output_tokens":22}}` + "\n\n"
			u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
			require.NotNil(t, apiErr, "usage counters alone do not finish a generation")
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotNil(t, u)
			require.Equal(t, 100, u.PromptTokens)
			require.Equal(t, 22, u.CompletionTokens)
			require.True(t, info.StreamStatus.IsAbnormalEnd())
			require.NotContains(t, recorder.Body.String(), "[DONE]")
		})
	}
}

func TestClaudeStreamTerminalAndEmptyEOFControls(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		for _, ending := range []string{"message_stop", "stop_reason", "metadata_only", "empty"} {
			t.Run(string(format)+"/"+ending, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				raw := ""
				if ending != "empty" {
					raw = "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":100}}}` + "\n\n"
				}
				if ending == "message_stop" || ending == "stop_reason" {
					raw += "data: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"finished"}}` + "\n\n"
					if ending == "message_stop" {
						raw += "data: " + `{"type":"message_delta","usage":{"output_tokens":22}}` + "\n\ndata: " + `{"type":"message_stop"}` + "\n\n"
					} else {
						raw += "data: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":22}}` + "\n\n"
					}
				}
				u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
				if ending == "empty" || ending == "metadata_only" {
					require.NotNil(t, apiErr)
					require.Nil(t, u)
					require.Equal(t, ending == "metadata_only", types.IsSkipRetryError(apiErr))
					require.NotContains(t, recorder.Body.String(), "[DONE]")
				} else {
					require.Nil(t, apiErr)
					require.Equal(t, 100, u.PromptTokens)
					require.Equal(t, 22, u.CompletionTokens)
					require.False(t, info.StreamStatus.IsAbnormalEnd())
				}
			})
		}
	}
}

func TestClaudeEmptyEOFRetryDoesNotKeepPriorFatalStatus(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: format, DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			u, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, info)
			require.Nil(t, u)
			require.NotNil(t, apiErr)
			require.False(t, types.IsSkipRetryError(apiErr))
			require.False(t, c.Writer.Written())
			info.RetryIndex++
			raw := "data: " + `{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":100}}}` + "\n\ndata: " +
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"second attempt"}}` + "\n\ndata: " +
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":22}}` + "\n\n"
			u, apiErr = ClaudeStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, info)
			require.Nil(t, apiErr)
			require.Equal(t, 22, u.CompletionTokens)
			require.False(t, info.StreamStatus.IsAbnormalEnd())
		})
	}
}

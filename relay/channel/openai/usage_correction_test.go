package openai

import (
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

func TestUsageCorrectionTTSZeroSnapshot(t *testing.T) {
	c, _, info := correctionContext(t, "/v1/audio/speech")
	info.IsStream = true
	raw := "data: " + `{"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}` + "\n\ndata: " + `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}` + "\n\ndata: " + `{"usage":{}}` + "\n\ndata: [DONE]\n\n"
	usage := OpenaiTTSHandler(c, correctionResponse(raw), info)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.TotalTokens)
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

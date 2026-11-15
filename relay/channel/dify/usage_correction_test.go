package dify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsageCorrectionDifyCounterPresence(t *testing.T) {
	service.InitTokenEncoders()
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	for _, tc := range []struct {
		name, counters     string
		prompt, completion int
	}{
		{"zero", `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`, 0, 0},
		{"reported", `{"prompt_tokens":100,"completion_tokens":22,"total_tokens":122}`, 100, 22},
		{"missing", `{}`, 38, -1},
		{"missing_input", `{"completion_tokens":22}`, 38, 22},
		{"input_zero", `{"prompt_tokens":0}`, 0, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{DisablePing: true, IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			info.SetEstimatePromptTokens(38)
			raw := "data: " + `{"event":"message","answer":"Visible Dify text for estimation"}` + "\n\ndata: " + `{"event":"message_end","metadata":{"usage":` + tc.counters + `}}` + "\n\n"
			usage, apiErr := difyStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))})
			require.Nil(t, apiErr)
			require.Equal(t, tc.prompt, usage.PromptTokens)
			if tc.completion < 0 {
				require.Positive(t, usage.CompletionTokens)
			} else {
				require.Equal(t, tc.completion, usage.CompletionTokens)
			}
			require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
		})
	}
}

func TestUsageCorrectionDifyReportedPartialTotal(t *testing.T) {
	service.InitTokenEncoders()
	for _, tc := range []struct {
		counts        string
		prompt, total int
	}{
		{`{"total_tokens":50}`, 38, 50},
		{`{"prompt_tokens":0,"total_tokens":10}`, 0, 10},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
		info.SetEstimatePromptTokens(38)
		raw := "data: " + `{"event":"message","answer":"hi"}` + "\n\ndata: " + `{"event":"message_end","metadata":{"usage":` + tc.counts + `}}` + "\n\n"
		u, apiErr := difyStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))})
		require.Nil(t, apiErr)
		require.Equal(t, tc.prompt, u.PromptTokens)
		require.Equal(t, tc.total, u.TotalTokens)
	}
}

package cohere

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type correctionRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func (w *correctionRecorder) CloseNotify() <-chan bool { return w.closed }

func TestUsageCorrectionCohereCounterPresence(t *testing.T) {
	service.InitTokenEncoders()
	for _, tc := range []struct {
		name, counters     string
		prompt, completion int
	}{
		{"zero", `"input_tokens":0,"output_tokens":0`, 0, 0},
		{"zero_input", `"input_tokens":0,"output_tokens":22`, 0, 22},
		{"reported", `"input_tokens":100,"output_tokens":22`, 100, 22},
		{"missing", ``, 38, -1},
		{"missing_input", `"output_tokens":22`, 38, 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &correctionRecorder{httptest.NewRecorder(), make(chan bool)}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "command-r"}}
			info.SetEstimatePromptTokens(38)
			raw := `{"text":"Visible Cohere output for fallback"}` + "\n" + `{"is_finished":true,"finish_reason":"COMPLETE","response":{"meta":{"billed_units":{` + tc.counters + `}}}}` + "\n"
			usage, apiErr := cohereStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))})
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

func TestUsageCorrectionCohereRerankPresence(t *testing.T) {
	for _, tc := range []struct {
		name, counters     string
		prompt, completion int
	}{
		{"zero", `"input_tokens":0,"output_tokens":0`, 0, 0},
		{"zero_input", `"input_tokens":0,"output_tokens":22`, 0, 22},
		{"reported", `"input_tokens":100,"output_tokens":22`, 100, 22},
		{"missing_input", `"output_tokens":22`, 38, 22},
		{"missing", ``, 38, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(38)
			u, apiErr := cohereRerankHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[],"meta":{"billed_units":{` + tc.counters + `}}}`))}, info)
			require.Nil(t, apiErr)
			require.Equal(t, tc.prompt, u.PromptTokens)
			require.Equal(t, tc.completion, u.CompletionTokens)
			require.Equal(t, tc.prompt+tc.completion, u.TotalTokens)
		})
	}
}

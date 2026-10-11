package claude

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeResponsesUsageDistinguishesNullAndNumericZero(t *testing.T) {
	for _, usageCase := range []struct {
		name, input, output string
		prompt              int
		estimatedOutput     bool
	}{
		{"null_output", "4", "null", 4, true},
		{"missing_output", "4", "", 4, true},
		{"zero_output", "4", "0", 4, false},
		{"null_input", "null", "2", 10, false},
		{"zero_input", "0", "2", 0, false},
		{"all_zero", "0", "0", 0, false},
	} {
		t.Run(usageCase.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, RelayFormat: types.RelayFormatOpenAIResponses,
				OriginModelName: "claude-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			info.SetEstimatePromptTokens(10)
			outputField := ""
			if usageCase.output != "" {
				outputField = `"output_tokens":` + usageCase.output
			}
			frames := []string{
				fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_test","model":"claude-test","usage":{"input_tokens":%s}}}`, usageCase.input),
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{` + outputField + `}}`,
			}
			usage, err := ClaudeStreamHandler(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))}, info)
			require.Nil(t, err)
			require.Equal(t, usageCase.prompt, usage.PromptTokens)
			if usageCase.estimatedOutput {
				require.Positive(t, usage.CompletionTokens)
			} else if usageCase.output == "0" {
				require.Zero(t, usage.CompletionTokens)
			} else {
				require.Equal(t, 2, usage.CompletionTokens)
			}
			require.NotNil(t, usage.BillingUsage)
			require.Equal(t, usageCase.estimatedOutput || usageCase.input == "null", usage.BillingUsage.Estimated)
		})
	}
}

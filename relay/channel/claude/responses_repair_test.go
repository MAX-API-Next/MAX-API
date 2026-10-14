package claude

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const claudeResponsesPartialFrames = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_partial\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":4}}}\n\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial output\"}}\n\n"

type claudeResponsesBrokenBody struct{ *strings.Reader }

func (b *claudeResponsesBrokenBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		return 0, errors.New("synthetic upstream disconnect")
	}
	return n, err
}

func (*claudeResponsesBrokenBody) Close() error { return nil }

type claudeResponsesTerminalFailureWriter struct{ *httptest.ResponseRecorder }

func (w *claudeResponsesTerminalFailureWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "response.completed") {
		return 0, errors.New("synthetic terminal write failure")
	}
	return w.ResponseRecorder.Write(p)
}

type claudeResponsesCancelWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *claudeResponsesCancelWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(string(p), `"delta":"partial output"`) {
		w.cancel()
	}
	return n, err
}

func TestClaudeResponsesRepairPreservesPartialUsageOnFailure(t *testing.T) {
	for _, failure := range []string{"malformed", "provider_error", "disconnect", "terminal_write", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			var writer http.ResponseWriter = recorder
			body := claudeResponsesPartialFrames
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch failure {
			case "malformed":
				body += "data: {broken\n\n"
			case "provider_error":
				body += "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"synthetic provider failure\"}}\n\n"
			case "terminal_write":
				writer = &claudeResponsesTerminalFailureWriter{recorder}
				body += "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n"
			case "cancel":
				writer = &claudeResponsesCancelWriter{recorder, cancel}
			}
			var reader io.ReadCloser = io.NopCloser(strings.NewReader(body))
			if failure == "disconnect" {
				reader = &claudeResponsesBrokenBody{strings.NewReader(body)}
			}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
			info := &relaycommon.RelayInfo{IsStream: true, RelayFormat: types.RelayFormatOpenAIResponses,
				OriginModelName: "claude-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}, DisablePing: true}
			usage, apiErr := ClaudeStreamHandler(c, &http.Response{StatusCode: http.StatusOK, Body: reader}, info)
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotNil(t, usage)
			require.Equal(t, 4, usage.PromptTokens)
			require.Positive(t, usage.CompletionTokens)
			require.NotNil(t, usage.BillingUsage)
			require.Contains(t, recorder.Body.String(), "partial output")
			require.NotContains(t, recorder.Body.String(), "event: response.completed")
		})
	}
}

func TestClaudeResponsesRepairPreservesAllTextBlocks(t *testing.T) {
	resp := ResponseClaude2OpenAI(&dto.ClaudeResponse{Content: []dto.ClaudeMediaMessage{
		{Type: "text", Text: common.GetPointer("first ")},
		{Type: "text", Text: common.GetPointer("second")},
	}})
	require.Equal(t, "first second", resp.Choices[0].Message.StringContent())
}

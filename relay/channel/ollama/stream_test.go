package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaChatHandlerNonStreamToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "compact json per-line parse path",
			raw:  `{"model":"llama3.1","created_at":"2026-05-27T12:00:00Z","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris","days":0}}}]},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":7}`,
		},
		{
			name: "pretty json fallback parse path",
			raw: `{
  "model": "llama3.1",
  "created_at": "2026-05-27T12:00:00Z",
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [
      {
        "function": {
          "name": "get_weather",
          "arguments": {
            "city": "Paris",
            "days": 0
          }
        }
      }
    ]
  },
  "done": true,
  "done_reason": "stop",
  "prompt_eval_count": 5,
  "eval_count": 7
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.raw)),
			}

			usage, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "fallback-model"},
			}, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 12, usage.TotalTokens)

			var out dto.OpenAITextResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &out))
			require.Len(t, out.Choices, 1)
			assert.Equal(t, constant.FinishReasonToolCalls, out.Choices[0].FinishReason)

			var toolCalls []dto.ToolCallResponse
			require.NoError(t, common.Unmarshal(out.Choices[0].Message.ToolCalls, &toolCalls))
			require.Len(t, toolCalls, 1)
			assert.NotEmpty(t, toolCalls[0].ID)
			assert.Equal(t, "function", toolCalls[0].Type)
			assert.Equal(t, "get_weather", toolCalls[0].Function.Name)
			assert.Nil(t, toolCalls[0].Index)

			var args map[string]any
			require.NoError(t, common.Unmarshal([]byte(toolCalls[0].Function.Arguments), &args))
			assert.Equal(t, "Paris", args["city"])
			assert.Equal(t, float64(0), args["days"])
		})
	}
}

func TestOllamaStreamHandlerToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)

	raw := strings.Join([]string{
		`{"model":"llama3.1","created_at":"2026-05-27T12:00:00Z","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris","days":0}}}]},"done":false}`,
		`{"model":"llama3.1","created_at":"2026-05-27T12:00:01Z","done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":7}`,
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(raw)),
	}

	usage, apiErr := ollamaStreamHandler(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "fallback-model"},
	}, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 12, usage.TotalTokens)

	chunks, doneSeen := parseOpenAIStreamChunks(t, recorder.Body.String())
	require.True(t, doneSeen)

	var toolCall *dto.ToolCallResponse
	var finishReason string
	for i := range chunks {
		if len(chunks[i].Choices) == 0 {
			continue
		}
		choice := chunks[i].Choices[0]
		if len(choice.Delta.ToolCalls) > 0 {
			toolCall = &choice.Delta.ToolCalls[0]
		}
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
	}

	require.NotNil(t, toolCall)
	assert.NotEmpty(t, toolCall.ID)
	assert.Equal(t, "function", toolCall.Type)
	assert.Equal(t, "get_weather", toolCall.Function.Name)
	require.NotNil(t, toolCall.Index)
	assert.Equal(t, 0, *toolCall.Index)

	var args map[string]any
	require.NoError(t, common.Unmarshal([]byte(toolCall.Function.Arguments), &args))
	assert.Equal(t, "Paris", args["city"])
	assert.Equal(t, float64(0), args["days"])
	assert.Equal(t, constant.FinishReasonToolCalls, finishReason)
}

func parseOpenAIStreamChunks(t *testing.T, body string) ([]dto.ChatCompletionsStreamResponse, bool) {
	t.Helper()

	var chunks []dto.ChatCompletionsStreamResponse
	var doneSeen bool
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		require.True(t, strings.HasPrefix(frame, "data: "), "unexpected SSE frame: %q", frame)
		payload := strings.TrimSpace(strings.TrimPrefix(frame, "data: "))
		if payload == "[DONE]" {
			doneSeen = true
			continue
		}

		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.Unmarshal([]byte(payload), &chunk))
		chunks = append(chunks, chunk)
	}
	return chunks, doneSeen
}

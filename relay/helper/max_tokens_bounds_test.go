package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMaxTokensBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newJSONContext := func(body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/relay", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	const tooLarge = "1073741824"

	t.Run("openai max_tokens rejected", func(t *testing.T) {
		c := newJSONContext(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":` + tooLarge + `}`)
		_, err := GetAndValidateTextRequest(c, relayconstant.RelayModeChatCompletions)
		require.Error(t, err)
		require.Contains(t, err.Error(), "max_tokens is invalid")
	})

	t.Run("openai max_completion_tokens rejected", func(t *testing.T) {
		c := newJSONContext(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":` + tooLarge + `}`)
		_, err := GetAndValidateTextRequest(c, relayconstant.RelayModeChatCompletions)
		require.Error(t, err)
		require.Contains(t, err.Error(), "max_tokens is invalid")
	})

	t.Run("claude max_tokens rejected", func(t *testing.T) {
		c := newJSONContext(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"max_tokens":` + tooLarge + `}`)
		_, err := GetAndValidateClaudeRequest(c)
		require.Error(t, err)
		require.Contains(t, err.Error(), "max_tokens is invalid")
	})

	t.Run("claude normal max_tokens accepted", func(t *testing.T) {
		c := newJSONContext(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"max_tokens":8192}`)
		req, err := GetAndValidateClaudeRequest(c)
		require.NoError(t, err)
		require.EqualValues(t, 8192, *req.MaxTokens)
	})

	t.Run("gemini maxOutputTokens rejected", func(t *testing.T) {
		c := newJSONContext(`{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":` + tooLarge + `}}`)
		_, err := GetAndValidateGeminiRequest(c)
		require.Error(t, err)
		require.Contains(t, err.Error(), "maxOutputTokens is invalid")
	})

	t.Run("responses max_output_tokens rejected", func(t *testing.T) {
		c := newJSONContext(`{"model":"gpt-4o","input":"hi","max_output_tokens":` + tooLarge + `}`)
		_, err := GetAndValidateResponsesRequest(c)
		require.Error(t, err)
		require.Contains(t, err.Error(), "max_output_tokens is invalid")
	})
}

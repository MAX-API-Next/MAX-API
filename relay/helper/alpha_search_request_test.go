package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateAlphaSearchRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	t.Run("requires model", func(t *testing.T) {
		_, err := GetAndValidateAlphaSearchRequest(newContext(`{"commands":{"search_query":[{"q":"weather"}]}}`))

		require.EqualError(t, err, "model is required")
	})

	t.Run("preserves original json bytes", func(t *testing.T) {
		raw := "{\n  \"model\": \"gpt-5.1\",\n  \"future_field\": {\"enabled\": true}\n}"

		request, err := GetAndValidateAlphaSearchRequest(newContext(raw))

		require.NoError(t, err)
		require.Equal(t, "gpt-5.1", request.Model)
		require.Equal(t, []byte(raw), []byte(request.RawBody))
	})
}

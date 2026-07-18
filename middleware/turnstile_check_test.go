package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetTurnstileTokenPrefersHeaderWithQueryFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		header string
		query  string
		want   string
	}{
		{name: "header", header: "header-token", query: "query-token", want: "header-token"},
		{name: "query fallback", query: "query-token", want: "query-token"},
		{name: "trim whitespace", header: "  header-token  ", want: "header-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/?turnstile="+tt.query, nil)
			if tt.header != "" {
				req.Header.Set(turnstileTokenHeader, tt.header)
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = req
			assert.Equal(t, tt.want, getTurnstileToken(ctx))
		})
	}
}

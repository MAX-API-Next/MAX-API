package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

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

func TestVerifyTurnstileUsesBoundedClientTimeout(t *testing.T) {
	oldClient := turnstileHTTPClient
	oldTimeout := turnstileVerificationTimeout
	turnstileVerificationTimeout = 50 * time.Millisecond
	turnstileHTTPClient = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}
	t.Cleanup(func() {
		turnstileHTTPClient = oldClient
		turnstileVerificationTimeout = oldTimeout
	})

	started := time.Now()
	_, err := verifyTurnstile(context.Background(), "token", "127.0.0.1")
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second)
}

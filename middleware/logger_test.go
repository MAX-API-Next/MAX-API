package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccessLoggerRedactsCredentialsWithoutChangingRequest(t *testing.T) {
	oldMode, oldWriter := gin.Mode(), gin.DefaultWriter
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode); gin.DefaultWriter = oldWriter })
	for _, target := range []string{
		"/v1/models?%6bey=AUDIT_SECRET&key=AUDIT_SECRET&API-Key=AUDIT_SECRET&session_token=AUDIT_SECRET&alt=sse",
		"/api/oauth/github?code=AUDIT_SECRET&state=AUDIT_SECRET",
		"/%61pi/oauth/github?code=AUDIT_SECRET&state=AUDIT_SECRET",
		"/v1/models?key=bad%ZZ&token=AUDIT_SECRET&alt=sse",
		"/v1/models?alt=sse",
	} {
		t.Run(target, func(t *testing.T) {
			var output bytes.Buffer
			gin.DefaultWriter = &output
			router := gin.New()
			SetUpLogger(router)
			router.NoRoute(func(c *gin.Context) {
				require.Equal(t, target, c.Request.RequestURI)
				c.Status(http.StatusUnauthorized)
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
			require.NotContains(t, output.String(), "AUDIT_SECRET")
			if strings.Contains(target, "alt=sse") {
				require.Contains(t, output.String(), "alt=sse")
			}
		})
	}
}

func TestAccessLoggerDoesNotRestoreRemovedQueryCredentials(t *testing.T) {
	oldWriter := gin.DefaultWriter
	defer func() { gin.DefaultWriter = oldWriter }()
	var output bytes.Buffer
	gin.DefaultWriter = &output
	router := gin.New()
	SetUpLogger(router)
	router.GET("/v1/models", func(c *gin.Context) {
		c.Request.URL.RawQuery = ""
		c.Status(http.StatusNoContent)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models?key=AUDIT_SECRET", nil))
	require.NotContains(t, output.String(), "AUDIT_SECRET")
}

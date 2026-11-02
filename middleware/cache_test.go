package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFrontendCachePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Cache())
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "file") })
	for _, target := range []string{"/", "/?v=2", "/index.html", "/index.html?v=2", "/max-api-logo.png", "/static/js/index.js"} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			require.Contains(t, response.Result().Header.Get("Cache-Control"), "no-cache")
		})
	}
	for _, target := range []string{"/static/js/index.123abc4567.js", "/static/css/index.123abc4567.css?v=2", "/assets/font.123abc4567.woff2"} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			require.Contains(t, response.Result().Header.Get("Cache-Control"), "immutable")
		})
	}
}

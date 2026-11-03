package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/middleware"
	"github.com/MAX-API-Next/MAX-API/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFrontendMissingResourcesDoNotReturnSPA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := common.GlobalWebRateLimitEnable
	common.GlobalWebRateLimitEnable = false
	t.Cleanup(func() { common.GlobalWebRateLimitEnable = previousLimit })
	engine := gin.New()
	index := []byte("<!doctype html><title>SPA fallback</title>")
	router.SetWebRouter(engine, router.ThemeAssets{DefaultBuildFS: buildFS, DefaultIndexPage: index})

	for _, target := range []string{
		"/static/js/async/42.oldhash.js", "/static/css/42.oldhash.css?v=1",
		"/static/js/async/42.abcdef12.js", "/static/css/42.01234567.css?v=1",
		"/static/missing/", "/static/missing", "/assets/missing.js", "/api/missing", "/v1/missing",
	} {
		t.Run(target, func(t *testing.T) {
			// For recognized hashes, prove the cache middleware starts immutable.
			// The full web chain must then override that policy for the missing file.
			if target == "/static/js/async/42.abcdef12.js" || target == "/static/css/42.01234567.css?v=1" {
				cacheOnly := gin.New()
				cacheOnly.Use(middleware.Cache())
				cacheOnly.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
				control := httptest.NewRecorder()
				cacheOnly.ServeHTTP(control, httptest.NewRequest(http.MethodGet, target, nil))
				require.Contains(t, control.Header().Get("Cache-Control"), "immutable")
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Equal(t, "no-store", response.Result().Header.Get("Cache-Control"))
			require.NotContains(t, response.Body.String(), "SPA fallback")
		})
	}
	for _, target := range []string{"/", "/?page=1", "/channels?page=2", "/static-guide", "/pricing/model.v1"} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Contains(t, response.Result().Header.Get("Cache-Control"), "no-cache")
			require.Equal(t, string(index), response.Body.String())
		})
	}
}

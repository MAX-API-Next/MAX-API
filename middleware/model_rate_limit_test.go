package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryModelSuccessRateLimitDoesNotCountFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldRedisEnabled := common.RedisEnabled
	oldDuration := setting.ModelRequestRateLimitDurationMinutes
	common.RedisEnabled = false
	setting.ModelRequestRateLimitDurationMinutes = 1
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		setting.ModelRequestRateLimitDurationMinutes = oldDuration
	})

	status := http.StatusInternalServerError
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 770001)
		c.Next()
	})
	router.Use(memoryRateLimitHandler(60, 0, 1))
	router.GET("/", func(c *gin.Context) {
		c.Status(status)
	})

	for range 3 {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		require.Equal(t, http.StatusInternalServerError, recorder.Code)
	}

	status = http.StatusOK
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

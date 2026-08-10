package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type healthResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func performHealthRequest(t *testing.T, handler gin.HandlerFunc, path string) (*httptest.ResponseRecorder, healthResponse) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)

	handler(ctx)

	var response healthResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func TestHealthEndpointsReturnLivenessStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
		status  string
	}{
		{name: "health", path: "/health", handler: GetHealth, status: "ok"},
		{name: "live", path: "/health/live", handler: GetHealthLive, status: "ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder, response := performHealthRequest(t, tt.handler, tt.path)

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.True(t, response.Success)
			assert.Equal(t, tt.status, response.Status)
			assert.Empty(t, response.Message)
		})
	}
}

func TestHealthReadyReturnsServiceUnavailableWhenDatabasePingFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalProbeDB := healthReadyProbeDB
	healthReadyProbeDB = func() error {
		return errors.New("driver-specific database failure")
	}
	t.Cleanup(func() {
		healthReadyProbeDB = originalProbeDB
	})

	recorder, response := performHealthRequest(t, GetHealthReady, "/health/ready")

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.False(t, response.Success)
	assert.Equal(t, "unhealthy", response.Status)
	assert.Equal(t, "database connection failed", response.Message)
	assert.NotContains(t, recorder.Body.String(), "driver-specific database failure")
}

func TestHealthReadyUsesUncachedDatabaseProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalProbeDB := healthReadyProbeDB
	probeCalls := 0
	healthReadyProbeDB = func() error {
		defer func() {
			probeCalls++
		}()
		if probeCalls == 0 {
			return nil
		}
		return errors.New("database failed after cached success")
	}
	t.Cleanup(func() {
		healthReadyProbeDB = originalProbeDB
	})

	firstRecorder, firstResponse := performHealthRequest(t, GetHealthReady, "/health/ready")
	secondRecorder, secondResponse := performHealthRequest(t, GetHealthReady, "/health/ready")

	require.Equal(t, http.StatusOK, firstRecorder.Code)
	assert.True(t, firstResponse.Success)
	assert.Equal(t, "ready", firstResponse.Status)
	require.Equal(t, http.StatusServiceUnavailable, secondRecorder.Code)
	assert.Equal(t, 2, probeCalls)
	assert.False(t, secondResponse.Success)
	assert.Equal(t, "unhealthy", secondResponse.Status)
}

package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPerformanceHandlersRejectMalformedNumericQueryParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
		key     string
	}{
		{name: "channel start", path: "/channel-performance?start=invalid", handler: GetChannelPerformance, key: "start"},
		{name: "channel end", path: "/channel-performance?end=invalid", handler: GetChannelPerformance, key: "end"},
		{name: "channel hours", path: "/channel-performance?hours=invalid", handler: GetChannelPerformance, key: "hours"},
		{name: "channel empty limit", path: "/channel-performance?limit=", handler: GetChannelPerformance, key: "limit"},
		{name: "channel id", path: "/channel-performance?channel_id=invalid", handler: GetChannelPerformance, key: "channel_id"},
		{name: "channel detail id", path: "/channel-performance/detail?channel_id=invalid", handler: GetChannelPerformanceDetail, key: "channel_id"},
		{name: "model start", path: "/model-performance?start=invalid", handler: GetModelPerformance, key: "start"},
		{name: "model end", path: "/model-performance?end=invalid", handler: GetModelPerformance, key: "end"},
		{name: "model hours", path: "/model-performance?hours=invalid", handler: GetModelPerformance, key: "hours"},
		{name: "model limit", path: "/model-performance?limit=invalid", handler: GetModelPerformance, key: "limit"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, test.path, nil)

			test.handler(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, strings.ToLower(recorder.Body.String()), test.key)
		})
	}
}

func TestParsePerformanceNumericQueryPreservesZeroForAbsentParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/performance", nil)

	query, err := parsePerformanceNumericQuery(ctx)

	require.NoError(t, err)
	require.Zero(t, query.StartAt)
	require.Zero(t, query.EndAt)
	require.Zero(t, query.Hours)
	require.Zero(t, query.Limit)
	channelID, err := parseIntQuery(ctx, "channel_id")
	require.NoError(t, err)
	require.Zero(t, channelID)
}

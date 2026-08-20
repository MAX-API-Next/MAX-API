package controller

import (
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

// GetChannelPerformance exposes the read-only legacy-log projection used by
// the administrator Smart Operations Center. It never probes, reroutes,
// disables or mutates a channel.
func GetChannelPerformance(c *gin.Context) {
	numericQuery, err := parsePerformanceNumericQuery(c)
	if err != nil {
		respondInvalidPerformanceQuery(c, err, service.ErrInvalidChannelPerformanceQuery, "failed to query channel performance")
		return
	}
	channelID, err := parseIntQuery(c, "channel_id")
	if err != nil {
		respondInvalidPerformanceQuery(c, err, service.ErrInvalidChannelPerformanceQuery, "failed to query channel performance")
		return
	}
	query := service.ChannelPerformanceQuery{
		StartAt:   numericQuery.StartAt,
		EndAt:     numericQuery.EndAt,
		Hours:     numericQuery.Hours,
		Limit:     numericQuery.Limit,
		ChannelID: channelID,
		ModelName: strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
	}

	result, err := service.GetChannelPerformance(c.Request.Context(), query)
	if err != nil {
		respondPerformanceError(c, err, service.ErrInvalidChannelPerformanceQuery, "failed to query channel performance")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetChannelPerformanceDetail exposes a channel-scoped 24-hour projection.
// It is queried only when an administrator opens the channel detail sheet.
func GetChannelPerformanceDetail(c *gin.Context) {
	channelID, err := parseIntQuery(c, "channel_id")
	if err != nil {
		respondInvalidPerformanceQuery(c, err, service.ErrInvalidChannelPerformanceQuery, "failed to query channel performance detail")
		return
	}
	result, err := service.GetChannelPerformanceDetail(
		c.Request.Context(),
		channelID,
	)
	if err != nil {
		respondPerformanceError(c, err, service.ErrInvalidChannelPerformanceQuery, "failed to query channel performance detail")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

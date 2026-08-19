package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

// GetChannelPerformance exposes the read-only legacy-log projection used by
// the administrator Smart Operations Center. It never probes, reroutes,
// disables or mutates a channel.
func GetChannelPerformance(c *gin.Context) {
	query := service.ChannelPerformanceQuery{
		StartAt:   parseInt64Query(c, "start"),
		EndAt:     parseInt64Query(c, "end"),
		Hours:     parseIntQuery(c, "hours"),
		Limit:     parseIntQuery(c, "limit"),
		ChannelID: parseIntQuery(c, "channel_id"),
		ModelName: strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
	}

	result, err := service.GetChannelPerformance(c.Request.Context(), query)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidChannelPerformanceQuery) {
			status = http.StatusBadRequest
		} else {
			logger.LogError(c.Request.Context(), "failed to query channel performance: "+err.Error())
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			message = "failed to query channel performance"
		}
		c.JSON(status, gin.H{
			"success": false,
			"message": message,
		})
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
	result, err := service.GetChannelPerformanceDetail(
		c.Request.Context(),
		parseIntQuery(c, "channel_id"),
	)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidChannelPerformanceQuery) {
			status = http.StatusBadRequest
		} else {
			logger.LogError(c.Request.Context(), "failed to query channel performance detail: "+err.Error())
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			message = "failed to query channel performance detail"
		}
		c.JSON(status, gin.H{
			"success": false,
			"message": message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func parseIntQuery(c *gin.Context, key string) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return 0
	}
	return value
}

func parseInt64Query(c *gin.Context, key string) int64 {
	value, err := strconv.ParseInt(c.Query(key), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

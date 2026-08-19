package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

// GetModelPerformance exposes the read-only legacy-log projection used by the
// administrator Smart Operations Center. It aggregates eligible production
// logs by model and never mutates, probes or reroutes a channel.
func GetModelPerformance(c *gin.Context) {
	query := service.ModelPerformanceQuery{
		StartAt:   parseInt64Query(c, "start"),
		EndAt:     parseInt64Query(c, "end"),
		Hours:     parseIntQuery(c, "hours"),
		Limit:     parseIntQuery(c, "limit"),
		ModelName: strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
	}

	result, err := service.GetModelPerformance(c.Request.Context(), query)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidModelPerformanceQuery) {
			status = http.StatusBadRequest
		} else {
			logger.LogError(c.Request.Context(), "failed to query model performance: "+err.Error())
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			message = "failed to query model performance"
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

// GetModelPerformanceDetail exposes the existing perf_metrics projection to
// administrators without coupling Smart Operations to public pricing-module
// visibility. The detail window is fixed at the latest 24 hours.
func GetModelPerformanceDetail(c *gin.Context) {
	result, err := service.GetModelPerformanceDetail(
		c.Request.Context(),
		strings.TrimSpace(c.Query("model")),
	)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidModelPerformanceQuery) {
			status = http.StatusBadRequest
		} else {
			logger.LogError(c.Request.Context(), "failed to query model performance detail: "+err.Error())
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			message = "failed to query model performance detail"
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

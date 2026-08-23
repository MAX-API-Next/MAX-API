package controller

import (
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

// GetModelPerformance exposes the read-only legacy-log projection used by the
// administrator Smart Operations Center. It aggregates eligible production
// logs by model and never mutates, probes or reroutes a channel.
func GetModelPerformance(c *gin.Context) {
	numericQuery, err := parsePerformanceNumericQuery(c)
	if err != nil {
		respondInvalidPerformanceQuery(c, err, service.ErrInvalidModelPerformanceQuery, "failed to query model performance")
		return
	}
	query := service.ModelPerformanceQuery{
		StartAt:   numericQuery.StartAt,
		EndAt:     numericQuery.EndAt,
		Hours:     numericQuery.Hours,
		Limit:     numericQuery.Limit,
		ModelName: strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
	}

	result, err := service.GetModelPerformance(c.Request.Context(), query)
	if err != nil {
		respondPerformanceError(c, err, service.ErrInvalidModelPerformanceQuery, "failed to query model performance")
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
		respondPerformanceError(c, err, service.ErrInvalidModelPerformanceQuery, "failed to query model performance detail")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

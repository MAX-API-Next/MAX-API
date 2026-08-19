package controller

import (
	"errors"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/gin-gonic/gin"
)

func respondPerformanceError(c *gin.Context, err error, invalidQueryError error, fallbackMessage string) {
	status := http.StatusInternalServerError
	message := fallbackMessage
	if errors.Is(err, invalidQueryError) {
		status = http.StatusBadRequest
		message = err.Error()
	} else {
		logger.LogError(c.Request.Context(), fallbackMessage+": "+err.Error())
	}
	c.JSON(status, gin.H{
		"success": false,
		"message": message,
	})
}

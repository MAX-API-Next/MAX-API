package controller

import (
	"net/http"

	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

// GetSmartOpsAlerts exposes the current in-process operational alerts to
// administrators. The endpoint is read-only and does not trigger remediation.
func GetSmartOpsAlerts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    service.GetSmartOpsAlerts(),
	})
}

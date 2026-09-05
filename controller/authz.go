package controller

import (
	"net/http"

	"github.com/MAX-API-Next/MAX-API/service/authz"
	"github.com/gin-gonic/gin"
)

// GetPermissionCatalog returns the stable resource/action schema used by
// administrative clients. It is read-only; policy mutations are intentionally
// deferred until the authorization editor and audit contract are complete.
func GetPermissionCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"resources": authz.Catalog(),
			"roles":     authz.Roles(),
		},
	})
}

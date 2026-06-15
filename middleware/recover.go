package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				common.SysLog(fmt.Sprintf("panic detected: %v", err))
				common.SysLog(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Panic detected, error: %v. Please submit a issue here: https://github.com/MAX-API-Next/MAX-API", err),
						"type":    "max_api_panic",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

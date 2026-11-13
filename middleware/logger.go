package middleware

import (
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
)

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			accessLogPath(param),
		)
	}))
}

// Only sanitize the log projection; request handlers still need original keys.
func accessLogPath(param gin.LogFormatterParams) string {
	if param.Request == nil || param.Request.URL == nil {
		path, _, _ := strings.Cut(param.Path, "?")
		return path
	}
	u := param.Request.URL
	path := u.EscapedPath()
	if strings.HasPrefix(u.Path, "/api/oauth/") || strings.HasPrefix(u.Path, "/oauth/") {
		return path
	}
	query := u.Query()
	for key := range query {
		name := strings.ToLower(strings.TrimSpace(key))
		sensitive := strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "signature")
		switch name {
		case "key", "api_key", "api-key", "apikey", "x-api-key", "authorization", "auth", "password", "passwd", "sig", "awsaccesskeyid", "x-amz-credential", "code", "state":
			sensitive = true
		}
		if sensitive {
			query.Set(key, "***masked***")
		}
	}
	if encoded := query.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

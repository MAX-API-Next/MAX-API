package middleware

import (
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var frontendHashedAsset = regexp.MustCompile(`\.[a-f0-9]{8,64}\.[a-zA-Z0-9]+$`)

func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		c.Header("Cache-Control", "no-cache")
		if (strings.HasPrefix(urlPath, "/static/") || strings.HasPrefix(urlPath, "/assets/")) &&
			frontendHashedAsset.MatchString(urlPath) {
			c.Header("Cache-Control", "public, max-age=604800, immutable")
		}
		c.Header("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}

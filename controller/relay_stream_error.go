package controller

import (
	"strings"

	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

// Once SSE is committed, a JSON response cannot change the HTTP status and
// would corrupt event framing. The controller remains the single error writer.
func writeStartedStreamError(c *gin.Context, format types.RelayFormat, apiErr *types.MaxAPIError) bool {
	if (format != types.RelayFormatOpenAI && format != types.RelayFormatOpenAIResponses) || !strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
		return false
	}
	if !c.Writer.Written() {
		// The scanner prepares SSE headers before receiving any output. Let
		// Gin's ordinary error responder select application/json if no bytes
		// were committed; otherwise clients would parse raw JSON as SSE.
		c.Writer.Header().Del("Content-Type")
		c.Writer.Header().Del("Cache-Control")
		c.Writer.Header().Del("Connection")
		c.Writer.Header().Del("Transfer-Encoding")
		c.Writer.Header().Del("X-Accel-Buffering")
		return false
	}
	if format == types.RelayFormatOpenAIResponses {
		_ = helper.WriteResponsesStreamFailure(c, apiErr)
		return true
	}
	_ = helper.ObjectData(c, gin.H{"error": apiErr.ToOpenAIError()})
	helper.Done(c)
	return true
}

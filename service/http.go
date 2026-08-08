package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"

	"github.com/gin-gonic/gin"
)

var blockedUpstreamResponseHeaders = map[string]struct{}{
	"connection":          {},
	"content-length":      {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"set-cookie":          {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header can
// be copied to the client response. Hop-by-hop headers and Set-Cookie stay
// under MAX API control, and malformed names/values are dropped before they
// reach net/http's writer.
func ShouldCopyUpstreamHeader(_ *gin.Context, k string, v []string) bool {
	return shouldCopyUpstreamHeader(k, v)
}

func shouldCopyUpstreamHeader(k string, v []string) bool {
	if len(v) == 0 {
		return false
	}
	headerName := strings.TrimSpace(k)
	if headerName == "" || headerName != k || !isSafeResponseHeaderName(headerName) || !isSafeResponseHeaderValue(v[0]) {
		return false
	}
	if strings.EqualFold(headerName, common.RequestIdKey) {
		return false
	}
	if _, blocked := blockedUpstreamResponseHeaders[strings.ToLower(headerName)]; blocked {
		return false
	}
	return true
}

func CopyUpstreamResponseHeaders(c *gin.Context, header http.Header) {
	if c == nil || c.Writer == nil {
		return
	}
	for key, values := range header {
		if len(values) > 0 && strings.EqualFold(key, common.RequestIdKey) && isSafeResponseHeaderName(key) && isSafeResponseHeaderValue(values[0]) {
			c.Set(common.UpstreamRequestIdKey, values[0])
		}
		if !shouldCopyUpstreamHeader(key, values) {
			continue
		}
		copied := false
		for _, value := range values {
			if !isSafeResponseHeaderValue(value) {
				continue
			}
			if copied {
				c.Writer.Header().Add(key, value)
			} else {
				c.Writer.Header().Set(key, value)
				copied = true
			}
		}
	}
}

func isSafeResponseHeaderName(name string) bool {
	for i := 0; i < len(name); i++ {
		if !isHTTPTokenChar(name[i]) {
			return false
		}
	}
	return true
}

func isHTTPTokenChar(c byte) bool {
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func isSafeResponseHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\r', '\n', 0, 0x7f:
			return false
		}
		if value[i] < 0x20 && value[i] != '\t' {
			return false
		}
	}
	return true
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		CopyUpstreamResponseHeaders(c, src.Header)
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}

package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewSSRFProtectedHTTPClientDisablesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8080")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")

	client := newSSRFProtectedHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatalf("expected SSRF-protected client to disable environment proxy support")
	}
}

func TestNewBaseTransportKeepsEnvironmentProxySupport(t *testing.T) {
	transport := newBaseTransport(http.ProxyFromEnvironment)
	if transport.Proxy == nil {
		t.Fatalf("expected base transport to keep environment proxy support")
	}
}

func TestShouldCopyUpstreamHeaderRejectsUnsafeResponseHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, ShouldCopyUpstreamHeader(c, "Content-Length", []string{"10"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Connection", []string{"keep-alive"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Set-Cookie", []string{"session=upstream"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Bad Header", []string{"value"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "X-Bad", []string{"ok\r\nX-Injected: yes"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "X-Del", []string{"bad" + string(rune(0x7f))}))
	require.False(t, ShouldCopyUpstreamHeader(c, "X-Empty", nil))
	require.False(t, ShouldCopyUpstreamHeader(c, common.RequestIdKey, []string{"upstream-request"}))
	_, capturedRequestID := c.Get(common.UpstreamRequestIdKey)
	require.False(t, capturedRequestID)
	require.True(t, ShouldCopyUpstreamHeader(c, "X-Safe", []string{"safe"}))
}

func TestCopyUpstreamResponseHeadersRejectsConnectionNominatedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	CopyUpstreamResponseHeaders(c, http.Header{
		"Connection":        []string{"keep-alive, X-Transient", strings.ToLower(common.RequestIdKey)},
		"X-Transient":       []string{"must-not-copy"},
		"X-Safe":            []string{"safe"},
		common.RequestIdKey: []string{"must-not-capture"},
	})

	require.Empty(t, recorder.Header().Get("Connection"))
	require.Empty(t, recorder.Header().Get("X-Transient"))
	require.Equal(t, "safe", recorder.Header().Get("X-Safe"))
	_, capturedRequestID := c.Get(common.UpstreamRequestIdKey)
	require.False(t, capturedRequestID)
}

func TestIOCopyBytesGracefullyCopiesOnlySafeUpstreamHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	src := &http.Response{
		StatusCode: http.StatusAccepted,
		Header: http.Header{
			"Connection":         []string{"keep-alive"},
			"Set-Cookie":         []string{"session=upstream"},
			"X-Bad":              []string{"ok\r\nX-Injected: yes"},
			"X-Multi":            []string{"one", "two\r\nX-Injected: yes", "three"},
			"X-Safe":             []string{"safe"},
			common.RequestIdKey:  []string{"upstream-request"},
			"Content-Type":       []string{"application/json"},
			"Transfer-Encoding":  []string{"chunked"},
			"Content-Length":     []string{"999"},
			"X-Empty-Value-List": nil,
		},
	}

	IOCopyBytesGracefully(c, src, []byte(`{"ok":true}`))

	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Equal(t, "safe", recorder.Header().Get("X-Safe"))
	require.Equal(t, []string{"one", "three"}, recorder.Header().Values("X-Multi"))
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "11", recorder.Header().Get("Content-Length"))
	require.Empty(t, recorder.Header().Get("Connection"))
	require.Empty(t, recorder.Header().Get("Set-Cookie"))
	require.Empty(t, recorder.Header().Get("X-Bad"))
	require.Empty(t, recorder.Header().Get(common.RequestIdKey))
	require.Empty(t, recorder.Header().Get("Transfer-Encoding"))
	require.Equal(t, "upstream-request", c.GetString(common.UpstreamRequestIdKey))
	require.JSONEq(t, `{"ok":true}`, recorder.Body.String())
}

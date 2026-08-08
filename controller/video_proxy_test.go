package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteVideoDataURLAllowsVideoPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	err := writeVideoDataURL(c, "data:video/mp4;base64,"+base64.StdEncoding.EncodeToString([]byte("mp4")))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "mp4", recorder.Body.String())
}

func TestWriteVideoDataURLRejectsNonVideoPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	err := writeVideoDataURL(c, "data:text/html;base64,"+base64.StdEncoding.EncodeToString([]byte("<script></script>")))

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported video data url mime type")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Body.String())
}

func TestWriteVideoDataURLRejectsOversizedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLimit := maxVideoDataURLDecodedBytes
	maxVideoDataURLDecodedBytes = 4
	t.Cleanup(func() {
		maxVideoDataURLDecodedBytes = originalLimit
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	payload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 5)))
	err := writeVideoDataURL(c, "data:video/mp4;base64,"+payload)

	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds size limit")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Body.String())
}

func TestCopyVideoResponseHeadersPreservesKnownContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		ContentLength: 1234,
		Header: http.Header{
			"Content-Type":   []string{"video/mp4"},
			"Content-Length": []string{"9999"},
			"Connection":     []string{"keep-alive"},
		},
	}

	copyVideoResponseHeaders(c, resp)

	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "1234", recorder.Header().Get("Content-Length"))
	require.Empty(t, recorder.Header().Get("Connection"))
}

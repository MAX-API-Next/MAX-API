package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoProxyServesPersistedInlineContentForOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/inline-video.db"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = pool.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	task := &model.Task{TaskID: "stored-inline", UserId: 71, ChannelId: 99, Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL:          "/v1/videos/stored-inline/content",
			InlineVideoDataURL: "data:video/webm;base64," + base64.StdEncoding.EncodeToString([]byte("fixture-video")),
		}}
	require.NoError(t, db.Create(task).Error)
	request := func(user int) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, task.PrivateData.ResultURL, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
		ctx.Set("id", user)
		VideoProxy(ctx)
		return recorder
	}
	response := request(71)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "video/webm", response.Header().Get("Content-Type"))
	require.Equal(t, "fixture-video", response.Body.String())
	foreign := request(72)
	require.Equal(t, http.StatusNotFound, foreign.Code)
	require.NotContains(t, foreign.Body.String(), "fixture-video")
}

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

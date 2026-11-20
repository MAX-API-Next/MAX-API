package controller

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
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
	request := func(user int, interrupted bool) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		var response http.ResponseWriter = recorder
		if interrupted {
			response = interruptedVideoWriter{ResponseWriter: recorder}
		}
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodGet, task.PrivateData.ResultURL, nil)
		ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
		ctx.Set("id", user)
		VideoProxy(ctx)
		return recorder
	}
	response := request(71, false)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "video/webm", response.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "fixture-video", response.Body.String())
	partial := request(71, true)
	require.Equal(t, http.StatusOK, partial.Code)
	require.Equal(t, "fix", partial.Body.String())
	foreign := request(72, false)
	require.Equal(t, http.StatusNotFound, foreign.Code)
	require.NotContains(t, foreign.Body.String(), "fixture-video")
}

type interruptedVideoWriter struct{ http.ResponseWriter }

func (w interruptedVideoWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p[:min(3, len(p))])
	return n, errors.Join(errors.New("synthetic write interruption"), err)
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

func TestVideoProxyFallsBackToProviderAfterStoredValidationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/provider-video/content" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "video/webm")
		w.Header().Set("Cache-Control", "public")
		_, _ = w.Write([]byte("provider video"))
	}))
	defer server.Close()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/provider-video.db"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	previousDB, previousCache := model.DB, common.MemoryCacheEnabled
	oldFetch := *system_setting.GetFetchSetting()
	model.DB, common.MemoryCacheEnabled = db, false
	system_setting.GetFetchSetting().EnableSSRFProtection = false // The upstream is an isolated loopback fixture.
	t.Cleanup(func() {
		model.DB, common.MemoryCacheEnabled = previousDB, previousCache
		*system_setting.GetFetchSetting() = oldFetch
		_ = pool.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &server.URL, Key: "fixture"}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{TaskID: "fallback-video", UserId: 71, ChannelId: channel.Id, Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "provider-video", InlineVideoDataURL: "data:application/octet-stream;base64,AA=="}}
	require.NoError(t, db.Create(task).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/fallback-video/content", nil)
	ctx.Set("id", 71)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	VideoProxy(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "provider video", recorder.Body.String())
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
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

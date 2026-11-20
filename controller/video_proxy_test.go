package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
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
	const fixtureVideo = "provider video"
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/native-video/content", "/v1/videos/invalid-provider-inline-video/content", "/v1/videos/native-url-video/content":
			if r.Header.Get("Authorization") != "Bearer fixture" {
				http.Error(w, "missing credential", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "video/webm")
			w.Header().Set("Cache-Control", "public")
			_, _ = w.Write([]byte(fixtureVideo))
		case "/jobs/configured-video":
			encoded := base64.StdEncoding.EncodeToString([]byte(fixtureVideo))
			_, _ = w.Write([]byte(`{"status":"completed","result":{"url":"data:video/webm;base64,` + encoded + `"}}`))
		case "/jobs/oversized-video":
			encoded := base64.StdEncoding.EncodeToString([]byte(fixtureVideo))
			_, _ = w.Write([]byte(`{"status":"completed","result":{"url":"data:video/webm;base64,` + encoded + `"},"other_output":"` + strings.Repeat("X", (1<<20)+128) + `"}`))
		case "/jobs/invalid-provider-inline-video":
			_, _ = w.Write([]byte(`{"status":"completed","result":{"url":"data:application/octet-stream;base64,AA=="}}`))
		case "/jobs/native-url-video":
			_, _ = w.Write([]byte(`{"status":"completed","result":{"url":"http://` + r.Host + `/v1/videos/native-url-video/content"}}`))
		case "/jobs/custom-url-video":
			_, _ = w.Write([]byte(`{"status":"completed","result":{"url":"http://` + r.Host + `/media/result.webm"}}`))
		case "/media/result.webm":
			if r.Header.Get("Authorization") != "" {
				http.Error(w, "unexpected credential", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "video/webm")
			_, _ = w.Write([]byte(fixtureVideo))
		default:
			http.NotFound(w, r)
		}
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
	const configured = `{"task_protocol":"generic_video_task","task_protocol_config":{"query_path":"/jobs/{task_id}","result_url_paths":["result.url"]}}`
	for _, tc := range []struct {
		name, settings string
		channelType    int
	}{
		{name: "native", channelType: constant.ChannelTypeOpenAI},
		{name: "configured", channelType: constant.ChannelTypeSora, settings: configured},
		{name: "oversized", channelType: constant.ChannelTypeSora, settings: configured},
		{name: "invalid-provider-inline", channelType: constant.ChannelTypeSora, settings: configured},
		{name: "native-url", channelType: constant.ChannelTypeSora, settings: configured},
		{name: "custom-url", channelType: constant.ChannelTypeSora, settings: configured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "oversized" {
				original := maxVideoDataURLDecodedBytes
				maxVideoDataURLDecodedBytes = 32
				t.Cleanup(func() { maxVideoDataURLDecodedBytes = original })
				settings := (&model.Channel{OtherSettings: configured}).GetOtherSettings()
				require.Empty(t, configuredInlineVideoFromPrefix([]byte(`{"result":{"url":"data:video/webm;base64,AA==`), settings))
			}
			channel := &model.Channel{Type: tc.channelType, BaseURL: &server.URL, Key: "fixture", OtherSettings: tc.settings}
			require.NoError(t, db.Create(channel).Error)
			task := &model.Task{TaskID: "fallback-" + tc.name, UserId: 71, ChannelId: channel.Id, Status: model.TaskStatusSuccess,
				PrivateData: model.TaskPrivateData{UpstreamTaskID: tc.name + "-video", InlineVideoDataURL: "data:application/octet-stream;base64,AA=="}}
			require.NoError(t, db.Create(task).Error)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
			ctx.Set("id", 71)
			ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
			VideoProxy(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, fixtureVideo, recorder.Body.String())
			require.Equal(t, "video/webm", recorder.Header().Get("Content-Type"))
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func TestVideoProxyDistinguishesLocalContentAndBoundsStatusReads(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.invalid/console"
	t.Cleanup(func() { system_setting.ServerAddress = previous })
	t.Setenv("TASK_VIDEO_PROXY_LEGACY_BASE_URLS", "https://legacy.invalid/old,https://retired.invalid")
	require.True(t, isTaskProxyContentURL("/v1/videos/task_demo/content", "task_demo"))
	require.True(t, isTaskProxyContentURL("https://gateway.invalid/console/v1/videos/task_demo/content", "task_demo"))
	require.False(t, isTaskProxyContentURL("https://provider.invalid/console/v1/videos/task_demo/content", "task_demo"))
	require.False(t, isTaskProxyContentURL("https://gateway.invalid/console/v1/videos/task_demo/content.mp4", "task_demo"))
	require.True(t, isTaskProxyContentURL("https://legacy.invalid/old/v1/videos/task_demo/content", "task_demo"))
	require.False(t, isTaskProxyContentURL("https://legacy.invalid/other/v1/videos/task_demo/content", "task_demo"))
	row := &model.Task{TaskID: "task_demo", PrivateData: model.TaskPrivateData{ResultURL: model.TaskVideoProxyPath("task_demo"), LocalVideoContent: true}}
	require.Equal(t, "https://gateway.invalid/console/v1/videos/task_demo/content", row.GetResultURL())
	body, err := readVideoProviderStatus(strings.NewReader(`{"status":"complete"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"complete"}`, string(body))
	previousLimit := maxVideoDataURLDecodedBytes
	maxVideoDataURLDecodedBytes = 8
	t.Cleanup(func() { maxVideoDataURLDecodedBytes = previousLimit })
	_, err = readVideoProviderStatus(strings.NewReader(strings.Repeat("x", (1<<20)+13)))
	require.ErrorContains(t, err, "size limit")
}

func TestVideoProviderStatusHonorsRequestCancellation(t *testing.T) {
	service.InitHttpClient()
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := fetchVideoTaskResultURL(ctx, &model.Channel{Type: constant.ChannelTypeVidu, BaseURL: &server.URL, Key: "fixture"},
			&model.Task{PrivateData: model.TaskPrivateData{UpstreamTaskID: "fixture"}})
		result <- err
	}()
	select {
	case <-started:
		cancel()
	case <-ctx.Done():
		t.Fatal("fixture did not receive status request")
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelled status request remained open")
	}
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

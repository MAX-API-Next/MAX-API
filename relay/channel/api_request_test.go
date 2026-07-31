package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	common2 "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type taskHeaderAdaptor struct {
	taskcommon.BaseBilling
	url string
}

func (a taskHeaderAdaptor) Init(*relaycommon.RelayInfo) {}

func (a taskHeaderAdaptor) ValidateRequestAndSetAction(*gin.Context, *relaycommon.RelayInfo) *dto.TaskError {
	return nil
}

func (a taskHeaderAdaptor) BuildRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a taskHeaderAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("X-Default", "default")
	return nil
}

func (a taskHeaderAdaptor) BuildRequestBody(*gin.Context, *relaycommon.RelayInfo) (io.Reader, error) {
	return nil, nil
}

func (a taskHeaderAdaptor) DoRequest(*gin.Context, *relaycommon.RelayInfo, io.Reader) (*http.Response, error) {
	return nil, nil
}

func (a taskHeaderAdaptor) DoResponse(*gin.Context, *http.Response, *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	return "", nil, nil
}

func (a taskHeaderAdaptor) GetModelList() []string {
	return nil
}

func (a taskHeaderAdaptor) GetChannelName() string {
	return "test"
}

func (a taskHeaderAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}

func (a taskHeaderAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a taskHeaderAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

type countingReader struct {
	reads int
}

func (r *countingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

type customReadSeeker struct {
	reader *strings.Reader
}

func (r *customReadSeeker) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *customReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return r.reader.Seek(offset, whence)
}

type blockingPingWriter struct {
	gin.ResponseWriter
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	once     sync.Once
}

func (w *blockingPingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.started)
	})
	<-w.release
	close(w.finished)
	return len(p), nil
}

func TestNewTaskHTTPRequestDoesNotPreReadRequestBody(t *testing.T) {
	t.Parallel()

	body := &countingReader{}
	info := &relaycommon.RelayInfo{
		UpstreamRequestBodySize: 512,
	}

	req, err := newTaskHTTPRequest(http.MethodPost, "https://example.com/tasks", body, info)

	require.NoError(t, err)
	require.Equal(t, 0, body.reads)
	require.Equal(t, int64(512), req.ContentLength)
	require.Nil(t, req.GetBody)

	_, _ = req.Body.Read(make([]byte, 1))
	require.Equal(t, 1, body.reads)
}

func TestNewTaskHTTPRequestUsesSeekableGetBodyWithoutBuffering(t *testing.T) {
	t.Parallel()

	body := &customReadSeeker{reader: strings.NewReader("abcdef")}
	_, err := body.Seek(2, io.SeekStart)
	require.NoError(t, err)

	req, err := newTaskHTTPRequest(http.MethodPost, "https://example.com/tasks", body, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	require.Equal(t, int64(4), req.ContentLength)
	require.NotNil(t, req.GetBody)

	replay, err := req.GetBody()
	require.NoError(t, err)
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, "cdef", string(replayed))
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestDoTaskApiRequestInitializesChannelMetaForApiKeyPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"task_123"}`))
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common2.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-task")

	info := &relaycommon.RelayInfo{
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"Authorization": "Bearer {api_key}",
		},
	}

	resp, err := DoTaskApiRequest(taskHeaderAdaptor{url: server.URL}, ctx, info, strings.NewReader(`{"prompt":"test"}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "Bearer sk-task", gotAuthorization)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}

func TestDoTaskApiRequestAppliesRuntimeHeaderOverride(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var gotDefault string
	var gotRuntime string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDefault = r.Header.Get("X-Default")
		gotRuntime = r.Header.Get("X-Task-Runtime")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"task_123"}`))
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"prompt":"test"}`))

	info := &relaycommon.RelayInfo{
		ChannelMeta:               &relaycommon.ChannelMeta{},
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]interface{}{
			"X-Default":      "overridden",
			"X-Task-Runtime": "enabled",
		},
	}

	resp, err := DoTaskApiRequest(taskHeaderAdaptor{url: server.URL}, ctx, info, strings.NewReader(`{"prompt":"test"}`))

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "overridden", gotDefault)
	require.Equal(t, "enabled", gotRuntime)
}

func TestDoTaskApiRequestMarksWrittenTransportFailureAsOutcomeUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	requestReceived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(requestReceived)
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hijacker.Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"prompt":"test"}`))
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	resp, err := DoTaskApiRequest(taskHeaderAdaptor{url: server.URL}, ctx, info, strings.NewReader(`{"prompt":"test"}`))

	require.Error(t, err)
	require.Nil(t, resp)
	require.True(t, info.UpstreamTaskOutcomeUnknown)
	select {
	case <-requestReceived:
	default:
		t.Fatal("upstream did not receive the task request")
	}
}

func TestSendPingDataReturnsWhenWriterBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	writer := &blockingPingWriter{
		ResponseWriter: ctx.Writer,
		started:        make(chan struct{}),
		release:        make(chan struct{}),
		finished:       make(chan struct{}),
	}
	ctx.Writer = writer

	oldTimeout := sendPingDataTimeout
	sendPingDataTimeout = 20 * time.Millisecond
	t.Cleanup(func() {
		sendPingDataTimeout = oldTimeout
	})

	errCh := make(chan error, 1)
	var mutex sync.Mutex
	go func() {
		errCh <- sendPingData(ctx, &mutex)
	}()

	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("ping write did not start")
	}

	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "timed out")
	case <-time.After(time.Second):
		t.Fatal("sendPingData did not return after timeout")
	}

	close(writer.release)
	select {
	case <-writer.finished:
	case <-time.After(time.Second):
		t.Fatal("blocked ping writer did not finish after release")
	}
}

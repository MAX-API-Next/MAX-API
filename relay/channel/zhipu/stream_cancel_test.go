package zhipu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.closeNotify
}

type panicReadCloser struct {
	closed bool
}

func (r *panicReadCloser) Read([]byte) (int, error) {
	panic("upstream stream read panic")
}

func (r *panicReadCloser) Close() error {
	r.closed = true
	return nil
}

type errorReadCloser struct {
	closed bool
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("upstream stream read failed")
}

func (r *errorReadCloser) Close() error {
	r.closed = true
	return nil
}

func assertZhipuStreamReadFailure(t *testing.T, body io.ReadCloser, isClosed func() bool, message string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	usage, maxErr := zhipuStreamHandler(c, &relaycommon.RelayInfo{}, &http.Response{Body: body})

	require.Nil(t, usage)
	require.NotNil(t, maxErr)
	require.Equal(t, types.ErrorCodeReadResponseBodyFailed, maxErr.GetErrorCode())
	require.Contains(t, maxErr.Error(), message)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
	require.True(t, isClosed())
}

func TestZhipuStreamHandlerStopsAfterRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(&closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx, cancel := context.WithCancel(request.Context())
	c.Request = request.WithContext(ctx)
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })

	returned := make(chan struct{})
	go func() {
		_, _ = zhipuStreamHandler(c, &relaycommon.RelayInfo{}, &http.Response{Body: reader})
		close(returned)
	}()
	cancel()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("zhipu stream handler did not stop after client cancellation")
	}

	writeResult := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("data: event\n"))
		writeResult <- err
	}()
	select {
	case err := <-writeResult:
		require.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("zhipu stream response body was not closed")
	}
}

func TestZhipuStreamHandlerReturnsProducerPanic(t *testing.T) {
	body := &panicReadCloser{}
	assertZhipuStreamReadFailure(t, body, func() bool { return body.closed }, "upstream stream read panic")
}

func TestZhipuStreamHandlerReturnsScannerError(t *testing.T) {
	body := &errorReadCloser{}
	assertZhipuStreamReadFailure(t, body, func() bool { return body.closed }, "upstream stream read failed")
}

func TestZhipuStreamHandlerEmitsDoneOnNormalEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	usage, maxErr := zhipuStreamHandler(c, &relaycommon.RelayInfo{}, &http.Response{
		Body: io.NopCloser(strings.NewReader("")),
	})

	require.Nil(t, usage)
	require.Nil(t, maxErr)
	require.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestRequestOpenAI2ZhipuPreservesExplicitTopPZero(t *testing.T) {
	zero := 0.0
	request := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{TopP: &zero})

	payload, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"prompt":[],"top_p":0}`, string(payload))
}

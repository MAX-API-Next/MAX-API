package zhipu

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
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

func TestRequestOpenAI2ZhipuPreservesExplicitTopPZero(t *testing.T) {
	zero := 0.0
	request := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{TopP: &zero})

	payload, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"prompt":[],"top_p":0}`, string(payload))
}

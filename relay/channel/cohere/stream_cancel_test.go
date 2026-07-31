package cohere

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestCohereStreamHandlerStopsAfterRequestCancellation(t *testing.T) {
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
		_, _ = cohereStreamHandler(c, &relaycommon.RelayInfo{
			ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "cohere-test"},
		}, &http.Response{Body: reader})
		close(returned)
	}()
	cancel()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("cohere stream handler did not stop after client cancellation")
	}

	writeResult := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("event\n"))
		writeResult <- err
	}()
	select {
	case err := <-writeResult:
		require.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("cohere stream response body was not closed")
	}
}

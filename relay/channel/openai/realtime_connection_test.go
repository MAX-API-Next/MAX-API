package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestConfigureRealtimeConnectionTimesOutSilentPeer(t *testing.T) {
	readErr := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			readErr <- err
			return
		}
		defer conn.Close()
		if err := configureRealtimeConnection(conn, 25*time.Millisecond); err != nil {
			readErr <- err
			return
		}
		_, _, err = conn.ReadMessage()
		readErr <- err
	}))
	t.Cleanup(server.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	select {
	case err := <-readErr:
		require.Error(t, err)
		require.True(t, websocket.IsCloseError(err, websocket.CloseNormalClosure) || isTimeoutError(err), "expected timeout, got %v", err)
	case <-time.After(time.Second):
		t.Fatal("silent websocket peer did not time out")
	}
}

func isTimeoutError(err error) bool {
	timeout, ok := err.(interface{ Timeout() bool })
	return ok && timeout.Timeout()
}

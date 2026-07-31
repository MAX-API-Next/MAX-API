package xunfei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestEmitXunfeiEventStopsWhenRequestContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan bool, 1)
	go func() {
		done <- emitXunfeiEvent(ctx, make(chan xunfeiEvent), xunfeiEvent{})
	}()

	select {
	case emitted := <-done:
		require.False(t, emitted)
	case <-time.After(time.Second):
		t.Fatal("xunfei event delivery blocked after request cancellation")
	}
}

func TestXunfeiMakeRequestRejectsNonSwitchingProtocolsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	authURL := "ws" + strings.TrimPrefix(server.URL, "http")
	events, err := xunfeiMakeRequest(context.Background(), dto.GeneralOpenAIRequest{}, "generalv3", authURL, "app-id")
	require.Error(t, err)
	require.Nil(t, events)
	require.Contains(t, err.Error(), "handshake failed")
}

func TestXunfeiMakeRequestSurfacesUpstreamHeaderError(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		_, _, err = conn.ReadMessage()
		require.NoError(t, err)
		require.NoError(t, conn.WriteJSON(map[string]any{
			"header": map[string]any{
				"code":    10013,
				"message": "invalid api key",
			},
		}))
	}))
	defer server.Close()

	authURL := "ws" + strings.TrimPrefix(server.URL, "http")
	events, err := xunfeiMakeRequest(context.Background(), dto.GeneralOpenAIRequest{}, "generalv3", authURL, "app-id")
	require.NoError(t, err)

	select {
	case event, ok := <-events:
		require.True(t, ok)
		require.Nil(t, event.Response)
		require.Error(t, event.Err)
		require.Contains(t, event.Err.Error(), "xunfei upstream error 10013: invalid api key")
	case <-time.After(time.Second):
		t.Fatal("xunfei upstream error was not delivered")
	}
}

func TestRequestOpenAI2XunfeiPreservesExplicitZeroValues(t *testing.T) {
	zeroInt := 0
	zeroUint := uint(0)
	completionCount := 2
	request := requestOpenAI2Xunfei(dto.GeneralOpenAIRequest{
		TopK:      &zeroInt,
		N:         &completionCount,
		MaxTokens: &zeroUint,
	}, "app-id", "generalv3")

	payload, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"header":{"app_id":"app-id"},"parameter":{"chat":{"domain":"generalv3","top_k":0,"max_tokens":0}},"payload":{"message":{"text":[]}}}`, string(payload))
}

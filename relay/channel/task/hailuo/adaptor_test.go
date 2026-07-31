package hailuo

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultWaitsForRetrievableVideoURL(t *testing.T) {
	service.InitHttpClient()

	requestPath := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath <- r.URL.Path
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	adaptor := &TaskAdaptor{apiKey: "test-key", baseURL: server.URL}
	result, err := adaptor.ParseTaskResult([]byte(`{
		"task_id":"task-1",
		"status":"Success",
		"file_id":"file-1",
		"base_resp":{"status_code":0}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusInProgress), result.Status)
	require.Equal(t, "90%", result.Progress)
	require.Empty(t, result.Url)

	select {
	case path := <-requestPath:
		require.Equal(t, "/v1/files/retrieve", path)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the Hailuo file retrieval request")
	}
}

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchUpstreamMetadataFailsWhenVendorsAreUnavailable(t *testing.T) {
	t.Setenv("SYNC_HTTP_RETRY", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case "/vendors":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"message":"temporarily unavailable"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, _, err := fetchUpstreamMetadata(context.Background(), server.URL+"/models", server.URL+"/vendors")
	require.Error(t, err)
	require.ErrorContains(t, err, "fetch upstream vendors")
}

func TestChooseStatusPreservesExplicitDisabledValue(t *testing.T) {
	disabled := 0
	enabled := 1

	require.Equal(t, 0, chooseStatus(&disabled, enabled))
	require.Equal(t, enabled, chooseStatus(nil, enabled))
}

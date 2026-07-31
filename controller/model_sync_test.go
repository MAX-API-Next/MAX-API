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

func TestFetchUpstreamMetadataFailsWhenModelsAreUnavailable(t *testing.T) {
	t.Setenv("SYNC_HTTP_RETRY", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"success":false,"message":"models unavailable"}`))
		case "/vendors":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"name":"Example Vendor"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, _, err := fetchUpstreamMetadata(context.Background(), server.URL+"/models", server.URL+"/vendors")
	require.Error(t, err)
	require.ErrorContains(t, err, "fetch upstream models")
	require.ErrorContains(t, err, "models unavailable")
}

func TestFetchUpstreamMetadataFailsWhenModelsTransportFails(t *testing.T) {
	t.Setenv("SYNC_HTTP_RETRY", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/vendors" {
			_, _ = w.Write([]byte(`{"success":true,"data":[{"name":"Example Vendor"}]}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := fetchUpstreamMetadata(context.Background(), server.URL+"/models", server.URL+"/vendors")
	require.Error(t, err)
	require.ErrorContains(t, err, "fetch upstream models")
	require.ErrorContains(t, err, "404 Not Found")
}

func TestFetchUpstreamMetadataAcceptsArrayResponses(t *testing.T) {
	t.Setenv("SYNC_HTTP_RETRY", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`[{"model_name":"example-model","vendor_name":"Example Vendor"}]`))
		case "/vendors":
			_, _ = w.Write([]byte(`[{"name":"Example Vendor"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	vendors, models, err := fetchUpstreamMetadata(context.Background(), server.URL+"/models", server.URL+"/vendors")
	require.NoError(t, err)
	require.True(t, vendors.Success)
	require.True(t, models.Success)
	require.Equal(t, "Example Vendor", vendors.Data[0].Name)
	require.Equal(t, "example-model", models.Data[0].ModelName)
}

func TestChooseStatusPreservesExplicitDisabledValue(t *testing.T) {
	disabled := 0
	enabled := 1

	require.Equal(t, 0, chooseStatus(&disabled, enabled))
	require.Equal(t, enabled, chooseStatus(nil, enabled))
}

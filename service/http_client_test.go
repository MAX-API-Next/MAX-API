package service

import (
	"net/http"
	"testing"
)

func TestNewSSRFProtectedHTTPClientDisablesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8080")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")

	client := newSSRFProtectedHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatalf("expected SSRF-protected client to disable environment proxy support")
	}
}

func TestNewBaseTransportKeepsEnvironmentProxySupport(t *testing.T) {
	transport := newBaseTransport(http.ProxyFromEnvironment)
	if transport.Proxy == nil {
		t.Fatalf("expected base transport to keep environment proxy support")
	}
}

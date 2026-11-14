// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestBrokerPoliciesRunBeforeAuthorizationOrNetwork(t *testing.T) {
	var sends, grants atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1) }))
	defer server.Close()
	b, err := NewHTTPBroker(server.URL, nil, server.Client(), func(string) error { return nil })
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		purpose RequestPurpose
		request RequestDescriptor
	}{
		{"foreign origin", SubmitRequest, RequestDescriptor{URL: "https://foreign.invalid/create"}},
		{"userinfo", SubmitRequest, RequestDescriptor{URL: "http://secret@" + server.Listener.Addr().String()}},
		{"bad scheme", SubmitRequest, RequestDescriptor{URL: "file:///etc/passwd"}},
		{"framing header", SubmitRequest, RequestDescriptor{URL: server.URL, Headers: map[string]string{"Content-Length": "0"}}},
		{"header injection", SubmitRequest, RequestDescriptor{URL: server.URL, Headers: map[string]string{"X-Key": "x\r\nX-Other:y"}}},
		{"duplicate auth", SubmitRequest, RequestDescriptor{URL: server.URL, Headers: map[string]string{"authorization": "a", "Authorization": "b"}}},
		{"query mutation", QueryRequest, RequestDescriptor{URL: server.URL, Method: "POST"}},
		{"unsupported SSE", SubmitRequest, RequestDescriptor{URL: server.URL, ResponseType: "sse"}},
		{"unsupported multipart", SubmitRequest, RequestDescriptor{URL: server.URL, BodyType: "multipart"}},
		{"unresolved attachment", SubmitRequest, RequestDescriptor{URL: server.URL, Body: []byte(`{"nested":[{"__fileRef":"request_file:image","encoding":"base64"}]}`)}},
		{"invalid JSON descriptor", SubmitRequest, RequestDescriptor{URL: server.URL, Body: []byte(`{"unfinished":`)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := b.Do(t.Context(), test.purpose, test.request, func() error { grants.Add(1); return nil })
			require.Error(t, err)
		})
	}
	require.Zero(t, sends.Load())
	require.Zero(t, grants.Load())
	_, err = b.Do(t.Context(), SubmitRequest, RequestDescriptor{URL: server.URL}, nil)
	require.ErrorIs(t, err, ErrRequestNotAuthorized)
	_, err = b.Do(t.Context(), SubmitRequest, RequestDescriptor{URL: server.URL}, func() error { return ErrBindingRejected })
	require.ErrorIs(t, err, ErrRequestNotAuthorized)
	require.Zero(t, sends.Load())
	b.validateURL = func(string) error { return ErrRequestPolicy }
	_, err = b.Do(t.Context(), SubmitRequest, RequestDescriptor{URL: server.URL}, func() error { t.Error("SSRF rejection must precede authorization"); return nil })
	require.ErrorIs(t, err, ErrRequestPolicy)
	for _, allowed := range [][]string{{"bad.invalid/path"}, {"user@host.invalid"}, {"a.invalid#fragment"}} {
		_, err := NewHTTPBroker(server.URL, allowed, server.Client(), func(string) error { return nil })
		require.ErrorIs(t, err, ErrRequestPolicy)
	}
}

func TestBrokerOriginAndCredentiallessHeaders(t *testing.T) {
	b, err := NewHTTPBroker("https://provider.invalid/base", []string{"api.provider.invalid:443"}, http.DefaultClient, func(string) error { return nil })
	require.NoError(t, err)
	_, err = b.allowedURL("https://PROVIDER.invalid:443/task", false)
	require.NoError(t, err)
	_, err = b.allowedURL("https://api.provider.invalid/task", false)
	require.NoError(t, err)
	_, err = b.allowedURL("https://provider.invalid:444/task", false)
	require.ErrorIs(t, err, ErrRequestPolicy)
	_, err = b.allowedURL("http://provider.invalid/task", true)
	require.ErrorIs(t, err, ErrRequestPolicy)
	_, err = b.allowedURL("https://artifact.invalid/result", true)
	require.NoError(t, err)
	h, err := safePluginHeaders(map[string]string{"Authorization": "secret", "X-API-Key": "secret", "Cookie": "secret", "Range": "bytes=0-1", "Accept": "video/mp4"}, true)
	require.NoError(t, err)
	require.Equal(t, http.Header{"Accept": []string{"video/mp4"}, "Range": []string{"bytes=0-1"}}, h)
}

func TestBrokerNeverFollowsRedirectsOrRetriesLostResponse(t *testing.T) {
	var redirects, posts atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
			return
		}
		posts.Add(1)
		// The provider accepted the POST and lost its response. The host must not
		// infer that nothing happened, leak its URL/key, or transparently retry.
		_, _ = io.Copy(io.Discard, r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	b, err := NewHTTPBroker(server.URL, nil, server.Client(), func(string) error { return nil })
	require.NoError(t, err)
	for _, purpose := range []RequestPurpose{SubmitRequest, QueryRequest} {
		response, err := b.Do(t.Context(), purpose, RequestDescriptor{URL: server.URL + "/redirect", Headers: map[string]string{"Authorization": "Bearer synthetic"}}, func() error { return nil })
		require.NoError(t, err)
		require.Equal(t, 307, response.StatusCode)
	}
	require.Zero(t, redirects.Load())
	_, err = b.Do(t.Context(), SubmitRequest, RequestDescriptor{URL: server.URL + "/lost", Headers: map[string]string{"Idempotency-Key": "synthetic"}}, func() error { return nil })
	require.ErrorIs(t, err, ErrSubmissionUnknown)
	require.NotContains(t, err.Error(), server.URL)
	require.EqualValues(t, 1, posts.Load())
}

func TestBrokerBoundsResponseAndUsesCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", MaxPackageBytes+1))
	}))
	defer server.Close()
	b, err := NewHTTPBroker(server.URL, nil, server.Client(), func(string) error { return nil })
	require.NoError(t, err)
	_, err = b.Do(t.Context(), SubmitRequest, RequestDescriptor{URL: server.URL}, func() error { return nil })
	require.ErrorIs(t, err, ErrSubmissionUnknown)
	_, err = b.Do(t.Context(), QueryRequest, RequestDescriptor{URL: server.URL}, func() error { return nil })
	require.ErrorIs(t, err, ErrResponseUnavailable)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = b.Do(ctx, SubmitRequest, RequestDescriptor{URL: server.URL}, func() error { t.Error("canceled before authorization"); return nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestBrokerSendsSerializedTextWithoutAddingJSONQuotes(t *testing.T) {
	const expected = `{"z":1,"a":0}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != expected {
			t.Error("descriptor text was quoted or reordered")
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	b, err := NewHTTPBroker(server.URL, nil, server.Client(), func(string) error { return nil })
	require.NoError(t, err)
	wire, err := common.Marshal(expected)
	require.NoError(t, err)
	_, err = b.Do(t.Context(), SubmitRequest, RequestDescriptor{URL: server.URL, Body: wire, Method: " post "}, func() error { return nil })
	require.NoError(t, err)
}

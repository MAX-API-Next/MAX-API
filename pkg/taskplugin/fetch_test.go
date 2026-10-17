/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskplugin

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/stretchr/testify/require"
)

type pluginRoundTrip func(*http.Request) (*http.Response, error)

func (f pluginRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testFetchScope(t *testing.T) *FetchScope {
	t.Helper()
	host := NewHost()
	manifest := validManifest()
	manifest.AllowedHosts = []string{"assets.vendor.example:443"}
	_, err := host.Install(testPackage(t, manifest, "original"))
	require.NoError(t, err)
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "generation-one"}
	binding, err := host.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.DoubaoVideoContract())
	require.NoError(t, err)
	session, err := host.Resolve(binding, actor)
	require.NoError(t, err)
	scope, err := session.NewFetchScope(context.Background(), "https://channel.vendor.example", "Bearer synthetic-only")
	require.NoError(t, err)
	t.Cleanup(scope.Close)
	transport := scope.client.Transport.(*http.Transport)
	require.Nil(t, transport.Proxy)
	require.NotNil(t, transport.DialContext)
	require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	require.Nil(t, scope.client.Jar)
	return scope
}

func TestPluginFetchSeparatesCredentialsAndDoesNotFollowRedirects(t *testing.T) {
	scope := testFetchScope(t)
	calls := 0
	scope.client.Transport = pluginRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "GET", request.Method)
		if request.URL.Host == "channel.vendor.example" {
			require.Equal(t, "Bearer synthetic-only", request.Header.Get("Authorization"))
		} else {
			require.Empty(t, request.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://blocked.invalid/token"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	})
	for _, target := range []string{"https://channel.vendor.example/status", "https://assets.vendor.example/status"} {
		result, err := scope.Fetch(target)
		require.NoError(t, err)
		require.Equal(t, 302, result.StatusCode)
	}
	require.Equal(t, 2, calls)
	for _, target := range []string{"https://blocked.invalid", "http://channel.vendor.example/status"} {
		_, err := scope.Fetch(target)
		require.ErrorIs(t, err, ErrFetchRejected)
	}
	require.Equal(t, 2, calls)
}

func TestPluginFetchBudgetsRevocationAndRedactedErrors(t *testing.T) {
	t.Run("body", func(t *testing.T) {
		scope := testFetchScope(t)
		scope.client.Transport = pluginRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxFetchBytes+1)))}, nil
		})
		_, err := scope.Fetch("https://channel.vendor.example/status?token=synthetic-only")
		require.ErrorIs(t, err, ErrFetchRejected)
		require.NotContains(t, err.Error(), "synthetic-only")
	})
	t.Run("calls", func(t *testing.T) {
		scope := testFetchScope(t)
		calls := 0
		scope.client.Transport = pluginRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})
		for i := 0; i < MaxFetchCalls; i++ {
			_, err := scope.Fetch("https://channel.vendor.example/status")
			require.NoError(t, err)
		}
		_, err := scope.Fetch("https://channel.vendor.example/status")
		require.ErrorIs(t, err, ErrFetchRejected)
		require.Equal(t, MaxFetchCalls, calls, "exhausted scope must reject before transport")
	})
	t.Run("revoked", func(t *testing.T) {
		scope := testFetchScope(t)
		require.NoError(t, scope.session.host.Revoke("vendor", "1.0.0"))
		_, err := scope.Fetch("https://channel.vendor.example/status")
		require.ErrorIs(t, err, ErrBindingRejected)
	})
	t.Run("canceled", func(t *testing.T) {
		scope := testFetchScope(t)
		scope.Close()
		_, err := scope.Fetch("https://channel.vendor.example/status")
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestPluginDialRejectsPrivateAndRebindingTargetsBeforeDial(t *testing.T) {
	for _, ips := range [][]string{{"127.0.0.1"}, {"169.254.169.254"}, {"::1"}, {"::ffff:127.0.0.1"}, {"8.8.8.8", "10.0.0.1"}} {
		called := false
		_, err := pluginDial(context.Background(), "tcp", "allowed.example:443", func(context.Context, string) ([]net.IPAddr, error) {
			var addresses []net.IPAddr
			for _, ip := range ips {
				addresses = append(addresses, net.IPAddr{IP: net.ParseIP(ip)})
			}
			return addresses, nil
		}, func(context.Context, string, string) (net.Conn, error) { called = true; return nil, nil })
		require.ErrorIs(t, err, ErrFetchRejected)
		require.False(t, called)
	}
	var actual string
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	connection, err := pluginDial(context.Background(), "tcp", "allowed.example:443", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		actual = address
		return left, nil
	})
	require.NoError(t, err)
	require.Same(t, left, connection)
	require.Equal(t, "8.8.8.8:443", actual)
}

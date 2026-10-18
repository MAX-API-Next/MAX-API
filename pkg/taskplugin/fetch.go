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
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
)

const (
	MaxFetchBytes = 1 << 20
	MaxFetchCalls = 4
)

var ErrFetchRejected = errors.New("task plugin fetch rejected or failed")

// FetchScope is a read-only HTTP capability for one hook invocation. It has no
// cookie jar, proxy, redirects, body, caller headers, pricing or write access.
type FetchScope struct {
	session       *Session
	ctx           context.Context
	cancel        context.CancelFunc
	client        *http.Client
	baseOrigin    string
	allowed       map[string]bool
	authorization string
	calls         atomic.Uint32
}

type FetchResult struct {
	StatusCode int
	Body       []byte
}

// NewFetchScope takes credentials from the host channel configuration. They are
// sent only to the exact HTTPS channel origin, never to extra allowed hosts.
// A new scope must be closed by the host after each hook, even on hook failure.
func (session *Session) NewFetchScope(ctx context.Context, channelBaseURL, authorization string) (*FetchScope, error) {
	if err := session.validate(); err != nil {
		return nil, err
	}
	if ctx == nil || validateHTTPBaseURL(channelBaseURL) != nil || strings.ContainsAny(authorization, "\r\n") {
		return nil, ErrFetchRejected
	}
	base, err := url.Parse(channelBaseURL)
	if err != nil || base.Scheme != "https" {
		return nil, ErrFetchRejected
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	scope := &FetchScope{session: session, ctx: bounded, cancel: cancel, allowed: map[string]bool{}, authorization: authorization, baseOrigin: httpsOrigin(base)}
	if scope.baseOrigin == "" {
		cancel()
		return nil, ErrFetchRejected
	}
	scope.allowed[scope.baseOrigin] = true
	for _, host := range session.plugin.manifest.AllowedHosts {
		parsed, err := url.Parse("https://" + host)
		if err != nil || httpsOrigin(parsed) == "" {
			cancel()
			return nil, ErrFetchRejected
		}
		scope.allowed[httpsOrigin(parsed)] = true
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            strictPluginDial,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DisableKeepAlives:      true,
	}
	scope.client = &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return scope, nil
}

func (scope *FetchScope) Close() {
	if scope != nil {
		scope.cancel()
		scope.client.CloseIdleConnections()
	}
}

// Fetch never follows a redirect and never returns network errors containing a
// signed URL, query, header or credential. The original context error is safe.
func (scope *FetchScope) Fetch(rawURL string) (FetchResult, error) {
	if scope == nil || len(rawURL) > 8192 {
		return FetchResult{}, ErrFetchRejected
	}
	if err := scope.session.validate(); err != nil {
		return FetchResult{}, err
	}
	if err := scope.ctx.Err(); err != nil {
		return FetchResult{}, err
	}
	if scope.calls.Add(1) > MaxFetchCalls {
		return FetchResult{}, ErrFetchRejected
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" || !isASCII(u.Host) || strings.ContainsAny(rawURL, "\\\r\n") {
		return FetchResult{}, ErrFetchRejected
	}
	origin := httpsOrigin(u)
	if !scope.allowed[origin] || origin == "" {
		return FetchResult{}, ErrFetchRejected
	}
	request, err := http.NewRequestWithContext(scope.ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return FetchResult{}, ErrFetchRejected
	}
	request.Header.Set("Accept", "application/json")
	if origin == scope.baseOrigin && scope.authorization != "" {
		request.Header.Set("Authorization", scope.authorization)
	}
	response, err := scope.client.Do(request)
	if err != nil {
		if ctxErr := scope.ctx.Err(); ctxErr != nil {
			return FetchResult{}, ctxErr
		}
		return FetchResult{}, ErrFetchRejected
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength > MaxFetchBytes {
		return FetchResult{}, ErrFetchRejected
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxFetchBytes+1))
	if err != nil || len(body) > MaxFetchBytes {
		return FetchResult{}, ErrFetchRejected
	}
	if err := scope.session.validate(); err != nil {
		return FetchResult{}, err
	}
	return FetchResult{StatusCode: response.StatusCode, Body: body}, nil
}

func httpsOrigin(u *url.URL) string {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || !isASCII(u.Host) {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number <= 0 || number > 65535 {
		return ""
	}
	return "https://" + net.JoinHostPort(strings.ToLower(u.Hostname()), strconv.Itoa(number))
}

// Resolve and validate every address, then dial the chosen literal IP. This
// avoids a second DNS lookup between the SSRF check and the actual connection.
func strictPluginDial(ctx context.Context, network, address string) (net.Conn, error) {
	return pluginDial(ctx, network, address, net.DefaultResolver.LookupIPAddr, (&net.Dialer{Timeout: 5 * time.Second}).DialContext)
}

func pluginDial(ctx context.Context, network, address string, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrFetchRejected
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, ErrFetchRejected
	}
	protection := &common.SSRFProtection{AllowPrivateIp: false, DomainFilterMode: false, IpFilterMode: false, AllowedPorts: []int{port}, ApplyIPFilterForDomain: true}
	if err := protection.ValidateNetworkTarget(host, port); err != nil {
		return nil, ErrFetchRejected
	}
	var addresses []net.IPAddr
	if ip := net.ParseIP(host); ip != nil {
		addresses = []net.IPAddr{{IP: ip}}
	} else {
		addresses, err = lookup(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, ErrFetchRejected
		}
	}
	for _, address := range addresses {
		if address.Zone != "" || protection.ValidateResolvedIP(host, address.IP) != nil {
			return nil, ErrFetchRejected
		}
	}
	for _, address := range addresses {
		connection, err := dial(ctx, network, net.JoinHostPort(address.IP.String(), portText))
		if err == nil {
			return connection, nil
		}
	}
	return nil, ErrFetchRejected
}

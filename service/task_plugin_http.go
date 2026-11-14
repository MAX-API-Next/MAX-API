// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"net/http"

	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
)

type taskPluginTransport struct {
	base  http.RoundTripper
	proxy bool
}

func (t taskPluginTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_, enabled, err := getCachedSSRFProtection()
	if err != nil || (enabled && t.proxy) || ValidateSSRFProtectedFetchURL(req.URL.String()) != nil {
		return nil, taskplugin.ErrRequestPolicy
	}
	return t.base.RoundTrip(req)
}

// NewTaskPluginHTTPBroker uses the host-selected channel and the same protected
// URL and dial policy as host fetches. A proxy cannot silently replace protected
// destination DNS/IP checks with proxy-side DNS. That combination stays closed
// until a protected proxy transport is implemented and tested.
func NewTaskPluginHTTPBroker(executor *taskplugin.Executor, channel taskplugin.ChannelInput, proxyURL string) (*taskplugin.HTTPBroker, error) {
	_, enabled, err := getCachedSSRFProtection()
	if err != nil {
		return nil, taskplugin.ErrRequestPolicy
	}
	// Keep the protected dialer even while policy is disabled, so a later
	// policy change cannot leave this broker with an unprotected transport.
	client := newSSRFProtectedHTTPClient()
	// Connections opened under a disabled policy must not bypass the protected
	// dialer through reuse after a policy change. This is scoped to this SDK.
	client.Transport.(*http.Transport).DisableKeepAlives = true
	validateURL := ValidateSSRFProtectedFetchURL
	if proxyURL != "" {
		if enabled {
			return nil, taskplugin.ErrPluginSDK
		}
		client, err = GetHttpClientWithProxy(proxyURL)
		if err != nil {
			return nil, taskplugin.ErrRequestPolicy
		}
		validateURL = func(url string) error {
			_, enabled, err := getCachedSSRFProtection()
			if err != nil || enabled {
				return taskplugin.ErrRequestPolicy
			}
			return ValidateSSRFProtectedFetchURL(url)
		}
	}
	copyClient := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copyClient.Transport = taskPluginTransport{base: base, proxy: proxyURL != ""}
	client = &copyClient
	return executor.NewHTTPBroker(channel, client, validateURL)
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import "github.com/MAX-API-Next/MAX-API/pkg/taskplugin"

// NewTaskPluginHTTPBroker uses the host-selected channel and the same protected
// URL and dial policy as host fetches. A proxy cannot silently replace protected
// destination DNS/IP checks with proxy-side DNS. That combination stays closed
// until a protected proxy transport is implemented and tested.
func NewTaskPluginHTTPBroker(executor *taskplugin.Executor, channel taskplugin.ChannelInput, proxyURL string) (*taskplugin.HTTPBroker, error) {
	_, enabled, err := getCachedSSRFProtection()
	if err != nil {
		return nil, taskplugin.ErrRequestPolicy
	}
	client := GetSSRFProtectedHttpClient()
	if proxyURL != "" {
		if enabled {
			return nil, taskplugin.ErrPluginSDK
		}
		client, err = GetHttpClientWithProxy(proxyURL)
		if err != nil {
			return nil, taskplugin.ErrRequestPolicy
		}
	}
	return executor.NewHTTPBroker(channel, client, ValidateSSRFProtectedFetchURL)
}

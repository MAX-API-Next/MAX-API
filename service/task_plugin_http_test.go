// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/plugins"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestOfficialTaskDriverUsesHostProtectedHTTP(t *testing.T) {
	configureSSRFProtectedFetchTest(t)
	source, err := plugins.TaskSources.ReadFile("tasks/kling/1.2.0/plugin.js")
	require.NoError(t, err)
	p, err := taskplugin.LoadJavaScript(bytes.NewReader(source), "f4b949679dba31e287d2b21d4b3b4c09980c9bb744da9ede5153f1801cf84240")
	require.NoError(t, err)
	host := taskplugin.NewHost()
	_, err = host.Install(p)
	require.NoError(t, err)
	e, err := host.BindExecutor(taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "isolated"}, "kling", "kling-v2-5-turbo", "kling-v2-5-turbo", "text_to_video")
	require.NoError(t, err)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"isolated-1"}}`)
	}))
	defer server.Close()
	channel := taskplugin.ChannelInput{ID: 3, BaseURL: server.URL, APIKey: "synthetic-only", UpstreamKind: "vendor"}
	prepared, err := e.Prepare(t.Context(), channel, []byte(`{"duration":5}`))
	require.NoError(t, err)
	b, err := NewTaskPluginHTTPBroker(e, channel, "")
	require.NoError(t, err)
	_, err = prepared.Submit(t.Context(), b, func(taskplugin.ExecutionBinding) error {
		t.Error("private address must fail before authorization")
		return nil
	})
	require.ErrorIs(t, err, taskplugin.ErrRequestPolicy)
	require.Zero(t, requests.Load())
	_, err = NewTaskPluginHTTPBroker(e, channel, "http://127.0.0.1:1234")
	require.ErrorIs(t, err, taskplugin.ErrPluginSDK)
	// Explicit test-only host policy permits the local fake provider. No global
	// application config or external endpoint is written or contacted.
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	b, err = NewTaskPluginHTTPBroker(e, channel, "")
	require.NoError(t, err)
	result, err := prepared.Submit(t.Context(), b, func(taskplugin.ExecutionBinding) error { return nil })
	require.NoError(t, err)
	require.Equal(t, "isolated-1", result.Task.UpstreamTaskID)
	require.EqualValues(t, 1, requests.Load())
}

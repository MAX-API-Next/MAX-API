package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func withLogAuditSettings(t *testing.T, requestEnabled bool, responseEnabled bool) {
	t.Helper()
	prevRequest := common.LogRequestContentEnabled
	prevResponse := common.LogResponseContentEnabled
	common.LogRequestContentEnabled = requestEnabled
	common.LogResponseContentEnabled = responseEnabled
	t.Cleanup(func() {
		common.LogRequestContentEnabled = prevRequest
		common.LogResponseContentEnabled = prevResponse
	})
}

func TestFormatUserLogsExposesOnlyUserAuditContent(t *testing.T) {
	withLogAuditSettings(t, true, true)

	logs := []*Log{
		{
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"request_content":            "user prompt",
					"request_content_truncated":  true,
					"response_content":           "model answer",
					"response_content_truncated": true,
					"local_count_tokens":         true,
					"use_channel":                []int{1, 2},
				},
				"audit_info": map[string]interface{}{
					"request_content": "stale audit content",
				},
				"stream_status": map[string]interface{}{
					"status": "error",
				},
			}),
		},
	}

	formatUserLogs(logs, 10)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, other, "admin_info")
	require.NotContains(t, other, "stream_status")
	require.Equal(t, 11, logs[0].Id)

	auditInfo, ok := other["audit_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "user prompt", auditInfo["request_content"])
	require.Equal(t, true, auditInfo["request_content_truncated"])
	require.Equal(t, "model answer", auditInfo["response_content"])
	require.Equal(t, true, auditInfo["response_content_truncated"])
	require.NotContains(t, auditInfo, "local_count_tokens")
	require.NotContains(t, auditInfo, "use_channel")
}

func TestFormatUserLogsHidesAuditContentWhenDisabled(t *testing.T) {
	withLogAuditSettings(t, false, false)

	logs := []*Log{
		{
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"request_content":  "user prompt",
					"response_content": "model answer",
				},
			}),
		},
	}

	formatUserLogs(logs, 0)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, other, "admin_info")
	require.NotContains(t, other, "audit_info")
}

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

func TestFormatUserLogDetailExposesOnlyUserAuditContent(t *testing.T) {
	withLogAuditSettings(t, true, true)

	log := &Log{
		Id:          42,
		ChannelName: "secret channel",
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
	}

	formatUserLog(log)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, 42, log.Id)
	require.Empty(t, log.ChannelName)
	require.NotContains(t, other, "admin_info")
	require.NotContains(t, other, "stream_status")

	auditInfo, ok := other["audit_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "user prompt", auditInfo["request_content"])
	require.Equal(t, true, auditInfo["request_content_truncated"])
	require.Equal(t, "model answer", auditInfo["response_content"])
	require.Equal(t, true, auditInfo["response_content_truncated"])
	require.NotContains(t, auditInfo, "local_count_tokens")
	require.NotContains(t, auditInfo, "use_channel")
}

func TestFormatUserLogsStripsAuditContentFromList(t *testing.T) {
	withLogAuditSettings(t, true, true)

	logs := []*Log{
		{
			Id: 42,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"request_content":            "user prompt",
					"request_content_truncated":  true,
					"response_content":           "model answer",
					"response_content_truncated": true,
					"local_count_tokens":         true,
					"use_channel":                []int{1, 2},
				},
			}),
		},
	}

	formatUserLogs(logs, 10)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, 42, logs[0].LogId)
	require.Equal(t, 11, logs[0].Id)
	require.NotContains(t, other, "admin_info")

	auditInfo, ok := other["audit_info"].(map[string]interface{})
	require.True(t, ok)
	require.NotContains(t, auditInfo, "request_content")
	require.NotContains(t, auditInfo, "response_content")
	require.Equal(t, true, auditInfo["request_content_truncated"])
	require.Equal(t, true, auditInfo["response_content_truncated"])
}

func TestStripLogAuditContentKeepsAdminMetadata(t *testing.T) {
	log := &Log{
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"request_content":            "user prompt",
				"request_content_truncated":  true,
				"response_content":           "model answer",
				"response_content_truncated": true,
				"local_count_tokens":         true,
				"use_channel":                []int{1, 2},
			},
		}),
	}

	stripLogAuditContent(log)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.NotContains(t, adminInfo, "request_content")
	require.NotContains(t, adminInfo, "response_content")
	require.Equal(t, true, adminInfo["request_content_truncated"])
	require.Equal(t, true, adminInfo["response_content_truncated"])
	require.Equal(t, true, adminInfo["local_count_tokens"])
	require.Contains(t, adminInfo, "use_channel")
}

func TestFormatUserLogsHidesAuditContentWhenDisabled(t *testing.T) {
	withLogAuditSettings(t, false, false)

	logs := []*Log{
		{
			Id: 42,
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
	require.Equal(t, 42, logs[0].LogId)
	require.NotContains(t, other, "admin_info")
	require.NotContains(t, other, "audit_info")
}

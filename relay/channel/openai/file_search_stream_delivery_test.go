package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestOaiFileSearchCompletedItemsRetainPartialUsage(t *testing.T) {
	oldEmpty, oldRetry := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldEmpty, oldRetry })
	for _, retry := range []bool{false, true} {
		for _, status := range []string{"completed", ""} {
			for _, malformed := range []bool{false, true} {
				name := "legacy_status"
				if status != "" {
					name = status
				}
				t.Run(fmt.Sprintf("retry_%t/%s/malformed_%t", retry, name, malformed), func(t *testing.T) {
					common.EmptyCompletionRetryEnabled = retry
					c, info := newResponsesUsageTestContext()
					info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
						dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
					}}
					item := map[string]any{"type": dto.BuildInCallFileSearchCall, "id": "fs_test", "status": status,
						"results": []any{map[string]any{"text": "synthetic-search-result"}}}
					data, err := common.Marshal(map[string]any{"type": dto.ResponsesOutputTypeItemDone, "output_index": 0, "item": item})
					require.NoError(t, err)
					body := "data: " + `{"type":"response.created","response":{"id":"resp_file_search","usage":{"input_tokens":4,"output_tokens":2}}}` + "\n\n"
					body += "data: " + string(data) + "\n\ndata: " + string(data) + "\n\n"
					if malformed {
						body += "data: {broken\n\n"
					}
					usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))})
					require.NotNil(t, apiErr)
					require.True(t, types.IsSkipRetryError(apiErr))
					require.NotNil(t, usage)
					require.Equal(t, 4, usage.PromptTokens)
					require.Equal(t, 2, usage.CompletionTokens)
					require.True(t, info.HasRecordedChannelFirstResult())
					require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
				})
			}
		}
	}
}

func TestOaiFileSearchLifecycleItemsRemainMetadata(t *testing.T) {
	oldEmpty := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmpty })
	for _, tc := range []struct{ name, eventType, status string }{
		{"added_running", dto.ResponsesOutputTypeItemAdded, "in_progress"},
		{"added_completed", dto.ResponsesOutputTypeItemAdded, "completed"},
		{"done_running", dto.ResponsesOutputTypeItemDone, "in_progress"},
		{"done_failed", dto.ResponsesOutputTypeItemDone, "failed"},
		{"done_cancelled", dto.ResponsesOutputTypeItemDone, "cancelled"},
		{"done_unknown", dto.ResponsesOutputTypeItemDone, "future_status"},
		{"nil_item", dto.ResponsesOutputTypeItemDone, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := dto.ResponsesStreamResponse{Type: tc.eventType, Item: &dto.ResponsesOutput{Type: dto.BuildInCallFileSearchCall, Status: tc.status}}
			if tc.name == "nil_item" {
				event.Item = nil
			}
			require.False(t, helper.ResponsesStreamEventHasOutput(event))
			require.False(t, helper.ResponsesStreamEventHasFirstResult(event))
			data, err := common.Marshal(event)
			require.NoError(t, err)
			c, info := newResponsesUsageTestContext()
			usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + string(data) + "\n\n"))})
			require.NotNil(t, apiErr)
			require.Nil(t, usage)
			require.False(t, info.HasRecordedChannelFirstResult())
		})
	}
}

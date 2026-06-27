package openai

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

const maxEmptyCompletionRetries = 1

func newEmptyCompletionRetryError() *types.MaxAPIError {
	return types.NewOpenAIError(
		fmt.Errorf("upstream returned an empty completion"),
		types.ErrorCodeEmptyCompletion,
		http.StatusBadGateway,
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func emptyCompletionInfo(c *gin.Context) map[string]any {
	info, ok := common.GetContextKeyType[map[string]any](c, constant.ContextKeyEmptyCompletionInfo)
	if ok && info != nil {
		return info
	}
	info = make(map[string]any)
	common.SetContextKey(c, constant.ContextKeyEmptyCompletionInfo, info)
	return info
}

func emptyCompletionRetryCount(c *gin.Context) int {
	info, ok := common.GetContextKeyType[map[string]any](c, constant.ContextKeyEmptyCompletionInfo)
	if !ok || info == nil {
		return 0
	}
	switch v := info["empty_retry_count"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func shouldRetryEmptyCompletion(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c == nil || info == nil {
		return false
	}
	if !common.EmptyCompletionRetryEnabled {
		return false
	}
	if c.Writer != nil && c.Writer.Written() {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if common.RetryTimes <= info.RetryIndex {
		return false
	}
	return emptyCompletionRetryCount(c) < maxEmptyCompletionRetries
}

func recordEmptyCompletion(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, reason string, outputTypes []string, willRetry bool) {
	if c == nil {
		return
	}
	m := emptyCompletionInfo(c)
	if reason == "" {
		reason = "empty_output"
	}
	m["empty_reason"] = reason
	if len(outputTypes) > 0 {
		m["empty_output_types"] = outputTypes
	}
	if usageMap := usageInfoMap(usage); usageMap != nil {
		if _, exists := m["first_empty_usage"]; !exists {
			m["first_empty_usage"] = usageMap
		}
		m["last_empty_usage"] = usageMap
	}
	if info != nil {
		channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
		if info.ChannelMeta != nil {
			channelID = info.ChannelId
			channelType = info.ChannelType
		}
		if _, exists := m["first_empty_channel_id"]; !exists {
			m["first_empty_channel_id"] = channelID
			m["first_empty_channel_type"] = channelType
			m["first_empty_retry_index"] = info.RetryIndex
		}
		m["last_empty_channel_id"] = channelID
		m["last_empty_channel_type"] = channelType
		m["last_empty_retry_index"] = info.RetryIndex
	}

	attempts, _ := m["empty_attempts"].(int)
	m["empty_attempts"] = attempts + 1

	if willRetry {
		count := emptyCompletionRetryCount(c) + 1
		m["empty_retry"] = true
		m["empty_retry_count"] = count
		m["empty_retry_result"] = "retrying"
		return
	}

	m["empty_completion"] = true
	if retried, _ := m["empty_retry"].(bool); retried {
		m["empty_retry_result"] = "empty_again"
	} else {
		m["empty_retry"] = false
		m["empty_retry_count"] = 0
		m["empty_retry_result"] = "no_retry"
	}
}

func recordEmptyCompletionRetrySuccess(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	if c == nil {
		return
	}
	m, ok := common.GetContextKeyType[map[string]any](c, constant.ContextKeyEmptyCompletionInfo)
	if !ok || len(m) == 0 {
		return
	}
	retried, _ := m["empty_retry"].(bool)
	if !retried {
		return
	}
	m["empty_retry_result"] = "success"
	if usageMap := usageInfoMap(usage); usageMap != nil {
		m["final_usage"] = usageMap
	}
	if info == nil {
		return
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
		channelType = info.ChannelType
	}
	m["final_channel_id"] = channelID
	m["final_channel_type"] = channelType
	m["final_retry_index"] = info.RetryIndex
}

func handleEmptyResponsesStreamCompletion(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, reason string, outputTypes []string) *types.MaxAPIError {
	willRetry := shouldRetryEmptyCompletion(c, info)
	recordEmptyCompletion(c, info, usage, reason, outputTypes, willRetry)
	if willRetry {
		return newEmptyCompletionRetryError()
	}
	return nil
}

func usageInfoMap(usage *dto.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 &&
		usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}
	return map[string]any{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
		"input_tokens":      usage.InputTokens,
		"output_tokens":     usage.OutputTokens,
	}
}

func responsesOutputTypes(resp *dto.OpenAIResponsesResponse) []string {
	if resp == nil || len(resp.Output) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(resp.Output))
	typesOut := make([]string, 0, len(resp.Output))
	for _, out := range resp.Output {
		if strings.TrimSpace(out.Type) == "" {
			continue
		}
		if _, ok := seen[out.Type]; ok {
			continue
		}
		seen[out.Type] = struct{}{}
		typesOut = append(typesOut, out.Type)
	}
	return typesOut
}

func responsesHasVisibleOutput(resp *dto.OpenAIResponsesResponse) bool {
	if resp == nil {
		return false
	}
	if resp.HasImageGenerationCall() {
		return true
	}
	for _, out := range resp.Output {
		if responsesOutputHasVisiblePayload(&out) {
			return true
		}
	}
	return false
}

func responsesStreamHasVisibleOutput(streamResp *dto.ResponsesStreamResponse) bool {
	if streamResp == nil {
		return false
	}
	switch streamResp.Type {
	case "response.output_text.delta",
		"response.refusal.delta",
		"response.reasoning_text.delta",
		"response.reasoning_summary_text.delta",
		"response.function_call_arguments.delta":
		return strings.TrimSpace(streamResp.Delta) != ""
	case dto.ResponsesOutputTypeItemAdded, dto.ResponsesOutputTypeItemDone:
		return responsesOutputHasVisiblePayload(streamResp.Item)
	case "response.completed":
		return responsesHasVisibleOutput(streamResp.Response)
	default:
		return false
	}
}

func responsesOutputHasVisiblePayload(output *dto.ResponsesOutput) bool {
	if output == nil {
		return false
	}
	switch output.Type {
	case "function_call", dto.ResponsesOutputTypeImageGenerationCall, dto.BuildInCallWebSearchCall:
		return true
	case "message":
		if output.Role != "" && output.Role != "assistant" {
			return false
		}
	}
	for _, content := range output.Content {
		if strings.TrimSpace(content.Text) != "" || strings.TrimSpace(content.Refusal) != "" {
			return true
		}
	}
	return false
}

func isEmptyResponsesCompletion(resp *dto.OpenAIResponsesResponse) bool {
	return resp != nil && !responsesHasVisibleOutput(resp)
}

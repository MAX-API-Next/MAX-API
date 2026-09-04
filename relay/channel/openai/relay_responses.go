package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

const maxPendingResponsesStreamEvents = 32

func applyResponsesUsage(dst *dto.Usage, src *dto.Usage) {
	if dst == nil || src == nil {
		return
	}

	promptTokens := src.InputTokens
	if promptTokens == 0 {
		promptTokens = src.PromptTokens
	}
	completionTokens := src.OutputTokens
	if completionTokens == 0 {
		completionTokens = src.CompletionTokens
	}

	dst.PromptTokens = promptTokens
	dst.CompletionTokens = completionTokens
	dst.TotalTokens = src.TotalTokens
	if dst.TotalTokens == 0 {
		dst.TotalTokens = promptTokens + completionTokens
	}
	dst.InputTokens = src.InputTokens
	dst.OutputTokens = src.OutputTokens
	dst.PromptCacheHitTokens = src.PromptCacheHitTokens
	dst.PromptTokensDetails = src.PromptTokensDetails
	dst.CompletionTokenDetails = src.CompletionTokenDetails
	if src.OutputTokensDetails != nil {
		outputDetails := *src.OutputTokensDetails
		dst.OutputTokensDetails = &outputDetails
		dto.CopyOutputTokenDetails(&dst.CompletionTokenDetails, &outputDetails, false)
	}
	if src.InputTokensDetails != nil {
		inputDetails := *src.InputTokensDetails
		dst.InputTokensDetails = &inputDetails
		dto.CopyInputTokenDetails(&dst.PromptTokensDetails, &inputDetails, true)
	}
}

func normalizedResponsesStatus(response *dto.OpenAIResponsesResponse) string {
	if response == nil || len(response.Status) == 0 {
		return ""
	}
	var status string
	if err := common.Unmarshal(response.Status, &status); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(status))
}

func responsesTerminalError(response *dto.OpenAIResponsesResponse, statusCode int, skipRetry bool) *types.MaxAPIError {
	status := normalizedResponsesStatus(response)
	switch status {
	case "failed", "cancelled", "canceled":
	default:
		return nil
	}

	if statusCode < http.StatusBadRequest {
		statusCode = http.StatusInternalServerError
	}
	if response != nil {
		if oaiErr := response.GetOpenAIError(); oaiErr != nil && (oaiErr.Type != "" || oaiErr.Message != "") {
			if skipRetry {
				return types.WithOpenAIError(*oaiErr, statusCode, types.ErrOptionWithSkipRetry())
			}
			return types.WithOpenAIError(*oaiErr, statusCode)
		}
	}
	err := fmt.Errorf("responses request terminated with status %s", status)
	if skipRetry {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, statusCode, types.ErrOptionWithSkipRetry())
	}
	return types.NewOpenAIError(err, types.ErrorCodeBadResponse, statusCode)
}

func responsesStreamTerminalError(streamResponse *dto.ResponsesStreamResponse, skipRetry bool) *types.MaxAPIError {
	if streamResponse != nil && streamResponse.Response != nil {
		if terminalErr := responsesTerminalError(streamResponse.Response, http.StatusInternalServerError, skipRetry); terminalErr != nil {
			return terminalErr
		}
		if oaiErr := streamResponse.Response.GetOpenAIError(); oaiErr != nil && (oaiErr.Type != "" || oaiErr.Message != "") {
			if skipRetry {
				return types.WithOpenAIError(*oaiErr, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
			}
			return types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
		}
	}
	eventType := "response.error"
	if streamResponse != nil && strings.TrimSpace(streamResponse.Type) != "" {
		eventType = streamResponse.Type
	}
	err := fmt.Errorf("responses stream error: %s", eventType)
	if skipRetry {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if terminalErr := responsesTerminalError(&responsesResponse, resp.StatusCode, false); terminalErr != nil {
		return nil, terminalErr
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, service.ExtractOutputTextFromResponses(&responsesResponse))
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// compute usage
	usage := dto.Usage{}
	applyResponsesUsage(&usage, responsesResponse.Usage)
	emptyCompletion := isEmptyResponsesCompletion(&responsesResponse)
	if emptyCompletion {
		willRetry := shouldRetryEmptyCompletion(c, info)
		recordEmptyCompletion(c, info, &usage, "empty_responses_output", responsesOutputTypes(&responsesResponse), willRetry)
		if willRetry {
			return nil, newEmptyCompletionRetryError()
		}
	}
	if !emptyCompletion {
		recordEmptyCompletionRetrySuccess(c, info, &usage)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	observeResponsesOutputs(info, responsesResponse.Output)
	return &usage, nil
}

func observeResponsesOutputs(info *relaycommon.RelayInfo, outputs []dto.ResponsesOutput) {
	for outputIndex := range outputs {
		observeResponsesToolOutput(info, &outputs[outputIndex], &outputIndex)
	}
}

func observeResponsesToolOutput(info *relaycommon.RelayInfo, output *dto.ResponsesOutput, outputIndex *int) {
	if info == nil || output == nil || !isBillableResponsesToolStatus(output.Status) {
		return
	}
	callID := strings.TrimSpace(output.CallId)
	if callID == "" {
		callID = strings.TrimSpace(output.ID)
	}
	position := ""
	if outputIndex != nil && *outputIndex >= 0 {
		position = fmt.Sprintf("output:%d", *outputIndex)
	}
	identity := relaycommon.ToolCallIdentity{
		Scope:    "openai-responses",
		CallID:   callID,
		Position: position,
	}

	switch output.Type {
	case dto.BuildInCallWebSearchCall:
		info.ObserveBuiltInToolCall(responsesWebSearchToolName(info), identity)
	case dto.BuildInCallFileSearchCall:
		info.ObserveBuiltInToolCall(dto.BuildInToolFileSearch, identity)
	case dto.BuildInCallFunctionCall, dto.BuildInCallCustomToolCall, dto.BuildInCallToolUse:
		info.ObserveCustomToolCall(output.Name, identity)
	}
}

func isBillableResponsesToolStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "cancelled", "canceled", "incomplete", "partial":
		return false
	default:
		return true
	}
}

func responsesWebSearchToolName(info *relaycommon.RelayInfo) string {
	if info != nil && info.ResponsesUsageInfo != nil {
		if _, ok := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; ok {
			return dto.BuildInToolWebSearchPreview
		}
		if _, ok := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch]; ok {
			return dto.BuildInToolWebSearch
		}
	}
	return dto.BuildInToolWebSearchPreview
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var streamErr *types.MaxAPIError
	type pendingResponsesStreamEvent struct {
		response dto.ResponsesStreamResponse
		data     string
	}
	pendingEvents := make([]pendingResponsesStreamEvent, 0, 4)
	streamForwarded := false
	hasVisiblePayload := false
	emptyCompletionRecorded := false

	flushPendingEvents := func() {
		for _, event := range pendingEvents {
			sendResponsesStreamData(c, event.response, event.data)
		}
		pendingEvents = pendingEvents[:0]
		streamForwarded = true
	}
	shouldBufferForEmptyRetry := func() bool {
		return !streamForwarded && shouldRetryEmptyCompletion(c, info)
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if responsesStreamHasVisibleOutput(&streamResponse) {
			hasVisiblePayload = true
		}
		var terminalEventErr *types.MaxAPIError
		switch streamResponse.Type {
		case "response.completed":
			if streamResponse.Response != nil {
				if terminalErr := responsesTerminalError(streamResponse.Response, http.StatusInternalServerError, true); terminalErr != nil {
					terminalEventErr = terminalErr
					break
				}
				applyResponsesUsage(usage, streamResponse.Response.Usage)
				observeResponsesOutputs(info, streamResponse.Response.Output)
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
			if !hasVisiblePayload {
				emptyCompletionRecorded = true
				if retryErr := handleEmptyResponsesStreamCompletion(c, info, usage, "empty_responses_stream_output", responsesOutputTypes(streamResponse.Response)); retryErr != nil {
					streamErr = retryErr
					sr.Stop(streamErr)
					return
				}
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall, dto.BuildInCallFunctionCall, dto.BuildInCallCustomToolCall, dto.BuildInCallToolUse:
					observeResponsesToolOutput(info, streamResponse.Item, streamResponse.OutputIndex)
				}
			}
		case "response.error", "response.failed", "response.cancelled", "response.canceled":
			terminalEventErr = responsesStreamTerminalError(&streamResponse, true)
		}

		if terminalEventErr != nil {
			if streamForwarded {
				sendResponsesStreamData(c, streamResponse, data)
			} else {
				pendingEvents = append(pendingEvents, pendingResponsesStreamEvent{
					response: streamResponse,
					data:     data,
				})
				flushPendingEvents()
			}
			streamErr = terminalEventErr
			sr.Stop(streamErr)
			return
		}

		if streamForwarded {
			sendResponsesStreamData(c, streamResponse, data)
			return
		}

		pendingEvents = append(pendingEvents, pendingResponsesStreamEvent{
			response: streamResponse,
			data:     data,
		})
		switch streamResponse.Type {
		case "response.completed":
			flushPendingEvents()
		default:
			if hasVisiblePayload || len(pendingEvents) >= maxPendingResponsesStreamEvents || !shouldBufferForEmptyRetry() {
				flushPendingEvents()
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}
	if !hasVisiblePayload && !emptyCompletionRecorded {
		emptyCompletionRecorded = true
		if retryErr := handleEmptyResponsesStreamCompletion(c, info, usage, "empty_responses_stream_eof", nil); retryErr != nil {
			return nil, retryErr
		}
	}
	if !streamForwarded && len(pendingEvents) > 0 {
		flushPendingEvents()
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, responseTextBuilder.String())
	}
	if hasVisiblePayload {
		recordEmptyCompletionRetrySuccess(c, info, usage)
	}

	return usage, nil
}

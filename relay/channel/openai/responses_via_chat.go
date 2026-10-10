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
	"github.com/MAX-API-Next/MAX-API/service/openaicompat"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	observeOpenAITextToolCalls(info, chatResp.Choices)

	responseID := helper.GetResponseID(c)
	responsesResp, usage, err := service.ChatCompletionsResponseToResponsesResponseWithCustomTools(&chatResp, responseID, service.ResponsesCustomToolNames(info))
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if (usage == nil || usage.TotalTokens == 0) && !helper.HasUsageTokenFields(string(body), "usage", "prompt_tokens", "completion_tokens") {
		text := service.ExtractOutputTextFromResponses(responsesResp)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		responsesResp.Usage = openaicompat.UsageFromChatUsage(usage)
	}
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, service.ExtractOutputTextFromResponses(responsesResp))
	}

	responseBody, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	info.EnableFirstResultTracking()
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseID := helper.GetResponseID(c)
	state := openaicompat.NewChatToResponsesStreamState(responseID, info.UpstreamModelName)
	state.CustomToolNames = service.ResponsesCustomToolNames(info)
	toolCallObserver := newOpenAIStreamToolCallObserver(info)
	var streamErr *types.MaxAPIError
	terminalResponseSeen := false
	usageReported := false
	outputDelivered := false
	var deliveredText strings.Builder
	var deliveredUsage *dto.Usage

	sendEvent := func(event openaicompat.ChatToResponsesStreamEvent) bool {
		data, err := common.Marshal(event.Payload)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		delivered, writeErr := helper.ResponseChunkDataWithDelivery(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data))
		if delivered && helper.ResponsesStreamEventHasOutput(event.Payload) {
			outputDelivered = true
			deliveredText.WriteString(event.Payload.Delta)
		}
		if writeErr != nil {
			streamErr = types.NewOpenAIError(writeErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		if delivered && helper.ResponsesStreamEventHasFirstResult(event.Payload) {
			info.SetFirstResultTime()
		}
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		if strings.Contains(data, `"error"`) {
			var errorResp dto.OpenAITextResponse
			if err := common.UnmarshalJsonStr(data, &errorResp); err == nil {
				if oaiError := errorResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
					streamErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
					sr.Stop(streamErr)
					return
				}
			}
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal chat stream response: "+err.Error())
			streamErr = types.NewError(err, types.ErrorCodeBadResponseBody)
			sr.Stop(streamErr)
			return
		}
		reported := helper.HasUsageTokenFields(data, "usage", "prompt_tokens", "completion_tokens") || dto.HasOpenAIUsageTokens(chunk.Usage)
		if !reported {
			chunk.Usage = nil
		} else if helper.HasUsageTokenFields(data, "usage", "prompt_tokens", "completion_tokens") {
			chunk.Usage.BillingUsage = dto.NewReportedOpenAIChatBillingUsage(chunk.Usage)
		}
		toolCallObserver.Observe(&chunk)
		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				terminalResponseSeen = true
			}
		}

		events, err := openaicompat.ChatCompletionsStreamChunkToResponsesEvents(&chunk, state)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		for _, event := range events {
			if !sendEvent(event) {
				sr.Stop(streamErr)
				return
			}
		}
		deliveredUsage = openaicompat.UsageFromChatUsage(state.Usage)
		usageReported = usageReported || reported
	})

	usage := deliveredUsage
	if usage == nil {
		usage = &dto.Usage{}
	}
	usageTextSize := -1
	refreshUsage := func() {
		if !usageReported && usageTextSize != deliveredText.Len() {
			usage = service.ResponseText2Usage(c, deliveredText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
			state.Usage = openaicompat.UsageFromChatUsage(usage)
			usageTextSize = deliveredText.Len()
		}
	}
	refreshUsage()
	finalizeStream := func() bool {
		for _, event := range openaicompat.FlushChatCompletionsStreamToResponsesOutput(state) {
			if !sendEvent(event) {
				refreshUsage()
				return false
			}
		}
		refreshUsage()
		for _, event := range openaicompat.FinalizeChatCompletionsStreamToResponses(state) {
			if !sendEvent(event) {
				return false
			}
		}
		return true
	}
	if apiErr := helper.FirstResultTimeoutError(c, info); apiErr != nil {
		// A whitespace delta is delivered output even while the meaningful
		// first-result deadline is still running. Preserve partial settlement.
		if outputDelivered {
			return usage, apiErr
		}
		return nil, apiErr
	}
	if streamErr != nil {
		if c.Writer.Written() {
			streamErr = types.NewError(streamErr, streamErr.GetErrorCode(), types.ErrOptionWithSkipRetry())
		}
		if outputDelivered {
			return usage, streamErr
		}
		return nil, streamErr
	}
	if !terminalResponseSeen {
		reason := "upstream_eof"
		if info.StreamStatus != nil && info.StreamStatus.IsAbnormalEnd() {
			reason = string(info.StreamStatus.EndReason)
		}
		state.MarkIncomplete(reason)
		if info.StreamStatus == nil || info.StreamStatus.EndReason == relaycommon.StreamEndReasonEOF {
			if !finalizeStream() {
				finalErr := types.NewError(streamErr, streamErr.GetErrorCode(), types.ErrOptionWithSkipRetry())
				if !outputDelivered {
					return nil, finalErr
				}
				return usage, finalErr
			}
		}
		if !outputDelivered {
			return nil, openaicompat.NewResponsesStreamIncompleteError(reason)
		}
		return usage, openaicompat.NewResponsesStreamIncompleteError(reason)
	}
	if info.StreamStatus != nil && info.StreamStatus.IsAbnormalEnd() {
		return usage, openaicompat.NewResponsesStreamIncompleteError(string(info.StreamStatus.EndReason))
	}

	if !finalizeStream() {
		return usage, types.NewError(streamErr, streamErr.GetErrorCode(), types.ErrOptionWithSkipRetry())
	}
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, state.UsageText())
	}
	return usage, nil
}

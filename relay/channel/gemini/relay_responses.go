package gemini

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/service/openaicompat"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func GeminiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	logger.LogDebug(c, "Gemini responses response body: %s", responseBody)

	var geminiResponse dto.GeminiChatResponse
	if err := common.Unmarshal(responseBody, &geminiResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(geminiResponse.Candidates) == 0 {
		usage := buildUsageFromGeminiResponse(c, info, &geminiResponse)
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			return &usage, types.NewOpenAIError(
				errors.New("request blocked by Gemini API: "+*geminiResponse.PromptFeedback.BlockReason),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
			)
		}
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
		return &usage, types.NewOpenAIError(
			errors.New("empty response from Gemini API"),
			types.ErrorCodeEmptyResponse,
			http.StatusInternalServerError,
		)
	}

	chatResp := responseGeminiChat2OpenAI(c, &geminiResponse)
	chatResp.Model = info.UpstreamModelName
	usage := buildUsageFromGeminiResponse(c, info, &geminiResponse)
	chatResp.Usage = usage

	responsesResp, responsesUsage, err := service.ChatCompletionsResponseToResponsesResponseWithCustomTools(chatResp, helper.GetResponseID(c), service.ResponsesCustomToolNames(info))
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if responsesUsage == nil || responsesUsage.TotalTokens == 0 {
		responsesResp.Usage = openaicompat.UsageFromChatUsage(&usage)
	}
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, service.BuildGeminiResponseAuditContent(&geminiResponse))
	}

	responseBody, err = common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return &usage, nil
}

func GeminiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	info.EnableFirstResultTracking()
	responseID := helper.GetResponseID(c)
	created := common.GetTimestamp()
	state := openaicompat.NewChatToResponsesStreamState(responseID, info.UpstreamModelName)
	state.CustomToolNames = service.ResponsesCustomToolNames(info)
	state.Created = created
	finishReason := constant.FinishReasonStop
	toolCallIndexByChoice := make(map[int]map[string]int)
	nextToolCallIndexByChoice := make(map[int]int)
	var streamErr *types.MaxAPIError
	terminalResponseSeen := false
	outputDelivered := false
	var deliveredText strings.Builder
	var reportedUsage *dto.Usage
	reportedOutputUsage := false
	functionCalls := newGeminiStreamFunctionCalls()

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
	sendChunk := func(chunk *dto.ChatCompletionsStreamResponse, retainMetadataUsage func()) bool {
		events, err := openaicompat.ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		if terminalResponseSeen && retainMetadataUsage != nil && openaicompat.ChatToResponsesEventsAreMetadataOnly(events) {
			retainMetadataUsage()
		}
		for _, event := range events {
			if !sendEvent(event) {
				return false
			}
		}
		return true
	}

	usage, err := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		if prepareErr := functionCalls.prepare(geminiResponse); prepareErr != nil {
			streamErr = types.NewError(prepareErr, types.ErrorCodeBadResponseBody)
			return false
		}
		if hasGeminiTerminalFinishReason(geminiResponse) {
			terminalResponseSeen = true
		}
		response, isStop := streamResponseGeminiChat2OpenAI(geminiResponse)
		response.Id = responseID
		response.Created = created
		response.Model = info.UpstreamModelName

		if response.IsToolCall() {
			finishReason = constant.FinishReasonToolCalls
		}
		if isStop {
			terminalResponseSeen = true
		}
		for choiceIdx := range response.Choices {
			choiceKey := response.Choices[choiceIdx].Index
			for toolIdx := range response.Choices[choiceIdx].Delta.ToolCalls {
				tool := &response.Choices[choiceIdx].Delta.ToolCalls[toolIdx]
				if tool.ID == "" {
					continue
				}
				indexByID := toolCallIndexByChoice[choiceKey]
				if indexByID == nil {
					indexByID = make(map[string]int)
					toolCallIndexByChoice[choiceKey] = indexByID
				}
				if idx, ok := indexByID[tool.ID]; ok {
					tool.SetIndex(idx)
					continue
				}
				idx := nextToolCallIndexByChoice[choiceKey]
				nextToolCallIndexByChoice[choiceKey] = idx + 1
				indexByID[tool.ID] = idx
				tool.SetIndex(idx)
			}
		}

		retainUsage := func() {
			inputReported := gjson.Get(data, "usageMetadata.promptTokenCount").Type == gjson.Number
			outputReported := gjson.Get(data, "usageMetadata.candidatesTokenCount").Type == gjson.Number
			if inputReported || outputReported || dto.HasGeminiUsageMetadataTokens(geminiResponse.GetUsageMetadata()) {
				if metadata := geminiResponse.GetUsageMetadata(); metadata != nil {
					fallbackPrompt := 0
					if !inputReported {
						fallbackPrompt = info.GetEstimatePromptTokens()
					}
					mapped := buildUsageFromGeminiMetadata(*metadata, fallbackPrompt)
					// A numeric zero completion count must not be inferred from total.
					if outputReported {
						mapped.CompletionTokens = metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount
					}
					if !outputReported {
						promptTokens := mapped.PromptTokens
						patchGeminiZeroCompletionUsage(c, info, &mapped, deliveredText.String(), 0)
						if inputReported {
							mapped.PromptTokens = promptTokens
							mapped.TotalTokens = mapped.PromptTokens + mapped.CompletionTokens
							if mapped.BillingUsage != nil && mapped.BillingUsage.Estimated {
								mapped.BillingUsage.GeminiUsageMetadata.PromptTokenCount = metadata.PromptTokenCount
								mapped.BillingUsage = dto.NewEstimatedGeminiChatBillingUsage(&mapped)
							}
						}
					}
					if mapped.BillingUsage == nil {
						copyMetadata := *metadata
						mapped.BillingUsage = &dto.BillingUsage{Source: dto.BillingUsageSourceGeminiChat, Semantic: dto.BillingUsageSemanticGemini, GeminiUsageMetadata: &copyMetadata}
					}
					if mapped.PromptTokens != metadata.PromptTokenCount+metadata.ToolUsePromptTokenCount {
						mapped.TotalTokens = mapped.PromptTokens + mapped.CompletionTokens
						mapped.BillingUsage = dto.NewEstimatedGeminiChatBillingUsage(&mapped)
					}
					if mapped.BillingUsage != nil {
						mapped.BillingUsage.TokenCountsReported = inputReported && outputReported
					}
					reportedUsage = &mapped
					reportedOutputUsage = outputReported
				}
			}
		}
		if !sendChunk(response, retainUsage) {
			return false
		}
		retainUsage()
		if isStop {
			return sendChunk(helper.GenerateStopResponse(responseID, created, info.UpstreamModelName, finishReason), nil)
		}
		return true
	})
	if err != nil {
		// The shared scanner's timeout may have no usage return even though
		// this bridge delivered whitespace and retained provider usage below.
		streamErr = err
	}
	if reportedUsage != nil {
		usage = reportedUsage
	}
	if usage != nil {
		state.Usage = openaicompat.UsageFromChatUsage(usage)
	}
	refreshUsage := func() {
		if reportedUsage == nil {
			usage = service.ResponseText2Usage(c, deliveredText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
			attachEstimatedGeminiBillingUsage(usage)
		} else if !reportedOutputUsage {
			// Partial metadata can report input (including zero) while leaving
			// output unknown. Preserve that input and estimate the delivered output.
			mapped := *reportedUsage
			estimated := service.ResponseText2Usage(c, deliveredText.String(), info.UpstreamModelName, mapped.PromptTokens)
			if estimated.CompletionTokens > mapped.CompletionTokens {
				mapped.CompletionTokens = estimated.CompletionTokens
			}
			mapped.TotalTokens = mapped.PromptTokens + mapped.CompletionTokens
			mapped.BillingUsage = dto.NewEstimatedGeminiChatBillingUsage(&mapped)
			usage = &mapped
		}
		if usage != nil {
			state.Usage = openaicompat.UsageFromChatUsage(usage)
		}
	}
	if streamErr != nil {
		if c.Writer.Written() {
			streamErr = types.NewError(streamErr, streamErr.GetErrorCode(), types.ErrOptionWithSkipRetry())
		}
		if !outputDelivered {
			return nil, streamErr
		}
		refreshUsage()
		return usage, streamErr
	}
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
	if !terminalResponseSeen || len(functionCalls.pending) > 0 {
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

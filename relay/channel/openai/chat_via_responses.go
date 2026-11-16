package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
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

func responsesStreamIndexKey(itemID string, idx *int) string {
	if itemID == "" {
		return ""
	}
	if idx == nil {
		return itemID
	}
	return fmt.Sprintf("%s:%d", itemID, *idx)
}

func stringDeltaFromPrefix(prev string, next string) string {
	if next == "" {
		return ""
	}
	if prev != "" && strings.HasPrefix(next, prev) {
		return next[len(prev):]
	}
	return next
}

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if terminalErr := responsesTerminalError(&responsesResp, resp.StatusCode, false); terminalErr != nil {
		return nil, terminalErr
	}
	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	observeResponsesOutputs(info, responsesResp.Output)

	chatId := helper.GetResponseID(c)
	chatResp, usage, err := service.ResponsesResponseToChatCompletionsResponse(&responsesResp, chatId)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	emptyCompletion := isEmptyResponsesCompletion(&responsesResp)
	if emptyCompletion {
		willRetry := shouldRetryEmptyCompletion(c, info)
		recordEmptyCompletion(c, info, usage, "empty_responses_output", responsesOutputTypes(&responsesResp), willRetry)
		if willRetry {
			return nil, newEmptyCompletionRetryError()
		}
	}
	promptReported := gjson.GetBytes(body, "usage.input_tokens").Type == gjson.Number
	completionReported := gjson.GetBytes(body, "usage.output_tokens").Type == gjson.Number
	if !promptReported || !completionReported {
		outputAuditText := service.ExtractOutputTextFromResponses(&responsesResp)
		var estimateText strings.Builder
		estimateText.WriteString(outputAuditText)
		for _, choice := range chatResp.Choices {
			for _, tool := range choice.Message.ParseToolCalls() {
				estimateText.WriteString(tool.Function.Name)
				estimateText.WriteString(tool.Function.Arguments)
			}
		}
		estimated := service.ResponseText2Usage(c, estimateText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		if !promptReported {
			usage.PromptTokens = max(estimated.PromptTokens, responsesInputTokenFloor(usage))
			usage.InputTokens = usage.PromptTokens
		}
		if !completionReported {
			usage.CompletionTokens = max(estimated.CompletionTokens, responsesOutputTokenFloor(usage))
			usage.OutputTokens = usage.CompletionTokens
		}
		if gjson.GetBytes(body, "usage.total_tokens").Type != gjson.Number {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		if service.ResponseAuditEnabled() {
			service.SetRelayResponseAuditContent(info, outputAuditText)
		}
	} else if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, service.ExtractOutputTextFromResponses(&responsesResp))
	}
	if total := gjson.GetBytes(body, "usage.total_tokens"); total.Type == gjson.Number {
		// Conversion helpers can normalize zero to a sum; raw presence wins.
		usage.TotalTokens = int(total.Int())
	}
	usage.BillingUsage = dto.NewReportedOpenAIResponsesBillingUsage(usage)
	usage.BillingUsage.TokenCountsReported = promptReported && completionReported
	usage.BillingUsage.Estimated = !usage.BillingUsage.TokenCountsReported
	chatResp.Usage = *usage
	if !emptyCompletion {
		recordEmptyCompletionRetrySuccess(c, info, usage)
	}

	var responseBody []byte
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(chatResp, info)
		responseBody, err = common.Marshal(claudeResp)
	case types.RelayFormatGemini:
		geminiResp := service.ResponseOpenAI2Gemini(chatResp, info)
		responseBody, err = common.Marshal(geminiResp)
	default:
		chatResp.Usage.BillingUsage = nil // Internal evidence stays on the settlement result.
		responseBody, err = common.Marshal(chatResp)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	info.EnableFirstResultTracking()
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)
	writeObserver := &chatStreamWriteObserver{ResponseWriter: c.Writer}
	c.Writer = writeObserver
	defer func() {
		if writeObserver.err == nil {
			c.Writer = writeObserver.ResponseWriter
		}
	}()

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	model := info.UpstreamModelName

	var (
		usage                   = &dto.Usage{}
		outputText              strings.Builder
		usageText               strings.Builder
		pendingTextPrefix       strings.Builder
		sentStart               bool
		sentStop                bool
		sawToolCall             bool
		hasVisiblePayload       bool
		outputDelivered         bool
		deliveredUsageText      strings.Builder
		usagePresence           dto.OpenAIUsagePresence
		emptyCompletionRecorded bool
		streamErr               *types.MaxAPIError
	)

	toolCallIndexByID := make(map[string]int)
	toolCallNameByID := make(map[string]string)
	toolCallArgsByID := make(map[string]string)
	toolCallNameSent := make(map[string]bool)
	toolCallCanonicalIDByItemID := make(map[string]string)
	hasSentReasoningSummary := false
	needsReasoningSummarySeparator := false
	//reasoningSummaryTextByKey := make(map[string]string)

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}
	partialUsageSupported := info.RelayFormat == types.RelayFormatOpenAI || info.RelayFormat == types.RelayFormatClaude
	finalizeUsage := func(text string) {
		if !usagePresence.Prompt || !usagePresence.Completion {
			estimate := service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
			if !usagePresence.Prompt {
				usage.PromptTokens = max(estimate.PromptTokens, responsesInputTokenFloor(usage))
				usage.InputTokens = usage.PromptTokens
			}
			if !usagePresence.Completion {
				usage.CompletionTokens = max(estimate.CompletionTokens, responsesOutputTokenFloor(usage))
				usage.OutputTokens = usage.CompletionTokens
			}
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		if usagePresence.Total {
			usage.TotalTokens = usagePresence.ReportedTotal
		}
		usage.BillingUsage = dto.NewReportedOpenAIResponsesBillingUsage(usage)
		usage.BillingUsage.TokenCountsReported = usagePresence.Prompt && usagePresence.Completion
		usage.BillingUsage.Estimated = !usage.BillingUsage.TokenCountsReported
	}

	sendChatChunk := func(chunk *dto.ChatCompletionsStreamResponse) bool {
		if chunk == nil {
			return true
		}
		recordFirstResult := func() {
			firstResult := false
			for _, choice := range chunk.Choices {
				content, reasoning := choice.Delta.GetContentString(), choice.Delta.GetReasoningContent()
				if content != "" || reasoning != "" || len(choice.Delta.ToolCalls) > 0 {
					outputDelivered = true
					deliveredUsageText.WriteString(content)
					deliveredUsageText.WriteString(reasoning)
					for _, tool := range choice.Delta.ToolCalls {
						deliveredUsageText.WriteString(tool.Function.Name)
						deliveredUsageText.WriteString(tool.Function.Arguments)
					}
				}
				if strings.TrimSpace(choice.Delta.GetContentString()) != "" ||
					strings.TrimSpace(choice.Delta.GetReasoningContent()) != "" || len(choice.Delta.ToolCalls) > 0 {
					firstResult = true
				}
			}
			if firstResult {
				info.SetFirstResultTime()
			}
		}
		if info.RelayFormat == types.RelayFormatOpenAI {
			info.SendResponseCount++
			if err := helper.ObjectData(c, chunk); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			if writeObserver.err != nil {
				streamErr = types.NewOpenAIError(writeObserver.err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			recordFirstResult()
			return true
		}

		chunkData, err := common.Marshal(chunk)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		before := c.Writer.Size()
		if err := HandleStreamFormat(c, info, string(chunkData), false, false); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		if writeObserver.err != nil {
			streamErr = types.NewOpenAIError(writeObserver.err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		if c.Writer.Size() > before {
			recordFirstResult()
		}
		return true
	}

	sendStartIfNeeded := func() bool {
		if sentStart {
			return true
		}
		if !sendChatChunk(helper.GenerateStartEmptyResponse(responseId, createAt, model, nil)) {
			return false
		}
		sentStart = true
		return true
	}

	//sendReasoningDelta := func(delta string) bool {
	//	if delta == "" {
	//		return true
	//	}
	//	if !sendStartIfNeeded() {
	//		return false
	//	}
	//
	//	usageText.WriteString(delta)
	//	chunk := &dto.ChatCompletionsStreamResponse{
	//		Id:      responseId,
	//		Object:  "chat.completion.chunk",
	//		Created: createAt,
	//		Model:   model,
	//		Choices: []dto.ChatCompletionsStreamResponseChoice{
	//			{
	//				Index: 0,
	//				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
	//					ReasoningContent: &delta,
	//				},
	//			},
	//		},
	//	}
	//	if err := helper.ObjectData(c, chunk); err != nil {
	//		streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	//		return false
	//	}
	//	return true
	//}

	sendReasoningSummaryDelta := func(delta string) bool {
		if strings.TrimSpace(delta) == "" {
			return true
		}
		if needsReasoningSummarySeparator {
			if strings.HasPrefix(delta, "\n\n") {
				needsReasoningSummarySeparator = false
			} else if strings.HasPrefix(delta, "\n") {
				delta = "\n" + delta
				needsReasoningSummarySeparator = false
			} else {
				delta = "\n\n" + delta
				needsReasoningSummarySeparator = false
			}
		}
		if !sendStartIfNeeded() {
			return false
		}

		usageText.WriteString(delta)
		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ReasoningContent: &delta,
					},
				},
			},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		hasSentReasoningSummary = true
		hasVisiblePayload = true
		return true
	}

	sendToolCallDelta := func(callID string, name string, argsDelta string) bool {
		if callID == "" {
			return true
		}
		if !sendStartIfNeeded() {
			return false
		}

		idx, ok := toolCallIndexByID[callID]
		if !ok {
			idx = len(toolCallIndexByID)
			toolCallIndexByID[callID] = idx
		}
		if name != "" {
			toolCallNameByID[callID] = name
		}
		if toolCallNameByID[callID] != "" {
			name = toolCallNameByID[callID]
		}

		tool := dto.ToolCallResponse{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionResponse{
				Arguments: argsDelta,
			},
		}
		tool.SetIndex(idx)
		if name != "" && !toolCallNameSent[callID] {
			tool.Function.Name = name
			toolCallNameSent[callID] = true
		}

		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{tool},
					},
				},
			},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		sawToolCall = true
		hasVisiblePayload = true

		// Include tool call data in the local builder for fallback token estimation.
		if tool.Function.Name != "" {
			usageText.WriteString(tool.Function.Name)
		}
		if argsDelta != "" {
			usageText.WriteString(argsDelta)
		}
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}
		// A delivered terminal fixes this generation's output and usage.
		// Its own usage was merged before sentStop became true.
		if sentStop {
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal responses stream event: "+err.Error())
			streamErr = types.NewError(err, types.ErrorCodeBadResponseBody)
			sr.Stop(streamErr)
			return
		}
		// Accept this frame's counters only after its required output writes.
		// Terminal metadata is committed before the converted stop/usage snapshot.
		usagePending := true
		defer func() {
			if usagePending {
				mergeResponsesStreamUsage(usage, &usagePresence, data)
			}
		}()

		switch streamResp.Type {
		case "response.created":
			if streamResp.Response != nil {
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
			}

		//case "response.reasoning_text.delta":
		//if !sendReasoningDelta(streamResp.Delta) {
		//	sr.Stop(streamErr)
		//	return
		//}

		//case "response.reasoning_text.done":

		case "response.reasoning_summary_text.delta":
			if !sendReasoningSummaryDelta(streamResp.Delta) {
				usagePending = false
				sr.Stop(streamErr)
				return
			}

		case "response.reasoning_summary_text.done":
			if hasSentReasoningSummary {
				needsReasoningSummarySeparator = true
			}

		//case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		//	key := responsesStreamIndexKey(strings.TrimSpace(streamResp.ItemID), streamResp.SummaryIndex)
		//	if key == "" || streamResp.Part == nil {
		//		break
		//	}
		//	// Only handle summary text parts, ignore other part types.
		//	if streamResp.Part.Type != "" && streamResp.Part.Type != "summary_text" {
		//		break
		//	}
		//	prev := reasoningSummaryTextByKey[key]
		//	next := streamResp.Part.Text
		//	delta := stringDeltaFromPrefix(prev, next)
		//	reasoningSummaryTextByKey[key] = next
		//	if !sendReasoningSummaryDelta(delta) {
		//		sr.Stop(streamErr)
		//		return
		//	}

		case "response.output_text.delta":
			if streamResp.Delta != "" {
				if outputText.Len() == 0 && strings.TrimSpace(streamResp.Delta) == "" {
					pendingTextPrefix.WriteString(streamResp.Delta)
					break
				}
				delta := streamResp.Delta
				if pendingTextPrefix.Len() > 0 {
					delta = pendingTextPrefix.String() + delta
					pendingTextPrefix.Reset()
				}
				if !sendStartIfNeeded() {
					usagePending = false
					sr.Stop(streamErr)
					return
				}
				outputText.WriteString(delta)
				usageText.WriteString(delta)
				hasVisiblePayload = true
				chunk := &dto.ChatCompletionsStreamResponse{
					Id:      responseId,
					Object:  "chat.completion.chunk",
					Created: createAt,
					Model:   model,
					Choices: []dto.ChatCompletionsStreamResponseChoice{
						{
							Index: 0,
							Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
								Content: &delta,
							},
						},
					},
				}
				if !sendChatChunk(chunk) {
					usagePending = false
					sr.Stop(streamErr)
					return
				}
			}

		case "response.output_item.added", "response.output_item.done":
			if streamResp.Item == nil {
				break
			}
			if streamResp.Item.Type != dto.BuildInCallFunctionCall && streamResp.Item.Type != dto.BuildInCallCustomToolCall {
				break
			}
			itemID := strings.TrimSpace(streamResp.Item.ID)
			callID := strings.TrimSpace(streamResp.Item.CallId)
			if callID == "" {
				callID = itemID
			}
			if itemID != "" && callID != "" {
				toolCallCanonicalIDByItemID[itemID] = callID
			}
			name := strings.TrimSpace(streamResp.Item.Name)
			if name != "" {
				toolCallNameByID[callID] = name
			}

			newArgs := streamResp.Item.ArgumentsString()
			prevArgs := toolCallArgsByID[callID]
			argsDelta := ""
			if newArgs != "" {
				if strings.HasPrefix(newArgs, prevArgs) {
					argsDelta = newArgs[len(prevArgs):]
				} else {
					argsDelta = newArgs
				}
				toolCallArgsByID[callID] = newArgs
			}

			if !sendToolCallDelta(callID, name, argsDelta) {
				usagePending = false
				sr.Stop(streamErr)
				return
			}
			if streamResp.Type == dto.ResponsesOutputTypeItemDone && callID != "" {
				observeResponsesToolOutput(info, streamResp.Item, streamResp.OutputIndex)
			}

		case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
			itemID := strings.TrimSpace(streamResp.ItemID)
			callID := toolCallCanonicalIDByItemID[itemID]
			if callID == "" {
				callID = itemID
			}
			if callID == "" {
				break
			}
			toolCallArgsByID[callID] += streamResp.Delta
			if !sendToolCallDelta(callID, "", streamResp.Delta) {
				usagePending = false
				sr.Stop(streamErr)
				return
			}

		case "response.function_call_arguments.done", "response.custom_tool_call_input.done":

		case "response.completed", "response.done", "response.incomplete":
			if streamResp.Type == "response.incomplete" {
				if streamResp.Response == nil {
					streamResp.Response = &dto.OpenAIResponsesResponse{}
				}
				if len(streamResp.Response.Status) == 0 {
					streamResp.Response.Status = []byte(`"incomplete"`)
				}
			}
			completedOutputTypes := []string(nil)
			if streamResp.Response != nil {
				if terminalErr := responsesTerminalError(streamResp.Response, http.StatusInternalServerError, info.SendResponseCount > 0); terminalErr != nil {
					streamErr = terminalErr
					sr.Stop(streamErr)
					return
				}
				completedOutputTypes = responsesOutputTypes(streamResp.Response)
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
				if outputText.Len() == 0 {
					completedText := service.ExtractOutputTextFromResponses(streamResp.Response)
					if strings.TrimSpace(completedText) != "" {
						if !sendStartIfNeeded() {
							usagePending = false
							sr.Stop(streamErr)
							return
						}
						outputText.WriteString(completedText)
						usageText.WriteString(completedText)
						delta := completedText
						chunk := &dto.ChatCompletionsStreamResponse{
							Id:      responseId,
							Object:  "chat.completion.chunk",
							Created: createAt,
							Model:   model,
							Choices: []dto.ChatCompletionsStreamResponseChoice{
								{
									Index: 0,
									Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
										Content: &delta,
									},
								},
							},
						}
						if !sendChatChunk(chunk) {
							usagePending = false
							sr.Stop(streamErr)
							return
						}
						hasVisiblePayload = true
					}
				}
				for outputIndex, out := range streamResp.Response.Output {
					if out.Type != dto.BuildInCallFunctionCall && out.Type != dto.BuildInCallCustomToolCall {
						// Server-side search facts have no converted tool payload.
						if out.Type == dto.BuildInCallWebSearchCall || out.Type == dto.BuildInCallFileSearchCall {
							observeResponsesToolOutput(info, &out, &outputIndex)
						}
						continue
					}
					itemID := strings.TrimSpace(out.ID)
					callID := strings.TrimSpace(out.CallId)
					if callID == "" {
						callID = itemID
					}
					if _, alreadyEmitted := toolCallIndexByID[callID]; alreadyEmitted {
						observeResponsesToolOutput(info, &out, &outputIndex)
						continue
					}
					if !sendToolCallDelta(callID, strings.TrimSpace(out.Name), out.ArgumentsString()) {
						usagePending = false
						sr.Stop(streamErr)
						return
					}
					if callID != "" {
						observeResponsesToolOutput(info, &out, &outputIndex)
					}
				}
				if streamResp.Response.HasImageGenerationCall() {
					hasVisiblePayload = true
				}
			}
			mergeResponsesStreamUsage(usage, &usagePresence, data)
			usagePending = false

			if !hasVisiblePayload {
				emptyCompletionRecorded = true
				if retryErr := handleEmptyResponsesStreamCompletion(c, info, usage, "empty_responses_stream_output", completedOutputTypes); retryErr != nil {
					streamErr = retryErr
					sr.Stop(streamErr)
					return
				}
			}

			if !sendStartIfNeeded() {
				sr.Stop(streamErr)
				return
			}
			// Converted terminal usage must be the same snapshot returned for
			// settlement. A reported numeric zero must never become an estimate.
			finalizeUsage(usageText.String())
			if !sentStop {
				if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
					info.ClaudeConvertInfo.Usage = usage
				}
				finishReason := "stop"
				if mappedReason, ok := openaicompat.ResponsesFinishReasonFromStatus(streamResp.Response); ok {
					finishReason = mappedReason
				} else if sawToolCall {
					finishReason = "tool_calls"
				}
				stop := helper.GenerateStopResponse(responseId, createAt, model, finishReason)
				if !sendChatChunk(stop) {
					sr.Stop(streamErr)
					return
				}
				sentStop = true
			}

		case "error", "response.error", "response.failed", "response.cancelled", "response.canceled":
			skipRetry := info.SendResponseCount > 0
			streamErr = responsesStreamTerminalError(&streamResp, skipRetry)
			sr.Stop(streamErr)
			return

		default:
		}
	})

	if apiErr := helper.FirstResultTimeoutError(c, info); apiErr != nil {
		if outputDelivered && partialUsageSupported {
			finalizeUsage(deliveredUsageText.String())
			return usage, apiErr
		}
		return nil, apiErr
	}

	if streamErr != nil {
		if c.Writer.Written() {
			types.ErrOptionWithSkipRetry()(streamErr)
		}
		if outputDelivered && partialUsageSupported {
			finalizeUsage(deliveredUsageText.String())
			return usage, streamErr
		}
		return nil, streamErr
	}
	if !hasVisiblePayload && !emptyCompletionRecorded {
		emptyCompletionRecorded = true
		if retryErr := handleEmptyResponsesStreamCompletion(c, info, usage, "empty_responses_stream_eof", nil); retryErr != nil {
			return nil, retryErr
		}
	}
	if !sentStop {
		reason := "upstream_eof"
		if info.StreamStatus != nil && info.StreamStatus.IsAbnormalEnd() {
			reason = string(info.StreamStatus.EndReason)
		}
		apiErr := openaicompat.NewResponsesStreamIncompleteError(reason)
		if outputDelivered && partialUsageSupported {
			finalizeUsage(deliveredUsageText.String())
			return usage, apiErr
		}
		return nil, apiErr
	}

	if !sentStart {
		if !sendChatChunk(helper.GenerateStartEmptyResponse(responseId, createAt, model, nil)) {
			return nil, streamErr
		}
	}
	if !sentStop {
		if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
			info.ClaudeConvertInfo.Usage = usage
		}
		finishReason := "stop"
		if sawToolCall {
			finishReason = "tool_calls"
		}
		stop := helper.GenerateStopResponse(responseId, createAt, model, finishReason)
		if !sendChatChunk(stop) {
			return nil, streamErr
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		clientUsage := *usage
		clientUsage.BillingUsage = nil
		err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, model, clientUsage))
		if err == nil {
			err = writeObserver.err
		}
		if err != nil {
			info.StreamStatus.RecordError(err.Error())
			return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
		if writeObserver.err != nil {
			info.StreamStatus.RecordError(writeObserver.err.Error())
			return usage, types.NewOpenAIError(writeObserver.err, types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
	}
	auditText := outputText.String()
	if strings.TrimSpace(auditText) == "" {
		auditText = usageText.String()
	}
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, auditText)
	}
	if hasVisiblePayload {
		recordEmptyCompletionRetrySuccess(c, info, usage)
	}
	return usage, nil
}

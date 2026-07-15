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

	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
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
		switch streamResponse.Type {
		case "response.completed":
			if streamResponse.Response != nil {
				applyResponsesUsage(usage, streamResponse.Response.Usage)
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
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
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
		case "response.completed", "response.error", "response.failed":
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

package openai

import (
	"encoding/json"
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
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

func OpenaiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	err = common.Unmarshal(responseBody, &usageResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if service.ResponseAuditEnabled() {
		var imageResp dto.ImageResponse
		if err := common.Unmarshal(responseBody, &imageResp); err == nil {
			service.SetRelayResponseAuditContent(info, service.BuildImageResponseAuditContent(&imageResp))
		}
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	normalizeOpenAIImageUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	return &usageResp.Usage, nil
}

func normalizeOpenAIImageUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.CacheWriteTokens = usage.InputTokensDetails.CacheWriteTokens
		usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.TextTokens = usage.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens = usage.InputTokensDetails.AudioTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func OpenaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid image stream response")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenaiImageHandler(c, info, resp)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return OpenaiImageJSONAsStreamHandler(c, info, resp)
	}

	usage := &dto.Usage{}
	var lastStreamData []byte
	var auditText strings.Builder

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		raw := common.StringToByteSlice(data)
		lastStreamData = raw
		if service.ResponseAuditEnabled() {
			if line := openaiImageStreamAuditLine(raw); line != "" {
				if auditText.Len() > 0 {
					auditText.WriteByte('\n')
				}
				auditText.WriteString(line)
			}
		}
		if isOpenAIImageStreamErrorEvent(raw) {
			sr.Error(fmt.Errorf("%s", extractOpenAIImageStreamErrorMessage(raw)))
		}
		var usageResp dto.SimpleResponse
		if err := common.Unmarshal(raw, &usageResp); err == nil {
			normalizeOpenAIImageUsage(&usageResp.Usage)
			if service.ValidUsage(&usageResp.Usage) {
				usage = &usageResp.Usage
			}
		}
		writeOpenaiImageStreamChunk(c, raw)
	})

	if info != nil && info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone {
		helper.Done(c)
	}

	applyUsagePostProcessing(info, usage, lastStreamData)
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, auditText.String())
	}
	return usage, nil
}

func writeOpenaiImageStreamChunk(c *gin.Context, data []byte) {
	var payload struct {
		Type string `json:"type"`
	}
	_ = common.Unmarshal(data, &payload)
	if eventName := strings.TrimSpace(payload.Type); eventName != "" {
		_ = helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventName}, string(data))
		return
	}
	_ = helper.StringData(c, string(data))
}

func isOpenAIImageStreamErrorEvent(data []byte) bool {
	if !common.ValidJSON(data) {
		return false
	}
	var payload struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	return payloadType == "error" || payloadType == "upstream_error" || len(payload.Error) > 0
}

func openaiImageStreamAuditLine(data []byte) string {
	if len(data) == 0 || !common.ValidJSON(data) {
		return ""
	}
	var payload struct {
		Type          string `json:"type"`
		Url           string `json:"url"`
		B64Json       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if payload.Type != "" {
		parts = append(parts, "event: "+payload.Type)
	}
	if payload.RevisedPrompt != "" {
		parts = append(parts, "revised_prompt: "+payload.RevisedPrompt)
	}
	if payload.Url != "" {
		parts = append(parts, "url: "+payload.Url)
	}
	if payload.B64Json != "" {
		parts = append(parts, "b64_json: "+service.Base64AuditPlaceholder)
	}
	return strings.Join(parts, "\n")
}

func extractOpenAIImageStreamErrorMessage(data []byte) string {
	if len(data) == 0 || !common.ValidJSON(data) {
		return "upstream image stream returned error event"
	}
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return "upstream image stream returned error event"
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg
	}
	if len(payload.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(payload.Error, &nested); err == nil {
			if msg := strings.TrimSpace(nested.Message); msg != "" {
				return msg
			}
		}
		if msg := strings.TrimSpace(common.JsonRawMessageToString(payload.Error)); msg != "" {
			return msg
		}
	}
	return "upstream image stream returned error event"
}

func OpenaiImageJSONAsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var imageResp dto.ImageResponse
	if err := common.Unmarshal(responseBody, &imageResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	_ = common.Unmarshal(responseBody, &usageResp)
	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, service.BuildImageResponseAuditContent(&imageResp))
	}
	normalizeOpenAIImageUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)

	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)

	created := imageResp.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	if info != nil {
		info.SetFirstResponseTime()
	}
	for _, image := range imageResp.Data {
		payload := map[string]any{
			"type":       "image_generation.completed",
			"created_at": created,
		}
		if image.Url != "" {
			payload["url"] = image.Url
		}
		if image.B64Json != "" {
			payload["b64_json"] = image.B64Json
		}
		if image.RevisedPrompt != "" {
			payload["revised_prompt"] = image.RevisedPrompt
		}
		if service.ValidUsage(&usageResp.Usage) {
			payload["usage"] = usageResp.Usage
		}
		if err := writeOpenaiImageStreamPayload(c, "image_generation.completed", payload); err != nil {
			if info != nil && info.StreamStatus != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			}
			return &usageResp.Usage, nil
		}
	}
	if err := writeOpenaiImageStreamDone(c); err != nil {
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		}
		return &usageResp.Usage, nil
	}
	if info != nil {
		info.ReceivedResponseCount += len(imageResp.Data)
		if info.StreamStatus == nil {
			info.StreamStatus = relaycommon.NewStreamStatus()
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	}
	return &usageResp.Usage, nil
}

func writeOpenaiImageStreamPayload(c *gin.Context, eventName string, payload any) error {
	data, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	if eventName != "" {
		return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventName}, string(data))
	}
	return helper.StringData(c, string(data))
}

func writeOpenaiImageStreamDone(c *gin.Context) error {
	return helper.StringData(c, "[DONE]")
}

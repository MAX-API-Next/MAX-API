package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

const (
	maxPendingChatStreamEvents = 32
	maxPendingChatStreamBytes  = 64 << 10
)

// Observe native Chat writes because legacy SSE rendering can discard writer
// errors. The scanner serializes this wrapper with ping writes.
type chatStreamWriteObserver struct {
	gin.ResponseWriter
	err error
}

func (w *chatStreamWriteObserver) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.ResponseWriter.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (w *chatStreamWriteObserver) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

// Keep the raw delta fields for the visibility decision. The forwarding path
// still owns formatting; unknown provider payloads must not become empty just
// because the typed Chat DTO does not yet describe them.
type chatStreamFrame struct {
	Error   json.RawMessage `json:"error"`
	Usage   *dto.Usage      `json:"usage"`
	Choices []struct {
		Delta        map[string]any `json:"delta"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
}

func chatStreamValueHasPayload(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	default:
		// Be conservative with provider extensions: never silently replay an
		// output whose semantics we cannot classify.
		return true
	}
}

func (frame *chatStreamFrame) hasPayload() bool {
	for _, choice := range frame.Choices {
		if choice.FinishReason == "content_filter" {
			return true
		}
		for key, value := range choice.Delta {
			if key != "role" && chatStreamValueHasPayload(value) {
				return true
			}
		}
	}
	return false
}

func (frame *chatStreamFrame) upstreamError() *types.MaxAPIError {
	if len(frame.Error) == 0 || strings.TrimSpace(string(frame.Error)) == "null" {
		return nil
	}
	var upstream types.OpenAIError
	if err := common.Unmarshal(frame.Error, &upstream); err != nil {
		// Some compatible providers send a string instead of an error object.
		_ = common.Unmarshal(frame.Error, &upstream.Message)
	}
	if strings.TrimSpace(upstream.Message) == "" {
		upstream.Message = "upstream returned a stream error"
	}
	if upstream.Type == "" {
		upstream.Type = "upstream_error"
	}
	apiErr := types.WithOpenAIError(upstream, http.StatusBadGateway)
	if chatStreamRequestErrorIsPermanent(upstream) {
		types.ErrOptionWithSkipRetry()(apiErr)
	}
	return apiErr
}

// Reject invalid requests without replaying them, but leave channel-specific
// credentials, quota, model availability and transient failures to routing.
func chatStreamRequestErrorIsPermanent(upstream types.OpenAIError) bool {
	code, _ := upstream.Code.(string)
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "rate_limit_exceeded", "insufficient_quota", "invalid_api_key", "model_not_found", "server_error", "overloaded_error":
		return false
	case "invalid_request", "invalid_request_error", "context_length_exceeded", "invalid_prompt", "content_policy_violation", "invalid_parameter", "unsupported_parameter", "missing_required_parameter", "invalid_value", "invalid_type":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(upstream.Type), "invalid_request_error")
}

// A failed attempt with no payload returns nil usage and can reach the retry
// policy. A partial response returns usage AND a SkipRetry error; TextHelper
// settles that usage before returning the terminal error to the controller.
func oaiChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	if info.RetryIndex > 0 && !c.Writer.Written() {
		info.StreamStatus = nil
		info.SendResponseCount, info.ReceivedResponseCount = 0, 0
		info.ThinkingContentInfo = relaycommon.ThinkingContentInfo{IsFirstThinkingContent: true}
	}
	usage := &dto.Usage{}
	containUsage, hasPayload := false, false
	var text strings.Builder
	var toolCount int
	var lastData string
	var streamErr *types.MaxAPIError
	pending := make([]string, 0, 4)
	pendingBytes := 0
	buffering := shouldRetryEmptyCompletion(c, info)
	observer := newOpenAIStreamToolCallObserver(info)

	send := func(data string) bool {
		writer := &chatStreamWriteObserver{ResponseWriter: c.Writer}
		c.Writer = writer
		defer func() { c.Writer = writer.ResponseWriter }()
		err := HandleStreamFormat(c, info, data, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
		if err == nil {
			err = writer.err
		}
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
			if c.Writer.Written() {
				types.ErrOptionWithSkipRetry()(streamErr)
			}
			return false
		}
		return true
	}
	recordUsage := func(frame *chatStreamFrame, data string) {
		lastData = data
		if frame.Usage != nil {
			usage, containUsage = frame.Usage, true
		}
	}
	flushPending := func() bool {
		buffering = false
		for _, data := range pending {
			if !send(data) {
				return false
			}
		}
		pending = nil
		pendingBytes = 0
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var frame chatStreamFrame
		if err := common.UnmarshalJsonStr(data, &frame); err != nil {
			streamErr = types.NewOpenAIError(fmt.Errorf("invalid upstream chat stream: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}
		// Inspect the current error before releasing any preceding role frame.
		if streamErr = frame.upstreamError(); streamErr != nil {
			sr.Stop(streamErr)
			return
		}
		visible := frame.hasPayload()
		if !info.ShouldIncludeUsage && frame.Usage != nil && len(frame.Choices) == 0 {
			recordUsage(&frame, data)
			return
		}
		if buffering {
			if visible || !shouldRetryEmptyCompletion(c, info) || len(pending) >= maxPendingChatStreamEvents || pendingBytes+len(data) > maxPendingChatStreamBytes {
				if !flushPending() {
					sr.Stop(streamErr)
					return
				}
			} else {
				recordUsage(&frame, data)
				pending = append(pending, data)
				pendingBytes += len(data)
				return
			}
		}
		if !send(data) {
			sr.Stop(streamErr)
			return
		}
		hasPayload = hasPayload || visible
		recordUsage(&frame, data)
		observeOpenAIStreamToolCalls(observer, data)
		if err := processTokenData(info.RelayMode, data, &text, &toolCount); err != nil {
			sr.Error(err)
		}
	})

	// The scanner has joined its workers, so a concurrent ping/write cannot
	// race the final retry decision. Cancellation is never an empty completion.
	if c.Request.Context().Err() != nil {
		streamErr = types.NewOpenAIError(c.Request.Context().Err(), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	} else if streamErr == nil && !info.StreamStatus.IsNormalEnd() {
		streamErr = types.NewOpenAIError(fmt.Errorf("upstream chat stream ended: %s", info.StreamStatus.Summary()), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if streamErr != nil {
		if c.Writer.Written() {
			types.ErrOptionWithSkipRetry()(streamErr)
		}
		if !hasPayload {
			return nil, streamErr
		}
	} else if !hasPayload {
		willRetry := shouldRetryEmptyCompletion(c, info)
		recordEmptyCompletion(c, info, usage, "empty_chat_stream_output", nil, willRetry)
		if willRetry {
			return nil, newEmptyCompletionRetryError()
		}
	}

	if !containUsage {
		usage = service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += toolCount * 7
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	applyUsagePostProcessing(info, usage, common.StringToByteSlice(lastData))
	if service.ResponseAuditEnabled() {
		service.SetRelayResponseAuditContent(info, text.String())
	}
	if streamErr != nil {
		return usage, streamErr
	}
	if !flushPending() {
		if !hasPayload {
			return nil, streamErr
		}
		return usage, streamErr
	}
	if hasPayload {
		recordEmptyCompletionRetrySuccess(c, info, usage)
	}

	var metadata dto.ChatCompletionsStreamResponse
	_ = common.UnmarshalJsonStr(lastData, &metadata)
	HandleFinalResponse(c, info, lastData, metadata.Id, metadata.Created, metadata.Model, metadata.GetSystemFingerprint(), usage, containUsage)
	return usage, nil
}

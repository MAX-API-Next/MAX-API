package helper

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	return nil
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return nil
	}

	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
	} else {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonData)})
	}
	_ = FlushWriter(c)
	return nil
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	if c == nil || c.Writer == nil {
		return
	}

	if requestContextDone(c) {
		return
	}

	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)})
	_ = FlushWriter(c)
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	_, err := ResponseChunkDataWithDelivery(c, resp, data)
	return err
}

// Delivery is true only after a complete SSE frame was written. Flush may
// still fail after delivery, and a short write must never count as a frame.
func ResponseChunkDataWithDelivery(c *gin.Context, resp dto.ResponsesStreamResponse, data string) (bool, error) {
	if c == nil || c.Writer == nil {
		return false, errors.New("context or writer is nil")
	}
	state := getResponsesStreamState(c)
	state.mu.Lock()
	defer state.mu.Unlock()
	return writeResponsesStreamFrame(c, state, resp.Type, data)
}

// The caller holds the request-local lock, including while flushing. A complete
// write still counts as delivery if flush fails, but no later frame may follow.
func writeResponsesStreamFrame(c *gin.Context, state *responsesStreamState, eventType, data string) (bool, error) {
	if state.writeErr != nil {
		return false, state.writeErr
	}
	if state.terminal {
		return false, errResponsesStreamClosed
	}

	if requestContextDone(c) {
		state.writeErr = fmt.Errorf("request context done: %w", c.Request.Context().Err())
		return false, state.writeErr
	}
	if (strings.HasPrefix(eventType, "response.") || eventType == "error") && !gjson.Get(data, "sequence_number").Exists() {
		var err error
		data, err = sjson.Set(data, "sequence_number", state.nextSequence)
		if err != nil {
			return false, err
		}
	}

	SetEventStreamHeaders(c)
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, strings.ReplaceAll(data, "\r", "\\r"))
	n, err := c.Writer.Write([]byte(frame))
	if err != nil {
		state.writeErr = err
		return false, err
	}
	if n != len(frame) {
		state.writeErr = io.ErrShortWrite
		return false, io.ErrShortWrite
	}
	state.recordDelivery(eventType, data)
	err = flushResponsesWriter(c)
	if err != nil {
		state.writeErr = err
	}
	return true, err
}

// ResponsesStreamEventHasOutput distinguishes delivered generation data from
// lifecycle metadata when deciding whether an interrupted stream has usage.
func ResponsesStreamEventHasOutput(event dto.ResponsesStreamResponse) bool {
	switch event.Type {
	case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta",
		"response.refusal.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		return event.Delta != ""
	case dto.ResponsesOutputTypeItemAdded, dto.ResponsesOutputTypeItemDone:
		if event.Item != nil {
			if event.Item.Type == dto.BuildInCallFileSearchCall {
				// A search lifecycle/failed item is metadata. Only a finished
				// invocation carries a delivered result and its existing fee.
				status := strings.ToLower(strings.TrimSpace(event.Item.Status))
				return event.Type == dto.ResponsesOutputTypeItemDone && (status == "" || status == "completed")
			}
			return event.Item.Type == dto.BuildInCallFunctionCall || event.Item.Type == dto.BuildInCallCustomToolCall
		}
	}
	return false
}

// First-result timing excludes whitespace-only metadata/text while retaining
// the independent output-delivery predicate used for partial usage accounting.
func ResponsesStreamEventHasFirstResult(event dto.ResponsesStreamResponse) bool {
	if !ResponsesStreamEventHasOutput(event) {
		return false
	}
	if event.Type == dto.ResponsesOutputTypeItemAdded || event.Type == dto.ResponsesOutputTypeItemDone {
		return true
	}
	return strings.TrimSpace(event.Delta) != ""
}

// An empty usage object is not a reported zero. Require both numeric counters
// before treating an otherwise empty usage payload as authoritative.
func HasUsageTokenFields(data, path, inputField, outputField string) bool {
	return gjson.Get(data, path+"."+inputField).Type == gjson.Number &&
		gjson.Get(data, path+"."+outputField).Type == gjson.Number
}

func StringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	c.Render(-1, common.CustomEvent{Data: "data: " + str})
	return FlushWriter(c)
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}

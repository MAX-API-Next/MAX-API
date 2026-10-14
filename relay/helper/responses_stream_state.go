package helper

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const responsesStreamStateKey = "max_responses_stream_state"

var errResponsesStreamClosed = errors.New("responses stream is already terminal")

// Request-local downstream state, independent of provider status and billing.
// Keep only the latest lifecycle snapshot, not an unbounded history of deltas.
type responsesStreamState struct {
	mu           sync.Mutex
	nextSequence int64
	response     string
	terminal     bool
	writeErr     error
}

func getResponsesStreamState(c *gin.Context) *responsesStreamState {
	if value, ok := c.Get(responsesStreamStateKey); ok {
		return value.(*responsesStreamState)
	}
	state := &responsesStreamState{}
	c.Set(responsesStreamStateKey, state)
	return state
}

func isResponsesTerminalEvent(eventType string) bool {
	switch eventType {
	case "response.completed", "response.incomplete", "response.failed", "response.done",
		"response.cancelled", "response.canceled", "error", "response.error":
		return true
	default:
		return false
	}
}

// A failed ping can happen before any Responses event reaches the client.
func LatchResponsesStreamWriteError(c *gin.Context, err error) {
	if c == nil || err == nil {
		return
	}
	state := getResponsesStreamState(c)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.writeErr == nil {
		state.writeErr = err
	}
}

// Gin's Flush delegates to http.Flusher and discards FlushError. Prefer a
// supported error-returning flush through wrappers; retain legacy fallback.
func flushResponsesWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()
	if requestContextDone(c) {
		return c.Request.Context().Err()
	}
	var writer http.ResponseWriter = c.Writer
	for {
		if flusher, ok := writer.(interface{ FlushError() error }); ok {
			return flusher.FlushError()
		}
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		writer = unwrapper.Unwrap()
		if writer == nil {
			break
		}
	}
	return FlushWriter(c)
}

// WriteResponsesStreamFailure is the controller's fallback after commitment.
// An already delivered terminal or a broken downstream owns the outcome; no
// later error writer may append to it. Return errors for observability only.
func WriteResponsesStreamFailure(c *gin.Context, apiErr *types.MaxAPIError) error {
	state := getResponsesStreamState(c)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.writeErr != nil {
		return state.writeErr
	}
	if state.terminal {
		return nil
	}
	if requestContextDone(c) {
		return c.Request.Context().Err()
	}
	oaiErr := apiErr.ToOpenAIError()
	var errorCode any
	if oaiErr.Code != nil {
		errorCode = common.Interface2String(oaiErr.Code)
	}
	event := map[string]any{"type": "error", "code": errorCode, "message": oaiErr.Message, "param": nil}
	if oaiErr.Param != "" {
		event["param"] = oaiErr.Param
	}
	if state.response != "" {
		var response map[string]any
		if err := common.UnmarshalJsonStr(state.response, &response); err != nil {
			return err
		}
		// No synthetic provider identity when the stream has no response object.
		if id, _ := response["id"].(string); id != "" {
			code := "server_error"
			if common.Interface2String(oaiErr.Code) == "rate_limit_exceeded" {
				code = "rate_limit_exceeded"
			}
			response["status"] = "failed"
			response["error"] = map[string]any{"code": code, "message": oaiErr.Message}
			response["incomplete_details"] = nil
			// Usage in an earlier lifecycle snapshot is not final usage.
			response["usage"] = nil
			event = map[string]any{"type": "response.failed", "response": response}
		}
	}
	event["sequence_number"] = state.nextSequence
	data, err := common.Marshal(event)
	if err != nil {
		return err
	}
	_, err = writeResponsesStreamFrame(c, state, event["type"].(string), string(data))
	return err
}

func (state *responsesStreamState) recordDelivery(eventType, data string) {
	if sequence := gjson.Get(data, "sequence_number"); sequence.Exists() && sequence.Type == gjson.Number {
		if next := sequence.Int() + 1; next > state.nextSequence {
			state.nextSequence = next
		}
	} else {
		state.nextSequence++
	}
	if response := gjson.Get(data, "response"); response.IsObject() {
		state.response = response.Raw
	}
	if isResponsesTerminalEvent(eventType) {
		state.terminal = true
	}
}

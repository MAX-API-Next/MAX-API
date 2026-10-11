package helper

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type responsesFlushErrorWriter struct {
	*httptest.ResponseRecorder
	writes  int
	flushes int
	err     error
}

func (w *responsesFlushErrorWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.ResponseRecorder.Write(p)
}

func (w *responsesFlushErrorWriter) FlushError() error {
	w.flushes++
	return w.err
}

func TestResponsesFullWriteFlushErrorLatchesDelivery(t *testing.T) {
	w := &responsesFlushErrorWriter{ResponseRecorder: httptest.NewRecorder(), err: io.ErrClosedPipe}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	delivered, err := ResponseChunkDataWithDelivery(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","delta":"partial"}`)
	require.True(t, delivered, "full write remains billable delivery when flush fails")
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.Equal(t, 1, w.writes)
	require.Equal(t, 1, w.flushes)
	before := w.Body.String()
	apiErr := types.NewError(errors.New("upstream failure"), types.ErrorCodeBadResponse)
	require.ErrorIs(t, WriteResponsesStreamFailure(c, apiErr), io.ErrClosedPipe)
	delivered, err = ResponseChunkDataWithDelivery(c, dto.ResponsesStreamResponse{Type: "response.failed"}, `{"type":"response.failed"}`)
	require.False(t, delivered)
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.Equal(t, 1, w.writes)
	require.Equal(t, before, w.Body.String())
}

func TestResponsesMissingIdentityUsesErrorEventAndSequence(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	require.NoError(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","delta":"partial"}`))
	apiErr := types.NewError(errors.New("local failure"), types.ErrorCodeBadResponse)
	require.NoError(t, WriteResponsesStreamFailure(c, apiErr))
	frames := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")
	require.Len(t, frames, 2)
	for idx, frame := range frames {
		data := strings.SplitN(frame, "data: ", 2)[1]
		require.EqualValues(t, idx, gjson.Get(data, "sequence_number").Int())
	}
	require.True(t, strings.HasPrefix(frames[1], "event: error\n"))
	require.NotContains(t, frames[1], "resp_")
	require.NotContains(t, frames[1], "[DONE]")
	before := w.Body.String()
	require.NoError(t, WriteResponsesStreamFailure(c, apiErr))
	require.Equal(t, before, w.Body.String())
	_, err := ResponseChunkDataWithDelivery(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{"type":"response.completed"}`)
	require.ErrorIs(t, err, errResponsesStreamClosed)
	require.Equal(t, before, w.Body.String())
}

func TestResponsesSequencePreservesProviderAndNonResponsesFrames(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	raw := `{"type":"response.extension","sequence_number":41,"extension":{"keep":true}}`
	require.NoError(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.extension"}, raw))
	require.Contains(t, w.Body.String(), "data: "+raw+"\n\n")
	require.NoError(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","delta":"partial"}`))
	last := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")[1]
	require.EqualValues(t, 42, gjson.Get(strings.SplitN(last, "data: ", 2)[1], "sequence_number").Int())
	image := `{"type":"image_generation.partial_image","b64_json":"synthetic"}`
	require.NoError(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "image_generation.partial_image"}, image))
	require.Contains(t, w.Body.String(), "data: "+image+"\n\n")
}

func TestResponsesPingFailureSuppressesFallbackWithoutEvent(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	SetEventStreamHeaders(c)
	require.NoError(t, PingData(c))
	LatchResponsesStreamWriteError(c, io.ErrClosedPipe)
	before := w.Body.String()
	apiErr := types.NewError(errors.New("timeout"), types.ErrorCodeBadResponse)
	require.ErrorIs(t, WriteResponsesStreamFailure(c, apiErr), io.ErrClosedPipe)
	require.Equal(t, before, w.Body.String())
}

package helper

import (
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesFailureWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w *responsesFailureWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return len(p) - 1, nil
}

func TestResponseChunkDataPropagatesWriteFailure(t *testing.T) {
	for _, writeErr := range []error{errors.New("synthetic write failure"), nil} {
		w := &responsesFailureWriter{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
		c, _ := gin.CreateTestContext(w)
		err := ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{}`)
		if writeErr == nil {
			require.ErrorIs(t, err, io.ErrShortWrite)
		} else {
			require.ErrorIs(t, err, writeErr)
		}
	}
}

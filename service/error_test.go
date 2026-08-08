package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
		{
			name:             "skip out of range target",
			statusCode:       429,
			statusCodeConfig: `{"429":999}`,
			expectedCode:     429,
		},
		{
			name:             "ignore unrelated invalid entry",
			statusCode:       429,
			statusCodeConfig: `{"429":503,"500":999}`,
			expectedCode:     503,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			maxAPIError := &types.MaxAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(maxAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, maxAPIError.StatusCode)
		})
	}
}

func TestResetTaskStatusCodePreservesOriginalUpstreamStatus(t *testing.T) {
	t.Parallel()

	upstreamErr := &dto.TaskError{StatusCode: http.StatusTooManyRequests}
	ResetTaskStatusCode(upstreamErr, `{"429":"503"}`)
	require.Equal(t, http.StatusServiceUnavailable, upstreamErr.StatusCode)
	require.Equal(t, http.StatusTooManyRequests, upstreamErr.UpstreamStatusCode)
}

func TestValidateStatusCodeMapping(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateStatusCodeMapping(`{"429":"503","500":502}`))
	require.Error(t, ValidateStatusCodeMapping(`null`))
	require.Error(t, ValidateStatusCodeMapping(`{"bad":503}`))
	require.Error(t, ValidateStatusCodeMapping(`{"429":999}`))
	require.Error(t, ValidateStatusCodeMapping(`{"429":503,"0429":502}`))
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	maxAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, maxAPIError)
	require.Equal(t, "bad response status code 500", maxAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	maxAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, maxAPIError)
	require.Equal(t, message, maxAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	maxAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, maxAPIError)
	require.Equal(t, message, maxAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	maxAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, maxAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func TestRelayErrorHandlerLimitsUpstreamErrorBodyRead(t *testing.T) {
	body := strings.Repeat("x", maxUpstreamErrorBodyBytes+4096)
	reader := &countingReadCloser{Reader: strings.NewReader(body)}
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       reader,
	}

	maxAPIError := RelayErrorHandler(context.Background(), resp, true)

	require.NotNil(t, maxAPIError)
	require.LessOrEqual(t, reader.read, maxUpstreamErrorBodyBytes+1)
	require.Contains(t, maxAPIError.Error(), "truncated after")
}

func TestTaskErrorFromUpstreamResponseKeepsStructuredSafeMessage(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream failed\r\ntry later","type":"server_error","code":"server_error"}}`)),
	}

	taskErr := TaskErrorFromUpstreamResponse(context.Background(), resp, "fail_to_fetch_task")

	require.NotNil(t, taskErr)
	require.Equal(t, "fail_to_fetch_task", taskErr.Code)
	require.Equal(t, http.StatusBadGateway, taskErr.StatusCode)
	require.Equal(t, "upstream failed  try later", taskErr.Message)
	require.NotContains(t, taskErr.Message, "\r")
	require.NotContains(t, taskErr.Message, "\n")
}

func TestTaskErrorFromUpstreamResponseDoesNotExposeRawInvalidBody(t *testing.T) {
	rawBody := `{"internal_prompt":"` + strings.Repeat("secret", 100) + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(rawBody)),
	}

	taskErr := TaskErrorFromUpstreamResponse(context.Background(), resp, "fail_to_fetch_task")

	require.NotNil(t, taskErr)
	require.Equal(t, "bad upstream response status code 500", taskErr.Message)
	require.NotContains(t, taskErr.Message, "secret")
}

func TestTaskErrorFromUpstreamResponseHandlesNilBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
	}

	taskErr := TaskErrorFromUpstreamResponse(context.Background(), resp, "fail_to_fetch_task")

	require.NotNil(t, taskErr)
	require.Equal(t, "bad upstream response status code 502", taskErr.Message)
}

type countingReadCloser struct {
	io.Reader
	read int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func (r *countingReadCloser) Close() error {
	return nil
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}

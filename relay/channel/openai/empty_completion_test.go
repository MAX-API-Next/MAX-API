package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

func TestIsEmptyResponsesCompletion(t *testing.T) {
	tests := []struct {
		name string
		resp *dto.OpenAIResponsesResponse
		want bool
	}{
		{
			name: "empty assistant message",
			resp: &dto.OpenAIResponsesResponse{
				Output: []dto.ResponsesOutput{{
					Type: "message",
					Role: "assistant",
				}},
			},
			want: true,
		},
		{
			name: "text output",
			resp: &dto.OpenAIResponsesResponse{
				Output: []dto.ResponsesOutput{{
					Type: "message",
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{{
						Type: "output_text",
						Text: "hello",
					}},
				}},
			},
			want: false,
		},
		{
			name: "tool call",
			resp: &dto.OpenAIResponsesResponse{
				Output: []dto.ResponsesOutput{{
					Type:   "function_call",
					ID:     "fc_1",
					Name:   "lookup",
					CallId: "call_1",
				}},
			},
			want: false,
		},
		{
			name: "refusal",
			resp: &dto.OpenAIResponsesResponse{
				Output: []dto.ResponsesOutput{{
					Type: "message",
					Role: "assistant",
					Content: []dto.ResponsesOutputContent{{
						Type:    "refusal",
						Refusal: "I cannot help with that.",
					}},
				}},
			},
			want: false,
		},
		{
			name: "image generation call",
			resp: &dto.OpenAIResponsesResponse{
				Output: []dto.ResponsesOutput{{
					Type: dto.ResponsesOutputTypeImageGenerationCall,
				}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyResponsesCompletion(tt.resp); got != tt.want {
				t.Fatalf("isEmptyResponsesCompletion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRetryEmptyCompletionOnlyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RetryIndex: 0}

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 2
	common.EmptyCompletionRetryEnabled = true
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	if !shouldRetryEmptyCompletion(c, info) {
		t.Fatal("first empty completion should be retryable")
	}

	recordEmptyCompletion(c, info, nil, "empty_responses_output", nil, true)
	if shouldRetryEmptyCompletion(c, info) {
		t.Fatal("second empty completion should not be retryable")
	}
}

func TestShouldRetryEmptyCompletionDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RetryIndex: 0}

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 2
	common.EmptyCompletionRetryEnabled = false
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	if shouldRetryEmptyCompletion(c, info) {
		t.Fatal("empty completion should not retry when the feature is disabled")
	}
}

func TestOaiResponsesStreamHandlerRetriesEmptyBeforeWriting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 1
	common.EmptyCompletionRetryEnabled = true
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","output":[]}}`,
			`data: {"type":"response.output_text.delta","delta":""}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","output":[],"usage":{"input_tokens":3,"output_tokens":0,"total_tokens":3}}}`,
			`data: [DONE]`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		RetryIndex:  0,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	if err == nil {
		t.Fatal("expected empty completion retry error")
	}
	if got := err.GetErrorCode(); got != types.ErrorCodeEmptyCompletion {
		t.Fatalf("error code = %s, want %s", got, types.ErrorCodeEmptyCompletion)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil on retry error", usage)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("response body = %q, want empty before retry", body)
	}
}

func TestOaiResponsesStreamHandlerRetriesEmptyOnEOFBeforeWriting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 1
	common.EmptyCompletionRetryEnabled = true
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","output":[]}}`,
			`data: {"type":"response.reasoning_summary_text.delta","delta":"   "}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		RetryIndex:  0,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	if err == nil {
		t.Fatal("expected empty completion retry error")
	}
	if got := err.GetErrorCode(); got != types.ErrorCodeEmptyCompletion {
		t.Fatalf("error code = %s, want %s", got, types.ErrorCodeEmptyCompletion)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil on retry error", usage)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("response body = %q, want empty before retry", body)
	}
}

func TestOaiResponsesStreamHandlerFlushesImmediatelyWhenEmptyRetryDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 1
	common.EmptyCompletionRetryEnabled = false
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","output":[]}}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		RetryIndex:  0,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
	}

	_, err := OaiResponsesStreamHandler(c, info, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "response.created") {
		t.Fatalf("response body = %q, want forwarded pre-content event", body)
	}
}

func TestOaiResponsesStreamHandlerCapsPendingEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 1
	common.EmptyCompletionRetryEnabled = true
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	events := make([]string, 0, maxPendingResponsesStreamEvents+2)
	for i := 0; i < maxPendingResponsesStreamEvents+1; i++ {
		events = append(events, `data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","output":[]}}`)
	}
	events = append(events, "")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Join(events, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		RetryIndex:  0,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
	}

	_, err := OaiResponsesStreamHandler(c, info, resp)
	if err != nil {
		t.Fatalf("unexpected error after pending cap forces forwarding: %v", err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "response.created") {
		t.Fatalf("response body = %q, want forwarded events after cap", body)
	}
}

func TestOaiResponsesToChatStreamHandlerRetriesEmptyOnEOFBeforeWriting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldRetryTimes := common.RetryTimes
	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	common.RetryTimes = 1
	common.EmptyCompletionRetryEnabled = true
	defer func() {
		common.RetryTimes = oldRetryTimes
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","output":[]}}`,
			`data: {"type":"response.reasoning_summary_text.delta","delta":"   "}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		RetryIndex:  0,
		DisablePing: true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	if err == nil {
		t.Fatal("expected empty completion retry error")
	}
	if got := err.GetErrorCode(); got != types.ErrorCodeEmptyCompletion {
		t.Fatalf("error code = %s, want %s", got, types.ErrorCodeEmptyCompletion)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil on retry error", usage)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("response body = %q, want empty before retry", body)
	}
}

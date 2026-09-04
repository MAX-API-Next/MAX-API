package openai

import (
	"bytes"
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
	"github.com/stretchr/testify/require"
)

func detailedResponsesUsage() *dto.Usage {
	return &dto.Usage{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         1,
			CachedCreationTokens: 2,
			CacheWriteTokens:     3,
			TextTokens:           4,
			ImageTokens:          5,
			AudioTokens:          6,
		},
		OutputTokensDetails: &dto.OutputTokenDetails{
			ReasoningTokens: 7,
			TextTokens:      8,
			ImageTokens:     9,
			AudioTokens:     10,
		},
	}
}

func assertDetailedResponsesUsage(t *testing.T, usage *dto.Usage) {
	t.Helper()
	require.NotNil(t, usage)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 50, usage.CompletionTokens)
	require.Equal(t, 150, usage.TotalTokens)
	require.Equal(t, dto.InputTokenDetails{
		CachedTokens:         1,
		CachedCreationTokens: 2,
		CacheWriteTokens:     3,
		TextTokens:           4,
		ImageTokens:          5,
		AudioTokens:          6,
	}, usage.PromptTokensDetails)
	require.NotNil(t, usage.InputTokensDetails)
	require.Equal(t, usage.PromptTokensDetails, *usage.InputTokensDetails)
	require.Equal(t, dto.OutputTokenDetails{
		ReasoningTokens: 7,
		TextTokens:      8,
		ImageTokens:     9,
		AudioTokens:     10,
	}, usage.CompletionTokenDetails)
}

func newResponsesUsageTestContext() (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	return c, info
}

func TestOaiResponsesHandlerPreservesDetailedUsage(t *testing.T) {
	c, info := newResponsesUsageTestContext()
	payload := dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{{
			Type: "message",
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: "ok",
			}},
		}},
		Usage: detailedResponsesUsage(),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, maxAPIError := OaiResponsesHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, maxAPIError)
	assertDetailedResponsesUsage(t, usage)
}

func TestOaiResponsesStreamHandlerPreservesDetailedUsage(t *testing.T) {
	c, info := newResponsesUsageTestContext()
	completed := dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			Output: []dto.ResponsesOutput{{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{{
					Type: "output_text",
					Text: "ok",
				}},
			}},
			Usage: detailedResponsesUsage(),
		},
	}
	completedData, err := common.Marshal(completed)
	require.NoError(t, err)
	body := append([]byte("data: "), completedData...)
	body = append(body, []byte("\ndata: [DONE]\n")...)

	usage, maxAPIError := OaiResponsesStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, maxAPIError)
	assertDetailedResponsesUsage(t, usage)
}

func TestOaiResponsesCompactionHandlerPreservesDetailedUsage(t *testing.T) {
	c, info := newResponsesUsageTestContext()
	payload := dto.OpenAIResponsesCompactionResponse{
		Usage: detailedResponsesUsage(),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, maxAPIError := OaiResponsesCompactionHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, maxAPIError)
	assertDetailedResponsesUsage(t, usage)
}

func TestOaiResponsesHandlerBillsActualFunctionOutputNotToolDeclaration(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{
		BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			"function": {ToolName: "function"},
		},
	}
	payload := dto.OpenAIResponsesResponse{
		Tools: []map[string]any{{"type": "function", "name": "lookup"}},
		Output: []dto.ResponsesOutput{{
			Type:   "function_call",
			ID:     "fc-1",
			CallId: "call-1",
			Name:   "lookup",
			Status: "completed",
		}},
		Usage: detailedResponsesUsage(),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	_, maxAPIError := OaiResponsesHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})
	require.Nil(t, maxAPIError)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, info.ToolUsageSnapshot().Items)
	require.Zero(t, info.ResponsesUsageInfo.BuiltInTools["function"].CallCount)
}

func TestOaiResponsesHandlerDoesNotBillDeclarationWithoutOutputCall(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{
		BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			"function": {ToolName: "function"},
		},
	}
	payload := dto.OpenAIResponsesResponse{
		Tools: []map[string]any{{"type": "function", "name": "lookup"}},
		Output: []dto.ResponsesOutput{{
			Type: "message",
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: "no tool needed",
			}},
		}},
		Usage: detailedResponsesUsage(),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	_, maxAPIError := OaiResponsesHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})
	require.Nil(t, maxAPIError)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
	require.Zero(t, info.ResponsesUsageInfo.BuiltInTools["function"].CallCount)
}

func TestOaiResponsesHandlerRecordsCustomToolCallOutput(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"code_exec": 6})
	c, info := newOpenAIToolBillingContext("gpt-test")
	payload := dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{{
			Type:   dto.BuildInCallCustomToolCall,
			ID:     "ct-1",
			CallId: "call-1",
			Name:   "code_exec",
			Status: "completed",
		}},
		Usage: &dto.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	_, maxAPIError := OaiResponsesHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})
	require.Nil(t, maxAPIError)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{
		Name:       "code_exec",
		CallCount:  1,
		PricePer1K: 6,
	}}, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesStreamHandlerDeduplicatesFunctionItemDoneAndCompleted(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	itemDone := dto.ResponsesStreamResponse{
		Type:        dto.ResponsesOutputTypeItemDone,
		OutputIndex: common.GetPointer(0),
		Item: &dto.ResponsesOutput{
			Type:   "function_call",
			ID:     "fc-1",
			CallId: "call-1",
			Name:   "lookup",
			Status: "completed",
		},
	}
	completed := dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			Output: []dto.ResponsesOutput{{
				Type:   "function_call",
				ID:     "fc-1",
				CallId: "call-1",
				Name:   "lookup",
				Status: "completed",
			}},
			Usage: detailedResponsesUsage(),
		},
	}
	itemData, err := common.Marshal(itemDone)
	require.NoError(t, err)
	completedData, err := common.Marshal(completed)
	require.NoError(t, err)
	body := append([]byte("data: "), itemData...)
	body = append(body, []byte("\ndata: ")...)
	body = append(body, itemData...)
	body = append(body, []byte("\ndata: ")...)
	body = append(body, completedData...)
	body = append(body, []byte("\ndata: [DONE]\n")...)

	_, maxAPIError := OaiResponsesStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})
	require.Nil(t, maxAPIError)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesHandlerRejectsFailedTerminalStatusWithoutBillingTools(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	payload := dto.OpenAIResponsesResponse{
		Status: []byte(`"failed"`),
		Output: []dto.ResponsesOutput{{
			Type:   dto.BuildInCallFunctionCall,
			ID:     "fc-1",
			CallId: "call-1",
			Name:   "lookup",
			Status: "completed",
		}},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	_, maxAPIError := OaiResponsesHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})
	require.NotNil(t, maxAPIError)
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesStreamHandlerRejectsFailedTerminalAfterToolEvent(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc-1","call_id":"call-1","name":"lookup","status":"completed"}}`,
		`data: {"type":"response.failed","response":{"id":"resp-1","status":"failed","error":{"type":"server_error","code":"server_error","message":"upstream failed"}}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	_, maxAPIError := OaiResponsesStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.NotNil(t, maxAPIError)
	require.True(t, types.IsSkipRetryError(maxAPIError))
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesStreamHandlerRejectsStandardErrorAfterToolEvent(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc-1","call_id":"call-1","name":"lookup","status":"completed"}}`,
		`data: {"type":"error","code":"server_error","message":"upstream failed","param":"tool"}`,
		`data: [DONE]`,
		"",
	}, "\n")

	_, maxAPIError := OaiResponsesStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.NotNil(t, maxAPIError)
	require.True(t, types.IsSkipRetryError(maxAPIError))
	openAIError := maxAPIError.ToOpenAIError()
	require.Equal(t, "upstream failed", openAIError.Message)
	require.Equal(t, "server_error", openAIError.Code)
	require.Equal(t, "tool", openAIError.Param)
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesStreamHandlerAllowsRetryWhenFirstEventIsError(t *testing.T) {
	tests := []struct {
		name  string
		event string
	}{
		{name: "standard error", event: `{"type":"error","code":"server_error","message":"upstream failed"}`},
		{name: "failed response", event: `{"type":"response.failed","response":{"id":"resp-1","status":"failed","error":{"message":"upstream failed"}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				RelayFormat:     types.RelayFormatOpenAIResponses,
				OriginModelName: "gpt-test",
				DisablePing:     true,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
			}
			body := strings.Join([]string{
				"data: " + tt.event,
				`data: [DONE]`,
				"",
			}, "\n")

			_, maxAPIError := OaiResponsesStreamHandler(c, info, &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			})
			require.NotNil(t, maxAPIError)
			require.False(t, types.IsSkipRetryError(maxAPIError))
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestOaiResponsesToChatStreamHandlerPreservesMessageOnlyTerminalError(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.SendResponseCount = 1

	body := strings.Join([]string{
		`data: {"type":"response.failed","response":{"id":"resp-1","status":"failed","error":{"message":"upstream failed"}}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	_, maxAPIError := OaiResponsesToChatStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.NotNil(t, maxAPIError)
	require.True(t, types.IsSkipRetryError(maxAPIError))
	require.Equal(t, "upstream failed", maxAPIError.Error())
}

func TestOaiResponsesToChatStreamHandlerRejectsStandardErrorEvent(t *testing.T) {
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.SendResponseCount = 1

	body := strings.Join([]string{
		`data: {"type":"error","code":"server_error","message":"upstream failed","param":"tool"}`,
		`data: [DONE]`,
		"",
	}, "\n")
	_, maxAPIError := OaiResponsesToChatStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.NotNil(t, maxAPIError)
	require.True(t, types.IsSkipRetryError(maxAPIError))
	openAIError := maxAPIError.ToOpenAIError()
	require.Equal(t, "upstream failed", openAIError.Message)
	require.Equal(t, "server_error", openAIError.Code)
	require.Equal(t, "tool", openAIError.Param)
}

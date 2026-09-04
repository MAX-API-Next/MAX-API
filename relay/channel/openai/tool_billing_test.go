package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setOpenAIToolPricesForTest(t *testing.T, additions map[string]float64) {
	t.Helper()

	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	original := make(map[string]float64, len(setting.Prices))
	for name, price := range setting.Prices {
		original[name] = price
	}
	setting.Prices = make(map[string]float64, len(original)+len(additions))
	for name, price := range original {
		setting.Prices[name] = price
	}
	for name, price := range additions {
		setting.Prices[name] = price
	}
	operation_setting.RebuildToolPriceIndex()

	t.Cleanup(func() {
		setting.Prices = original
		operation_setting.RebuildToolPriceIndex()
	})
}

func newOpenAIToolBillingContext(model string) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/chat/completions", nil)
	ledger := relaycommon.NewToolUsageLedger(model)
	ledger.BeginAttempt(0)
	return c, &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: model,
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: model,
		},
		ToolUsage: ledger,
	}
}

func TestOpenaiHandlerRecordsActualCustomToolCalls(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	payload := `{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"model":"gpt-test",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"","tool_calls":[
				{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}
			]},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}
	}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

	_, maxErr := OpenaiHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, info.ToolUsageSnapshot().Items)
}

func TestOpenaiHandlerDoesNotRecordIncompleteCustomToolCalls(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	payload := `{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"model":"gpt-test",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"","tool_calls":[
				{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}
			]},
			"finish_reason":"length"
		}],
		"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}
	}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

	_, maxErr := OpenaiHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOaiStreamHandlerDeduplicatesCustomToolCallChunks(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{
		"lookup":   5,
		"get_time": 7,
	})
	c, info := newOpenAIToolBillingContext("gpt-test")

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call-2","type":"function","function":{"name":"get_time","arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{
		{Name: "get_time", CallCount: 1, PricePer1K: 7},
		{Name: "lookup", CallCount: 1, PricePer1K: 5},
	}, info.ToolUsageSnapshot().Items)
}

func TestOaiStreamHandlerDoesNotRecordIncompleteCustomToolCall(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOaiStreamHandlerDoesNotRecordUnterminatedCustomToolCall(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOpenAIStreamChunkMayAffectToolBilling(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "text delta with null finish", data: `{"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`, want: false},
		{name: "tool fragment", data: `{"choices":[{"delta":{"tool_calls":[{"index":0}]},"finish_reason":null}]}`, want: true},
		{name: "terminal choice", data: `{"choices":[{"delta":{},"finish_reason" : "length"}]}`, want: true},
		{name: "second choice terminal", data: `{"choices":[{"delta":{},"finish_reason":null},{"delta":{},"finish_reason":"tool_calls"}]}`, want: true},
		{name: "all choices open", data: `{"choices":[{"delta":{},"finish_reason":null},{"delta":{},"finish_reason":null}]}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, openAIStreamChunkMayAffectToolBilling(tt.data))
		})
	}
}

func TestOaiResponsesToChatHandlerRecordsConvertedToolCallOnce(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	payload := `{
		"id":"resp-1","object":"response","status":"completed","model":"gpt-test",
		"output":[{"type":"function_call","id":"fc-1","call_id":"call-1","name":"lookup","arguments":"{}","status":"completed"}],
		"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
	}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

	_, maxErr := OaiResponsesToChatHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{Name: "lookup", CallCount: 1, PricePer1K: 5}}, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesToChatStreamHandlerDeduplicatesDoneAndCompleted(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")

	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc-1","call_id":"call-1","name":"lookup","arguments":"{}","status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"resp-1","status":"completed","model":"gpt-test","output":[{"type":"function_call","id":"fc-1","call_id":"call-1","name":"lookup","arguments":"{}","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{Name: "lookup", CallCount: 1, PricePer1K: 5}}, info.ToolUsageSnapshot().Items)
}

func TestOaiResponsesToChatStreamHandlerReplaysToolCallAfterText(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	recorder := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-test",
		DisablePing:     true,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		ToolUsage:       relaycommon.NewToolUsageLedger("gpt-test"),
	}
	info.ToolUsage.BeginAttempt(0)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp-1","status":"completed","model":"gpt-test","output":[{"type":"message","id":"msg-1","status":"completed","content":[{"type":"output_text","text":"hello"}]},{"type":"custom_tool_call","id":"ct-1","call_id":"call-1","name":"lookup","input":"payload","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	output := recorder.Body.String()
	require.Contains(t, output, `"arguments":"payload"`)
	require.Contains(t, output, `"name":"lookup"`)
	require.Contains(t, output, `"finish_reason":"tool_calls"`)
}

func TestOaiChatToResponsesHandlerRecordsConvertedToolCallOnce(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.RelayFormat = types.RelayFormatOpenAIResponses

	payload := `{
		"id":"chatcmpl-1","object":"chat.completion","model":"gpt-test",
		"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

	_, maxErr := OaiChatToResponsesHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{Name: "lookup", CallCount: 1, PricePer1K: 5}}, info.ToolUsageSnapshot().Items)
}

func TestOaiChatToResponsesHandlerDoesNotRecordIncompleteToolCall(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.RelayFormat = types.RelayFormatOpenAIResponses

	payload := `{
		"id":"chatcmpl-1","object":"chat.completion","model":"gpt-test",
		"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"length"}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

	_, maxErr := OaiChatToResponsesHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestOaiChatToResponsesStreamHandlerDeduplicatesToolFragments(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.RelayFormat = types.RelayFormatOpenAIResponses

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{Name: "lookup", CallCount: 1, PricePer1K: 5}}, info.ToolUsageSnapshot().Items)
}

func TestOaiChatToResponsesStreamHandlerDoesNotRecordIncompleteToolCall(t *testing.T) {
	setOpenAIToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newOpenAIToolBillingContext("gpt-test")
	info.RelayFormat = types.RelayFormatOpenAIResponses

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

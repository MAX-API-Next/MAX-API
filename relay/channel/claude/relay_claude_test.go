package claude

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func commonPointer[T any](value T) *T {
	return &value
}

func setClaudeToolPricesForTest(t *testing.T, additions map[string]float64) {
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

func TestFormatClaudeResponseInfo_MessageStart(t *testing.T) {
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id:    "msg_123",
			Model: "claude-3-5-sonnet",
			Usage: &dto.ClaudeUsage{
				InputTokens:              100,
				OutputTokens:             1,
				CacheCreationInputTokens: 50,
				CacheReadInputTokens:     30,
			},
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens != 50 {
		t.Errorf("CachedCreationTokens = %d, want 50", claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
	}
	if claudeInfo.ResponseId != "msg_123" {
		t.Errorf("ResponseId = %s, want msg_123", claudeInfo.ResponseId)
	}
	if claudeInfo.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %s, want claude-3-5-sonnet", claudeInfo.Model)
	}
}

func TestHandleClaudeResponseDataRecordsActualCustomToolUse(t *testing.T) {
	setClaudeToolPricesForTest(t, map[string]float64{"lookup": 5})
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ledger := relaycommon.NewToolUsageLedger("claude-test")
	ledger.BeginAttempt(0)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-test",
		ToolUsage:       ledger,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	response := dto.ClaudeResponse{
		Type:       "message",
		StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{{
			Type:  "tool_use",
			Id:    "toolu-1",
			Name:  "lookup",
			Input: map[string]any{},
		}},
		Usage: &dto.ClaudeUsage{InputTokens: 10, OutputTokens: 2},
	}
	data, err := common.Marshal(response)
	require.NoError(t, err)

	maxErr := HandleClaudeResponseData(c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}}, nil, data)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{Name: "lookup", CallCount: 1, PricePer1K: 5}}, info.ToolUsageSnapshot().Items)
}

func TestHandleStreamResponseDataBillsCustomToolUseOnlyAfterToolUseTerminal(t *testing.T) {
	setClaudeToolPricesForTest(t, map[string]float64{"lookup": 5})
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ledger := relaycommon.NewToolUsageLedger("claude-test")
	ledger.BeginAttempt(0)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-test",
		ToolUsage:       ledger,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	startData := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"lookup","input":{}}}`
	terminalData := `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`

	require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, startData))
	require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, startData))
	require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, terminalData))
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{Name: "lookup", CallCount: 1, PricePer1K: 5}}, info.ToolUsageSnapshot().Items)
}

func TestHandleStreamResponseDataDoesNotBillIncompleteCustomToolUse(t *testing.T) {
	setClaudeToolPricesForTest(t, map[string]float64{"lookup": 5})

	tests := []struct {
		name         string
		terminalData string
	}{
		{name: "max tokens", terminalData: `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":2}}`},
		{name: "refusal", terminalData: `{"type":"message_delta","delta":{"stop_reason":"refusal"},"usage":{"output_tokens":2}}`},
		{name: "missing terminal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ledger := relaycommon.NewToolUsageLedger("claude-test")
			ledger.BeginAttempt(0)
			info := &relaycommon.RelayInfo{
				RelayFormat:     types.RelayFormatClaude,
				OriginModelName: "claude-test",
				ToolUsage:       ledger,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
			}
			claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

			require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"lookup","input":{}}}`))
			if tt.terminalData != "" {
				require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, tt.terminalData))
			}
			HandleStreamFinalResponse(c, info, claudeInfo)
			require.True(t, info.CommitToolUsageAttempt())
			require.Empty(t, info.ToolUsageSnapshot().Items)
		})
	}
}

func TestHandleClaudeResponseDataDoesNotBillIncompleteCustomToolUse(t *testing.T) {
	setClaudeToolPricesForTest(t, map[string]float64{"lookup": 5})
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ledger := relaycommon.NewToolUsageLedger("claude-test")
	ledger.BeginAttempt(0)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-test",
		ToolUsage:       ledger,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	response := dto.ClaudeResponse{
		Type:       "message",
		StopReason: "max_tokens",
		Content: []dto.ClaudeMediaMessage{{
			Type:  "tool_use",
			Id:    "toolu-1",
			Name:  "lookup",
			Input: map[string]any{},
		}},
		Usage: &dto.ClaudeUsage{InputTokens: 10, OutputTokens: 2},
	}
	data, err := common.Marshal(response)
	require.NoError(t, err)

	maxErr := HandleClaudeResponseData(c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}}, nil, data)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Empty(t, info.ToolUsageSnapshot().Items)
}

func TestFormatClaudeResponseInfo_MessageDelta_FullUsage(t *testing.T) {
	// message_start 先积累 usage
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
			CompletionTokens: 1,
		},
	}

	// message_delta 带完整 usage（原生 Anthropic 场景）
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			InputTokens:              100,
			OutputTokens:             200,
			CacheCreationInputTokens: 50,
			CacheReadInputTokens:     30,
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", claudeInfo.Usage.CompletionTokens)
	}
	if claudeInfo.Usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", claudeInfo.Usage.TotalTokens)
	}
	if !claudeInfo.Done {
		t.Error("expected Done = true")
	}
}

func TestFormatClaudeResponseInfo_MessageDelta_OnlyOutputTokens(t *testing.T) {
	// 模拟 Bedrock: message_start 已积累 usage
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
			CompletionTokens:            1,
			ClaudeCacheCreation5mTokens: 10,
			ClaudeCacheCreation1hTokens: 20,
		},
	}

	// Bedrock 的 message_delta 只有 output_tokens，缺少 input_tokens 和 cache 字段
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			OutputTokens: 200,
			// InputTokens, CacheCreationInputTokens, CacheReadInputTokens 都是 0
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	// PromptTokens 应保持 message_start 的值（因为 message_delta 的 InputTokens=0，不更新）
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", claudeInfo.Usage.CompletionTokens)
	}
	if claudeInfo.Usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", claudeInfo.Usage.TotalTokens)
	}
	// cache 字段应保持 message_start 的值
	if claudeInfo.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens != 50 {
		t.Errorf("CachedCreationTokens = %d, want 50", claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
	}
	if claudeInfo.Usage.ClaudeCacheCreation5mTokens != 10 {
		t.Errorf("ClaudeCacheCreation5mTokens = %d, want 10", claudeInfo.Usage.ClaudeCacheCreation5mTokens)
	}
	if claudeInfo.Usage.ClaudeCacheCreation1hTokens != 20 {
		t.Errorf("ClaudeCacheCreation1hTokens = %d, want 20", claudeInfo.Usage.ClaudeCacheCreation1hTokens)
	}
	if !claudeInfo.Done {
		t.Error("expected Done = true")
	}
}

func TestHandleStreamFinalResponsePreservesOutputTokensInBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-test",
		},
	}
	claudeInfo := &ClaudeResponseInfo{
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	require.True(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Model: "claude-sonnet-test",
			Usage: &dto.ClaudeUsage{InputTokens: 100},
		},
	}, nil, claudeInfo))
	require.True(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type:  "message_delta",
		Usage: &dto.ClaudeUsage{OutputTokens: 53},
	}, nil, claudeInfo))

	HandleStreamFinalResponse(ctx, info, claudeInfo)

	require.Equal(t, 53, claudeInfo.Usage.CompletionTokens)
	require.NotNil(t, claudeInfo.Usage.BillingUsage)
	require.NotNil(t, claudeInfo.Usage.BillingUsage.ClaudeUsage)
	require.Equal(t, 53, claudeInfo.Usage.BillingUsage.ClaudeUsage.OutputTokens)
}

func TestFormatClaudeResponseInfo_NilClaudeInfo(t *testing.T) {
	claudeResponse := &dto.ClaudeResponse{Type: "message_start"}
	ok := FormatClaudeResponseInfo(claudeResponse, nil, nil)
	if ok {
		t.Error("expected false for nil claudeInfo")
	}
}

func TestFormatClaudeResponseInfo_ContentBlockDelta(t *testing.T) {
	text := "hello"
	claudeInfo := &ClaudeResponseInfo{
		Usage:        &dto.Usage{},
		ResponseText: strings.Builder{},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{
			Text: &text,
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.ResponseText.String() != "hello" {
		t.Errorf("ResponseText = %q, want %q", claudeInfo.ResponseText.String(), "hello")
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
		UsageSemantic:               "anthropic",
	}

	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

	if openAIUsage.PromptTokens != 180 {
		t.Fatalf("PromptTokens = %d, want 180", openAIUsage.PromptTokens)
	}
	if openAIUsage.InputTokens != 180 {
		t.Fatalf("InputTokens = %d, want 180", openAIUsage.InputTokens)
	}
	if openAIUsage.TotalTokens != 200 {
		t.Fatalf("TotalTokens = %d, want 200", openAIUsage.TotalTokens)
	}
	if openAIUsage.UsageSemantic != "openai" {
		t.Fatalf("UsageSemantic = %s, want openai", openAIUsage.UsageSemantic)
	}
	if openAIUsage.UsageSource != "anthropic" {
		t.Fatalf("UsageSource = %s, want anthropic", openAIUsage.UsageSource)
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsagePreservesCacheCreationRemainder(t *testing.T) {
	tests := []struct {
		name                    string
		cachedCreationTokens    int
		cacheCreationTokens5m   int
		cacheCreationTokens1h   int
		expectedTotalInputToken int
	}{
		{
			name:                    "prefers aggregate when it includes remainder",
			cachedCreationTokens:    50,
			cacheCreationTokens5m:   10,
			cacheCreationTokens1h:   20,
			expectedTotalInputToken: 180,
		},
		{
			name:                    "falls back to split tokens when aggregate missing",
			cachedCreationTokens:    0,
			cacheCreationTokens5m:   10,
			cacheCreationTokens1h:   20,
			expectedTotalInputToken: 160,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &dto.Usage{
				PromptTokens:     100,
				CompletionTokens: 20,
				PromptTokensDetails: dto.InputTokenDetails{
					CachedTokens:         30,
					CachedCreationTokens: tt.cachedCreationTokens,
				},
				ClaudeCacheCreation5mTokens: tt.cacheCreationTokens5m,
				ClaudeCacheCreation1hTokens: tt.cacheCreationTokens1h,
				UsageSemantic:               "anthropic",
			}

			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

			if openAIUsage.PromptTokens != tt.expectedTotalInputToken {
				t.Fatalf("PromptTokens = %d, want %d", openAIUsage.PromptTokens, tt.expectedTotalInputToken)
			}
			if openAIUsage.InputTokens != tt.expectedTotalInputToken {
				t.Fatalf("InputTokens = %d, want %d", openAIUsage.InputTokens, tt.expectedTotalInputToken)
			}
		})
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsageDefaultsAggregateCacheCreationTo5m(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 50,
		},
		UsageSemantic: "anthropic",
	}

	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

	require.Equal(t, 50, openAIUsage.ClaudeCacheCreation5mTokens)
	require.Equal(t, 0, openAIUsage.ClaudeCacheCreation1hTokens)
}

func TestRequestOpenAI2ClaudeMessage_IgnoresUnsupportedFileContent(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					dto.MediaContent{
						Type: dto.ContentTypeText,
						Text: "see attachment",
					},
					dto.MediaContent{
						Type: dto.ContentTypeFile,
						File: &dto.MessageFile{
							FileName: "blob.bin",
							FileData: "JVBERi0xLjQK",
						},
					},
				},
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.NotEmpty(t, claudeRequest.Messages)

	claudeMessage := claudeRequest.Messages[len(claudeRequest.Messages)-1]
	require.Equal(t, "user", claudeMessage.Role)
	content, ok := claudeMessage.Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 1)
	require.Equal(t, "text", content[0].Type)
	require.NotNil(t, content[0].Text)
	require.Equal(t, "see attachment", *content[0].Text)
}

func TestRequestOpenAI2ClaudeMessage_PreservesToolUseWithEmptyArguments(t *testing.T) {
	rawToolCalls, err := common.Marshal([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: "",
			},
		},
	})
	require.NoError(t, err)

	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{
				Role:      "assistant",
				ToolCalls: rawToolCalls,
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.NotEmpty(t, claudeRequest.Messages)

	assistantMessage := claudeRequest.Messages[len(claudeRequest.Messages)-1]
	require.Equal(t, "assistant", assistantMessage.Role)
	content, ok := assistantMessage.Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	var toolUse *dto.ClaudeMediaMessage
	for i := range content {
		if content[i].Type == "tool_use" {
			toolUse = &content[i]
			break
		}
	}
	require.NotNil(t, toolUse)
	require.Equal(t, "call_1", toolUse.Id)
	require.Equal(t, "lookup", toolUse.Name)
	require.Empty(t, toolUse.Input)
}

func TestRequestOpenAI2ClaudeMessage_ReturnsErrorForMalformedToolArguments(t *testing.T) {
	rawToolCalls, err := common.Marshal([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: "{",
			},
		},
	})
	require.NoError(t, err)

	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{
				Role:      "assistant",
				ToolCalls: rawToolCalls,
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.Error(t, err)
	require.Nil(t, claudeRequest)
	require.Contains(t, err.Error(), "tool call function arguments is not valid JSON object")
}

func TestRequestOpenAI2ClaudeMessageNormalizesParameterlessToolsAndOmitsEmptyTools(t *testing.T) {
	parameterless, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "ping"}}},
	})
	require.NoError(t, err)
	parameterlessTools := parameterless.GetTools()
	require.Len(t, parameterlessTools, 1)
	tool, ok := parameterlessTools[0].(*dto.Tool)
	require.True(t, ok)
	require.Equal(t, "object", tool.InputSchema["type"])
	require.Equal(t, map[string]any{}, tool.InputSchema["properties"])

	customSchema := map[string]any{"type": 123, "properties": map[string]any{"value": map[string]any{"type": "string"}}, "additionalProperties": false}
	converted, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "custom", Parameters: customSchema}}},
	})
	require.NoError(t, err)
	convertedTools := converted.GetTools()
	require.Len(t, convertedTools, 1)
	customTool := convertedTools[0].(*dto.Tool)
	require.Equal(t, 123, customTool.InputSchema["type"])
	require.Equal(t, false, customTool.InputSchema["additionalProperties"])

	unsupported, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Tools: []dto.ToolCallRequest{{
			Type: "custom",
			Function: dto.FunctionRequest{
				Name:       "must-not-be-converted",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	})
	require.NoError(t, err)
	require.Empty(t, unsupported.GetTools())

	empty, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{Model: "claude-3-5-sonnet"})
	require.NoError(t, err)
	payload, err := common.Marshal(empty)
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"tools"`)
}

func TestRequestOpenAI2ClaudeMessageOmitsForcedToolChoiceWhenAllToolsAreUnsupported(t *testing.T) {
	converted, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Tools: []dto.ToolCallRequest{{
			Type: "custom",
			Function: dto.FunctionRequest{
				Name:       "unsupported-tool",
				Parameters: map[string]any{"type": "object"},
			},
		}},
		ToolChoice: "required",
	})
	require.NoError(t, err)
	require.Empty(t, converted.GetTools())
	require.Nil(t, converted.ToolChoice)

	payload, err := common.Marshal(converted)
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"tools"`)
	require.NotContains(t, string(payload), `"tool_choice"`)
}

func TestRequestOpenAI2ClaudeMessage_ClaudeOpus48HighUsesAdaptiveThinking(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-8-high",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}

func TestRequestOpenAI2ClaudeMessage_ClaudeOpus48ThinkingUsesAdaptiveHighEffort(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-8-thinking",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}

func TestRequestOpenAI2ClaudeMessage_SupportsPDFFileContent(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					dto.MediaContent{
						Type: dto.ContentTypeFile,
						File: &dto.MessageFile{
							FileName: "spec.pdf",
							FileData: "JVBERi0xLjQK",
						},
					},
					dto.MediaContent{
						Type: dto.ContentTypeText,
						Text: "summarize it",
					},
				},
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.Len(t, claudeRequest.Messages, 1)

	content, ok := claudeRequest.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 2)
	require.Equal(t, "document", content[0].Type)
	require.NotNil(t, content[0].Source)
	require.Equal(t, "base64", content[0].Source.Type)
	require.Equal(t, "application/pdf", content[0].Source.MediaType)
	require.Equal(t, "JVBERi0xLjQK", content[0].Source.Data)
	require.Equal(t, "text", content[1].Type)
	require.NotNil(t, content[1].Text)
	require.Equal(t, "summarize it", *content[1].Text)
}

func TestRequestOpenAI2ClaudeMessage_ConvertsTextFileContentToText(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					dto.MediaContent{
						Type: dto.ContentTypeFile,
						File: &dto.MessageFile{
							FileName: "notes.txt",
							FileData: base64.StdEncoding.EncodeToString([]byte("alpha\nbeta")),
						},
					},
				},
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.Len(t, claudeRequest.Messages, 1)

	content, ok := claudeRequest.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 1)
	require.Equal(t, "text", content[0].Type)
	require.NotNil(t, content[0].Text)
	require.Equal(t, "alpha\nbeta", *content[0].Text)
}

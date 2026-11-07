package service

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeStreamFinishUsesCachedUsage(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "wait_for_usage", true: "cached_usage"}[cached], func(t *testing.T) {
			info := &relaycommon.RelayInfo{ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{}}
			var start dto.ChatCompletionsStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(`{"choices":[{"delta":{"content":"hello"}}]}`, &start))
			StreamResponseOpenAI2Claude(&start, info)
			if cached {
				info.ClaudeConvertInfo.Usage = &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
			}
			var finish dto.ChatCompletionsStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`, &finish))
			got := StreamResponseOpenAI2Claude(&finish, info)
			if !cached {
				require.Empty(t, got)
				require.False(t, info.ClaudeConvertInfo.Done)
				finish.Choices = nil
				finish.Usage = &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
				got = StreamResponseOpenAI2Claude(&finish, info)
			}
			require.Len(t, got, 3)
			require.Equal(t, "content_block_stop", got[0].Type)
			require.Equal(t, "message_delta", got[1].Type)
			require.Equal(t, 100, got[1].Usage.InputTokens)
			require.Equal(t, 20, got[1].Usage.OutputTokens)
			require.Equal(t, "message_stop", got[2].Type)
			require.True(t, info.ClaudeConvertInfo.Done)
			require.Empty(t, StreamResponseOpenAI2Claude(&finish, info))
		})
	}
}

func TestClaudeStreamRetainsFinishDeltaBeforeLateUsage(t *testing.T) {
	for _, tc := range []struct{ name, delta, kind, field, value string }{
		{"text", `{"content":"x"}`, "text_delta", "text", "x"},
		{"thinking", `{"reasoning_content":"x"}`, "thinking_delta", "thinking", "x"},
		{"tool", `{"tool_calls":[{"index":0,"id":"call_finish","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}`, "input_json_delta", "partial_json", `{"q":"x"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{}}
			var start, finish dto.ChatCompletionsStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(`{"choices":[{"delta":{"content":"hello"}}]}`, &start))
			StreamResponseOpenAI2Claude(&start, info)
			require.NoError(t, common.UnmarshalJsonStr(`{"choices":[{"delta":`+tc.delta+`,"finish_reason":"stop"}]}`, &finish))
			pending := StreamResponseOpenAI2Claude(&finish, info)
			var delivered []string
			for _, event := range pending {
				require.NotEqual(t, "message_delta", event.Type)
				require.NotEqual(t, "message_stop", event.Type)
				if event.Delta != nil && event.Delta.Type == tc.kind {
					wire, err := common.Marshal(event.Delta)
					require.NoError(t, err)
					var fields map[string]string
					require.NoError(t, common.Unmarshal(wire, &fields))
					delivered = append(delivered, fields[tc.field])
				}
			}
			require.Equal(t, []string{tc.value}, delivered)
			require.False(t, info.ClaudeConvertInfo.Done)
			terminal := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
				Usage: &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
			}, info)
			require.Len(t, terminal, 3)
			require.Equal(t, "content_block_stop", terminal[0].Type)
			require.Equal(t, "message_delta", terminal[1].Type)
			require.Equal(t, 100, terminal[1].Usage.InputTokens)
			require.Equal(t, 20, terminal[1].Usage.OutputTokens)
			require.Equal(t, "message_stop", terminal[2].Type)
			require.True(t, info.ClaudeConvertInfo.Done)
			require.Empty(t, StreamResponseOpenAI2Claude(&finish, info))
		})
	}
}

func TestClaudeFirstChunkFinishPresence(t *testing.T) {
	for _, reported := range []bool{false, true} {
		t.Run(map[bool]string{false: "delayed_usage", true: "reported_usage"}[reported], func(t *testing.T) {
			info := &relaycommon.RelayInfo{SendResponseCount: 1, ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{}}
			var first dto.ChatCompletionsStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(`{"choices":[{"delta":{"content":"only"},"finish_reason":"stop"}]}`, &first))
			if reported {
				first.Usage = &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
			}
			events := StreamResponseOpenAI2Claude(&first, info)
			if !reported {
				require.False(t, info.ClaudeConvertInfo.Done)
				for _, event := range events {
					require.NotEqual(t, "message_stop", event.Type)
				}
				info.SendResponseCount++ // the real stream caller increments per conversion
				events = append(events, StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
					Usage: &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
				}, info)...)
			}
			counts := map[string]int{}
			for _, event := range events {
				counts[event.Type]++
				if event.Type == "message_delta" {
					require.Equal(t, 100, event.Usage.InputTokens)
					require.Equal(t, 20, event.Usage.OutputTokens)
				}
			}
			require.Equal(t, 1, counts["message_start"])
			require.Equal(t, 1, counts["message_delta"])
			require.Equal(t, 1, counts["message_stop"])
			require.True(t, info.ClaudeConvertInfo.Done)
		})
	}
}

func TestResponseOpenAI2ClaudePreservesTextWithToolCalls(t *testing.T) {
	resp := openAIResponseWithTextAndToolCall(t)
	got := ResponseOpenAI2Claude(resp, &relaycommon.RelayInfo{})

	require.NotNil(t, got)
	require.Len(t, got.Content, 2)
	assert.Equal(t, "text", got.Content[0].Type)
	require.NotNil(t, got.Content[0].Text)
	assert.Equal(t, "I will look that up.", *got.Content[0].Text)
	assert.Equal(t, "tool_use", got.Content[1].Type)
	assert.Equal(t, "call_1", got.Content[1].Id)
	assert.Equal(t, "lookup", got.Content[1].Name)
	assert.Equal(t, map[string]interface{}{"q": "max-api"}, got.Content[1].Input)
}

func TestResponseOpenAI2ClaudeWrapsMalformedToolArguments(t *testing.T) {
	resp := openAIResponseWithToolCallArguments(t, "{")
	got := ResponseOpenAI2Claude(resp, &relaycommon.RelayInfo{})

	require.NotNil(t, got)
	require.Len(t, got.Content, 2)
	assert.Equal(t, "tool_use", got.Content[1].Type)
	assert.Equal(t, map[string]interface{}{"arguments": "{"}, got.Content[1].Input)
}

func TestResponseOpenAI2ClaudeTreatsNullToolArgumentsAsEmptyObject(t *testing.T) {
	resp := openAIResponseWithToolCallArguments(t, "null")
	got := ResponseOpenAI2Claude(resp, &relaycommon.RelayInfo{})

	require.NotNil(t, got)
	require.Len(t, got.Content, 2)
	assert.Equal(t, "tool_use", got.Content[1].Type)
	assert.Equal(t, map[string]interface{}{}, got.Content[1].Input)
}

func TestResponseOpenAI2GeminiPreservesTextWithToolCalls(t *testing.T) {
	resp := openAIResponseWithTextAndToolCall(t)
	got := ResponseOpenAI2Gemini(resp, &relaycommon.RelayInfo{})

	require.NotNil(t, got)
	require.Len(t, got.Candidates, 1)
	parts := got.Candidates[0].Content.Parts
	require.Len(t, parts, 2)
	assert.Equal(t, "I will look that up.", parts[0].Text)
	require.NotNil(t, parts[1].FunctionCall)
	assert.Equal(t, "lookup", parts[1].FunctionCall.FunctionName)
	assert.Equal(t, map[string]interface{}{"q": "max-api"}, parts[1].FunctionCall.Arguments)
}

func TestResponseOpenAI2GeminiWrapsMalformedToolArguments(t *testing.T) {
	resp := openAIResponseWithToolCallArguments(t, "{")
	resp.Choices[0].Message.Content = ""
	got := ResponseOpenAI2Gemini(resp, &relaycommon.RelayInfo{})

	require.NotNil(t, got)
	require.Len(t, got.Candidates, 1)
	parts := got.Candidates[0].Content.Parts
	require.Len(t, parts, 1)
	require.NotNil(t, parts[0].FunctionCall)
	assert.Equal(t, map[string]interface{}{"arguments": "{"}, parts[0].FunctionCall.Arguments)
}

func TestResponseOpenAI2GeminiTreatsNullToolArgumentsAsEmptyObject(t *testing.T) {
	resp := openAIResponseWithToolCallArguments(t, "null")
	resp.Choices[0].Message.Content = ""
	got := ResponseOpenAI2Gemini(resp, &relaycommon.RelayInfo{})

	require.NotNil(t, got)
	require.Len(t, got.Candidates, 1)
	parts := got.Candidates[0].Content.Parts
	require.Len(t, parts, 1)
	require.NotNil(t, parts[0].FunctionCall)
	assert.Equal(t, map[string]interface{}{}, parts[0].FunctionCall.Arguments)
}

func openAIResponseWithTextAndToolCall(t *testing.T) *dto.OpenAITextResponse {
	return openAIResponseWithToolCallArguments(t, `{"q":"max-api"}`)
}

func openAIResponseWithToolCallArguments(t *testing.T, arguments string) *dto.OpenAITextResponse {
	t.Helper()
	rawToolCalls, err := common.Marshal([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: arguments,
			},
		},
	})
	require.NoError(t, err)

	return &dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index: 0,
				Message: dto.Message{
					Role:      "assistant",
					Content:   "I will look that up.",
					ToolCalls: rawToolCalls,
				},
				FinishReason: constant.FinishReasonToolCalls,
			},
		},
	}
}

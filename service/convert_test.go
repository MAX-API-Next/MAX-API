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

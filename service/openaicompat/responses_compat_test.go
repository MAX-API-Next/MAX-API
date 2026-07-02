package openaicompat

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesRequestToChatCompletionsRequestInstructionsScalarInputAndZeroTemperature(t *testing.T) {
	stream := true
	temperature := 0.0
	maxOutputTokens := uint(128)
	parallelToolCalls := false

	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:             "gpt-test",
		Instructions:      mustCompatRawMessage(t, "system rules"),
		Input:             mustCompatRawMessage(t, "hello"),
		Stream:            &stream,
		MaxOutputTokens:   &maxOutputTokens,
		Temperature:       &temperature,
		ParallelToolCalls: mustCompatRawMessage(t, parallelToolCalls),
		Reasoning:         &dto.Reasoning{Effort: "medium"},
	})
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "system", got.Messages[0].Role)
	assert.Equal(t, "system rules", got.Messages[0].StringContent())
	assert.Equal(t, "user", got.Messages[1].Role)
	assert.Equal(t, "hello", got.Messages[1].StringContent())
	assert.Same(t, &stream, got.Stream)
	assert.Equal(t, maxOutputTokens, lo.FromPtr(got.MaxCompletionTokens))
	require.NotNil(t, got.Temperature)
	assert.Equal(t, 0.0, *got.Temperature)
	require.NotNil(t, got.ParallelTooCalls)
	assert.False(t, *got.ParallelTooCalls)
	assert.Equal(t, "medium", got.ReasoningEffort)
}

func TestResponsesRequestToChatCompletionsRequestFunctionCallConversation(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustCompatRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 2)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "I will call.", got.Messages[0].StringContent())
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "lookup", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "tool", got.Messages[1].Role)
	assert.Equal(t, "call_1", got.Messages[1].ToolCallId)
	assert.JSONEq(t, `{"ok":true}`, got.Messages[1].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestToolsToolChoiceAndTextFormat(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustCompatRawMessage(t, "hello"),
		Tools: mustCompatRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
		}),
		ToolChoice: mustCompatRawMessage(t, map[string]any{
			"type": "function",
			"name": "lookup",
		}),
		Text: mustCompatRawMessage(t, map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
				"strict": true,
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Tools, 1)
	assert.Equal(t, "function", got.Tools[0].Type)
	assert.Equal(t, "lookup", got.Tools[0].Function.Name)
	assert.Equal(t, "Lookup data", got.Tools[0].Function.Description)
	assert.Equal(t, "object", got.Tools[0].Function.Parameters.(map[string]any)["type"])
	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "lookup",
		},
	}, got.ToolChoice)
	require.NotNil(t, got.ResponseFormat)
	assert.Equal(t, "json_schema", got.ResponseFormat.Type)
	assert.False(t, gjson.GetBytes(got.ResponseFormat.JsonSchema, "type").Exists())
	assert.Equal(t, "answer", gjson.GetBytes(got.ResponseFormat.JsonSchema, "name").String())
	assert.True(t, gjson.GetBytes(got.ResponseFormat.JsonSchema, "strict").Bool())
	assert.Equal(t, "object", gjson.GetBytes(got.ResponseFormat.JsonSchema, "schema.type").String())
}

func TestResponsesRequestToChatCompletionsRequestSkipsNonMessageOutputItems(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustCompatRawMessage(t, []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "answer"},
				},
			},
			{
				"type": "reasoning",
				"content": []map[string]any{
					{"type": "summary_text", "text": "thinking"},
				},
			},
			{"type": "web_search_call", "id": "ws_1", "status": "completed"},
			{"type": "file_search_call", "id": "fs_1", "status": "completed"},
			{"type": "computer_call", "id": "cc_1", "status": "completed"},
			{"type": "item_reference", "id": "item_1"},
			{"type": "unknown", "content": "do not replay"},
			{
				"role":    "user",
				"content": "continue",
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 2)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "answer", got.Messages[0].StringContent())
	assert.Equal(t, "user", got.Messages[1].Role)
	assert.Equal(t, "continue", got.Messages[1].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestRejectsCustomToolCallItems(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustCompatRawMessage(t, []map[string]any{
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom",
				"name":    "apply_patch",
				"input":   "patch body",
			},
		}),
	})

	require.Error(t, err)
	require.Nil(t, got)
	assert.Contains(t, err.Error(), "custom_tool_call")
}

func TestResponsesRequestToChatCompletionsRequestRejectsMissingToolCallIDs(t *testing.T) {
	cases := []struct {
		name string
		item map[string]any
		want string
	}{
		{
			name: "function call",
			item: map[string]any{
				"type":      "function_call",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			want: "function_call item is missing call_id",
		},
		{
			name: "function call output",
			item: map[string]any{
				"type":   "function_call_output",
				"output": map[string]any{"ok": true},
			},
			want: "function_call_output item is missing call_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
				Model: "gpt-test",
				Input: mustCompatRawMessage(t, []map[string]any{
					tc.item,
				}),
			})

			require.Error(t, err)
			require.Nil(t, got)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedToolTypes(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustCompatRawMessage(t, "hello"),
		Tools: mustCompatRawMessage(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
		}),
	})

	require.Error(t, err)
	require.Nil(t, got)
	assert.Contains(t, err.Error(), `tool type "custom"`)
}

func TestChatCompletionsResponseToResponsesResponsePreservesToolCallsAndIncompleteReason(t *testing.T) {
	chat := &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 456,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      compatAssistantMessageWithTool("partial", "call_1", "lookup", `{"q":"x"}`),
				FinishReason: "length",
			},
		},
		Usage: dto.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
	}

	resp, usage, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1")
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, "resp_1", resp.ID)
	assert.Equal(t, `"incomplete"`, string(resp.Status))
	require.NotNil(t, resp.IncompleteDetails)
	assert.Equal(t, responsesIncompleteReasonMaxTokens, resp.IncompleteDetails.Reason)
	assert.Equal(t, 3, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)
	require.Len(t, resp.Output, 2)
	assert.Equal(t, responsesOutputTypeMessage, resp.Output[0].Type)
	assert.Equal(t, "partial", resp.Output[0].Content[0].Text)
	assert.Equal(t, responsesOutputTypeFunctionCall, resp.Output[1].Type)
	assert.Equal(t, "call_1", resp.Output[1].CallId)
	assert.Equal(t, "lookup", resp.Output[1].Name)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(resp.Output[1].Arguments))
}

func TestResponsesResponseToChatCompletionsResponsePreservesUsageDetails(t *testing.T) {
	resp, usage, err := ResponsesResponseToChatCompletionsResponse(&dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		Model:     "gpt-test",
		CreatedAt: 456,
		Status:    []byte(`"completed"`),
		Usage: &dto.Usage{
			InputTokens:  7,
			OutputTokens: 11,
			TotalTokens:  18,
			InputTokensDetails: &dto.InputTokenDetails{
				CachedTokens:         1,
				CachedCreationTokens: 2,
				TextTokens:           3,
				AudioTokens:          4,
				ImageTokens:          5,
			},
			CompletionTokenDetails: dto.OutputTokenDetails{
				ReasoningTokens: 6,
				TextTokens:      7,
				AudioTokens:     8,
				ImageTokens:     9,
			},
		},
		Output: []dto.ResponsesOutput{{
			Type: responsesOutputTypeMessage,
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: "done",
			}},
		}},
	}, "chatcmpl_1")
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, 7, usage.PromptTokens)
	assert.Equal(t, 11, usage.CompletionTokens)
	assert.Equal(t, 18, usage.TotalTokens)
	assert.Equal(t, 1, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 2, usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 4, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 5, usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 6, usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 7, usage.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 8, usage.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 9, usage.CompletionTokenDetails.ImageTokens)
	assert.Equal(t, usage.PromptTokensDetails, resp.Usage.PromptTokensDetails)
	assert.Equal(t, usage.CompletionTokenDetails, resp.Usage.CompletionTokenDetails)
}

func TestChatCompletionsStreamToResponsesEventsAggregatesUsage(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_1", "gpt-test")
	state.Created = 123
	toolIndex := 0
	finishReason := "tool_calls"

	var events []ChatToResponsesStreamEvent
	events = append(events, mustCompatResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 123,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
		},
	})...)
	events = append(events, mustCompatResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: lo.ToPtr("hello")}},
		},
	})...)
	events = append(events, mustCompatResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, ID: "call_1", Type: "function", Function: dto.FunctionResponse{Name: "lookup"}},
			}}},
		},
	})...)
	events = append(events, mustCompatResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, Function: dto.FunctionResponse{Arguments: `{"q":"x"}`}},
			}}},
		},
	})...)
	events = append(events, mustCompatResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, mustCompatResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 4, TotalTokens: 6},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	require.Len(t, events, 10)
	assert.Equal(t, responsesEventCreated, events[0].Type)
	assert.Equal(t, responsesEventOutputTextDelta, events[2].Type)
	assert.Equal(t, "hello", events[2].Payload.Delta)
	assert.Equal(t, responsesEventFunctionArgsDelta, events[4].Type)
	assert.Equal(t, `{"q":"x"}`, events[4].Payload.Delta)
	assert.Equal(t, responsesEventCompleted, events[9].Type)
	require.NotNil(t, events[9].Payload.Response)
	assert.Equal(t, 6, events[9].Payload.Response.Usage.TotalTokens)
	require.Len(t, events[9].Payload.Response.Output, 2)
	assert.Equal(t, "hello", events[9].Payload.Response.Output[0].Content[0].Text)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(events[9].Payload.Response.Output[1].Arguments))
}

func compatAssistantMessageWithTool(content string, id string, name string, args string) dto.Message {
	msg := dto.Message{Role: "assistant", Content: content}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   id,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      name,
				Arguments: args,
			},
		},
	})
	return msg
}

func mustCompatResponsesEventsFromChatChunk(t *testing.T, state *ChatToResponsesStreamState, chunk *dto.ChatCompletionsStreamResponse) []ChatToResponsesStreamEvent {
	t.Helper()
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
	require.NoError(t, err)
	return events
}

func mustCompatRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

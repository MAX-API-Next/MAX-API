package openaicompat

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesCustomToolPreservesRawInputAndOrdinaryFunctions(t *testing.T) {
	for _, input := range []string{"", "patch body", "引号 \" 和反斜线 \\ 及换行\nsecond line"} {
		t.Run(input, func(t *testing.T) {
			args, err := common.Marshal(map[string]string{"input": input})
			require.NoError(t, err)
			tools := []dto.ToolCallResponse{
				{ID: "call_custom", Type: "function", Function: dto.FunctionResponse{Name: "apply_patch", Arguments: string(args)}},
				{ID: "call_function", Type: "function", Function: dto.FunctionResponse{Name: "lookup", Arguments: `{"input":"function argument"}`}},
			}
			message := dto.Message{Role: "assistant"}
			message.SetToolCalls(tools)
			response, _, err := ChatCompletionsResponseToResponsesResponseWithCustomTools(&dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}}}, "resp_test", map[string]struct{}{"apply_patch": {}})
			require.NoError(t, err)
			require.Equal(t, input, response.Output[0].Input)
			require.Equal(t, "custom_tool_call", response.Output[0].Type)
			require.Equal(t, "call_custom", response.Output[0].CallId)
			require.Equal(t, "function_call", response.Output[1].Type)
			require.Equal(t, `{"input":"function argument"}`, response.Output[1].ArgumentsString())
			payload, err := common.Marshal(response)
			require.NoError(t, err)
			require.True(t, gjson.GetBytes(payload, "output.0.input").Exists())
			require.False(t, gjson.GetBytes(payload, "output.1.input").Exists())
			state := NewChatToResponsesStreamState("resp_test", "test")
			state.CustomToolNames = map[string]struct{}{"apply_patch": {}}
			var events []ChatToResponsesStreamEvent
			for index, tool := range tools {
				tool.SetIndex(index)
				chunkEvents, err := ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{tool}}}}}, state)
				require.NoError(t, err)
				events = append(events, chunkEvents...)
			}
			events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)
			customDone := 0
			functionDone := 0
			for _, event := range events {
				if event.Type == "response.custom_tool_call_input.done" {
					customDone++
					require.NotNil(t, event.Payload.Input)
					require.Equal(t, input, *event.Payload.Input)
					require.Equal(t, "call_custom", event.Payload.ItemID)
				}
				if event.Type == "response.function_call_arguments.done" {
					functionDone++
					require.Equal(t, "call_function", event.Payload.ItemID)
				}
			}
			require.Equal(t, 1, customDone)
			require.Equal(t, 1, functionDone)
		})
	}
}

func TestResponsesCustomToolWhitespaceNameMatchesStream(t *testing.T) {
	for _, name := range []string{" apply_patch", "apply_patch\t\n", " \tapply_patch \n"} {
		t.Run(name, func(t *testing.T) {
			tool := dto.ToolCallResponse{ID: "call_custom", Type: "function", Function: dto.FunctionResponse{Name: name, Arguments: `{"input":"raw patch"}`}}
			message := dto.Message{Role: "assistant"}
			message.SetToolCalls([]dto.ToolCallResponse{tool})
			names := map[string]struct{}{"apply_patch": {}}
			response, _, err := ChatCompletionsResponseToResponsesResponseWithCustomTools(&dto.OpenAITextResponse{
				Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}},
			}, "resp_custom", names)
			require.NoError(t, err)
			require.Len(t, response.Output, 1)
			require.Equal(t, "custom_tool_call", response.Output[0].Type)
			require.Equal(t, "apply_patch", response.Output[0].Name)
			_, registered := names[response.Output[0].Name]
			require.True(t, registered, "clients must resolve the returned custom name against the tool registry")
			require.Equal(t, "raw patch", response.Output[0].Input)
			require.Nil(t, response.Output[0].Arguments)
			require.Equal(t, "call_custom", response.Output[0].CallId)

			state := NewChatToResponsesStreamState("resp_custom", "test")
			state.CustomToolNames = names
			tool.SetIndex(0)
			_, err = ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{tool}}}},
			}, state)
			require.NoError(t, err)
			events := FinalizeChatCompletionsStreamToResponses(state)
			require.NotEmpty(t, events)
			stream := events[len(events)-1].Payload.Response
			require.NotNil(t, stream)
			require.Equal(t, response.Output[0].Type, stream.Output[0].Type)
			require.Equal(t, "apply_patch", stream.Output[0].Name)
			require.Equal(t, response.Output[0].Name, stream.Output[0].Name)
			require.Equal(t, response.Output[0].Input, stream.Output[0].Input)

			tool.Function.Name = " lookup "
			message.SetToolCalls([]dto.ToolCallResponse{tool})
			ordinary, _, err := ChatCompletionsResponseToResponsesResponseWithCustomTools(&dto.OpenAITextResponse{
				Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}},
			}, "resp_function", names)
			require.NoError(t, err)
			require.Equal(t, "function_call", ordinary.Output[0].Type)
			require.Equal(t, " lookup ", ordinary.Output[0].Name, "custom restoration must preserve ordinary function behavior")
		})
	}
}

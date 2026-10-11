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

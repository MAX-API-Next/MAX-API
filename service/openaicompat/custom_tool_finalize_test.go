package openaicompat

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/require"
)

func TestFlushChatCompletionsStreamToResponsesOutputDefersTerminalUsage(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		for _, zero := range []bool{false, true} {
			name := map[bool]string{false: "completed", true: "incomplete"}[incomplete] + "/" + map[bool]string{false: "estimated", true: "zero"}[zero]
			t.Run(name, func(t *testing.T) {
				state := NewChatToResponsesStreamState("resp_flush", "test")
				state.CustomToolNames = map[string]struct{}{"apply_patch": {}}
				_, err := ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{
					Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{{ID: "call_flush", Function: dto.FunctionResponse{Name: "apply_patch", Arguments: `{"input":"patch body"}`}}},
					}}},
				}, state)
				require.NoError(t, err)
				if incomplete {
					state.MarkIncomplete("upstream_eof")
				}
				events := FlushChatCompletionsStreamToResponsesOutput(state)
				require.Len(t, events, 3)
				require.Equal(t, "response.custom_tool_call_input.delta", events[0].Type)
				require.Equal(t, "patch body", events[0].Payload.Delta)
				require.Empty(t, FlushChatCompletionsStreamToResponsesOutput(state), "buffered output must drain once")
				usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13}
				if zero {
					usage = &dto.Usage{}
				}
				state.Usage = UsageFromChatUsage(usage)
				terminal := FinalizeChatCompletionsStreamToResponses(state)
				require.Len(t, terminal, 1, "terminal construction must not duplicate flushed input")
				require.Equal(t, "response."+map[bool]string{false: "completed", true: "incomplete"}[incomplete], terminal[0].Type)
				require.Equal(t, usage.CompletionTokens, terminal[0].Payload.Response.Usage.OutputTokens)
				require.Empty(t, FinalizeChatCompletionsStreamToResponses(state))
				require.Empty(t, FlushChatCompletionsStreamToResponsesOutput(state))
			})
		}
	}
}

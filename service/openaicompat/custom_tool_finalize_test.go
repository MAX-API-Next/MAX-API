package openaicompat

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
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

func TestChatToResponsesUnnamedToolCallCannotComplete(t *testing.T) {
	for _, finish := range []string{"", "tool_calls", "length"} {
		for _, flushFirst := range []bool{false, true} {
			for _, lateName := range []bool{false, true} {
				for _, mixedOutput := range []bool{false, true} {
					t.Run(fmt.Sprintf("finish_%q/flush_%t/late_name_%t/mixed_%t", finish, flushFirst, lateName, mixedOutput), func(t *testing.T) {
						state := NewChatToResponsesStreamState("resp_name", "test")
						state.CustomToolNames = map[string]struct{}{"named_custom": {}}
						delta := dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
							{Index: common.GetPointer(0), ID: "call_unnamed", Function: dto.FunctionResponse{Arguments: `{"q":"buffered"}`}},
						}}
						if mixedOutput {
							delta.SetContentString("kept text")
							delta.ToolCalls = append(delta.ToolCalls, dto.ToolCallResponse{Index: common.GetPointer(1), ID: "call_named", Function: dto.FunctionResponse{Name: "named_custom", Arguments: `{"input":"kept input"}`}})
						}
						_, err := ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{
							Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: delta}},
						}, state)
						require.NoError(t, err)
						last := dto.ChatCompletionsStreamResponseChoice{}
						if lateName {
							last.Delta.ToolCalls = []dto.ToolCallResponse{{Index: common.GetPointer(0), Function: dto.FunctionResponse{Name: "lookup"}}}
						}
						if finish != "" {
							last.FinishReason = common.GetPointer(finish)
						}
						_, err = ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{last}}, state)
						require.NoError(t, err)
						if finish != "" {
							_, err = ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{
								Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: common.GetPointer(finish)}},
							}, state)
							require.NoError(t, err, "repeated finish cannot hide an unnamed call")
						}
						if flushFirst {
							FlushChatCompletionsStreamToResponsesOutput(state)
						}
						state.Usage = UsageFromChatUsage(&dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12})
						terminal := FinalizeChatCompletionsStreamToResponses(state)
						require.NotEmpty(t, terminal)
						final := terminal[len(terminal)-1]
						expectedStatus := "completed"
						if !lateName || finish == "length" {
							expectedStatus = "incomplete"
							reason := "missing_tool_name"
							if finish == "length" {
								reason = responsesIncompleteReasonMaxTokens
							}
							require.NotNil(t, final.Payload.Response.IncompleteDetails)
							require.Equal(t, reason, final.Payload.Response.IncompleteDetails.Reason)
						}
						require.Equal(t, "response."+expectedStatus, final.Type)
						payload, err := common.Marshal(final.Payload)
						require.NoError(t, err)
						var wire struct {
							Response struct {
								Status string
								Output []struct {
									Type, Name, Status, Input, Arguments string
									CallID                               string `json:"call_id"`
								}
							}
						}
						require.NoError(t, common.Unmarshal(payload, &wire))
						require.Equal(t, expectedStatus, wire.Response.Status)
						expectedCount := 0
						if mixedOutput {
							expectedCount = 2
						}
						if lateName {
							expectedCount++
						}
						require.Len(t, wire.Response.Output, expectedCount, "valid output remains in the terminal")
						for _, item := range wire.Response.Output {
							require.Equal(t, expectedStatus, item.Status)
							if item.Type == "function_call" {
								require.Equal(t, "lookup", item.Name)
								require.Equal(t, "call_unnamed", item.CallID)
								require.JSONEq(t, `{"q":"buffered"}`, item.Arguments)
							} else if item.Type == "custom_tool_call" {
								require.Equal(t, "named_custom", item.Name)
								require.Equal(t, "call_named", item.CallID)
								require.Equal(t, "kept input", item.Input)
							}
						}
						require.Equal(t, 10, final.Payload.Response.Usage.InputTokens)
						require.Equal(t, 2, final.Payload.Response.Usage.OutputTokens)
						require.Empty(t, FlushChatCompletionsStreamToResponsesOutput(state))
						require.Empty(t, FinalizeChatCompletionsStreamToResponses(state), "terminal output is emitted once")
					})
				}
			}
		}
	}
}

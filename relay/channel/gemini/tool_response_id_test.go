package gemini

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/require"
)

func TestGeminiToolResponseIDsSurviveOutboundJSON(t *testing.T) {
	for _, route := range []string{"chat", "responses_function", "responses_custom"} {
		for _, stream := range []bool{false, true} {
			for _, prefix := range []string{"call", "调用\"\\\n"} {
				t.Run(fmt.Sprintf("%s/stream_%t/prefix_%q", route, stream, prefix), func(t *testing.T) {
					ids := []string{prefix + "_object", prefix + "_array", prefix + "_text"}
					outputs := []string{`{"value":0}`, `["result",1]`, "plain result"}
					order := []int{2, 0, 1}
					info := &relaycommon.RelayInfo{IsStream: stream, OriginModelName: "gemini-test",
						ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
					var converted any
					var err error
					if route == "chat" {
						calls := make([]dto.ToolCallRequest, 0, len(ids))
						for i, id := range ids {
							calls = append(calls, dto.ToolCallRequest{ID: id, Type: "function",
								Function: dto.FunctionRequest{Name: "lookup", Arguments: fmt.Sprintf(`{"index":%d}`, i)}})
						}
						assistant := dto.Message{Role: "assistant"}
						assistant.SetToolCalls(calls)
						messages := []dto.Message{{Role: "user", Content: "run tools"}, assistant}
						for _, i := range order {
							messages = append(messages, dto.Message{Role: "tool", ToolCallId: ids[i], Content: outputs[i]})
						}
						converted, err = (&Adaptor{}).ConvertOpenAIRequest(nil, info, &dto.GeneralOpenAIRequest{
							Model: "gemini-test", Stream: common.GetPointer(stream), Messages: messages,
						})
					} else {
						input := []map[string]any{{"role": "user", "content": "run tools"}}
						custom := route == "responses_custom"
						for i, id := range ids {
							call := map[string]any{"type": "function_call", "call_id": id, "name": "lookup",
								"arguments": fmt.Sprintf(`{"index":%d}`, i)}
							if custom {
								call = map[string]any{"type": "custom_tool_call", "call_id": id, "name": "lookup", "input": fmt.Sprintf("input %d", i)}
							}
							input = append(input, call)
						}
						for _, i := range order {
							outputType := "function_call_output"
							if custom {
								outputType = "custom_tool_call_output"
							}
							input = append(input, map[string]any{"type": outputType, "call_id": ids[i], "output": outputs[i]})
						}
						tool := map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}
						if custom {
							tool = map[string]any{"type": "custom", "name": "lookup"}
						}
						converted, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
							Model: "gemini-test", Stream: common.GetPointer(stream),
							Input: mustGeminiRawMessage(t, input), Tools: mustGeminiRawMessage(t, []map[string]any{tool}),
						})
					}
					require.NoError(t, err)
					wire, err := common.Marshal(converted)
					require.NoError(t, err)
					var request struct {
						Contents []struct {
							Role  string
							Parts []struct {
								FunctionCall     *struct{ ID, Name string }
								FunctionResponse *struct {
									ID, Name string
									Response map[string]any
								}
							}
						}
					}
					require.NoError(t, common.Unmarshal(wire, &request))
					callIDs, resultIDs := []string{}, []string{}
					results := []map[string]any{}
					for _, content := range request.Contents {
						for _, part := range content.Parts {
							if part.FunctionCall != nil {
								require.Equal(t, "model", content.Role)
								require.Equal(t, "lookup", part.FunctionCall.Name)
								callIDs = append(callIDs, part.FunctionCall.ID)
							}
							if part.FunctionResponse != nil {
								require.Equal(t, "user", content.Role)
								require.Equal(t, "lookup", part.FunctionResponse.Name)
								resultIDs = append(resultIDs, part.FunctionResponse.ID)
								results = append(results, part.FunctionResponse.Response)
							}
						}
					}
					require.Equal(t, ids, callIDs)
					require.Equal(t, []string{ids[2], ids[0], ids[1]}, resultIDs, "results must keep their own call ID in completion order")
					require.Equal(t, []map[string]any{{"content": "plain result"}, {"value": float64(0)}, {"result": []any{"result", float64(1)}}}, results)
				})
			}
		}
	}
}

func TestGeminiLegacyFunctionResponseIDPresence(t *testing.T) {
	for _, role := range []string{"tool", "function"} {
		for _, id := range []string{"", "0"} {
			t.Run(role+"/id_"+id, func(t *testing.T) {
				info := &relaycommon.RelayInfo{OriginModelName: "gemini-test", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
				converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, &dto.GeneralOpenAIRequest{
					Model: "gemini-test", Messages: []dto.Message{
						{Role: "user", Content: "legacy conversation"},
						{Role: role, Name: common.GetPointer("lookup"), ToolCallId: id, Content: "legacy result"},
					},
				})
				require.NoError(t, err)
				wire, err := common.Marshal(converted)
				require.NoError(t, err)
				var request dto.GeminiChatRequest
				require.NoError(t, common.Unmarshal(wire, &request))
				result := request.Contents[0].Parts[1].FunctionResponse
				require.NotNil(t, result)
				require.Equal(t, "lookup", result.Name)
				require.Equal(t, "legacy result", result.Response["content"])
				if id == "" {
					// omitempty must omit ID rather than emitting null or an invented value.
					var fields map[string]any
					payload, err := common.Marshal(result)
					require.NoError(t, err)
					require.NoError(t, common.Unmarshal(payload, &fields))
					require.NotContains(t, fields, "id")
				} else {
					require.JSONEq(t, `"0"`, string(result.ID))
				}
			})
		}
	}
}

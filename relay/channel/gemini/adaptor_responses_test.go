package gemini

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestToGeminiInstructionsAndInput(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model:        "gemini-test",
		Instructions: mustGeminiRawMessage(t, "system rules"),
		Input:        mustGeminiRawMessage(t, "hello"),
	})

	require.NotNil(t, got.SystemInstructions)
	require.Len(t, got.SystemInstructions.Parts, 1)
	assert.Equal(t, "system rules", got.SystemInstructions.Parts[0].Text)
	require.Len(t, got.Contents, 1)
	assert.Equal(t, "user", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 1)
	assert.Equal(t, "hello", got.Contents[0].Parts[0].Text)
}

func TestConvertOpenAIResponsesRequestToGeminiPreservesCustomToolCalls(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "before custom"},
				},
			},
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom",
				"name":    "apply_patch",
				"input":   "patch body",
			},
			{
				"type":    "custom_tool_call_output",
				"call_id": "call_custom",
				"output":  "ok",
			},
			{
				"type":    "function_call_output",
				"call_id": "call_custom",
				"output":  "legacy custom output",
			},
			{
				"role":    "user",
				"content": "next turn",
			},
		}),
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type": "object",
				},
			},
			{
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Apply a patch",
			},
		}),
	})

	require.Len(t, got.GetTools(), 1)
	require.Len(t, got.Contents, 3)
	functions, err := common.Marshal(got.GetTools()[0].FunctionDeclarations)
	require.NoError(t, err)
	assert.Contains(t, string(functions), "apply_patch")
	assert.Equal(t, "model", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 2)
	var functionCall *dto.FunctionCall
	var textPart string
	for _, part := range got.Contents[0].Parts {
		if part.FunctionCall != nil {
			functionCall = part.FunctionCall
		}
		if part.Text != "" {
			textPart = part.Text
		}
	}
	require.NotNil(t, functionCall)
	assert.Equal(t, "before custom", textPart)
	assert.Equal(t, "call_custom", functionCall.ID)
	assert.Equal(t, "apply_patch", functionCall.FunctionName)
	arguments, ok := functionCall.Arguments.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "patch body", arguments["input"])

	assert.Equal(t, "user", got.Contents[1].Role)
	require.Len(t, got.Contents[1].Parts, 2)
	assert.Equal(t, "ok", got.Contents[1].Parts[0].FunctionResponse.Response["content"])
	assert.Equal(t, "legacy custom output", got.Contents[1].Parts[1].FunctionResponse.Response["content"])
	assert.Equal(t, "user", got.Contents[2].Role)
	assert.Equal(t, "next turn", got.Contents[2].Parts[0].Text)
}

func TestConvertOpenAIResponsesRequestToGeminiRejectsUnsupportedTools(t *testing.T) {
	got, err := convertResponsesToGemini(dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, "hello"),
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{"type": "web_search_preview"},
		}),
	})

	require.Error(t, err)
	require.Nil(t, got)
	assert.Contains(t, err.Error(), `tool type "web_search_preview"`)
}

func mustConvertResponsesToGemini(t *testing.T, req dto.OpenAIResponsesRequest) *dto.GeminiChatRequest {
	t.Helper()
	got, err := convertResponsesToGemini(req)
	require.NoError(t, err)
	return got
}

func convertResponsesToGemini(req dto.OpenAIResponsesRequest) (*dto.GeminiChatRequest, error) {
	info := &relaycommon.RelayInfo{
		OriginModelName: req.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: req.Model,
		},
	}
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	if err != nil {
		return nil, err
	}
	geminiReq, ok := got.(*dto.GeminiChatRequest)
	if !ok {
		return nil, fmt.Errorf("unexpected converted request type %T", got)
	}
	return geminiReq, nil
}

func mustGeminiRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

package dto

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestUsageCorrectionChatToolHistory(t *testing.T) {
	var request GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"HISTORY_ARGUMENT\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"HISTORY_RESULT"}],"tools":[{"type":"function","function":{"name":"read_file","description":"TOOL_DESCRIPTION","parameters":{"type":"object"}}}]}`, &request))
	meta := request.GetTokenCountMeta()
	for _, marker := range []string{"HISTORY_ARGUMENT", "HISTORY_RESULT", "TOOL_DESCRIPTION"} {
		require.Contains(t, meta.CombineText, marker)
	}
	require.Equal(t, 1, meta.ToolsCount, "history calls are not new tool declarations")
}

func TestUsageCorrectionResponsesCustomMultimodalHistory(t *testing.T) {
	var request OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"input":[{"type":"custom_tool_call","name":"computer","input":"CUSTOM_ARGUMENT"},{"type":"custom_tool_call_output","output":[{"type":"input_text","text":"CUSTOM_RESULT"},{"type":"input_image","image_url":"data:image/png;base64,YQ=="}]}]}`, &request))
	meta := request.GetTokenCountMeta()
	require.Contains(t, meta.CombineText, "CUSTOM_ARGUMENT")
	require.Contains(t, meta.CombineText, "CUSTOM_RESULT")
	require.Len(t, meta.Files, 1)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
}

func TestResponsesToolOutputImageDetail(t *testing.T) {
	for _, kind := range []string{"function_call_output", "custom_tool_call_output"} {
		var request OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"input":[{"type":"`+kind+`","output":[{"type":"input_image","image_url":"https://example.invalid/image.png","detail":"low"}]}]}`, &request))
		meta := request.GetTokenCountMeta()
		require.Len(t, meta.Files, 1)
		require.Equal(t, "low", meta.Files[0].Detail)
	}
}

func TestUsageCorrectionGeminiMultimodalHistoryIsStable(t *testing.T) {
	var request GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"systemInstruction":{"parts":[{"text":"SYSTEM"}]},"contents":[{"role":"user","parts":[{"text":"QUESTION"},{"functionResponse":{"name":"computer","response":{"z":"LAST","a":"FIRST"},"parts":[{"inlineData":{"mimeType":"image/png","data":"YQ=="}}]}}]}]}`, &request))
	before, err := common.Marshal(request)
	require.NoError(t, err)
	first, second := request.GetTokenCountMeta(), request.GetTokenCountMeta()
	require.Equal(t, first.CombineText, second.CombineText)
	require.Contains(t, first.CombineText, "a\nFIRST\nz\nLAST")
	require.Len(t, first.Files, 1)
	require.Equal(t, types.FileTypeImage, first.Files[0].FileType)
	after, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "estimation must not mutate the upstream request")
}

func TestUsageCorrectionResponsesToolHistory(t *testing.T) {
	var request OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"instructions":"SYSTEM_INSTRUCTION","input":[{"type":"function_call","name":"read_file","arguments":"{\"path\":\"HISTORY_ARGUMENT\"}"},{"type":"function_call_output","output":"HISTORY_RESULT"}]}`, &request))
	meta := request.GetTokenCountMeta()
	for _, marker := range []string{"SYSTEM_INSTRUCTION", "HISTORY_ARGUMENT", "HISTORY_RESULT"} {
		require.Contains(t, meta.CombineText, marker)
	}
}

func TestUsageCorrectionClaudeDecodedTools(t *testing.T) {
	var request ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"tools":[{"name":"read_file","description":"TOOL_DESCRIPTION","input_schema":{"type":"object","properties":{"path":{"description":"SCHEMA_DETAIL","type":"string"}}}}]}`, &request))
	meta := request.GetTokenCountMeta()
	require.Contains(t, meta.CombineText, "TOOL_DESCRIPTION")
	require.Contains(t, meta.CombineText, "SCHEMA_DETAIL")
	require.Equal(t, 1, meta.ToolsCount)
}

func TestUsageCorrectionGeminiAgentInput(t *testing.T) {
	var request GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"systemInstruction":{"parts":[{"text":"SYSTEM_INSTRUCTION"}]},"contents":[{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"HISTORY_ARGUMENT"}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read_file","response":{"result":"HISTORY_RESULT"}}}]}],"tools":[{"functionDeclarations":[{"name":"read_file","description":"TOOL_DESCRIPTION","parameters":{"type":"object"}}]}]}`, &request))
	meta := request.GetTokenCountMeta()
	for _, marker := range []string{"SYSTEM_INSTRUCTION", "HISTORY_ARGUMENT", "HISTORY_RESULT", "TOOL_DESCRIPTION"} {
		require.Contains(t, meta.CombineText, marker)
	}
}

func TestGeminiFunctionResponseKeyContributesToTokenMetadata(t *testing.T) {
	var request GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"HISTORY_RESULT_FOUND":true}}}]}]}`, &request))
	meta := request.GetTokenCountMeta()
	require.Contains(t, meta.CombineText, "HISTORY_RESULT_FOUND")
	require.Contains(t, meta.CombineText, "true")
}

func TestGeminiFileDataTokenMetadata(t *testing.T) {
	for _, tool := range []bool{false, true} {
		for _, tc := range []struct {
			mime string
			kind types.FileType
		}{
			{"image/png", types.FileTypeImage}, {"audio/wav", types.FileTypeAudio},
			{"video/mp4", types.FileTypeVideo}, {"application/pdf", types.FileTypeFile},
		} {
			part := `{"fileData":{"mimeType":"` + tc.mime + `","fileUri":"https://example.invalid/media"}}`
			if tool {
				part = `{"functionResponse":{"name":"lookup","response":{},"parts":[` + part + `]}}`
			}
			var request GeminiChatRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[`+part+`]}]}`, &request))
			before, err := common.Marshal(request)
			require.NoError(t, err)
			meta := request.GetTokenCountMeta()
			require.Len(t, meta.Files, 1)
			require.Equal(t, tc.kind, meta.Files[0].FileType)
			require.True(t, meta.Files[0].Source.IsURL())
			require.Equal(t, "https://example.invalid/media", meta.Files[0].Source.GetRawData())
			after, err := common.Marshal(request)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		}
	}
	for _, uri := range []string{"", "gs://bucket/media", "file:///media"} {
		request := GeminiChatRequest{Contents: []GeminiChatContent{{Parts: []GeminiPart{{FileData: &GeminiFileData{MimeType: "image/png", FileUri: uri}}}}}}
		require.Empty(t, request.GetTokenCountMeta().Files)
	}
}

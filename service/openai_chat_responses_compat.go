package service

import (
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service/openaicompat"
	"strings"
)

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return openaicompat.ChatCompletionsRequestToResponsesRequest(req)
}

func ResponsesCustomToolNames(info *relaycommon.RelayInfo) map[string]struct{} {
	if info == nil {
		return nil
	}
	req, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok || req == nil {
		return nil
	}
	names := make(map[string]struct{})
	for _, tool := range req.GetToolsMap() {
		if tool["type"] != "custom" {
			continue
		}
		name, _ := tool["name"].(string)
		if name = strings.TrimSpace(name); name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

func ChatCompletionsResponseToResponsesResponseWithCustomTools(resp *dto.OpenAITextResponse, id string, customToolNames map[string]struct{}) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return openaicompat.ChatCompletionsResponseToResponsesResponseWithCustomTools(resp, id, customToolNames)
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequest(req)
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return openaicompat.ChatCompletionsResponseToResponsesResponse(resp, id)
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	return openaicompat.ResponsesResponseToChatCompletionsResponse(resp, id)
}

func ResponsesFinishReasonFromStatus(resp *dto.OpenAIResponsesResponse) (string, bool) {
	return openaicompat.ResponsesFinishReasonFromStatus(resp)
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractOutputTextFromResponses(resp)
}

func ExtractReasoningTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractReasoningTextFromResponses(resp)
}

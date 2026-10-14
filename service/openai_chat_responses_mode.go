package service

import (
	"github.com/MAX-API-Next/MAX-API/service/openaicompat"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
)

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	return openaicompat.ShouldChatCompletionsUseResponsesPolicy(policy, channelID, channelType, model)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return openaicompat.ShouldChatCompletionsUseResponsesGlobal(channelID, channelType, model)
}

func SupportsOpenAIResponsesUpstream(channelType int) bool {
	return openaicompat.SupportsOpenAIResponsesUpstream(channelType)
}

func SupportsOpenAIResponsesUpstreamForBaseURL(channelType int, baseURL string) bool {
	return openaicompat.SupportsOpenAIResponsesUpstreamForBaseURL(channelType, baseURL)
}

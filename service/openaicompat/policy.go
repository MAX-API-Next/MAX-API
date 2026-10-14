package openaicompat

import (
	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
)

// The global bridge parses upstream OpenAI Responses, so a native Messages or
// GenerateContent conversion cannot use it. Keep this aligned with actual
// adaptor request/URL capabilities, not merely channel selection settings.
func SupportsOpenAIResponsesUpstream(channelType int) bool {
	apiType, _ := common.ChannelType2APIType(channelType)
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
		constant.APITypeAli, constant.APITypeZhipuV4, constant.APITypePerplexity,
		constant.APITypeCloudflare, constant.APITypeDeepSeek, constant.APITypeVolcEngine,
		constant.APITypeXai, constant.APITypeCodex, constant.APITypeAdvancedCustom:
		return true
	default:
		return false
	}
}

// Endpoint aliases can narrow a Responses-capable adaptor's support. Use the
// same alias registry as its URL resolver, while retaining standard endpoints.
func SupportsOpenAIResponsesUpstreamForBaseURL(channelType int, baseURL string) bool {
	if !SupportsOpenAIResponsesUpstream(channelType) {
		return false
	}
	apiType, _ := common.ChannelType2APIType(channelType)
	if apiType == constant.APITypeZhipuV4 || apiType == constant.APITypeVolcEngine {
		_, codingPlan := constant.ChannelSpecialBases[baseURL]
		return !codingPlan
	}
	return true
}

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	if !SupportsOpenAIResponsesUpstream(channelType) || !policy.IsChannelEnabled(channelID, channelType) {
		return false
	}
	return matchAnyRegex(policy.ModelPatterns, model)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return ShouldChatCompletionsUseResponsesPolicy(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		channelID,
		channelType,
		model,
	)
}

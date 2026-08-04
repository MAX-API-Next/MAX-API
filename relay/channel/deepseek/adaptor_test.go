package deepseek

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLSupportsResponses(t *testing.T) {
	adaptor := &Adaptor{}
	url, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.deepseek.com",
		},
		RelayMode: constant.RelayModeResponses,
	})

	require.NoError(t, err)
	require.Equal(t, "https://api.deepseek.com/responses", url)
}

func TestConvertOpenAIResponsesRequestAppliesDeepSeekV4ThinkingSuffix(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-pro-none",
	})

	require.NoError(t, err)
	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "deepseek-v4-pro", request.Model)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "none", request.Reasoning.Effort)
	require.Equal(t, "none", info.ReasoningEffort)
}

func TestConvertOpenAIResponsesRequestKeepsPlainModel(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model:     "deepseek-chat",
		Reasoning: &dto.Reasoning{Effort: "low"},
	})

	require.NoError(t, err)
	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "deepseek-chat", request.Model)
	require.Equal(t, "low", request.Reasoning.Effort)
	require.Equal(t, "low", info.ReasoningEffort)
}

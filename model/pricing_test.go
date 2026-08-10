package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/require"
)

func TestGetPricingEndpointTypesForAdvancedCustomAbilityUsesConfiguredRoutes(t *testing.T) {
	config := &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/alpha/search",
				UpstreamPath: "/v1/alpha/search",
				Converter:    dto.AdvancedCustomConverterNone,
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/responses",
				Converter:    dto.AdvancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, config.Validate())

	ability := AbilityWithChannel{
		Ability:     Ability{ChannelId: 17, Model: "gpt-advanced-alpha"},
		ChannelType: constant.ChannelTypeAdvancedCustom,
	}
	configs := map[int]*dto.AdvancedCustomConfig{
		17: config,
	}

	endpointTypes := getPricingEndpointTypesForAbility(ability, configs)

	require.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAIAlphaSearch,
		constant.EndpointTypeOpenAIResponse,
	}, endpointTypes)
}

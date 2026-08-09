package common

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/stretchr/testify/require"
)

func TestCodexEndpointTypesIncludeAlphaSearch(t *testing.T) {
	endpointTypes := GetEndpointTypesByChannelType(constant.ChannelTypeCodex, "gpt-5.1")

	require.Contains(t, endpointTypes, constant.EndpointTypeOpenAIResponse)
	require.Contains(t, endpointTypes, constant.EndpointTypeOpenAIResponseCompact)
	require.Contains(t, endpointTypes, constant.EndpointTypeOpenAIAlphaSearch)
}

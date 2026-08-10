package codex

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: "https://chatgpt.com",
		},
		RelayMode: relayconstant.RelayModeAlphaSearch,
	}

	requestURL, err := adaptor.GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", requestURL)
}

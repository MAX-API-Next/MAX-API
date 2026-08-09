package vertex

import (
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/require"
)

func TestGetAccessTokenRejectsUnexpectedCachedValueWithoutPanicking(t *testing.T) {
	const channelID = 987654321
	cacheKey := "access-token-987654321"
	Cache.DeleteIf(func(key string) bool { return key == cacheKey })
	require.False(t, Cache.SetDefault(cacheKey, 123))
	t.Cleanup(func() { Cache.DeleteIf(func(key string) bool { return key == cacheKey }) })

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID}}
	var err error
	require.NotPanics(t, func() {
		_, err = getAccessToken(adaptor, info)
	})
	require.ErrorContains(t, err, "failed to create signed JWT")
}

func TestCreateSignedJWTRejectsMalformedPEM(t *testing.T) {
	_, err := createSignedJWT("service@example.com", "not-a-private-key")
	require.ErrorContains(t, err, "failed to parse PEM block")
}

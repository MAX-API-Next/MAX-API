package zhipu

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetZhipuTokenIgnoresUnexpectedCachedValue(t *testing.T) {
	const apiKey = "test-id.test-secret"
	zhipuTokens.Store(apiKey, 123)
	t.Cleanup(func() { zhipuTokens.Delete(apiKey) })

	var token string
	require.NotPanics(t, func() {
		var err error
		token, err = getZhipuToken(apiKey)
		require.NoError(t, err)
	})
	require.NotEmpty(t, token)
}

func TestGetZhipuTokenRejectsMalformedAPIKey(t *testing.T) {
	_, err := getZhipuToken("not-a-zhipu-key")
	require.EqualError(t, err, "invalid zhipu API key format")
}

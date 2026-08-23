package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRateLimitEnvironmentDefaults(t *testing.T) {
	envKeys := []string{
		"GLOBAL_API_RATE_LIMIT_ENABLE",
		"GLOBAL_API_RATE_LIMIT",
		"GLOBAL_API_RATE_LIMIT_DURATION",
		"GLOBAL_WEB_RATE_LIMIT_ENABLE",
		"GLOBAL_WEB_RATE_LIMIT",
		"GLOBAL_WEB_RATE_LIMIT_DURATION",
		"CRITICAL_RATE_LIMIT_ENABLE",
		"CRITICAL_RATE_LIMIT",
		"CRITICAL_RATE_LIMIT_DURATION",
		"CRITICAL_ROUTE_RATE_LIMIT",
		"CRITICAL_ROUTE_RATE_LIMIT_DURATION",
		"LOGIN_RATE_LIMIT_ENABLE",
		"LOGIN_RATE_LIMIT",
		"LOGIN_RATE_LIMIT_DURATION",
		"SEARCH_RATE_LIMIT_ENABLE",
		"SEARCH_RATE_LIMIT",
		"SEARCH_RATE_LIMIT_DURATION",
	}
	for _, key := range envKeys {
		t.Setenv(key, "")
	}

	initRateLimitEnv()

	require.True(t, GlobalApiRateLimitEnable)
	require.Equal(t, 720, GlobalApiRateLimitNum)
	require.EqualValues(t, 180, GlobalApiRateLimitDuration)
	require.True(t, GlobalWebRateLimitEnable)
	require.Equal(t, 600, GlobalWebRateLimitNum)
	require.EqualValues(t, 180, GlobalWebRateLimitDuration)
	require.True(t, CriticalRateLimitEnable)
	require.Equal(t, 200, CriticalRateLimitNum)
	require.EqualValues(t, 20*60, CriticalRateLimitDuration)
	require.Equal(t, 200, CriticalRouteRateLimitNum)
	require.EqualValues(t, 20*60, CriticalRouteRateLimitDuration)
	require.True(t, LoginRateLimitEnable)
	require.Equal(t, 100, LoginRateLimitNum)
	require.EqualValues(t, 15*60, LoginRateLimitDuration)
	require.True(t, SearchRateLimitEnable)
	require.Equal(t, 100, SearchRateLimitNum)
	require.EqualValues(t, 60, SearchRateLimitDuration)
}

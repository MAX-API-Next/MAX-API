package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenRoutingPolicyRoundTrip(t *testing.T) {
	token := &Token{}
	policy := TokenRoutingPolicy{
		Version:        TokenRoutingPolicyVersion,
		Mode:           TokenRoutingModeManual,
		Groups:         []string{"vip", "default"},
		RetryOnFailure: true,
	}

	require.NoError(t, token.SetRoutingPolicy(&policy))
	stored, err := token.GetStoredRoutingPolicy()
	require.NoError(t, err)
	require.Equal(t, policy, *stored)

	stored.Groups[0] = "changed"
	require.Equal(t, "vip", policy.Groups[0])
}

func TestTokenRoutingPolicyIgnoresRemovedStrategyFields(t *testing.T) {
	raw := `{"version":1,"mode":"smart","route":"auto","strategy":"speed","allow_request_override":true,"retry_on_failure":true}`
	token := &Token{RoutingPolicyJSON: &raw}

	stored, err := token.GetStoredRoutingPolicy()
	require.NoError(t, err)
	require.Equal(t, TokenRoutingPolicy{
		Version:        TokenRoutingPolicyVersion,
		Mode:           TokenRoutingModeSmart,
		Route:          "auto",
		RetryOnFailure: true,
	}, *stored)
}

func TestTokenRoutingPolicyNilPreservesLegacyStorage(t *testing.T) {
	token := &Token{RoutingPolicyJSON: nil}
	stored, err := token.GetStoredRoutingPolicy()
	require.NoError(t, err)
	require.Nil(t, stored)

	require.NoError(t, token.SetRoutingPolicy(nil))
	require.Nil(t, token.RoutingPolicyJSON)
}

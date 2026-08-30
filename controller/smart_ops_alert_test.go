package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingSettlementMutationRequestsPreserveExplicitFalse(t *testing.T) {
	var policy billingSettlementBlockingPolicyRequest
	require.NoError(t, common.Unmarshal([]byte(`{"block_user_by_default":false}`), &policy))
	require.NotNil(t, policy.BlockUserByDefault)
	assert.False(t, *policy.BlockUserByDefault)

	var review billingSettlementReviewRequest
	require.NoError(t, common.Unmarshal([]byte(`{"block_user":false,"note":"verified"}`), &review))
	require.NotNil(t, review.BlockUser)
	assert.False(t, *review.BlockUser)
}

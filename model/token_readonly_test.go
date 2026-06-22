package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func seedReadOnlyToken(t *testing.T, name string, rawKey string) *Token {
	t.Helper()

	token := &Token{
		UserId:         1,
		Name:           name,
		Key:            rawKey,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: true,
		Group:          "default",
	}
	require.NoError(t, DB.Create(token).Error)
	return token
}

func TestValidateUserTokenForReadOnlyStatusSemantics(t *testing.T) {
	truncateTables(t)

	exhausted := seedReadOnlyToken(t, "exhausted-readonly-token", "readonly-exhausted")
	exhausted.Status = common.TokenStatusExhausted
	exhausted.RemainQuota = 0
	exhausted.UnlimitedQuota = false
	require.NoError(t, exhausted.Update())
	_, err := ValidateUserTokenForReadOnly(exhausted.Key)
	require.NoError(t, err, "exhausted tokens should still read their own usage data")

	disabled := seedReadOnlyToken(t, "disabled-readonly-token", "readonly-disabled")
	disabled.Status = common.TokenStatusDisabled
	require.NoError(t, disabled.Update())
	_, err = ValidateUserTokenForReadOnly(disabled.Key)
	require.ErrorIs(t, err, ErrTokenInvalid)

	expired := seedReadOnlyToken(t, "expired-readonly-token", "readonly-expired")
	expired.ExpiredTime = common.GetTimestamp() - 1
	require.NoError(t, expired.Update())
	_, err = ValidateUserTokenForReadOnly(expired.Key)
	require.ErrorIs(t, err, ErrTokenInvalid)
}

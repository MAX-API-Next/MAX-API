package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionUserIDAcceptsJSONDecodedNumber(t *testing.T) {
	id, ok := sessionUserID(float64(42))

	require.True(t, ok)
	require.Equal(t, 42, id)
}

func TestSessionUserIDRejectsMissingOrInvalidID(t *testing.T) {
	_, ok := sessionUserID(nil)
	require.False(t, ok)

	_, ok = sessionUserID(0)
	require.False(t, ok)
}

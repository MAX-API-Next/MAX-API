package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionCookieSecureForServerAddress(t *testing.T) {
	require.True(t, SessionCookieSecureForServerAddress("https://max-api.example"))
	require.True(t, SessionCookieSecureForServerAddress(" HTTPS://max-api.example/path "))
	require.False(t, SessionCookieSecureForServerAddress("http://max-api.example"))
	require.False(t, SessionCookieSecureForServerAddress("localhost:3000"))
	require.False(t, SessionCookieSecureForServerAddress(""))
}

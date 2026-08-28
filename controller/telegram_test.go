package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func signedTelegramParams(t *testing.T, token string, authDate time.Time) url.Values {
	t.Helper()
	params := url.Values{
		"id":         {"123456"},
		"first_name": {"Security"},
		"auth_date":  {strconv.FormatInt(authDate.Unix(), 10)},
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+params.Get(key))
	}
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	_, err := mac.Write([]byte(strings.Join(lines, "\n")))
	require.NoError(t, err)
	params.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return params
}

func TestTelegramAuthorizationRejectsStaleSignedPayload(t *testing.T) {
	const token = "123456:telegram-test-token"
	params := signedTelegramParams(t, token, time.Now().Add(-time.Hour))
	require.False(t, checkTelegramAuthorization(params, token))
}

func TestTelegramAuthorizationAcceptsFreshSignedPayload(t *testing.T) {
	const token = "123456:telegram-test-token"
	params := signedTelegramParams(t, token, time.Now().Add(-time.Minute))
	require.True(t, checkTelegramAuthorization(params, token))
}

func TestTelegramAuthorizationIgnoresLocalBindState(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(-time.Minute))
	params.Set("state", "local-bind-state")

	require.True(t, checkTelegramAuthorizationAt(params, token, now))
}

func TestTelegramAuthorizationRejectsAuthDateBeyondClockSkew(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(telegramClockSkew+time.Second))

	require.False(t, checkTelegramAuthorizationAt(params, token, now))
}

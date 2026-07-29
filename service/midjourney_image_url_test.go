package service

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyImageURLBindsTaskUserAndExpiry(t *testing.T) {
	oldSecret := common.CryptoSecret
	common.CryptoSecret = "midjourney-image-test-secret"
	t.Cleanup(func() { common.CryptoSecret = oldSecret })

	now := time.Unix(1_700_000_000, 0)
	rawURL := BuildMidjourneyImageURL("https://max-api.example/", "mj-task/one", 42, now)
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.Equal(t, "/mj/image/mj-task/one", parsed.Path)

	expiresAt, err := strconv.ParseInt(parsed.Query().Get("expires"), 10, 64)
	require.NoError(t, err)
	signature := parsed.Query().Get("signature")
	require.True(t, ValidateMidjourneyImageURL("mj-task/one", 42, expiresAt, signature, now))
	require.False(t, ValidateMidjourneyImageURL("mj-task/one", 43, expiresAt, signature, now))
	require.False(t, ValidateMidjourneyImageURL("other-task", 42, expiresAt, signature, now))
	require.False(t, ValidateMidjourneyImageURL("mj-task/one", 42, expiresAt, signature, time.Unix(expiresAt, 0)))
}

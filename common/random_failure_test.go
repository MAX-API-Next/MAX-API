package common

import (
	crand "crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

func TestRandomConvenienceHelpersDoNotPanicWhenEntropyFails(t *testing.T) {
	originalReader := crand.Reader
	originalSMTPFrom := SMTPFrom
	crand.Reader = io.Reader(failingRandomReader{})
	SMTPFrom = "sender@example.com"
	t.Cleanup(func() {
		crand.Reader = originalReader
		SMTPFrom = originalSMTPFrom
	})

	require.NotPanics(t, func() {
		require.Equal(t, 0, GetRandomInt(10))
		require.Empty(t, GetRandomString(8))
	})
	_, err := GenerateRandomCharsKey(8)
	require.ErrorContains(t, err, "random source unavailable")
	_, err = GenerateRandomKey(8)
	require.ErrorContains(t, err, "random source unavailable")
	_, err = SecureRandomInt(10)
	require.ErrorContains(t, err, "random source unavailable")
	_, err = generateMessageID()
	require.ErrorContains(t, err, "generate message ID")
}

func TestGetRandomIntDoesNotPanicForInvalidBound(t *testing.T) {
	require.NotPanics(t, func() {
		require.Equal(t, 0, GetRandomInt(0))
	})
}

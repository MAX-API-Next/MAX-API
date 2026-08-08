package common

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestSanitizePersistedLogContentNormalizesControlsAndInvalidUTF8(t *testing.T) {
	input := " first\r\nsecond\tthird\x00" + string([]byte{0xff, 0xfe}) + " "

	got := SanitizePersistedLogContent(input)

	require.Equal(t, "first  second third", got)
	require.True(t, utf8.ValidString(got))
	require.NotContains(t, got, "\r")
	require.NotContains(t, got, "\n")
	require.NotContains(t, got, "\t")
	require.NotContains(t, got, "\x00")
}

func TestSanitizePersistedLogContentTruncatesByRune(t *testing.T) {
	input := strings.Repeat("x", PersistedLogContentLimit+1)

	got := SanitizePersistedLogContent(input)

	require.Equal(t, PersistedLogContentLimit+utf8.RuneCountInString(persistedLogContentTruncatedSuffix), utf8.RuneCountInString(got))
	require.True(t, strings.HasSuffix(got, persistedLogContentTruncatedSuffix))
}

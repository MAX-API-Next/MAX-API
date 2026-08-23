package console_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExceedsMaxCharactersMatchesJavaScriptUTF16Length(t *testing.T) {
	require.False(t, exceedsMaxCharacters("abc", 3))
	require.False(t, exceedsMaxCharacters("中文", 2))
	require.False(t, exceedsMaxCharacters("é", 1))
	require.True(t, exceedsMaxCharacters("😀", 1))
	require.False(t, exceedsMaxCharacters("😀", 2))
}

func TestValidateFAQUsesUTF16CodeUnits(t *testing.T) {
	valid := `[{"question":"` + strings.Repeat("中", 200) + `","answer":"ok"}]`
	require.NoError(t, validateFAQ(valid))

	tooLongEmoji := `[{"question":"` + strings.Repeat("😀", 101) + `","answer":"ok"}]`
	require.ErrorContains(t, validateFAQ(tooLongEmoji), "不能超过200字符")
}

func TestParseJSONArrayReturnsInvalidJSONError(t *testing.T) {
	_, err := parseJSONArray(`[{`, "FAQ信息")
	require.ErrorContains(t, err, "FAQ信息格式错误")
}

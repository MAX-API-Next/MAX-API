package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeOpenAIUsageDistinguishesAbsentPartialAndZero(t *testing.T) {
	usage := Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
		PromptTokensDetails:    InputTokenDetails{CachedTokens: 60},
		CompletionTokenDetails: OutputTokenDetails{ReasoningTokens: 5}}
	for _, empty := range []string{`{}`, `{"usage":null}`, `{"usage":{}}`} {
		require.False(t, MergeOpenAIUsage(&usage, []byte(empty)))
		require.Equal(t, 100, usage.PromptTokens)
		require.Equal(t, 20, usage.CompletionTokens)
	}
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"completion_tokens":25}}`)))
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 25, usage.CompletionTokens)
	require.Equal(t, 125, usage.TotalTokens)
	require.Equal(t, 60, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 5, usage.CompletionTokenDetails.ReasoningTokens)
	usage.InputTokens, usage.OutputTokens, usage.PromptCacheHitTokens = 100, 25, 60
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0}}`)))
	require.Equal(t, Usage{}, usage)
}

func TestMergeOpenAIUsageRetainsOmittedNestedDetails(t *testing.T) {
	usage := Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
		PromptTokensDetails: InputTokenDetails{CachedTokens: 60, AudioTokens: 30}}
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"prompt_tokens_details":{"cached_tokens":0}}}`)))
	require.Equal(t, 0, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 30, usage.PromptTokensDetails.AudioTokens)
	require.Equal(t, 100, usage.PromptTokens)
}

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

func TestMergeOpenAIUsageCanonicalizesNumericAliases(t *testing.T) {
	var usage Usage
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"input_tokens":100,"output_tokens":20}}`)))
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)
	require.Equal(t, 120, usage.TotalTokens)
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"input_tokens_details":{"cached_tokens":60},"output_tokens_details":{"reasoning_tokens":5}}}`)))
	require.Equal(t, 60, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 5, usage.CompletionTokenDetails.ReasoningTokens)
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"output_tokens":25}}`)))
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 25, usage.CompletionTokens)
	require.Equal(t, 125, usage.TotalTokens)
	// Canonical numeric fields, including zero, win over conflicting aliases.
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"prompt_tokens":7,"input_tokens":9,"completion_tokens":0,"output_tokens":20,"prompt_tokens_details":{"cached_tokens":0},"input_tokens_details":{"cached_tokens":60}}}`)))
	require.Equal(t, 7, usage.PromptTokens)
	require.Equal(t, 7, usage.InputTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.OutputTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Zero(t, usage.PromptTokensDetails.CachedTokens)
	require.NotNil(t, usage.InputTokensDetails)
	require.Zero(t, usage.InputTokensDetails.CachedTokens)
	require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"input_tokens":0,"output_tokens":0}}`)))
	require.Equal(t, Usage{}, usage)
}

func TestMergeOpenAIUsageRejectsProviderBillingEvidence(t *testing.T) {
	for _, evidence := range []string{`{"semantic":"openai","openai_usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`, `42`, `"private"`} {
		var usage Usage
		require.True(t, MergeOpenAIUsage(&usage, []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"cost":0.25,"prompt_tokens_details":{"cached_tokens":60},"billing_usage":`+evidence+`}}`)))
		require.Nil(t, usage.BillingUsage)
		require.Equal(t, 100, usage.PromptTokens)
		require.Equal(t, 20, usage.CompletionTokens)
		require.Equal(t, 60, usage.PromptTokensDetails.CachedTokens)
		require.Equal(t, 0.25, usage.Cost)
	}
}

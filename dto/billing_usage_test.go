package dto

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGeminiChatBillingUsageRequiresTokenContent(t *testing.T) {
	require.Nil(t, NewGeminiChatBillingUsage(nil))
	require.Nil(t, NewGeminiChatBillingUsage(&GeminiUsageMetadata{}))

	billingUsage := NewGeminiChatBillingUsage(&GeminiUsageMetadata{PromptTokenCount: 1})
	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.GeminiUsageMetadata)
	assert.Equal(t, BillingUsageSourceGeminiChat, billingUsage.Source)
	assert.Equal(t, BillingUsageSemanticGemini, billingUsage.Semantic)
	assert.False(t, billingUsage.Estimated)
}

func TestNewClaudeMessagesBillingUsageRequiresTokenContent(t *testing.T) {
	require.Nil(t, NewClaudeMessagesBillingUsage(nil))
	require.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{}))
	require.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{CacheCreation: &ClaudeCacheCreationUsage{}}))

	billingUsage := NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 1})
	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.ClaudeUsage)
	assert.Equal(t, BillingUsageSourceClaudeMessages, billingUsage.Source)
	assert.Equal(t, BillingUsageSemanticAnthropic, billingUsage.Semantic)

	cacheOnly := NewClaudeMessagesBillingUsage(&ClaudeUsage{
		CacheCreation: &ClaudeCacheCreationUsage{Ephemeral5mInputTokens: 4},
	})
	require.NotNil(t, cacheOnly)
}

func TestNewOpenAIChatBillingUsageRequiresTokenContent(t *testing.T) {
	require.Nil(t, NewOpenAIChatBillingUsage(nil))
	require.Nil(t, NewOpenAIChatBillingUsage(&Usage{}))

	billingUsage := NewOpenAIChatBillingUsage(&Usage{PromptTokens: 1})
	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.OpenAIUsage)
	assert.Equal(t, BillingUsageSourceOAIChat, billingUsage.Source)
	assert.Equal(t, BillingUsageSemanticOpenAI, billingUsage.Semantic)
	assert.Equal(t, 1, billingUsage.OpenAIUsage.PromptTokens)
}

func TestNewOpenAIResponsesBillingUsageSupportsOutputTokenDetails(t *testing.T) {
	source := &Usage{OutputTokensDetails: &OutputTokenDetails{AudioTokens: 9}}
	billingUsage := NewOpenAIResponsesBillingUsage(source)

	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.OpenAIUsage)
	require.NotNil(t, billingUsage.OpenAIUsage.OutputTokensDetails)
	assert.Equal(t, 9, billingUsage.OpenAIUsage.OutputTokensDetails.AudioTokens)
	assert.NotSame(t, source.OutputTokensDetails, billingUsage.OpenAIUsage.OutputTokensDetails)
}

func TestNewEstimatedGeminiChatBillingUsage(t *testing.T) {
	billingUsage := NewEstimatedGeminiChatBillingUsage(&Usage{
		PromptTokens:     11,
		CompletionTokens: 7,
	})

	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.GeminiUsageMetadata)
	assert.True(t, billingUsage.Estimated)
	assert.Equal(t, 11, billingUsage.GeminiUsageMetadata.PromptTokenCount)
	assert.Equal(t, 7, billingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	assert.Equal(t, 18, billingUsage.GeminiUsageMetadata.TotalTokenCount)
}

func TestCopyInputTokenDetails(t *testing.T) {
	src := &InputTokenDetails{
		CachedTokens:         1,
		CachedCreationTokens: 2,
		CacheWriteTokens:     3,
		TextTokens:           4,
		ImageTokens:          5,
		AudioTokens:          6,
	}

	overwritten := InputTokenDetails{
		CachedTokens:         10,
		CachedCreationTokens: 20,
		CacheWriteTokens:     30,
		TextTokens:           40,
		ImageTokens:          50,
		AudioTokens:          60,
	}
	CopyInputTokenDetails(&overwritten, src, true)
	assert.Equal(t, *src, overwritten)

	filled := InputTokenDetails{
		CachedTokens:         10,
		CachedCreationTokens: 0,
		CacheWriteTokens:     30,
		TextTokens:           0,
		ImageTokens:          50,
		AudioTokens:          0,
	}
	CopyInputTokenDetails(&filled, src, false)
	assert.Equal(t, InputTokenDetails{
		CachedTokens:         10,
		CachedCreationTokens: 2,
		CacheWriteTokens:     30,
		TextTokens:           4,
		ImageTokens:          50,
		AudioTokens:          6,
	}, filled)
}

func TestBillingUsageJSONUsesProtocolNamedFields(t *testing.T) {
	billingUsage := &BillingUsage{
		OpenAIUsage:         &Usage{PromptTokens: 1, BillingUsage: NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 9})},
		ClaudeUsage:         &ClaudeUsage{InputTokens: 2, BillingUsage: NewOpenAIChatBillingUsage(&Usage{PromptTokens: 8})},
		GeminiUsageMetadata: &GeminiUsageMetadata{PromptTokenCount: 3, BillingUsage: NewOpenAIChatBillingUsage(&Usage{PromptTokens: 7})},
	}

	data, err := common.Marshal(billingUsage)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"openai_usage"`)
	assert.Contains(t, string(data), `"claude_usage"`)
	assert.Contains(t, string(data), `"gemini_usage_metadata"`)
	assert.NotContains(t, string(data), `"usage":`)
	assert.NotContains(t, string(data), `"usage_metadata"`)

	clone := CloneBillingUsage(billingUsage)
	require.NotNil(t, clone.OpenAIUsage)
	require.NotNil(t, clone.ClaudeUsage)
	require.NotNil(t, clone.GeminiUsageMetadata)
	assert.Nil(t, clone.OpenAIUsage.BillingUsage)
	assert.Nil(t, clone.ClaudeUsage.BillingUsage)
	assert.Nil(t, clone.GeminiUsageMetadata.BillingUsage)
}

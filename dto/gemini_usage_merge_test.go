package dto

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestMergeGeminiUsageMetadataRetainsOmittedModalities(t *testing.T) {
	current := &GeminiUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    10,
		TotalTokenCount:         110,
		HasPromptTokenCount:     true,
		HasCandidatesTokenCount: true,
		PromptTokensDetails: []GeminiPromptTokensDetails{
			{Modality: "IMAGE", TokenCount: 90},
			{Modality: "TEXT", TokenCount: 10},
		},
	}
	incoming := &GeminiUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    20,
		TotalTokenCount:         120,
		HasPromptTokenCount:     true,
		HasCandidatesTokenCount: true,
		PromptTokensDetails: []GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 20},
		},
	}

	merged := MergeGeminiUsageMetadataNonZero(current, incoming)
	require.Equal(t, 100, merged.PromptTokenCount)
	require.Equal(t, 20, merged.CandidatesTokenCount)
	require.Equal(t, 120, merged.TotalTokenCount)
	require.Contains(t, merged.PromptTokensDetails, GeminiPromptTokensDetails{Modality: "IMAGE", TokenCount: 90})
	require.Contains(t, merged.PromptTokensDetails, GeminiPromptTokensDetails{Modality: "TEXT", TokenCount: 20})
	require.NotContains(t, merged.PromptTokensDetails, GeminiPromptTokensDetails{Modality: "TEXT", TokenCount: 10})
}

func TestMergeGeminiUsageMetadataHonorsExplicitZeroCounters(t *testing.T) {
	current := &GeminiUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    20,
		TotalTokenCount:         120,
		HasPromptTokenCount:     true,
		HasCandidatesTokenCount: true,
	}
	incoming := &GeminiUsageMetadata{
		HasPromptTokenCount:     true,
		HasCandidatesTokenCount: true,
	}

	merged := MergeGeminiUsageMetadataNonZero(current, incoming)
	require.Zero(t, merged.PromptTokenCount)
	require.Zero(t, merged.CandidatesTokenCount)
}

func TestMergeGeminiUsageMetadataPresenceFlagsCoverAllCounters(t *testing.T) {
	current := &GeminiUsageMetadata{
		ToolUsePromptTokenCount: 7, TotalTokenCount: 9,
		ThoughtsTokenCount: 8, CachedContentTokenCount: 6,
	}
	incoming := &GeminiUsageMetadata{
		HasToolUsePromptTokenCount: true, HasTotalTokenCount: true,
		HasThoughtsTokenCount: true, HasCachedContentTokenCount: true,
	}
	merged := MergeGeminiUsageMetadataNonZero(current, incoming)
	require.Zero(t, merged.ToolUsePromptTokenCount)
	require.Zero(t, merged.TotalTokenCount)
	require.Zero(t, merged.ThoughtsTokenCount)
	require.Zero(t, merged.CachedContentTokenCount)

	absent := &GeminiUsageMetadata{}
	merged = MergeGeminiUsageMetadataNonZero(current, absent)
	require.Equal(t, 7, merged.ToolUsePromptTokenCount)
	require.Equal(t, 9, merged.TotalTokenCount)
	require.Equal(t, 8, merged.ThoughtsTokenCount)
	require.Equal(t, 6, merged.CachedContentTokenCount)
}

func TestGeminiUsageMetadataPresenceFlagsFollowJSONKeyPresence(t *testing.T) {
	var usage GeminiUsageMetadata
	require.NoError(t, common.Unmarshal([]byte(`{"toolUsePromptTokenCount":0,"totalTokenCount":0,"thoughtsTokenCount":0,"cachedContentTokenCount":0}`), &usage))
	require.True(t, usage.HasToolUsePromptTokenCount)
	require.True(t, usage.HasTotalTokenCount)
	require.True(t, usage.HasThoughtsTokenCount)
	require.True(t, usage.HasCachedContentTokenCount)

	usage = GeminiUsageMetadata{}
	require.NoError(t, common.Unmarshal([]byte(`{"promptTokenCount":0}`), &usage))
	require.False(t, usage.HasToolUsePromptTokenCount)
	require.False(t, usage.HasTotalTokenCount)
	require.False(t, usage.HasThoughtsTokenCount)
	require.False(t, usage.HasCachedContentTokenCount)
}

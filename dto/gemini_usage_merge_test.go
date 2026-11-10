package dto

import (
	"testing"

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

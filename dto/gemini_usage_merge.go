package dto

import "strings"

// MergeGeminiUsageMetadataNonZero merges Gemini stream snapshots without
// treating omitted modality entries as zero. Scalar fields with explicit wire
// presence, including zero, remain authoritative; hand-built snapshots use a
// non-zero value as presence for compatibility with existing callers.
func MergeGeminiUsageMetadataNonZero(current, incoming *GeminiUsageMetadata) *GeminiUsageMetadata {
	if current == nil && incoming == nil {
		return nil
	}
	if current == nil {
		merged := cloneGeminiUsageMetadata(*incoming)
		merged.BillingUsage = CloneBillingUsage(incoming.BillingUsage)
		copyGeminiUsagePresence(&merged, incoming)
		return &merged
	}
	merged := cloneGeminiUsageMetadata(*current)
	merged.BillingUsage = CloneBillingUsage(current.BillingUsage)
	copyGeminiUsagePresence(&merged, current)
	if incoming == nil {
		return &merged
	}

	if incoming.HasPromptTokenCount || incoming.PromptTokenCount != 0 {
		merged.PromptTokenCount = incoming.PromptTokenCount
	}
	if incoming.HasToolUsePromptTokenCount || incoming.ToolUsePromptTokenCount != 0 {
		merged.ToolUsePromptTokenCount = incoming.ToolUsePromptTokenCount
	}
	if incoming.HasCandidatesTokenCount || incoming.CandidatesTokenCount != 0 {
		merged.CandidatesTokenCount = incoming.CandidatesTokenCount
	}
	if incoming.HasTotalTokenCount || incoming.TotalTokenCount != 0 {
		merged.TotalTokenCount = incoming.TotalTokenCount
	}
	if incoming.HasThoughtsTokenCount || incoming.ThoughtsTokenCount != 0 {
		merged.ThoughtsTokenCount = incoming.ThoughtsTokenCount
	}
	if incoming.HasCachedContentTokenCount || incoming.CachedContentTokenCount != 0 {
		merged.CachedContentTokenCount = incoming.CachedContentTokenCount
	}

	merged.PromptTokensDetails = mergeGeminiTokenDetails(merged.PromptTokensDetails, incoming.PromptTokensDetails)
	merged.ToolUsePromptTokensDetails = mergeGeminiTokenDetails(merged.ToolUsePromptTokensDetails, incoming.ToolUsePromptTokensDetails)
	merged.CandidatesTokensDetails = mergeGeminiTokenDetails(merged.CandidatesTokensDetails, incoming.CandidatesTokensDetails)
	if incoming.BillingUsage != nil {
		merged.BillingUsage = CloneBillingUsage(incoming.BillingUsage)
	}
	copyGeminiUsagePresence(&merged, incoming)
	return &merged
}

func copyGeminiUsagePresence(dst, src *GeminiUsageMetadata) {
	dst.HasPromptTokenCount = dst.HasPromptTokenCount || src.HasPromptTokenCount
	dst.HasToolUsePromptTokenCount = dst.HasToolUsePromptTokenCount || src.HasToolUsePromptTokenCount
	dst.HasCandidatesTokenCount = dst.HasCandidatesTokenCount || src.HasCandidatesTokenCount
	dst.HasTotalTokenCount = dst.HasTotalTokenCount || src.HasTotalTokenCount
	dst.HasThoughtsTokenCount = dst.HasThoughtsTokenCount || src.HasThoughtsTokenCount
	dst.HasCachedContentTokenCount = dst.HasCachedContentTokenCount || src.HasCachedContentTokenCount
}

func mergeGeminiTokenDetails(current, incoming []GeminiPromptTokensDetails) []GeminiPromptTokensDetails {
	reported := make(map[string]bool, len(incoming))
	for _, detail := range incoming {
		if detail.TokenCount > 0 {
			reported[normalizeGeminiModality(detail.Modality)] = true
		}
	}
	merged := make([]GeminiPromptTokensDetails, 0, len(current)+len(incoming))
	for _, detail := range current {
		if !reported[normalizeGeminiModality(detail.Modality)] {
			merged = append(merged, detail)
		}
	}
	for _, detail := range incoming {
		if detail.TokenCount > 0 {
			merged = append(merged, detail)
		}
	}
	return merged
}

func normalizeGeminiModality(modality string) string {
	return strings.ToUpper(strings.TrimSpace(modality))
}

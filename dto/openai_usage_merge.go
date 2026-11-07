package dto

import (
	"encoding/json"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/tidwall/gjson"
)

// OpenAIUsagePresence belongs to one stream, never to provider JSON.
type OpenAIUsagePresence struct {
	Any, Prompt, Completion, Total bool
	ReportedTotal                  int
}

// A provider cannot attach host-owned accounting evidence. RawMessage shadows
// the embedded field and ignores even differently typed private metadata.
type providerOpenAIUsage struct {
	*Usage
	BillingUsage json.RawMessage `json:"billing_usage"`
}

// MergeOpenAIUsage retains omitted counters/details in cumulative usage updates.
// An empty/null object is not a zero snapshot; numeric zeros are authoritative.
func MergeOpenAIUsage(dst *Usage, response []byte) bool {
	return MergeOpenAIUsageWithPresence(dst, response, nil)
}

func MergeOpenAIUsageWithPresence(dst *Usage, response []byte, presence *OpenAIUsagePresence) bool {
	if dst == nil {
		return false
	}
	raw := gjson.GetBytes(response, "usage")
	if !raw.IsObject() {
		return false
	}
	var incoming Usage
	if common.Unmarshal([]byte(raw.Raw), &providerOpenAIUsage{Usage: &incoming}) != nil {
		return false
	}
	prompt := raw.Get("prompt_tokens")
	completion := raw.Get("completion_tokens")
	if prompt.Type != gjson.Number {
		prompt = raw.Get("input_tokens")
	}
	if completion.Type != gjson.Number {
		completion = raw.Get("output_tokens")
	}
	if !hasOpenAIUsageUpdate(raw) {
		return false
	}
	next := *cloneOpenAIUsage(dst)
	// A zero total for one side also excludes stale sub-category counts from
	// earlier events. Explicit details in this update are decoded below.
	if prompt.Type == gjson.Number && prompt.Int() == 0 {
		next.InputTokens = 0
		next.PromptCacheHitTokens = 0
		next.ClaudeCacheCreation5mTokens = 0
		next.ClaudeCacheCreation1hTokens = 0
		next.PromptTokensDetails = InputTokenDetails{}
		next.InputTokensDetails = nil
	}
	if completion.Type == gjson.Number && completion.Int() == 0 {
		next.OutputTokens = 0
		next.CompletionTokenDetails = OutputTokenDetails{}
		next.OutputTokensDetails = nil
	}
	// Decode aliases first; canonical detail fields below win per field,
	// including explicit zeros. Omitted nested fields retain their prior value.
	if details := raw.Get("input_tokens_details"); details.IsObject() {
		if common.Unmarshal([]byte(details.Raw), &next.PromptTokensDetails) != nil {
			return false
		}
	}
	if details := raw.Get("output_tokens_details"); details.IsObject() {
		if common.Unmarshal([]byte(details.Raw), &next.CompletionTokenDetails) != nil {
			return false
		}
	}
	if common.Unmarshal([]byte(raw.Raw), &providerOpenAIUsage{Usage: &next}) != nil {
		return false
	}
	next.BillingUsage = nil
	if prompt.Type == gjson.Number {
		next.PromptTokens = int(prompt.Int())
		if raw.Get("input_tokens").Type == gjson.Number {
			next.InputTokens = next.PromptTokens
		}
	}
	if completion.Type == gjson.Number {
		next.CompletionTokens = int(completion.Int())
		if raw.Get("output_tokens").Type == gjson.Number {
			next.OutputTokens = next.CompletionTokens
		}
	}
	// Keep retained aliases coherent so later normalization cannot resurrect
	// values that an explicit canonical zero just cleared.
	if next.InputTokensDetails != nil {
		details := next.PromptTokensDetails
		next.InputTokensDetails = &details
	}
	if next.OutputTokensDetails != nil {
		details := next.CompletionTokenDetails
		next.OutputTokensDetails = &details
	}
	if raw.Get("total_tokens").Type != gjson.Number && (prompt.Type == gjson.Number || completion.Type == gjson.Number) {
		next.TotalTokens = next.PromptTokens + next.CompletionTokens
	}
	*dst = next
	if presence != nil {
		presence.Any = true
		presence.Prompt = presence.Prompt || prompt.Type == gjson.Number
		presence.Completion = presence.Completion || completion.Type == gjson.Number
		if raw.Get("total_tokens").Type == gjson.Number {
			presence.Total, presence.ReportedTotal = true, next.TotalTokens
		} else if prompt.Type == gjson.Number || completion.Type == gjson.Number {
			// A later parent update supersedes the earlier cumulative total.
			presence.Total = false
		}
	}
	return true
}

func hasOpenAIUsageUpdate(raw gjson.Result) bool {
	for _, field := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "input_tokens", "output_tokens", "prompt_cache_hit_tokens", "claude_cache_creation_5_m_tokens", "claude_cache_creation_1_h_tokens"} {
		if raw.Get(field).Type == gjson.Number {
			return true
		}
	}
	for _, field := range []string{"prompt_tokens_details", "input_tokens_details", "completion_tokens_details", "output_tokens_details"} {
		details := raw.Get(field)
		for _, count := range []string{"cached_tokens", "cached_creation_tokens", "cache_write_tokens", "text_tokens", "audio_tokens", "image_tokens", "reasoning_tokens"} {
			if details.Get(count).Type == gjson.Number {
				return true
			}
		}
	}
	return false
}

package dto

import (
	"encoding/json"
	"strings"

	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

type OpenAIResponsesCompactionRequest struct {
	Model                string          `json:"model"`
	Input                json.RawMessage `json:"input,omitempty"`
	Instructions         json.RawMessage `json:"instructions,omitempty"`
	PreviousResponseID   string          `json:"previous_response_id,omitempty"`
	Tools                json.RawMessage `json:"tools,omitempty"`
	ParallelToolCalls    json.RawMessage `json:"parallel_tool_calls,omitempty"`
	Reasoning            *Reasoning      `json:"reasoning,omitempty"`
	ServiceTier          string          `json:"service_tier,omitempty"`
	PromptCacheKey       json.RawMessage `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions   json.RawMessage `json:"prompt_cache_options,omitempty"`
	PromptCacheRetention json.RawMessage `json:"prompt_cache_retention,omitempty"`
	Text                 json.RawMessage `json:"text,omitempty"`
}

// ToResponsesRequest preserves compact fields through the common adaptor and
// channel field-filter pipeline.
func (r *OpenAIResponsesCompactionRequest) ToResponsesRequest() *OpenAIResponsesRequest {
	return &OpenAIResponsesRequest{
		Model: r.Model, Input: r.Input, Instructions: r.Instructions, PreviousResponseID: r.PreviousResponseID,
		Tools: r.Tools, ParallelToolCalls: r.ParallelToolCalls, Reasoning: r.Reasoning, ServiceTier: r.ServiceTier,
		PromptCacheKey: r.PromptCacheKey, PromptCacheOptions: r.PromptCacheOptions, PromptCacheRetention: r.PromptCacheRetention, Text: r.Text,
	}
}

func (r *OpenAIResponsesCompactionRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var parts []string
	if len(r.Instructions) > 0 {
		parts = append(parts, string(r.Instructions))
	}
	if len(r.Input) > 0 {
		parts = append(parts, string(r.Input))
	}
	if len(r.Tools) > 0 {
		parts = append(parts, string(r.Tools))
	}
	return &types.TokenCountMeta{
		CombineText: strings.Join(parts, "\n"),
	}
}

func (r *OpenAIResponsesCompactionRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *OpenAIResponsesCompactionRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

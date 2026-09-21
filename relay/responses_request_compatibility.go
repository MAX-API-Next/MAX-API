package relay

import (
	"errors"
	"net/url"
	"strings"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
)

// Filter only converted Responses payloads, using the actual destination (which
// may be an absolute advanced-custom route). Shared conversion must preserve
// vendor extensions for third-party endpoints. Explicit raw pass-through and
// administrator parameter overrides retain their existing separate semantics.
func filterResponsesRequestFields(request any, info *relaycommon.RelayInfo, adaptor channel.Adaptor) (any, error) {
	var filtered dto.OpenAIResponsesRequest
	pointer := false
	switch req := request.(type) {
	case dto.OpenAIResponsesRequest:
		filtered = req
	case *dto.OpenAIResponsesRequest:
		if req == nil {
			return request, nil
		}
		filtered, pointer = *req, true
	default:
		return request, nil
	}
	if info == nil || info.ChannelMeta == nil {
		return request, nil
	}
	strict := info.ChannelType == constant.ChannelTypeCodex
	if !strict {
		endpoint, err := adaptor.GetRequestURL(info)
		if err != nil {
			return nil, err
		}
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, errors.New("invalid Responses upstream URL")
		}
		host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		strict = host == "api.openai.com" || (host == "chatgpt.com" && strings.HasPrefix(parsed.Path, "/backend-api/codex/"))
	}
	if !strict {
		return request, nil
	}
	// These are vendor-only extensions, not official Responses create fields:
	// https://developers.openai.com/api/reference/typescript/resources/responses/methods/create
	// Use a narrow denylist, not a whitelist that would strip future native fields.
	filtered.Stop = nil
	filtered.TopK = nil
	filtered.MinP = nil
	filtered.RepetitionPenalty = nil
	filtered.CacheSalt = nil
	filtered.ChatTemplateKwargs = nil
	filtered.FrequencyPenalty = nil
	filtered.PresencePenalty = nil
	filtered.EnableThinking = nil
	filtered.ThinkingBudget = nil
	if pointer {
		return &filtered, nil
	}
	return filtered, nil
}

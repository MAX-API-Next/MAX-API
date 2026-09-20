// Package reasoningcompat translates request reasoning controls only. It does
// not own routing, usage, token limits, reservation, or settlement.
package reasoningcompat

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/setting/reasoning"
)

type Intent struct {
	Mode    string
	Effort  string
	Budget  *int
	Include *bool
}

func (i Intent) Empty() bool {
	return i.Mode == "" && i.Effort == "" && i.Budget == nil && i.Include == nil
}

func NormalizeEffort(effort string) (string, error) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return effort, nil
	}
	return "", fmt.Errorf("unsupported reasoning effort %q", effort)
}

func FromChat(req dto.GeneralOpenAIRequest) (Intent, error) {
	effort, err := NormalizeEffort(req.ReasoningEffort)
	if err != nil {
		return Intent{}, err
	}
	intent := Intent{Effort: effort, Include: req.IncludeReasoning}
	if effort != "" {
		intent.Mode = "enabled"
		if effort == "none" {
			intent.Mode = "disabled"
		}
	}
	if len(req.Reasoning) > 0 {
		var raw struct {
			Enabled   *bool  `json:"enabled"`
			Effort    string `json:"effort"`
			MaxTokens *int   `json:"max_tokens"`
			Exclude   *bool  `json:"exclude"`
		}
		if err := common.Unmarshal(req.Reasoning, &raw); err != nil {
			return intent, err
		}
		if raw.Effort != "" {
			e, err := NormalizeEffort(raw.Effort)
			if err != nil {
				return intent, err
			}
			if effort != "" && e != effort {
				return intent, fmt.Errorf("conflicting reasoning efforts")
			}
			intent.Effort = e
			intent.Mode = "enabled"
			if e == "none" {
				intent.Mode = "disabled"
			}
		}
		if raw.Enabled != nil {
			if (!*raw.Enabled && intent.Effort != "" && intent.Effort != "none") || (*raw.Enabled && intent.Effort == "none") {
				return intent, fmt.Errorf("conflicting reasoning enabled and effort")
			}
			if *raw.Enabled {
				intent.Mode = "enabled"
			} else {
				intent.Mode = "disabled"
				intent.Effort = "none"
			}
		}
		if raw.MaxTokens != nil {
			if raw.Enabled != nil && *raw.Enabled && *raw.MaxTokens == 0 {
				return intent, fmt.Errorf("conflicting reasoning enabled and zero budget")
			}
			if (intent.Mode == "disabled" && *raw.MaxTokens != 0) || (*raw.MaxTokens == 0 && intent.Effort != "" && intent.Effort != "none") {
				return intent, fmt.Errorf("conflicting reasoning budget and mode")
			}
			if *raw.MaxTokens < -1 {
				return intent, fmt.Errorf("reasoning budget must be >= -1")
			}
			intent.Budget = raw.MaxTokens
			intent.Mode = "enabled"
			if *raw.MaxTokens == 0 {
				intent.Mode = "disabled"
				intent.Effort = "none"
			}
		}
		if raw.Exclude != nil {
			if intent.Include != nil && *intent.Include == *raw.Exclude {
				return intent, fmt.Errorf("conflicting reasoning visibility")
			}
			intent.Include = common.GetPointer(!*raw.Exclude)
		}
	}
	return intent, nil
}

// ParseSuffix keeps real model IDs and explicitly configured names opaque.
func ParseSuffix(model, provider string, thinkingAliases bool) (string, Intent, bool, error) {
	if reasoning.PreserveModelSuffix(model) || !strings.HasPrefix(model, provider+"-") {
		return model, Intent{}, false, nil
	}
	if base, effort, ok := reasoning.TrimEffortSuffixWithSuffixes(model, reasoning.OpenAIEffortSuffixes); ok {
		mode := "enabled"
		if effort == "none" {
			mode = "disabled"
		}
		return base, Intent{Mode: mode, Effort: effort}, true, nil
	}
	if !thinkingAliases {
		return model, Intent{}, false, nil
	}
	if base, ok := strings.CutSuffix(model, "-nothinking"); ok {
		return base, Intent{Mode: "disabled", Effort: "none"}, true, nil
	}
	if base, ok := strings.CutSuffix(model, "-thinking"); ok {
		return base, Intent{Mode: "enabled"}, true, nil
	}
	if pos := strings.LastIndex(model, "-thinking-"); pos >= 0 {
		budget, err := strconv.Atoi(model[pos+10:])
		if err != nil || budget < -1 {
			return model, Intent{}, false, fmt.Errorf("invalid thinking budget suffix")
		}
		mode := "enabled"
		if budget == 0 {
			mode = "disabled"
		}
		return model[:pos], Intent{Mode: mode, Budget: &budget}, true, nil
	}
	return model, Intent{}, false, nil
}

func MergeSuffix(body, suffix Intent) (Intent, error) {
	if (body.Budget != nil && *body.Budget < -1) || (suffix.Budget != nil && *suffix.Budget < -1) {
		return body, fmt.Errorf("reasoning budget must be >= -1")
	}
	if body.Include != nil && suffix.Include != nil && *body.Include != *suffix.Include {
		return body, fmt.Errorf("conflicting reasoning visibility")
	}
	if body.Mode != "" && suffix.Mode != "" && body.Mode != suffix.Mode {
		return body, fmt.Errorf("conflicting reasoning mode and model suffix")
	}
	if body.Effort != "" && suffix.Effort != "" && body.Effort != suffix.Effort {
		return body, fmt.Errorf("conflicting reasoning effort and model suffix")
	}
	if body.Budget != nil && suffix.Budget != nil && *body.Budget != *suffix.Budget {
		return body, fmt.Errorf("conflicting reasoning budget and model suffix")
	}
	if suffix.Mode != "" {
		body.Mode = suffix.Mode
	}
	if suffix.Effort != "" {
		body.Effort = suffix.Effort
	}
	if suffix.Budget != nil {
		body.Budget = suffix.Budget
	}
	return body, nil
}

// These ranges follow the reviewed New API conversion table, not pricing.
func EffortFromBudget(budget int) string {
	switch {
	case budget == 0:
		return "none"
	case budget < 0:
		return "high"
	case budget <= 1024:
		return "low"
	case budget <= 8192:
		return "medium"
	default:
		return "high"
	}
}

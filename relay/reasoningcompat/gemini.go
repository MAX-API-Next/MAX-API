package reasoningcompat

import (
	"fmt"
	"math"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
)

func geminiKind(model string) string {
	switch {
	case strings.HasPrefix(model, "gemini-2.5-flash-native-audio"), strings.HasPrefix(model, "gemini-live-2.5-flash-preview-native-audio"):
		return "budget"
	case strings.HasPrefix(model, "gemini-2.5-flash-image"), strings.Contains(model, "-tts"), strings.Contains(model, "-native-audio"), strings.Contains(model, "-live"):
		return "unsupported"
	case strings.HasPrefix(model, "gemini-3-pro-image"), strings.HasPrefix(model, "nano-banana-pro"):
		return "include-only"
	case strings.HasPrefix(model, "gemini-2.5-"):
		return "budget"
	case strings.HasPrefix(model, "gemini-3"), model == "gemini-pro-latest", model == "gemini-flash-latest", model == "gemini-flash-lite-latest":
		return "level"
	default:
		return "unknown"
	}
}

func geminiLevel(model, effort string) string {
	if effort == "none" {
		effort = "minimal"
	}
	if strings.HasPrefix(model, "gemini-3.1-flash-image") || strings.HasPrefix(model, "gemini-3.1-flash-lite-image") {
		if effort == "minimal" || effort == "low" {
			return "minimal"
		}
		return "high"
	}
	if strings.HasPrefix(model, "gemini-3-pro") {
		if effort == "minimal" || effort == "low" {
			return "low"
		}
		return "high"
	}
	if (strings.HasPrefix(model, "gemini-3.1-pro") || model == "gemini-pro-latest") && effort == "minimal" {
		return "low"
	}
	if effort == "max" || effort == "xhigh" {
		return "high"
	}
	return effort
}

func GeminiConfig(model string, intent Intent, maxTokens *uint, percentage float64) (*dto.GeminiThinkingConfig, string, []string, error) {
	model = strings.ToLower(model)
	effort, err := NormalizeEffort(intent.Effort)
	if err != nil {
		return nil, "", nil, err
	}
	if intent.Empty() {
		return nil, "", nil, nil
	}
	notes := []string{}
	config := &dto.GeminiThinkingConfig{IncludeThoughts: intent.Include}
	kind := geminiKind(model)
	if kind == "unknown" {
		// Capability lookup is not a model allowlist. Keep older and custom
		// model requests usable without inventing a budget or thinking level.
		return nil, "", []string{fmt.Sprintf("unknown Gemini thinking capabilities for model %q; compatibility reasoning controls were not applied", model)}, nil
	}
	if kind == "unsupported" {
		return nil, "", []string{"model does not support configurable thinking"}, nil
	}
	if intent.Mode == "" && intent.Effort == "" && intent.Budget == nil {
		return config, "", notes, nil
	}
	switch kind {
	case "include-only":
		return config, "high", []string{"model only supports includeThoughts; reasoning strength is fixed"}, nil
	case "level":
		if intent.Mode == "disabled" {
			effort = "none"
		}
		if effort == "" && intent.Budget != nil {
			effort = EffortFromBudget(*intent.Budget)
			notes = append(notes, "thinking budget converted to level")
		}
		if effort == "" {
			effort = "high"
			switch {
			case model == "gemini-flash-lite-latest", strings.HasPrefix(model, "gemini-3.5-flash-lite"), strings.HasPrefix(model, "gemini-3.1-flash-lite"):
				effort = "minimal"
			case model == "gemini-flash-latest", strings.HasPrefix(model, "gemini-3.5-flash"), strings.HasPrefix(model, "gemini-3.6-flash"):
				effort = "medium"
			}
		}
		level := geminiLevel(model, effort)
		if level != effort {
			notes = append(notes, fmt.Sprintf("thinking effort %s adjusted to supported level %s", effort, level))
		}
		config.ThinkingLevel = level
		return config, level, notes, nil
	}
	budget := -1
	minBudget, maxBudget := 0, 24576
	if strings.HasPrefix(model, "gemini-2.5-pro") {
		minBudget, maxBudget = 128, 32768
	} else if strings.HasPrefix(model, "gemini-2.5-flash-lite") {
		minBudget = 512
	}
	switch {
	case intent.Mode == "disabled" || effort == "none":
		budget = 0
	case intent.Budget != nil:
		budget = *intent.Budget
	case effort != "":
		switch effort {
		case "minimal", "low":
			budget = 1024
		case "medium":
			budget = 8192
		default:
			budget = 24576
		}
	case maxTokens != nil && *maxTokens > 0:
		if uint64(*maxTokens) > uint64(math.MaxInt) {
			return nil, "", notes, fmt.Errorf("output limit overflows thinking budget")
		}
		if percentage <= 0 || math.IsNaN(percentage) || math.IsInf(percentage, 0) {
			percentage = .6
		}
		percentage = min(percentage, 1)
		computed := float64(*maxTokens) * percentage
		if computed >= float64(maxBudget) {
			budget = maxBudget
		} else {
			budget = int(computed)
		}
	}
	if budget < -1 {
		return nil, "", notes, fmt.Errorf("thinking budget must be >= -1")
	}
	if budget != -1 {
		adjusted := budget
		if budget != 0 || strings.HasPrefix(model, "gemini-2.5-pro") {
			adjusted = min(max(budget, minBudget), maxBudget)
		}
		if adjusted != budget {
			notes = append(notes, fmt.Sprintf("thinking budget %d adjusted to supported value %d", budget, adjusted))
		}
		budget = adjusted
	}
	config.ThinkingBudget = common.GetPointer(budget)
	return config, EffortFromBudget(budget), notes, nil
}

package reasoningcompat

import (
	"fmt"
	"math"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
)

type claudeCapabilities struct{ adaptive, manual, defaultThinking, effort, xhigh, max, strict, disable bool }

func ClaudeEffort(req *dto.ClaudeRequest) string {
	if req.Thinking != nil && req.Thinking.Type == "disabled" {
		return "none"
	}
	if effort := req.GetEfforts(); effort != "" {
		return effort
	}
	if req.Thinking != nil {
		if req.Thinking.BudgetTokens != nil {
			return EffortFromBudget(*req.Thinking.BudgetTokens)
		}
		if req.Thinking.Type == "adaptive" {
			return "high"
		}
	}
	return ""
}

func claudeModelCapabilities(model string) claudeCapabilities {
	model = strings.ToLower(model)
	c := claudeCapabilities{manual: true, disable: true}
	switch {
	case strings.HasPrefix(model, "claude-fable-5"), strings.HasPrefix(model, "claude-mythos-5"):
		c.adaptive = true
		c.manual = false
		c.defaultThinking = true
		c.disable = false
		c.strict = true
		c.xhigh = true
		c.max = true
	case strings.HasPrefix(model, "claude-mythos-preview"):
		c.adaptive = true
		c.defaultThinking = true
		c.disable = false
		c.strict = true
		c.max = true
	case strings.HasPrefix(model, "claude-opus-5"), strings.HasPrefix(model, "claude-sonnet-5"), strings.HasPrefix(model, "claude-opus-4-8"), strings.HasPrefix(model, "claude-opus-4-7"):
		c.adaptive = true
		c.manual = false
		c.defaultThinking = strings.HasPrefix(model, "claude-opus-5") || strings.HasPrefix(model, "claude-sonnet-5")
		c.effort = true
		c.xhigh = true
		c.max = true
		c.strict = true
	case strings.HasPrefix(model, "claude-opus-4-6"), strings.HasPrefix(model, "claude-sonnet-4-6"):
		c.adaptive = true
		c.effort = true
		c.max = true
	case strings.HasPrefix(model, "claude-opus-4-5"):
		c.effort = true
	}
	return c
}

func setClaudeEffort(req *dto.ClaudeRequest, effort string) error {
	config := map[string]any{}
	if len(req.OutputConfig) > 0 {
		if err := common.Unmarshal(req.OutputConfig, &config); err != nil {
			return err
		}
		if config == nil {
			config = map[string]any{}
		}
	}
	delete(config, "effort")
	if effort != "" {
		config["effort"] = effort
	}
	req.OutputConfig = nil
	if len(config) > 0 {
		b, err := common.Marshal(config)
		if err != nil {
			return err
		}
		req.OutputConfig = b
	}
	return nil
}

// ApplyClaude is for translated controls. Native requests without a modifier
// never call it, so native display, effort-only, and omission semantics survive.
func ApplyClaude(req *dto.ClaudeRequest, intent Intent) (string, []string, error) {
	c := claudeModelCapabilities(req.Model)
	notes := []string{}
	if c.strict {
		req.Temperature = nil
		req.TopP = nil
		req.TopK = nil
	}
	if intent.Empty() {
		return "", notes, nil
	}
	if intent.Mode == "" && intent.Effort == "" && intent.Budget == nil {
		// Visibility alone must not enable manual thinking on a model which
		// defaults to disabled. New default-adaptive families can express it.
		if c.adaptive && c.defaultThinking && intent.Include != nil {
			req.Thinking = &dto.Thinking{Type: "adaptive", Display: "omitted"}
			if *intent.Include {
				req.Thinking.Display = "summarized"
			}
			return "high", notes, nil
		}
		return "", notes, nil
	}
	effort, err := NormalizeEffort(intent.Effort)
	if err != nil {
		return "", notes, err
	}
	if intent.Mode == "disabled" || effort == "none" || (intent.Budget != nil && *intent.Budget == 0) {
		if c.disable {
			req.Thinking = &dto.Thinking{Type: "disabled"}
			return "none", notes, setClaudeEffort(req, "")
		}
		notes = append(notes, "thinking cannot be disabled for this Claude model; using adaptive thinking")
		intent.Mode = "enabled"
		intent.Budget = nil
		effort = "low"
	}
	if effort == "" && intent.Budget != nil {
		effort = EffortFromBudget(*intent.Budget)
	}
	requestedEffort := effort
	if effort == "minimal" && c.adaptive {
		effort = "low"
	}
	if effort == "xhigh" && !c.xhigh {
		effort = "high"
		if c.max {
			effort = "max"
		}
	}
	if effort == "max" && !c.max {
		effort = "high"
	}
	if effort != requestedEffort {
		notes = append(notes, fmt.Sprintf("reasoning effort %s adjusted to %s", requestedEffort, effort))
	}
	if c.adaptive && (!c.manual || intent.Budget == nil || intent.Mode == "adaptive") {
		if intent.Budget != nil {
			notes = append(notes, "thinking budget converted to adaptive effort")
		}
		if effort == "" {
			effort = "high"
		}
		req.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
		if !c.effort {
			notes = append(notes, "output effort is not supported by this Claude model")
			effort = "high"
		}
		outEffort := ""
		if c.effort {
			outEffort = effort
		}
		if err := setClaudeEffort(req, outEffort); err != nil {
			return "", notes, err
		}
	} else {
		if req.MaxTokens == nil {
			req.MaxTokens = common.GetPointer(uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(req.Model)))
		}
		// A compatibility conversion must not increase a client's output/charge cap.
		if *req.MaxTokens <= 1024 || uint64(*req.MaxTokens) > uint64(math.MaxInt) {
			return "", notes, fmt.Errorf("manual thinking requires 1024 < max_tokens <= platform integer limit")
		}
		var budget int
		if intent.Budget != nil && *intent.Budget >= 0 {
			budget = *intent.Budget
		} else {
			percent := model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage
			switch requestedEffort {
			case "minimal":
				percent = .05
			case "low":
				percent = .2
			case "medium":
				percent = .5
			case "high":
				percent = .8
			case "xhigh", "max":
				percent = .95
			}
			if percent <= 0 || math.IsNaN(percent) || math.IsInf(percent, 0) {
				percent = .8
			}
			if percent > 1 {
				percent = 1
			}
			computed := float64(*req.MaxTokens) * percent
			if computed >= float64(math.MaxInt) {
				budget = int(*req.MaxTokens) - 1
			} else {
				budget = int(computed)
			}
		}
		adjusted := min(max(budget, 1024), int(*req.MaxTokens)-1)
		if adjusted != budget {
			notes = append(notes, "manual thinking budget adjusted to 1024 <= budget_tokens < max_tokens")
		}
		req.Thinking = &dto.Thinking{Type: "enabled", BudgetTokens: &adjusted}
		if effort == "" {
			effort = EffortFromBudget(adjusted)
		}
		outEffort := ""
		if c.effort {
			outEffort = effort
		}
		if err := setClaudeEffort(req, outEffort); err != nil {
			return "", notes, err
		}
	}
	if intent.Include != nil {
		req.Thinking.Display = "omitted"
		if *intent.Include {
			req.Thinking.Display = "summarized"
		}
	}
	if !c.strict {
		req.Temperature = common.GetPointer(1.0)
		req.TopP = nil
		req.TopK = nil
	}
	return effort, notes, nil
}

func ApplyClaudeSuffix(req *dto.ClaudeRequest, origin string) (string, []string, error) {
	if model_setting.ShouldPreserveThinkingSuffix(origin) {
		return "", nil, nil
	}
	base, intent, found, err := ParseSuffix(req.Model, "claude", model_setting.GetClaudeSettings().ThinkingAdapterEnabled)
	if err != nil || !found {
		return "", nil, err
	}
	if req.Thinking != nil || len(req.OutputConfig) > 0 {
		native := Intent{Effort: req.GetEfforts()}
		if req.Thinking != nil {
			native.Mode = req.Thinking.Type
			native.Budget = req.Thinking.BudgetTokens
			if req.Thinking.Display != "" {
				native.Include = common.GetPointer(req.Thinking.Display == "summarized")
			}
		}
		// Native adaptive is an enabled control for conflict comparison.
		if native.Mode == "adaptive" {
			native.Mode = "enabled"
		}
		intent, err = MergeSuffix(native, intent)
		if err != nil {
			return "", nil, err
		}
	}
	req.Model = base
	return ApplyClaude(req, intent)
}

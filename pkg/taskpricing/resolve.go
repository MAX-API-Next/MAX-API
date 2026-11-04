/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskpricing

import (
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/types"
)

const (
	LegacyRateCard = "legacy-rate-card@1"
	LegacyMiniMax  = "legacy-minimax@1"
	LegacyToken    = "legacy-token@1"
	NewAPITask     = billingexpr.TaskExpressionProfile
)

// Source must come from actual host driver selection, never a client hint.
type Source struct {
	Driver    string `json:"driver"`
	PluginKey string `json:"plugin_key,omitempty"`
}

func (s Source) validate() error {
	if s.Driver == "go_adapter" && s.PluginKey == "" {
		return nil
	}
	if s.Driver == "task_plugin" && s.PluginKey != "" && strings.TrimSpace(s.PluginKey) == s.PluginKey && !strings.Contains(s.PluginKey, "::") {
		return nil
	}
	return fmt.Errorf("invalid actual task pricing source")
}

// SelectProfile does not infer migration from price-map presence. The caller
// supplies the existing legacy path and independently verified driver capability.
func SelectProfile(explicit, legacy string, supportsNew bool) (string, error) {
	switch legacy {
	case LegacyRateCard, LegacyMiniMax, LegacyToken:
	default:
		return "", fmt.Errorf("invalid legacy profile")
	}
	if explicit == "" || explicit == legacy {
		return legacy, nil
	}
	if explicit == NewAPITask && supportsNew {
		return NewAPITask, nil
	}
	return "", fmt.Errorf("unsupported task pricing profile transition")
}

type Selection struct {
	Expression string `json:"expression"`
	Model      string `json:"model"`
	PluginKey  string `json:"plugin_key,omitempty"`
}

// SelectExpression resolves exact keys: actual plugin/original, same
// plugin/mapped, original default, mapped default. It selects text; compilation
// and task money semantics still belong to the shared billing core.
func SelectExpression(prices types.TaskPricingMaps, source Source, original, mapped string) (Selection, error) {
	if err := source.validate(); err != nil {
		return Selection{}, err
	}
	if strings.TrimSpace(original) == "" {
		return Selection{}, fmt.Errorf("missing original model")
	}
	models := []string{original}
	if mapped != "" && mapped != original {
		models = append(models, mapped)
	}
	if source.Driver == "task_plugin" {
		for _, model := range models {
			if expr, present := prices.PluginBillingExpr[source.PluginKey+"::"+model]; present {
				return selection(expr, model, source.PluginKey)
			}
		}
	}
	for _, model := range models {
		if prices.BillingMode[model] == "tiered_expr" {
			return selection(prices.BillingExpr[model], model, "")
		}
	}
	return Selection{}, fmt.Errorf("no task expression configured for selected source/models")
}

func selection(expr, model, plugin string) (Selection, error) {
	if strings.TrimSpace(expr) == "" {
		return Selection{}, fmt.Errorf("selected task expression is empty")
	}
	return Selection{Expression: expr, Model: model, PluginKey: plugin}, nil
}

// Guards are equality tokens, not timestamps. Pure comparison is not a DB CAS
// or proof of cache freshness; the future writer must check in its transaction.
type Guards struct{ PriceVersion, ContractDigest, BindingVersion string }

func CheckGuards(expected, current Guards) error {
	if expected.PriceVersion == "" || expected.ContractDigest == "" || expected.BindingVersion == "" || current != expected {
		return fmt.Errorf("stale or missing task pricing context")
	}
	return nil
}

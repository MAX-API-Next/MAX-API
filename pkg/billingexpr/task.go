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

package billingexpr

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// TaskExpressionProfile is an explicit total-currency interpretation. A token
// expression's v1 prefix alone never selects it. References: New API AGPL-3.0
// a2defd2a8256abe039e5295bd583aeb2d02f111f, pkg/billingexpr/{compile,run,settle}.go
// and setting/billing_setting/tiered_billing.go. MAX requires static literal
// usage keys, final presence, a frozen clock and strict overflow rejection.
const TaskExpressionProfile = "new-api-task@1"

// TaskQuoteInput must contain host-validated, normalized scalar facts. Contract
// selection, authoritative configuration reads and persistence belong to the
// host. PricingAt is explicit so a replay does not read a different wall clock.
type TaskQuoteInput struct {
	Profile      string
	Expression   string
	Usage        map[string]any
	Request      RequestInput
	PricingAt    time.Time
	QuotaPerUnit float64
	GroupRatio   float64
}

// TaskQuote is a pure price calculation, not a reservation or ledger mutation.
// Amount is total currency, before group ratio. Quota is rounded exactly once
// after conversion and group ratio; task prices are never divided by a million.
type TaskQuote struct {
	Profile          string    `json:"profile"`
	Amount           float64   `json:"amount"`
	QuotaBeforeGroup float64   `json:"quota_before_group"`
	QuotaAfterGroup  int       `json:"quota_after_group"`
	MatchedTier      string    `json:"matched_tier"`
	PricingAt        time.Time `json:"pricing_at"`
}

var taskTokenVariables = map[string]bool{
	"p": true, "c": true, "len": true, "cr": true, "cc": true, "cc1h": true,
	"img": true, "img_o": true, "ai": true, "ao": true,
}

func taskCompileEnv(base map[string]interface{}) map[string]interface{} {
	prototype := make(map[string]interface{}, len(base)+1)
	for key, value := range base {
		if !taskTokenVariables[key] {
			prototype[key] = value
		}
	}
	prototype["u"] = func(string) any { return nil }
	return prototype
}

// Inspect the original tree, not the optimized program: a hidden branch is
// still part of the expression's schema contract. Indirect u references and
// $env are forbidden so aliasing cannot bypass this static key set.
func inspectTaskExpression(body string) ([]string, error) {
	tree, err := parser.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("task expression parse: %w", err)
	}
	keys := map[string]bool{}
	identifiers, calls := 0, 0
	var invalid error
	ast.Find(tree.Node, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.IdentifierNode:
			if n.Value == "u" {
				identifiers++
			}
			if n.Value == "fixed" || n.Value == "$env" || taskTokenVariables[n.Value] {
				invalid = fmt.Errorf("identifier %q is unsupported in task prices", n.Value)
			}
		case *ast.VariableDeclaratorNode:
			if n.Name == "u" || n.Name == "$env" {
				invalid = fmt.Errorf("task usage accessor cannot be shadowed")
			}
		case *ast.BuiltinNode:
			// Time objects expose methods such as Local() and can bypass the
			// frozen scalar helpers. Inspect before optimization so even a
			// currently unreachable branch cannot introduce an ambient clock.
			switch n.Name {
			case "now", "date", "timezone":
				invalid = fmt.Errorf("builtin %q is unsupported in task prices; use frozen hour/minute/weekday/month/day helpers", n.Name)
			}
		case *ast.CallNode:
			callee, ok := n.Callee.(*ast.IdentifierNode)
			if !ok {
				break
			}
			switch callee.Value {
			case "hour", "minute", "weekday", "month", "day":
				if len(n.Arguments) == 1 {
					if literal, ok := n.Arguments[0].(*ast.StringNode); ok {
						if _, err := taskTimezoneName(literal.Value); err != nil {
							invalid = err
						}
					}
				}
			}
			if callee.Value != "u" {
				break
			}
			calls++
			if len(n.Arguments) != 1 {
				invalid = fmt.Errorf("u() requires one literal usage key")
				break
			}
			literal, ok := n.Arguments[0].(*ast.StringNode)
			if !ok || strings.TrimSpace(literal.Value) == "" {
				invalid = fmt.Errorf("u() requires a nonempty literal usage key")
				break
			}
			keys[literal.Value] = true
		}
		return false
	})
	if invalid != nil {
		return nil, invalid
	}
	if calls != identifiers {
		return nil, fmt.Errorf("u() must be called directly with a literal key")
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	slices.Sort(out)
	return out, nil
}

func taskEntry(expression string) (*cachedEntry, error) {
	// A raw SHA256 used by legacy token snapshots cannot collide with this key.
	return compileEntry(expression, TaskExpressionProfile+":"+ExprHashString(expression), true)
}

// TaskUsageKeys returns an owned, deterministic list, including unreachable
// branches. It compiles syntax but cannot prove a key belongs to a model schema.
func TaskUsageKeys(expression string) ([]string, error) {
	entry, err := taskEntry(expression)
	if err != nil {
		return nil, err
	}
	return slices.Clone(entry.usageKeys), nil
}

func QuoteTaskExpression(input TaskQuoteInput) (TaskQuote, error) {
	if input.Profile != TaskExpressionProfile || input.PricingAt.IsZero() {
		return TaskQuote{}, fmt.Errorf("task quote requires an explicit profile and pricing time")
	}
	if !finite(input.QuotaPerUnit) || input.QuotaPerUnit <= 0 || !finite(input.GroupRatio) || input.GroupRatio < 0 {
		return TaskQuote{}, fmt.Errorf("invalid task quote conversion context")
	}
	// Do not coerce null, missing, strings or boolean into numeric zero. The
	// contract layer normalizes all numeric Go/JSON inputs to float64 first.
	for key, value := range input.Usage {
		switch v := value.(type) {
		case float64:
			if !finite(v) || v < 0 || v > 1<<53-1 {
				return TaskQuote{}, fmt.Errorf("invalid normalized numeric task fact %q", key)
			}
		case bool, string:
		default:
			return TaskQuote{}, fmt.Errorf("invalid normalized task fact %q", key)
		}
	}
	entry, err := taskEntry(input.Expression)
	if err != nil {
		return TaskQuote{}, err
	}
	amount, trace, err := runProgramWithTask(entry.prog, TokenParams{}, input.Request, &input)
	if err != nil {
		return TaskQuote{}, err
	}
	if amount < 0 {
		return TaskQuote{}, fmt.Errorf("task price must be non-negative")
	}
	beforeGroup := amount * input.QuotaPerUnit
	afterGroup := beforeGroup * input.GroupRatio
	if !finite(beforeGroup) || !finite(afterGroup) || math.Round(afterGroup) > common.MaxQuota {
		// Check before common's saturating helper: a rejected preview must not
		// write an audit log or return a plausible clamped amount.
		return TaskQuote{}, fmt.Errorf("task quote exceeds representable quota")
	}
	return TaskQuote{Profile: TaskExpressionProfile, Amount: amount, QuotaBeforeGroup: beforeGroup,
		QuotaAfterGroup: QuotaRound(afterGroup), MatchedTier: trace.MatchedTier, PricingAt: input.PricingAt.UTC()}, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Local resolves through time.Local and therefore is not determined by the
// frozen instant. Check both literal arguments before optimization and computed
// arguments at runtime. Legacy token timezone handling is deliberately separate.
func taskTimezoneName(tz string) (string, error) {
	name := strings.TrimSpace(tz)
	if name == "Local" {
		return "", fmt.Errorf("task time helpers do not support node-local timezone Local; use UTC or an explicit IANA timezone")
	}
	return name, nil
}

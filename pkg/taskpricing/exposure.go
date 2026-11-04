// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

var ErrExposureUnproven = errors.New("task charge upper bound cannot be proved")

const ChargeBoundMethod = "nonnegative-monotone-endpoint@1"

type ExposureInput struct {
	Profile          string
	Expression       string
	Contract         types.TaskFactContract
	Submit           types.TaskPricingFacts
	PricingAt        time.Time
	QuotaPerUnit     float64
	GroupRatio       float64
	ReservationFloor int
}

// ExposureQuote is a charge-domain configuration sample. EndpointFacts are
// synthetic proof witnesses, never provider usage. Neither trusted provider
// cost limits, actual funding nor paid admission are certified by this result.
type ExposureQuote struct {
	Method                   string                 `json:"method"`
	ContractDigest           string                 `json:"contract_digest"`
	Estimate                 billingexpr.TaskQuote  `json:"estimate"`
	Maximum                  billingexpr.TaskQuote  `json:"maximum"`
	RequiredReservationQuota int                    `json:"required_reservation_quota"`
	EndpointFacts            types.TaskPricingFacts `json:"synthetic_endpoint_facts"`
	AdmissionReady           bool                   `json:"admission_ready"`
	ProviderCostProven       bool                   `json:"provider_cost_proven"`
}

// QuoteExposure proves monotonicity of a deliberately small syntax subset and
// evaluates its upper endpoint with the ONE existing billing core. It is not
// an interval-price evaluator or a claim that arbitrary Expr is bounded.
func QuoteExposure(in ExposureInput) (*ExposureQuote, error) {
	if in.Profile != NewAPITask {
		return nil, fmt.Errorf("explicit task expression profile required for a charge upper bound")
	}
	if in.ReservationFloor < 0 || in.ReservationFloor > common.MaxQuota {
		return nil, fmt.Errorf("invalid task reservation floor")
	}
	c, err := NormalizeContract(in.Contract)
	if err != nil {
		return nil, err
	}
	if err := ValidateExpressionContract(in.Expression, c); err != nil {
		return nil, err
	}
	_, body := billingexpr.ParseExprVersion(in.Expression)
	tree, err := parser.Parse(body)
	if err != nil || !monotoneCharge(tree.Node, c) {
		return nil, ErrExposureUnproven
	}
	frozen, err := SubmitFacts(c, in.Submit)
	if err != nil {
		return nil, err
	}
	keys, err := billingexpr.TaskUsageKeys(in.Expression)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	for _, key := range keys {
		referenced[key] = true
	}
	endpoint := types.TaskPricingFacts{}
	for name, p := range c.FieldPolicy {
		if p.Phase == "request" {
			continue
		}
		field := c.UsageSchema[name]
		switch field.Type {
		case "number":
			value := 0.0
			if p.Min != nil {
				value = *p.Min
			}
			if p.Integer {
				value = math.Ceil(value)
			}
			if referenced[name] {
				if p.Max == nil {
					return nil, fmt.Errorf("%w: actual fact %q has no trusted maximum", ErrExposureUnproven, name)
				}
				value = *p.Max
				if p.Integer {
					value = math.Floor(value)
				}
			}
			endpoint[name] = value
		case "boolean":
			endpoint[name] = false
		case "enum":
			endpoint[name] = field.Enum[0]
		}
	}
	endpoint, err = CompleteFacts(c, frozen, endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExposureUnproven, err)
	}
	quote := func(facts types.TaskPricingFacts) (billingexpr.TaskQuote, error) {
		return billingexpr.QuoteTaskExpression(billingexpr.TaskQuoteInput{Profile: in.Profile, Expression: in.Expression,
			Usage: facts, PricingAt: in.PricingAt, QuotaPerUnit: in.QuotaPerUnit, GroupRatio: in.GroupRatio})
	}
	estimate, err := quote(frozen)
	if err != nil {
		return nil, err
	}
	maximum, err := quote(endpoint)
	if err != nil {
		return nil, err
	}
	if maximum.QuotaAfterGroup < estimate.QuotaAfterGroup {
		return nil, ErrExposureUnproven
	}
	digest, err := ContractDigest(c)
	if err != nil {
		return nil, err
	}
	return &ExposureQuote{Method: ChargeBoundMethod, ContractDigest: digest, Estimate: estimate, Maximum: maximum,
		RequiredReservationQuota: reservationQuota(maximum.QuotaAfterGroup, in.GroupRatio, in.ReservationFloor), EndpointFacts: endpoint}, nil
}

// Nonnegative literals/u(number), +, *, division by a positive literal, and
// conditionals depending ONLY on frozen request facts are monotone in actual
// numeric usage. IEEE nonnegative +/* and positive division, followed by the
// shared nonnegative conversion/rounding, preserve that ordering. All other
// syntax fails closed, even when an optimizer could remove a hidden branch.
func monotoneCharge(node ast.Node, c types.TaskFactContract) bool {
	valid, _ := chargeShape(node, c)
	return valid
}

// Every arithmetic node must have a normalized float operand. Pure integer
// arithmetic could wrap before the billing runtime converts the result.
func chargeShape(node ast.Node, c types.TaskFactContract) (valid, floating bool) {
	switch n := node.(type) {
	case *ast.IntegerNode:
		return n.Value >= 0 && n.Value <= 1<<53-1, false
	case *ast.FloatNode:
		return n.Value >= 0 && !math.IsNaN(n.Value) && !math.IsInf(n.Value, 0), true
	case *ast.CallNode:
		key, ok := usageKey(n)
		return ok && c.UsageSchema[key].Type == "number", true
	case *ast.BinaryNode:
		left, leftFloat := chargeShape(n.Left, c)
		right, rightFloat := chargeShape(n.Right, c)
		switch n.Operator {
		case "+", "*":
			return left && right && (leftFloat || rightFloat), true
		case "/":
			positive := false
			switch denominator := n.Right.(type) {
			case *ast.IntegerNode:
				positive = denominator.Value > 0
			case *ast.FloatNode:
				positive = denominator.Value > 0 && !math.IsInf(denominator.Value, 0)
			}
			return positive && left && right && (leftFloat || rightFloat), true
		}
	case *ast.ConditionalNode:
		first, firstFloat := chargeShape(n.Exp1, c)
		second, secondFloat := chargeShape(n.Exp2, c)
		return frozenCondition(n.Cond, c) && first && second, firstFloat && secondFloat
	}
	return false, false
}

func usageKey(n *ast.CallNode) (string, bool) {
	id, ok := n.Callee.(*ast.IdentifierNode)
	if !ok || id.Value != "u" || len(n.Arguments) != 1 {
		return "", false
	}
	key, ok := n.Arguments[0].(*ast.StringNode)
	if !ok {
		return "", false
	}
	return key.Value, true
}

func frozenScalar(node ast.Node, c types.TaskFactContract) bool {
	switch n := node.(type) {
	case *ast.IntegerNode, *ast.FloatNode, *ast.StringNode, *ast.BoolNode:
		return true
	case *ast.CallNode:
		key, ok := usageKey(n)
		return ok && c.FieldPolicy[key].Phase == "request"
	}
	return false
}

func frozenCondition(node ast.Node, c types.TaskFactContract) bool {
	switch n := node.(type) {
	case *ast.BoolNode:
		return true
	case *ast.CallNode:
		key, ok := usageKey(n)
		return ok && c.FieldPolicy[key].Phase == "request" && c.UsageSchema[key].Type == "boolean"
	case *ast.UnaryNode:
		return n.Operator == "!" && frozenCondition(n.Node, c)
	case *ast.BinaryNode:
		switch n.Operator {
		case "&&", "||", "and", "or":
			return frozenCondition(n.Left, c) && frozenCondition(n.Right, c)
		case "==", "!=", ">", ">=", "<", "<=":
			return frozenScalar(n.Left, c) && frozenScalar(n.Right, c)
		}
	}
	return false
}

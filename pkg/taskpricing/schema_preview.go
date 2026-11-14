// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"fmt"
	"math"
	"slices"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
)

// PreviewPluginSchema is a configuration calculator only. It follows New API
// EvaluateTaskCompletionUsage: completion facts override the submitted facts,
// preserving explicit zero; omitted completion fields retain the estimate.
// This does not certify a provider lifecycle, authorize funds, or produce a
// frozen production plan. The existing phase-vetted Preview remains separate.
func PreviewPluginSchema(input PreviewInput, schema map[string]types.TaskUsageField) (PreviewResult, error) {
	out := PreviewResult{Profile: NewAPITask, Source: input.Source}
	if input.Profile != NewAPITask || input.Source.Driver != "task_plugin" || input.ReservationFloor < 0 || input.ReservationFloor > common.MaxQuota {
		return out, fmt.Errorf("plugin configuration sample required")
	}
	selected, err := SelectExpression(input.Prices, input.Source, input.OriginalModel, input.MappedModel)
	if err != nil {
		return out, err
	}
	out.Selection = selected
	keys, err := billingexpr.TaskUsageKeys(selected.Expression)
	if err != nil {
		return out, err
	}
	for _, key := range keys {
		if _, ok := schema[key]; !ok {
			return out, fmt.Errorf("undeclared plugin price fact %q", key)
		}
	}
	check := func(facts types.TaskPricingFacts) error {
		for key, value := range facts {
			field, ok := schema[key]
			if !ok {
				return fmt.Errorf("undeclared plugin fact %q", key)
			}
			if len(field.Enum) > 0 {
				v, ok := value.(string)
				if !ok || !slices.Contains(field.Enum, v) {
					return fmt.Errorf("invalid enum fact %q", key)
				}
				continue
			}
			switch field.Type {
			case "number":
				v, ok := value.(float64)
				if !ok || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
					return fmt.Errorf("invalid numeric fact %q", key)
				}
			case "boolean":
				if _, ok := value.(bool); !ok {
					return fmt.Errorf("invalid boolean fact %q", key)
				}
			default:
				return fmt.Errorf("unsupported plugin fact %q", key)
			}
		}
		return nil
	}
	// Validate the schema even for constant expressions and empty sample maps.
	s := map[string]taskplugin.UsageField{}
	for k, f := range schema {
		kind := f.Type
		if kind == "" && len(f.Enum) > 0 {
			kind = "enum"
		}
		s[k] = taskplugin.UsageField{Type: kind, Unit: f.Unit, Enum: f.Enum}
	}
	if err := taskplugin.ValidateUsageSchema(s); err != nil {
		return out, err
	}
	if err := check(input.Submit); err != nil {
		return out, err
	}
	quote := func(facts types.TaskPricingFacts) (billingexpr.TaskQuote, error) {
		return billingexpr.QuoteTaskExpression(billingexpr.TaskQuoteInput{Profile: NewAPITask, Expression: selected.Expression, Usage: facts, Request: input.Request, PricingAt: input.PricingAt, QuotaPerUnit: input.QuotaPerUnit, GroupRatio: input.GroupRatio})
	}
	out.Estimate, err = quote(input.Submit)
	if err != nil {
		return out, err
	}
	out.ReservedQuota = out.Estimate.QuotaAfterGroup
	if input.GroupRatio > 0 {
		out.ReservedQuota = max(out.ReservedQuota, input.ReservationFloor)
	}
	if input.Actual != nil {
		if err := check(input.Actual); err != nil {
			return out, err
		}
		merged := types.TaskPricingFacts{}
		for k, v := range input.Submit {
			merged[k] = v
		}
		for k, v := range input.Actual {
			merged[k] = v
		}
		final, err := quote(merged)
		if err != nil {
			return out, err
		}
		out.Final = &final
		delta := final.QuotaAfterGroup - out.ReservedQuota
		out.FinalMinusReserved = &delta
	}
	return out, nil
}

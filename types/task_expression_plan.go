// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package types

import "time"

// TaskExpressionPlan carries no credentials, raw request body or live registry
// pointers. Request-priced dimensions are scalar SubmitFacts under Contract.
// The surrounding task private data retains the original funding identity.
type TaskExpressionPlan struct {
	Version           int              `json:"version"`
	OriginalModel     string           `json:"original_model"`
	MappedModel       string           `json:"mapped_model"`
	SelectedModel     string           `json:"selected_model"`
	SelectedPluginKey string           `json:"selected_plugin_key,omitempty"`
	Driver            string           `json:"driver"`
	PluginKey         string           `json:"plugin_key,omitempty"`
	PluginVersion     string           `json:"plugin_version,omitempty"`
	PackageDigest     string           `json:"package_digest,omitempty"`
	ChannelID         int              `json:"channel_id"`
	Action            string           `json:"action"`
	RequestID         string           `json:"request_id"`
	BindingVersion    string           `json:"binding_version"`
	Expression        string           `json:"expression"`
	ExpressionHash    string           `json:"expression_hash"`
	PricingAt         time.Time        `json:"pricing_at"`
	Rounding          string           `json:"rounding"`
	ReservationFloor  int              `json:"reservation_floor"`
	Contract          TaskFactContract `json:"contract"`
	SubmitFacts       TaskPricingFacts `json:"submit_facts"`
}

// TaskPricingUsageEnvelope preserves the plugin's typed dimensions, including
// fractional credit and explicit zero. It does not coerce them to token counts
// or the older canonical TaskUsage fields. Identity is supplied by the host.
type TaskPricingUsageEnvelope struct {
	ProducerKind   string           `json:"producer_kind"`
	SourceID       string           `json:"source_id"`
	PluginVersion  string           `json:"plugin_version,omitempty"`
	PackageDigest  string           `json:"package_digest,omitempty"`
	ChannelID      int              `json:"channel_id"`
	RequestID      string           `json:"request_id"`
	Action         string           `json:"action"`
	SchemaVersion  int              `json:"schema_version"`
	ContractDigest string           `json:"contract_digest"`
	Stage          string           `json:"stage"`
	Completeness   string           `json:"completeness"`
	Facts          TaskPricingFacts `json:"facts"`
}

func CloneTaskPricingFacts(facts TaskPricingFacts) TaskPricingFacts {
	if facts == nil {
		return nil
	}
	cloned := make(TaskPricingFacts, len(facts))
	for key, value := range facts {
		cloned[key] = value // Validated pricing facts contain only scalar values.
	}
	return cloned
}

func CloneTaskExpressionPlan(plan *TaskExpressionPlan) *TaskExpressionPlan {
	if plan == nil {
		return nil
	}
	cloned := *plan
	cloned.SubmitFacts = CloneTaskPricingFacts(plan.SubmitFacts)
	cloned.Contract.UsageSchema = make(map[string]TaskUsageField, len(plan.Contract.UsageSchema))
	for key, field := range plan.Contract.UsageSchema {
		// Labels are not stored in normalized contracts; do not retain their maps.
		field.Enum = append([]string(nil), field.Enum...)
		field.Description, field.UnitLabel, field.EnumLabels = nil, nil, nil
		cloned.Contract.UsageSchema[key] = field
	}
	cloned.Contract.FieldPolicy = make(map[string]TaskFactPolicy, len(plan.Contract.FieldPolicy))
	for key, policy := range plan.Contract.FieldPolicy {
		if policy.Min != nil {
			value := *policy.Min
			policy.Min = &value
		}
		if policy.Max != nil {
			value := *policy.Max
			policy.Max = &value
		}
		cloned.Contract.FieldPolicy[key] = policy
	}
	return &cloned
}

func CloneTaskPricingUsageEnvelope(envelope *TaskPricingUsageEnvelope) *TaskPricingUsageEnvelope {
	if envelope == nil {
		return nil
	}
	cloned := *envelope
	cloned.Facts = CloneTaskPricingFacts(envelope.Facts)
	return &cloned
}

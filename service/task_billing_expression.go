// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

const expressionUsageManualReasonPrefix = "Task expression terminal usage requires manual reconciliation:"

func prepareTaskExpressionTerminalDecision(task *model.Task, result *relaycommon.TaskInfo) taskTerminalBillingDecision {
	decision := taskTerminalBillingDecision{UsesPlan: true, Settlement: buildTaskManualSettlementInput(task)}
	plan := taskBillingPlan(task)
	if err := taskpricing.ValidateSnapshot(plan); err != nil {
		decision.ManualReason = "Task expression frozen plan requires manual reconciliation: " + err.Error()
		return decision
	}
	p := plan.ExpressionPlan
	if task == nil || task.ID <= 0 || task.Quota < plan.ReserveQuota ||
		task.ChannelId != p.ChannelID || taskModelName(task) != p.OriginalModel ||
		task.Properties.UpstreamModelName != p.MappedModel || task.Action != p.Action ||
		task.PrivateData.BillingRequestId != p.RequestID {
		decision.ManualReason = "Task expression task identity or actual reservation conflicts with the frozen plan"
		return decision
	}
	var envelope *types.TaskPricingUsageEnvelope
	if bc := task.PrivateData.BillingContext; bc != nil {
		envelope = types.CloneTaskPricingUsageEnvelope(bc.PricingUsageEnvelope)
	}
	if result != nil {
		candidate := types.CloneTaskPricingUsageEnvelope(result.PricingUsageEnvelope)
		// Only missing evidence can be replaced automatically. Complete, partial,
		// invalid and ambiguous observations remain immutable for reconciliation.
		if envelope == nil {
			envelope = candidate
		} else if envelope.Completeness == types.TaskUsageCompletenessMissing {
			if _, err := taskpricing.QuoteFinal(plan, candidate); err == nil {
				envelope = candidate
			}
		}
	}
	decision.PricingUsageEnvelope = envelope
	quote, err := taskpricing.QuoteFinal(plan, envelope)
	if err != nil {
		decision.ManualReason = expressionUsageManualReasonPrefix + " " + err.Error()
		return decision
	}
	if quote.QuotaAfterGroup > task.Quota {
		decision.ManualReason = "Task expression terminal quota exceeds the actual task reservation"
		return decision
	}
	decision.Settlement = buildTaskExactFinalSettlementInput(task, quote.QuotaAfterGroup, nil, "Task expression actual usage settlement")
	// Project the same frozen price authority used for the charge. Legacy
	// ModelPrice/OtherRatios may belong to a different source for this model;
	// carrying them into the log would explain a different fee.
	other := map[string]any{
		"task_id": task.TaskID, "pre_consumed_quota": task.Quota, "actual_quota": quote.QuotaAfterGroup,
		"group_ratio": plan.GroupRatio, "quota_per_unit": plan.QuotaPerUnit,
		"task_pricing_usage":   types.CloneTaskPricingUsageEnvelope(envelope),
		"task_pricing_profile": plan.Profile, "task_billing_plan": taskBillingPlanLogMetadata(plan),
		"task_expression_hash": p.ExpressionHash, "task_pricing_at": p.PricingAt,
		"task_price_version": plan.ConfigHash, "task_binding_version": p.BindingVersion,
	}
	if p.MappedModel != p.OriginalModel {
		other["is_model_mapped"], other["upstream_model_name"] = true, p.MappedModel
	}
	decision.Settlement.Effect.Other = other
	return decision
}

func taskUsageRecoveryReason(task *model.Task) (string, string) {
	if plan := taskBillingPlan(task); plan != nil && plan.Profile == taskpricing.NewAPITask {
		return expressionUsageManualReasonPrefix, "Task expression settlement recovered after a complete provider usage response"
	}
	return "H3 terminal usage requires manual reconciliation:", "H3 task settlement automatically recovered after a complete provider usage response"
}

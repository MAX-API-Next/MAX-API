// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

// ApplyApprovedTaskPluginFacts adapts only the exact reviewed package/policy
// persisted on the task. It verifies raw completion JSON before reusing the
// existing typed-envelope path; no legacy token conversion or fund write occurs.
// The caller owns provider/task association. No public/JS runtime calls it yet.
func ApplyApprovedTaskPluginFacts(ctx context.Context, catalog *taskplugin.Catalog, registry *taskpricing.PhaseRegistry, task *model.Task,
	actor taskplugin.Actor, result *relaycommon.TaskInfo, facts types.TaskPricingFacts, raw []byte) error {
	if result == nil {
		return taskplugin.ErrBindingRejected
	}
	session, err := ResolveTaskPricingPluginSession(ctx, catalog, task, actor)
	if err != nil {
		return err
	}
	pkg, err := session.PricingPackage()
	if err != nil {
		return err
	}
	binding := session.Binding()
	approved, err := registry.Resolve(pkg, binding.UpstreamModel, binding.Action)
	if err != nil || binding.PhaseApprovalID != approved.ApprovalID || approved.ContractDigest != binding.ContractDigest {
		return taskplugin.ErrBindingRejected
	}
	projected, err := approved.ProjectFinal(task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan.SubmitFacts, facts, raw)
	if err != nil {
		return err
	}
	return applyTaskPricingPluginFacts(ctx, catalog, task, actor, result, projected.Facts, approved.ApprovalID)
}

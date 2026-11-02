// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"
	"slices"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

func pricingActorMatches(task *model.Task, actor taskplugin.Actor) bool {
	return task != nil && actor.UserID > 0 && actor.TokenID > 0 && actor.ChannelID > 0 && actor.RequestID != "" &&
		actor.UserID == task.UserId && actor.TokenID == task.PrivateData.TokenId && actor.ChannelID == task.ChannelId && actor.RequestID == task.PrivateData.BillingRequestId
}

// The caller supplies a bounded service-owned lifecycle context, not a raw
// client HTTP context. Refresh failure suspends Catalog's Host by its existing
// policy; no stale-generation fallback or provider retry is introduced here.
func refreshTaskPricingCatalog(ctx context.Context, catalog *taskplugin.Catalog) error {
	if ctx == nil || catalog == nil {
		return taskplugin.ErrCatalogUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return catalog.Refresh(bounded)
}

// BindTaskPricingPlugin pins the actual route-selected package before metadata
// is attached to the caller's task. It does not create/persist the task, freeze
// a price or authorize paid admission. The host must already have vetted the
// frozen contract for this exact package; a matching schema alone is not proof.
func BindTaskPricingPlugin(ctx context.Context, catalog *taskplugin.Catalog, task *model.Task, actor taskplugin.Actor, method, path string) error {
	if !pricingActorMatches(task, actor) || task.PrivateData.BillingContext == nil {
		return taskplugin.ErrBindingRejected
	}
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	if taskpricing.ValidateSnapshot(plan) != nil || plan.ExpressionPlan.Driver != types.TaskUsageProducerKindTaskPlugin {
		return taskplugin.ErrBindingRejected
	}
	if err := refreshTaskPricingCatalog(ctx, catalog); err != nil {
		return err
	}
	p := plan.ExpressionPlan
	binding, err := catalog.Host().BindPricingMapped(actor, method, path, p.OriginalModel, p.MappedModel,
		taskplugin.PricingContractIdentity{Action: p.Action, SchemaVersion: plan.UsageSchemaVersion, ContractDigest: plan.UsageContractDigest})
	if err != nil {
		return err
	}
	return AttachTaskPluginBinding(catalog.Host(), task, binding)
}

// ResolveTaskPricingPluginSession restores only the persisted package identity.
// Disable blocks new binds while history remains usable; revoke/missing history
// fails closed. No latest-version lookup or Go fallback can replace an old task.
func ResolveTaskPricingPluginSession(ctx context.Context, catalog *taskplugin.Catalog, task *model.Task, actor taskplugin.Actor) (*taskplugin.Session, error) {
	if !pricingActorMatches(task, actor) || task.ID <= 0 || task.PrivateData.PluginBinding == nil ||
		task.PrivateData.PluginBinding.Profile != taskpricing.NewAPITask || !taskPluginBindingMatches(task, *task.PrivateData.PluginBinding) {
		return nil, taskplugin.ErrBindingRejected
	}
	if err := refreshTaskPricingCatalog(ctx, catalog); err != nil {
		return nil, err
	}
	session, err := catalog.Host().Resolve(*task.PrivateData.PluginBinding, actor)
	if err != nil || !taskPricingPluginSchemaMatches(session, task.PrivateData.BillingContext.TaskBillingPlan) {
		return nil, taskplugin.ErrBindingRejected
	}
	return session, nil
}

func taskPricingPluginSchemaMatches(session *taskplugin.Session, plan *types.TaskBillingPlan) bool {
	if session == nil || plan == nil || plan.ExpressionPlan == nil {
		return false
	}
	contract, err := taskpricing.NormalizeContract(plan.ExpressionPlan.Contract)
	if err != nil {
		return false
	}
	schema := session.PricingSchema()
	if len(schema) != len(contract.UsageSchema) {
		return false
	}
	for key, field := range contract.UsageSchema {
		actual, found := schema[key]
		kind := actual.Type
		if kind == "" && len(actual.Enum) > 0 {
			kind = "enum"
		}
		enum := slices.Clone(actual.Enum)
		slices.Sort(enum)
		if !found || kind != field.Type || actual.Unit != field.Unit || !slices.Equal(enum, field.Enum) {
			return false
		}
	}
	return true
}

// ApplyTaskPricingPluginFacts attaches typed completion evidence only. Financial
// decisions remain in the existing polling/settlement path, using the frozen
// expression. Fractional credit and explicit zero never become token integers.
func ApplyTaskPricingPluginFacts(ctx context.Context, catalog *taskplugin.Catalog, task *model.Task, actor taskplugin.Actor, result *relaycommon.TaskInfo, facts types.TaskPricingFacts) error {
	return applyTaskPricingPluginFacts(ctx, catalog, task, actor, result, facts, "")
}

// A binding with a reviewed projection cannot bypass its raw-evidence adapter
// through the older typed-facts entrypoint.
func applyTaskPricingPluginFacts(ctx context.Context, catalog *taskplugin.Catalog, task *model.Task, actor taskplugin.Actor, result *relaycommon.TaskInfo, facts types.TaskPricingFacts, approvalID string) error {
	if result == nil {
		return taskplugin.ErrBindingRejected
	}
	session, err := ResolveTaskPricingPluginSession(ctx, catalog, task, actor)
	if err != nil {
		return err
	}
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	normalized, err := taskpricing.CompleteFacts(plan.ExpressionPlan.Contract, plan.ExpressionPlan.SubmitFacts, facts)
	if err != nil {
		return err
	}
	binding := session.Binding()
	if binding.PhaseApprovalID != approvalID {
		return taskplugin.ErrBindingRejected
	}
	envelope := &types.TaskPricingUsageEnvelope{ProducerKind: types.TaskUsageProducerKindTaskPlugin, SourceID: binding.SourceID,
		PluginVersion: binding.Version, PackageDigest: binding.PackageDigest, ChannelID: binding.ChannelID,
		RequestID: binding.RequestID, Action: binding.Action, SchemaVersion: binding.SchemaVersion, ContractDigest: binding.ContractDigest,
		Stage: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete, Facts: normalized}
	// Revalidate after normalization in case this Host was concurrently revoked
	// or suspended. This is still an observation, not an atomic execution permit.
	if _, err := catalog.Host().Resolve(binding, actor); err != nil {
		return err
	}
	result.PricingUsageEnvelope = envelope
	return nil
}

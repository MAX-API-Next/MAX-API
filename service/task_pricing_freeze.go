// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Lifecycle reference: QuantumNous/new-api c046559b relay/relay_task.go
// and service/task_billing.go. MAX retains its own money/identity contracts.
package service

import (
	"context"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
)

type TaskPricingFreezeGuards struct {
	Pricing      taskpricing.Guards
	TermsVersion string
}

// TaskPricingPluginPlan is a detached, configured-group preparation. It is not
// an atomic configuration/funding commit, a user-specific ratio resolution, or
// an execution lease. No production route consumes it in this slice.
type TaskPricingPluginPlan struct {
	Plan           *types.TaskBillingPlan
	Binding        types.TaskPluginBinding
	AdmissionReady bool
}

// FreezeApprovedTaskPricingPluginPlan rereads configured prices, terms, channel
// mapping and the exact active package before accepting an earlier observation.
// It creates no task/reservation and never accepts draft prices or caller caps.
func FreezeApprovedTaskPricingPluginPlan(ctx context.Context, catalog *taskplugin.Catalog, registry *taskpricing.PhaseRegistry,
	request model.TaskPricingShadowRequest, actor taskplugin.Actor, method, path, mapped, action string,
	expected TaskPricingFreezeGuards, submit types.TaskPricingFacts) (*TaskPricingPluginPlan, error) {
	observed, err := ReadApprovedTaskPricingPluginContext(ctx, catalog, registry, request, actor, method, path, mapped, action)
	if err != nil {
		return nil, err
	}
	s := observed.Shadow
	if expected.TermsVersion == "" || expected.TermsVersion != s.TermsVersion ||
		taskpricing.CheckGuards(expected.Pricing, s.Guards) != nil {
		return nil, model.ErrModelPricingConflict
	}
	// A transient view of the authority's selected rule, not a second config
	// store. The shared selector/freezer still verifies exact source/model keys.
	prices := types.TaskPricingMaps{}
	if s.Selection.PluginKey != "" {
		prices.PluginBillingExpr = map[string]string{s.Selection.PluginKey + "::" + s.Selection.Model: s.Selection.Expression}
	} else {
		prices.BillingMode = map[string]string{s.Selection.Model: "tiered_expr"}
		prices.BillingExpr = map[string]string{s.Selection.Model: s.Selection.Expression}
	}
	plan, err := taskpricing.FreezeBounded(taskpricing.FreezeInput{
		PreviewInput: taskpricing.PreviewInput{
			Profile: s.Profile, Prices: prices, Source: s.Source, OriginalModel: s.OriginalModel, MappedModel: s.MappedModel,
			Contract: s.Contract, Expected: expected.Pricing, Current: s.Guards, Submit: submit,
			PricingAt: s.PricingAt, QuotaPerUnit: s.QuotaPerUnit, GroupRatio: s.GroupRatio, ReservationFloor: s.ReservationFloor,
		},
		ChannelID: s.ChannelID, Action: observed.Binding.Action, RequestID: observed.Binding.RequestID,
		SourceID: observed.Binding.SourceID, PluginVersion: observed.Binding.Version, PackageDigest: observed.Binding.PackageDigest,
	})
	if err != nil {
		return nil, err
	}
	return &TaskPricingPluginPlan{Plan: plan, Binding: observed.Binding}, nil
}

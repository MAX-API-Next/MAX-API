// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
)

type TaskPricingPluginContext struct {
	Shadow  *model.TaskPricingShadowContext
	Binding types.TaskPluginBinding
}

// ReadApprovedTaskPricingPluginContext composes actual route selection with
// exact-package host policy and the authoritative configured-price transaction.
// Inputs are host-owned routing/authentication metadata. No public route calls
// this seam. It does not execute JS, persist a task or authorize a reservation.
func ReadApprovedTaskPricingPluginContext(ctx context.Context, catalog *taskplugin.Catalog, registry *taskpricing.PhaseRegistry,
	request model.TaskPricingShadowRequest, actor taskplugin.Actor, method, path, mapped, action string) (*TaskPricingPluginContext, error) {
	if actor.ChannelID != request.ChannelID {
		return nil, taskplugin.ErrBindingRejected
	}
	if err := refreshTaskPricingCatalog(ctx, catalog); err != nil {
		return nil, err
	}
	binding, _, err := registry.Bind(catalog.Host(), actor, method, path, request.ModelName, mapped, action)
	if err != nil {
		return nil, err
	}
	shadow, err := model.ReadTaskPricingPluginShadowContext(ctx, request, binding, registry)
	if err != nil {
		return nil, err
	}
	// A concurrent local revoke/suspension cannot be silently accepted. This
	// remains an observation, not a distributed CAS or execution lease.
	if _, err := catalog.Host().Resolve(binding, actor); err != nil {
		return nil, err
	}
	return &TaskPricingPluginContext{Shadow: shadow, Binding: binding}, nil
}

// QuoteApprovedTaskPricingPluginExposure reads a fresh context for each sample;
// callers cannot quote using a stale returned price/contract or inject terms.
// The resulting required reservation is informational. Existing Freeze and
// funding remain unchanged; all production task-expression admission is closed.
func QuoteApprovedTaskPricingPluginExposure(ctx context.Context, catalog *taskplugin.Catalog, registry *taskpricing.PhaseRegistry,
	request model.TaskPricingShadowRequest, actor taskplugin.Actor, method, path, mapped, action string, submit types.TaskPricingFacts) (*TaskPricingPluginContext, *taskpricing.ExposureQuote, error) {
	observed, err := ReadApprovedTaskPricingPluginContext(ctx, catalog, registry, request, actor, method, path, mapped, action)
	if err != nil {
		return nil, nil, err
	}
	s := observed.Shadow
	quote, err := taskpricing.QuoteExposure(taskpricing.ExposureInput{Profile: s.Profile, Expression: s.Selection.Expression, Contract: s.Contract, Submit: submit,
		PricingAt: s.PricingAt, QuotaPerUnit: s.QuotaPerUnit, GroupRatio: s.GroupRatio, ReservationFloor: s.ReservationFloor})
	if err != nil {
		return nil, nil, err
	}
	return observed, quote, nil
}

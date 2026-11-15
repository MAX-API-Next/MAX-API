// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
)

// ReadTaskPricingPluginShadowContext is a trusted host seam, not an HTTP DTO.
// Its binding must be issued by actual route selection. The transaction then
// independently verifies current configured mapping/group and exact active DB
// package/policy. A supplied binding alone is never an execution/admission proof.
func ReadTaskPricingPluginShadowContext(ctx context.Context, request TaskPricingShadowRequest, binding types.TaskPluginBinding, registry *taskpricing.PhaseRegistry) (*TaskPricingShadowContext, error) {
	return readTaskPricingShadowWithPlugin(DB, ctx, request, time.Now().UTC(), &binding, registry)
}

func approvedPluginShadow(a *pricingAuthority, request TaskPricingShadowRequest, binding types.TaskPluginBinding, registry *taskpricing.PhaseRegistry) (ModelPricingContext, string, error) {
	rejected := func() (ModelPricingContext, string, error) {
		return ModelPricingContext{}, "", fmt.Errorf("plugin pricing has no matching configured binding and vetted package contract")
	}
	if binding.Profile != taskpricing.NewAPITask || binding.UserID <= 0 || binding.TokenID <= 0 || binding.RequestID == "" ||
		strings.TrimSpace(binding.RequestID) != binding.RequestID || binding.Action == "" || strings.TrimSpace(binding.Action) != binding.Action ||
		binding.ChannelID != request.ChannelID || binding.Model != request.ModelName || binding.SourceID != binding.PluginKey {
		return rejected()
	}
	selected, found := findPricingContext(a.contexts(request.ModelName), fmt.Sprintf("channel:%d", request.ChannelID))
	channel, valid := selected.binding.(pricingChannel)
	if !found || !valid || channel.Status != 1 || !slices.Contains(strings.Split(channel.Models, ","), request.ModelName) ||
		selected.Profile != taskpricing.NewAPITask || !slices.Contains(selected.Groups, request.Group) {
		return rejected()
	}
	// A malformed mapping must not masquerade as an unmapped model.
	mapping := map[string]string{}
	if channel.ModelMapping != nil && *channel.ModelMapping != "" && common.UnmarshalJsonStr(*channel.ModelMapping, &mapping) != nil {
		return rejected()
	}
	if selected.MappedModel != binding.UpstreamModel {
		return rejected()
	}
	for _, entry := range a.plugins {
		if !entry.Active || !entry.Enabled || entry.Revoked || entry.Digest != binding.PackageDigest {
			continue
		}
		pkg, err := taskplugin.LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
		if err != nil {
			return rejected()
		}
		m := pkg.Manifest()
		if m.Key != binding.PluginKey || m.Version != binding.Version {
			continue
		}
		approved, err := registry.Resolve(pkg, binding.UpstreamModel, binding.Action)
		if err != nil || approved.ContractDigest != binding.ContractDigest || approved.Contract.Version != binding.SchemaVersion || binding.PhaseApprovalID != approved.ApprovalID {
			return rejected()
		}
		selected.Contract, selected.Schema = &approved.Contract, approved.Contract.UsageSchema
		selected.PreviewOnly = false // Only the exact vetted phase proof exits sample-only scope.
		selected.source = taskpricing.Source{Driver: types.TaskUsageProducerKindTaskPlugin, PluginKey: m.Key}
		selected.PluginKey, selected.Available, selected.Reason = m.Key, true, ""
		return selected, approved.ApprovalID, nil
	}
	return rejected()
}

// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"encoding/hex"
	"strings"

	"github.com/MAX-API-Next/MAX-API/types"
)

// PricingContractIdentity is supplied by the trusted host after it validates
// the frozen fact contract. Host pins code identity; it neither vets phase
// semantics from matching schemas nor grants paid execution from this metadata.
type PricingContractIdentity struct {
	Action         string
	SchemaVersion  int
	ContractDigest string
}

// BindPricingMapped pins the package actually selected by one Host generation.
// No client/plugin can choose the resulting key, version, digest or source ID.
// References: New API ea343c4a pkg/jsplugin/registry.go generation selection;
// MAX retains immutable package history and its independent billing authority.
func (host *Host) BindPricingMapped(actor Actor, method, path, model, upstreamModel string, contract PricingContractIdentity) (types.TaskPluginBinding, error) {
	if host == nil || !validActor(actor) || model == "" || strings.TrimSpace(model) != model {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	p, err := host.PricingPackage(method, path, upstreamModel)
	if err != nil {
		return types.TaskPluginBinding{}, err
	}
	binding := types.TaskPluginBinding{Profile: types.TaskPluginExpressionProfile, Action: contract.Action,
		PluginKey: p.manifest.Key, Version: p.manifest.Version, PackageDigest: p.digest, SourceID: p.manifest.Key,
		SchemaVersion: contract.SchemaVersion, ContractDigest: contract.ContractDigest,
		Model: model, UpstreamModel: upstreamModel, UserID: actor.UserID, TokenID: actor.TokenID, ChannelID: actor.ChannelID, RequestID: actor.RequestID}
	if !validPricingBinding(binding) {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	return binding, nil
}

// PricingPackage observes the actual submit-route package in one generation.
// Its immutable bytes can be checked against host approvals; this observation
// is not an execution permit. BindPricingMapped must still pin and recheck it.
func (host *Host) PricingPackage(method, path, upstreamModel string) (*Package, error) {
	if host == nil {
		return nil, ErrBindingRejected
	}
	state := host.state.Load()
	if state == nil {
		return nil, ErrBindingRejected
	}
	match, ok := state.routes.MatchRoute(method, path, upstreamModel)
	if !ok || match.Route.Type != "submit" {
		return nil, ErrBindingRejected
	}
	p := state.active[match.PluginKey]
	if p == nil || state.revoked[packageIdentity(p.manifest.Key, p.manifest.Version)] || !declaresModel(p.manifest, upstreamModel) {
		return nil, ErrBindingRejected
	}
	return p, nil
}

func validPricingBinding(binding types.TaskPluginBinding) bool {
	if binding.PhaseApprovalID != "" {
		approval, err := hex.DecodeString(binding.PhaseApprovalID)
		if err != nil || len(approval) != 32 {
			return false
		}
	}
	digest, err := hex.DecodeString(binding.ContractDigest)
	return binding.SourceID == binding.PluginKey && binding.SchemaVersion == 1 && err == nil && len(digest) == 32 &&
		binding.Action != "" && strings.TrimSpace(binding.Action) == binding.Action
}

// PricingPackage returns the immutable original package after local session
// revalidation. It never resolves the currently active replacement version.
func (session *Session) PricingPackage() (*Package, error) {
	if session == nil || session.binding.Profile != types.TaskPluginExpressionProfile {
		return nil, ErrBindingRejected
	}
	if err := session.validate(); err != nil {
		return nil, err
	}
	return session.plugin, nil
}

// PricingSchema returns detached manifest metadata from the pinned package,
// not the currently active package or a claim of a vetted fact phase contract.
func (session *Session) PricingSchema() map[string]UsageField {
	if session == nil || session.plugin == nil || session.binding.Profile != types.TaskPluginExpressionProfile {
		return nil
	}
	schema, _ := session.plugin.ModelUsageSchema(session.binding.UpstreamModel)
	return schema
}

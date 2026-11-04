// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
)

// PhaseApproval is trusted host policy, NOT an admin/client/plugin DTO. Evidence
// cites the review establishing phase/bounds for these exact bytes and model.
// Constructors cannot prove that review happened. No production approvals are
// installed here; familiar names/schemas and estimates confer no approval.
// Reference: New API d0abbf6e plugins/tasks/{hailuo,kling,doubao}/plugin.js
// distinguishes submit estimates from completion facts; MAX owns phase policy.
type PhaseApproval struct {
	Projection                                       *FactProjection `json:"projection,omitempty"`
	PluginKey, Version, PackageDigest, Model, Action string
	Contract                                         types.TaskFactContract
	Evidence                                         string
}

type ApprovedPhaseContract struct {
	Projection                 FactProjection
	Contract                   types.TaskFactContract
	ContractDigest, ApprovalID string
}

// PhaseRegistry is an immutable exact-package policy table, not price storage.
type PhaseRegistry struct {
	approvals map[string]ApprovedPhaseContract
}

func phaseKey(key, version, digest, model, action string) string {
	return key + "\x00" + version + "\x00" + digest + "\x00" + model + "\x00" + action
}

func NewPhaseRegistry(entries []PhaseApproval) (*PhaseRegistry, error) {
	r := &PhaseRegistry{approvals: map[string]ApprovedPhaseContract{}}
	for _, entry := range entries {
		for _, value := range []string{entry.PluginKey, entry.Version, entry.Model, entry.Action, entry.Evidence} {
			if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, 0) {
				return nil, fmt.Errorf("invalid host phase approval identity/evidence")
			}
		}
		decoded, err := hex.DecodeString(entry.PackageDigest)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != entry.PackageDigest {
			return nil, fmt.Errorf("invalid host phase package digest")
		}
		contract, err := NormalizeContract(entry.Contract)
		if err != nil {
			return nil, err
		}
		entry.Contract = contract
		projection := FactProjection{}
		if entry.Projection != nil {
			projection, err = normalizeProjection(contract, *entry.Projection)
			if err != nil {
				return nil, err
			}
			entry.Projection = &projection
		}
		key := phaseKey(entry.PluginKey, entry.Version, entry.PackageDigest, entry.Model, entry.Action)
		if _, duplicate := r.approvals[key]; duplicate {
			return nil, fmt.Errorf("duplicate host phase approval")
		}
		digest, err := ContractDigest(contract)
		if err != nil {
			return nil, err
		}
		wire, err := common.Marshal(entry)
		if err != nil {
			return nil, err
		}
		id := sha256.Sum256(wire)
		r.approvals[key] = ApprovedPhaseContract{Contract: contract, Projection: projection, ContractDigest: digest, ApprovalID: hex.EncodeToString(id[:])}
	}
	return r, nil
}

// Resolve validates the actual loaded package, not a key supplied as proof.
func (r *PhaseRegistry) Resolve(pkg *taskplugin.Package, model, action string) (ApprovedPhaseContract, error) {
	if r == nil || pkg == nil {
		return ApprovedPhaseContract{}, fmt.Errorf("no vetted host phase contract")
	}
	m := pkg.Manifest()
	approved, ok := r.approvals[phaseKey(m.Key, m.Version, pkg.Digest(), model, action)]
	schema, schemaErr := pkg.ModelUsageSchema(model)
	if !ok || !slices.Contains(m.Models, model) || schemaErr != nil || !MatchesPluginSchema(schema, approved.Contract) {
		return ApprovedPhaseContract{}, fmt.Errorf("package/model has no matching vetted host phase contract")
	}
	// Normalize copies maps, enums and bound pointers; callers cannot rewrite policy.
	approved.Contract, _ = NormalizeContract(approved.Contract)
	approved.Projection, _ = normalizeProjection(approved.Contract, approved.Projection)
	return approved, nil
}

func MatchesPluginSchema(schema map[string]taskplugin.UsageField, contract types.TaskFactContract) bool {
	contract, err := NormalizeContract(contract)
	if err != nil || len(schema) != len(contract.UsageSchema) {
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

// Bind verifies the route-selected package then rechecks the identity issued by
// Host. An upgrade between observation and binding cannot inherit approval.
func (r *PhaseRegistry) Bind(host *taskplugin.Host, actor taskplugin.Actor, method, path, original, mapped, action string) (types.TaskPluginBinding, ApprovedPhaseContract, error) {
	pkg, err := host.PricingPackage(method, path, mapped)
	if err != nil {
		return types.TaskPluginBinding{}, ApprovedPhaseContract{}, err
	}
	approved, err := r.Resolve(pkg, mapped, action)
	if err != nil {
		return types.TaskPluginBinding{}, ApprovedPhaseContract{}, err
	}
	binding, err := host.BindPricingMapped(actor, method, path, original, mapped, taskplugin.PricingContractIdentity{
		Action: action, SchemaVersion: approved.Contract.Version, ContractDigest: approved.ContractDigest})
	if err != nil || binding.PackageDigest != pkg.Digest() || binding.Version != pkg.Manifest().Version || binding.PluginKey != pkg.Manifest().Key {
		return types.TaskPluginBinding{}, ApprovedPhaseContract{}, taskplugin.ErrBindingRejected
	}
	binding.PhaseApprovalID = approved.ApprovalID
	return binding, approved, nil
}

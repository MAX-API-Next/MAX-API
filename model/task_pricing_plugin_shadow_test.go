// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func shadowPhasePackage(t *testing.T, version, source string) *taskplugin.Package {
	t.Helper()
	doc := taskplugin.PackageDocument{FormatVersion: 1, Source: source, Manifest: taskplugin.Manifest{APIVersion: 1,
		Key: "kling", Name: "Synthetic phase fixture", Version: version, Models: []string{"kling-v1"},
		Routes:      []taskplugin.Route{{Method: "POST", Path: "/phase/jobs", Type: "submit", Decode: "submit", Render: "created"}},
		UsageSchema: map[string]taskplugin.UsageField{"units": {Type: "number", Unit: "credit"}}}}
	wire, err := common.Marshal(doc)
	require.NoError(t, err)
	p, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	return p
}

func shadowPhaseRegistry(t *testing.T, pkg *taskplugin.Package, c types.TaskFactContract, evidence string) *taskpricing.PhaseRegistry {
	t.Helper()
	r, err := taskpricing.NewPhaseRegistry([]taskpricing.PhaseApproval{{PluginKey: "kling", Version: pkg.Manifest().Version, PackageDigest: pkg.Digest(),
		Model: "kling-v1", Action: "generate", Contract: c, Evidence: evidence}})
	require.NoError(t, err)
	return r
}

func TestTaskPricingApprovedPluginShadowUsesDBAndExactPolicy(t *testing.T) {
	db := setupModelPricingTest(t)
	request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "alias", `u("units") * 99`)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", request.ChannelID).Update("model_mapping", `{"alias":"kling-v1"}`).Error)
	require.NoError(t, db.Create(&Option{Key: "billing_setting.plugin_billing_expr", Value: `{"kling::alias":"u(\"units\") * 0.1"}`}).Error)
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	pkg := shadowPhasePackage(t, "1.0.0", "synthetic opaque phase package")
	// The concrete profile is narrower than the bundle fallback. Both the
	// management projection and approved shadow must use this mapped model.
	m := pkg.Manifest()
	m.UsageProfiles = []taskplugin.UsageProfile{{Models: m.Models, Schema: m.UsageSchema}}
	m.UsageSchema = map[string]taskplugin.UsageField{"units": {Type: "number", Unit: "credit"}, "fallback_only": {Type: "number", Unit: "count"}}
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 2, Manifest: m, Source: "synthetic exact-profile fixture"})
	require.NoError(t, err)
	pkg, err = taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	snapshot, err := getModelPricingSnapshot(db, []string{"kling-v1"})
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 1)
	pluginContext, found := findPricingContext(snapshot.Entries[0].Contexts, "plugin:kling")
	require.True(t, found)
	require.Len(t, pluginContext.Schema, 1)
	require.Equal(t, "credit", pluginContext.Schema["units"].Unit)
	c, _ := taskpricing.KnownContract("kling", "kling-v1")
	cap := 8.0 // fixture-only upper limit, not a provider claim
	p := c.FieldPolicy["units"]
	p.Max = &cap
	c.FieldPolicy["units"] = p
	r := shadowPhaseRegistry(t, pkg, c, "fixture approval1; no real JS review")
	actor := taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: request.ChannelID, RequestID: "approved-shadow"}
	binding, approved, err := r.Bind(catalog.Host(), actor, "POST", "/phase/jobs", "alias", "kling-v1", "generate")
	require.NoError(t, err)
	// A stale node cache must not supply this sample price.
	require.NoError(t, updateOptionMap("billing_setting.plugin_billing_expr", `{"kling::alias":"999"}`))
	got, err := ReadTaskPricingPluginShadowContext(t.Context(), request, binding, r)
	require.NoError(t, err)
	require.Equal(t, taskpricing.Source{Driver: "task_plugin", PluginKey: "kling"}, got.Source)
	require.Equal(t, `u("units") * 0.1`, got.Selection.Expression)
	require.Equal(t, "kling", got.Selection.PluginKey)
	require.Equal(t, pkg.Digest(), got.PackageDigest)
	require.Equal(t, approved.ApprovalID, got.PhaseApprovalID)
	require.False(t, got.AdmissionReady)
	quote, err := taskpricing.QuoteExposure(taskpricing.ExposureInput{Profile: got.Profile, Expression: got.Selection.Expression, Contract: got.Contract,
		Submit: types.TaskPricingFacts{"units": 6}, PricingAt: got.PricingAt, QuotaPerUnit: got.QuotaPerUnit, GroupRatio: got.GroupRatio, ReservationFloor: got.ReservationFloor})
	require.NoError(t, err)
	require.Equal(t, 1200, quote.Maximum.QuotaAfterGroup) // 8*.1*1000*1.5
	// Identical price/schema but a different explicit host review changes binding guard.
	r2 := shadowPhaseRegistry(t, pkg, c, "fixture approval2")
	_, err = ReadTaskPricingPluginShadowContext(t.Context(), request, binding, r2)
	require.Error(t, err, "an old binding cannot acquire a replacement review")
	secondBinding, _, err := r2.Bind(catalog.Host(), actor, "POST", "/phase/jobs", "alias", "kling-v1", "generate")
	require.NoError(t, err)
	second, err := ReadTaskPricingPluginShadowContext(t.Context(), request, secondBinding, r2)
	require.NoError(t, err)
	require.Equal(t, got.Guards.PriceVersion, second.Guards.PriceVersion)
	require.NotEqual(t, got.Guards.BindingVersion, second.Guards.BindingVersion)
	_, err = ReadTaskPricingPluginShadowContext(t.Context(), request, binding, nil)
	require.Error(t, err)
	// A phase-only policy change still invalidates the old binding's contract.
	p.Phase = "request"
	c.FieldPolicy["units"] = p
	phaseChanged := shadowPhaseRegistry(t, pkg, c, "fixture phase changed")
	_, err = ReadTaskPricingPluginShadowContext(t.Context(), request, binding, phaseChanged)
	require.Error(t, err)
	// Current database activation wins even if the caller kept an old Host.
	secondPkg := shadowPhasePackage(t, "2.0.0", "upgraded same schema")
	require.NoError(t, catalog.Install(t.Context(), secondPkg))
	_, err = ReadTaskPricingPluginShadowContext(t.Context(), request, binding, r)
	require.Error(t, err)
}

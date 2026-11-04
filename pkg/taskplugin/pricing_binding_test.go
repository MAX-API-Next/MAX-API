// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestPricingBindingPinsHistoryAndSeparatesLegacy(t *testing.T) {
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	manifest := validManifest()
	first := testPackage(t, manifest, "synthetic first package")
	require.NoError(t, catalog.Install(t.Context(), first))
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "pricing-binding"}
	identity := PricingContractIdentity{Action: "generate", SchemaVersion: 1, ContractDigest: strings.Repeat("b", 64)}
	binding, err := catalog.Host().BindPricingMapped(actor, "POST", "/vendor/v1/jobs", "alias", "vendor-model", identity)
	require.NoError(t, err)
	require.Equal(t, types.TaskPluginExpressionProfile, binding.Profile)
	require.Equal(t, first.Digest(), binding.PackageDigest)
	require.Equal(t, "vendor", binding.SourceID)
	_, err = catalog.Host().BindPricingMapped(actor, "GET", "/vendor/v1/jobs/id", "alias", "vendor-model", identity)
	require.ErrorIs(t, err, ErrBindingRejected, "query routes must not create a submit binding")
	wire, err := common.Marshal(binding)
	require.NoError(t, err)
	var restored types.TaskPluginBinding
	require.NoError(t, common.Unmarshal(wire, &restored))
	manifest.Version = "2.0.0"
	require.NoError(t, catalog.Install(t.Context(), testPackage(t, manifest, "second package")))
	restarted, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	session, err := restarted.Host().Resolve(restored, actor)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", session.Binding().Version)
	schema := session.PricingSchema()
	schema["duration"] = UsageField{Type: "number", Unit: "token"}
	require.Equal(t, "second", session.PricingSchema()["duration"].Unit)
	_, err = session.UsageEnvelope(&types.TaskUsage{})
	require.ErrorIs(t, err, ErrBindingRejected)
	for _, mutate := range []func(*types.TaskPluginBinding){
		func(b *types.TaskPluginBinding) { b.Profile = "unknown" },
		func(b *types.TaskPluginBinding) { b.Profile = "" },
		func(b *types.TaskPluginBinding) { b.SourceID = "other" },
		func(b *types.TaskPluginBinding) { b.ContractDigest = "not-a-digest" },
		func(b *types.TaskPluginBinding) { b.PackageDigest = strings.Repeat("a", 64) },
		func(b *types.TaskPluginBinding) { b.RequestID = "another-task" },
	} {
		invalid := restored
		mutate(&invalid)
		_, err := restarted.Host().Resolve(invalid, actor)
		require.ErrorIs(t, err, ErrBindingRejected)
	}
	require.NoError(t, catalog.SetEnabled(t.Context(), manifest.Key, false))
	require.NoError(t, restarted.Refresh(t.Context()))
	_, err = restarted.Host().BindPricingMapped(actor, "POST", "/vendor/v1/jobs", "alias", "vendor-model", identity)
	require.ErrorIs(t, err, ErrBindingRejected)
	_, err = restarted.Host().Resolve(restored, actor)
	require.NoError(t, err, "disable blocks new bindings but retains old history")
	require.NoError(t, catalog.Revoke(t.Context(), restored.PluginKey, restored.Version))
	require.NoError(t, restarted.Refresh(t.Context()))
	_, err = restarted.Host().Resolve(restored, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	missing := NewHost()
	_, err = missing.Resolve(restored, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
}

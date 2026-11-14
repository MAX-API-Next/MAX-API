// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/plugins"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestOfficialPluginSavedPriceAndConfigurationQuotes(t *testing.T) {
	db := setupModelPricingTest(t)
	require.NoError(t, db.Create(&Option{Key: "PreConsumedQuota", Value: "0"}).Error)
	source, err := plugins.TaskSources.ReadFile("tasks/kling/1.2.0/plugin.js")
	require.NoError(t, err)
	p, err := taskplugin.LoadJavaScript(bytes.NewReader(source), "f4b949679dba31e287d2b21d4b3b4c09980c9bb744da9ede5153f1801cf84240")
	require.NoError(t, err)
	_, err = NewTaskPluginCatalogStore(db).CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{{Document: p.Document(), Digest: p.Digest(), Active: true, Enabled: true}})
	require.NoError(t, err)
	entry := pricingEntryForTest(t, db, "kling-v2-5-turbo")
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{billing_setting.PluginBillingExprOption: map[string]any{"kling": `u("units") * 0.1`}})})
	require.NoError(t, err)
	entry = pricingEntryForTest(t, db, entry.ModelName)
	require.True(t, entry.PluginVariants[0].PreviewOnly)
	require.False(t, entry.PluginVariants[0].ExecutionReady)
	for _, tc := range []struct {
		name          string
		actual        types.TaskPricingFacts
		target, delta int
	}{
		{"refund", types.TaskPricingFacts{"units": 0.6}, 60, -90},
		{"supplement", types.TaskPricingFacts{"units": 1.8}, 180, 30},
		{"explicit zero", types.TaskPricingFacts{"units": 0.0}, 0, -150},
		{"completion field omitted", types.TaskPricingFacts{}, 150, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quote, err := QuoteModelPricing(ModelPricingQuoteRequest{ModelName: entry.ModelName, ContextID: "plugin:kling", ExpectedVersion: entry.Version, ExpectedBindingVersion: entry.BindingVersion, ExpectedContractDigest: entry.ContractDigest, Group: "default", Submit: types.TaskPricingFacts{"units": 1.5}, Actual: tc.actual})
			require.NoError(t, err)
			// Independent oracle: credits * USD .1 * 1000 quota/USD, rounded once.
			require.Equal(t, 150, quote.Preview.ReservedQuota)
			require.NotNil(t, quote.Preview.Final)
			require.Equal(t, tc.target, quote.Preview.Final.QuotaAfterGroup)
			require.Equal(t, tc.delta, *quote.Preview.FinalMinusReserved)
		})
	}
	// Explicit zero is a configured override, not a request to inherit a price.
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{billing_setting.PluginBillingExprOption: map[string]any{"kling": "0"}})})
	require.NoError(t, err)
	after := pricingEntryForTest(t, db, entry.ModelName)
	require.Equal(t, "0", *after.PluginVariants[0].Effective)
	_, err = QuoteModelPricing(ModelPricingQuoteRequest{ModelName: entry.ModelName, ContextID: "plugin:kling", ExpectedVersion: entry.Version, ExpectedBindingVersion: entry.BindingVersion, ExpectedContractDigest: entry.ContractDigest, Group: "default", Submit: types.TaskPricingFacts{"units": 1.5}})
	require.ErrorIs(t, err, ErrModelPricingConflict, "stale price cannot quote after a successful update")
	require.False(t, db.Migrator().HasTable(&BillingSettlement{}), "configuration samples must not initialize or write a ledger")
}

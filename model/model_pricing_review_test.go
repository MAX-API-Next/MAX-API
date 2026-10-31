// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestModelPricingReviewLegacyTieredCannotCreateTaskPrice(t *testing.T) {
	db := setupModelPricingTest(t)
	pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	err := UpdateLegacyTieredPricing(map[string]LegacyTieredPricingItem{"kling-v1": {Enabled: true, Expr: `u("units") * 0.1`}})
	require.ErrorIs(t, err, ErrModelPricingLegacyWrite)
	var rows int64
	require.NoError(t, db.Model(&Option{}).Count(&rows).Error)
	require.Zero(t, rows, "reject before committing either prices or the coordination row")
	require.NoError(t, UpdateLegacyTieredPricing(map[string]LegacyTieredPricingItem{"ordinary": {Enabled: true, Expr: `p * 2`}}))
	require.Equal(t, `p * 2`, rawPricingObjectForTest(t, db, "billing_setting.billing_expr")["ordinary"])
}

func TestModelPricingReviewPluginNamespacedModelRoundTrip(t *testing.T) {
	db := setupModelPricingTest(t)
	name := "foo::bar"
	document := taskplugin.PackageDocument{FormatVersion: 1, Source: "synthetic; never executed", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "kling", Name: "Synthetic", Version: "1.0.0", Models: []string{name}, BaseURL: "https://example.com", AllowedHosts: []string{"example.com"}, UsageSchema: map[string]taskplugin.UsageField{"units": {Type: "number", Unit: "credit"}}}}
	data, err := common.Marshal(document)
	require.NoError(t, err)
	_, err = NewTaskPluginCatalogStore(db).CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{{Document: data, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Active: true, Enabled: true}})
	require.NoError(t, err)
	entry := pricingEntryForTest(t, db, name)
	key := billing_setting.PluginBillingExprOption
	for _, expression := range []string{"0", `u("units") * 0.1`} {
		result, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{key: map[string]any{"kling": expression}})})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"kling": expression}, result.Entries[0].Configured[key])
		require.Equal(t, expression, rawPricingObjectForTest(t, db, key)["kling::foo::bar"])
		entry = pricingEntryForTest(t, db, name)
		require.Equal(t, map[string]any{"kling": expression}, entry.Configured[key])
		snapshot, err := GetModelPricingSnapshot(nil)
		require.NoError(t, err, "a successful save must not break the complete snapshot")
		found := false
		for _, item := range snapshot.Entries {
			if item.ModelName == name {
				found = true
				require.NotNil(t, item.PluginVariants[0].Configured)
				require.Equal(t, expression, *item.PluginVariants[0].Configured)
			}
		}
		require.True(t, found)
	}
	err = UpdateOption(key, `{}`)
	require.ErrorIs(t, err, ErrModelPricingLegacyWrite, "the parsed model remains protected from unversioned deletion")
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{})})
	require.NoError(t, err)
	require.Empty(t, rawPricingObjectForTest(t, db, key), "normal PATCH removes the exact composite key")
	_, err = GetModelPricingSnapshot(nil)
	require.NoError(t, err)
}

func TestModelPricingReviewMixedSourcesKeepIsolatedPluginOverride(t *testing.T) {
	db := setupModelPricingTest(t)
	installPricingPluginForTest(t, db, "1.0.0", "credit", nil, 0)
	legacy := pricingChannelForTest(t, db, constant.ChannelTypeOpenAI, "kling-v1")
	require.NoError(t, db.Model(&legacy).Update("status", 2).Error)
	require.NoError(t, UpdateLegacyTieredPricing(map[string]LegacyTieredPricingItem{"kling-v1": {Enabled: true, Expr: `p * 2`}}))
	entry := pricingEntryForTest(t, db, "kling-v1")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`})})
	require.ErrorContains(t, err, "legacy channel", "a disabled channel still prevents an unsafe shared default")
	draft := types.ModelPricingValues{}
	for key, value := range entry.Configured {
		draft[key] = value
	}
	draft[billing_setting.PluginBillingExprOption] = map[string]any{"kling": `u("units") * 0.1`}
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, draft)})
	require.NoError(t, err)
	require.Equal(t, `p * 2`, rawPricingObjectForTest(t, db, "billing_setting.billing_expr")["kling-v1"])
	expression, ok := billing_setting.GetBillingExpr("kling-v1")
	require.True(t, ok)
	require.Equal(t, `p * 2`, expression)
	require.Equal(t, `u("units") * 0.1`, rawPricingObjectForTest(t, db, billing_setting.PluginBillingExprOption)["kling::kling-v1"])
}

func TestModelPricingReviewMappedTargetIsNotDirectLegacyConsumer(t *testing.T) {
	db := setupModelPricingTest(t)
	task := pricingChannelForTest(t, db, constant.ChannelTypeKling, "task-alias")
	require.NoError(t, db.Model(&task).Update("model_mapping", `{"task-alias":"kling-v1"}`).Error)
	chat := pricingChannelForTest(t, db, constant.ChannelTypeOpenAI, "chat-alias")
	require.NoError(t, db.Model(&chat).Update("model_mapping", `{"chat-alias":"kling-v1"}`).Error)
	target := pricingEntryForTest(t, db, "kling-v1")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(target, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`})})
	require.NoError(t, err, "legacy token lookup uses the advertised original model, not this referenced target")
	entry := pricingEntryForTest(t, db, "task-alias")
	quote, err := QuoteModelPricing(ModelPricingQuoteRequest{ModelName: "task-alias", ContextID: fmt.Sprintf("channel:%d", task.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Submit: types.TaskPricingFacts{"units": 1.0}, Actual: types.TaskPricingFacts{"units": 0.6}})
	require.NoError(t, err)
	require.Equal(t, 60, quote.Preview.Final.QuotaAfterGroup, "0.6 credit * USD0.1 * QPU1000, group1")
	require.Equal(t, 500, quote.Preview.ReservedQuota)
	_, exists := billing_setting.GetBillingExpr("chat-alias")
	require.False(t, exists, "do not publish the target expression as the legacy alias default")
}

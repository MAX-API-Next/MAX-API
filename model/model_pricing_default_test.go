// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"fmt"
	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestModelPricingPluginDefaultSchemaAndUnitInheritance(t *testing.T) {
	db := setupModelPricingTest(t)
	packages := installPricingPluginForTest(t, db, "1.0.0", "credit", nil, 0)
	entry := pricingEntryForTest(t, db, "kling-v1")
	draft := types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, draft)})
	require.NoError(t, err, "a declared schema permits configuration; it does not grant quote phases")
	entry = pricingEntryForTest(t, db, "kling-v1")
	require.True(t, entry.PluginVariants[0].SchemaCompatible)
	require.False(t, entry.PluginVariants[0].ExecutionReady)
	_, err = QuoteModelPricing(ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: "plugin:kling", ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Submit: types.TaskPricingFacts{"units": 1.0}})
	require.Error(t, err)
	pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	installPricingPluginForTest(t, db, "1.0.1", "second", packages, 1)
	entry = pricingEntryForTest(t, db, "kling-v1")
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, draft)})
	require.ErrorContains(t, err, "incompatible")
	withOverride := types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`, billing_setting.PluginBillingExprOption: map[string]any{"kling": `u("units") * 0.2`}}
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, withOverride)})
	require.NoError(t, err)
	entry = pricingEntryForTest(t, db, "kling-v1")
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, draft)})
	require.Error(t, err, "removing the explicit override must not silently inherit a different unit")
	require.Equal(t, `u("units") * 0.2`, rawPricingObjectForTest(t, db, billing_setting.PluginBillingExprOption)["kling::kling-v1"])
}

func TestModelPricingAuthoritativeTermsAndVolcengineSource(t *testing.T) {
	db := setupModelPricingTest(t)
	oldGroup := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroup)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":99}`)) // deliberately stale node cache
	require.NoError(t, db.Create(&Option{Key: "GroupRatio", Value: `{"vip":2}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "PreConsumedQuota", Value: "1200"}).Error)
	channel := pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	require.NoError(t, db.Model(&channel).Update("group", "vip").Error)
	entry := pricingEntryForTest(t, db, "kling-v1")
	quote, err := QuoteModelPricing(ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: fmt.Sprintf("channel:%d", channel.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "vip", Pricing: types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}, Submit: types.TaskPricingFacts{"units": 1.0}, Actual: types.TaskPricingFacts{"units": 0.6}})
	require.NoError(t, err)
	require.Equal(t, float64(2), quote.GroupRatio)
	require.Equal(t, 1200, quote.Preview.ReservedQuota)
	require.Equal(t, 120, quote.Preview.Final.QuotaAfterGroup)
	model := "doubao-seedance-1-5-pro-251215"
	channel = pricingChannelForTest(t, db, constant.ChannelTypeVolcEngine, model)
	require.NoError(t, db.Model(&channel).Update("group", "vip").Error)
	entry = pricingEntryForTest(t, db, model)
	quote, err = QuoteModelPricing(ModelPricingQuoteRequest{ModelName: model, ContextID: fmt.Sprintf("channel:%d", channel.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "vip", Pricing: types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("tokens") / 1000000 * (u("generate_audio") ? 4 : 2)`}, Submit: types.TaskPricingFacts{"tokens": 4000000.0, "resolution": "720p", "generate_audio": false}, Actual: types.TaskPricingFacts{"tokens": 2000000.0}})
	require.NoError(t, err)
	require.Equal(t, 8000, quote.Preview.Final.QuotaAfterGroup)
	require.Equal(t, common.QuotaPerUnit, quote.QuotaPerUnit)
}

func TestModelPricingWriteLimitMatchesAuthorityReadLimit(t *testing.T) {
	db := setupModelPricingTest(t)
	raw := `{"dormant":"` + strings.Repeat(" ", 2*1024*1024) + `1"}`
	err := UpdateOption("billing_setting.billing_expr", raw)
	require.Error(t, err, "do not commit an option the authority reader will reject")
	var count int64
	require.NoError(t, db.Model(&Option{}).Count(&count).Error)
	require.Zero(t, count)
}

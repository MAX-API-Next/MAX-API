// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"fmt"
	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestModelPricingExplicitTaskConstantRejectsMixedLegacyDefault(t *testing.T) {
	for _, expression := range []string{"0", "0.5"} {
		t.Run(expression, func(t *testing.T) {
			db := setupModelPricingTest(t)
			task := pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
			setting := `{"task_pricing_profile":"` + taskpricing.NewAPITask + `"}`
			require.NoError(t, db.Model(&task).Update("setting", setting).Error)
			pricingChannelForTest(t, db, constant.ChannelTypeOpenAI, "kling-v1")
			entry := pricingEntryForTest(t, db, "kling-v1")
			// A dormant constant has no active task-money meaning yet.
			_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "ratio", "billing_setting.billing_expr": expression})})
			require.NoError(t, err)
			entry = pricingEntryForTest(t, db, "kling-v1")
			var before []Option
			require.NoError(t, db.Order("key").Find(&before).Error)
			_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression})})
			require.ErrorContains(t, err, "task default conflicts with legacy channel")
			var after []Option
			require.NoError(t, db.Order("key").Find(&after).Error)
			require.Equal(t, before, after)
		})
	}
}
func TestModelPricingKnownContractDoesNotMigrateLegacyConstant(t *testing.T) {
	db := setupModelPricingTest(t)
	pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	pricingChannelForTest(t, db, constant.ChannelTypeOpenAI, "kling-v1")
	for _, expression := range []string{"0.5", "0"} {
		require.NoError(t, UpdateLegacyTieredPricing(map[string]LegacyTieredPricingItem{"kling-v1": {Enabled: true, Expr: expression}}))
		entry := pricingEntryForTest(t, db, "kling-v1")
		require.Equal(t, expression, entry.Configured["billing_setting.billing_expr"])
	}
}
func TestModelPricingExplicitTaskConstantKeepsLegacyWritesProtected(t *testing.T) {
	db := setupModelPricingTest(t)
	oldUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() { common.QuotaPerUnit = oldUnit })
	task := pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	setting := `{"task_pricing_profile":"` + taskpricing.NewAPITask + `"}`
	require.NoError(t, db.Model(&task).Update("setting", setting).Error)
	require.NoError(t, db.Create(&Option{Key: "GroupRatio", Value: `{"default":1}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "PreConsumedQuota", Value: "0"}).Error)
	for _, tc := range []struct {
		expression string
		quota      int
	}{{"0", 0}, {"0.5", 500}} {
		entry := pricingEntryForTest(t, db, "kling-v1")
		_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": tc.expression})})
		require.NoError(t, err)
		require.ErrorIs(t, UpdateOption("ModelPrice", `{"kling-v1":7}`), ErrModelPricingLegacyWrite)
		require.ErrorIs(t, UpdateLegacyTieredPricing(map[string]LegacyTieredPricingItem{}), ErrModelPricingLegacyWrite)
		entry = pricingEntryForTest(t, db, "kling-v1")
		quote, err := QuoteModelPricing(ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: fmt.Sprintf("channel:%d", task.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Submit: types.TaskPricingFacts{"units": 1.0}})
		require.NoError(t, err)
		require.Equal(t, tc.quota, quote.Preview.Estimate.QuotaAfterGroup)
		require.False(t, quote.AdmissionReady)
	}
}

// Publish after the final terms query, when the old code has already sampled
// the unit for that digest but still rereads it for the response metadata.
func TestModelPricingQuoteUnitSurvivesLatePublication(t *testing.T) {
	db := setupModelPricingTest(t)
	channel := pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	require.NoError(t, db.Model(&channel).Update("setting", "{\"task_pricing_profile\":\"new-api-task@1\"}").Error)
	entry := pricingEntryForTest(t, db, "kling-v1")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "u(\"units\") * 0.5"})})
	require.NoError(t, err)
	require.NoError(t, db.Create(&Option{Key: "GroupRatio", Value: "{\"default\":1}"}).Error)
	require.NoError(t, db.Create(&Option{Key: "PreConsumedQuota", Value: "0"}).Error)
	entry = pricingEntryForTest(t, db, "kling-v1")
	termsReads := 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("quote_unit_publish", func(tx *gorm.DB) {
		if !strings.Contains(fmt.Sprint(tx.Statement.Vars), "PreConsumedQuota") {
			return
		}
		termsReads++
		if termsReads == 2 {
			require.NoError(t, updateOptionMap("QuotaPerUnit", "2000"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("quote_unit_publish") })
	request := ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: fmt.Sprintf("channel:%d", channel.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Submit: types.TaskPricingFacts{"units": 1.0}}
	quote, err := QuoteModelPricing(request)
	require.NoError(t, err)
	require.Equal(t, 2, termsReads, "publication must execute at the intended query boundary")
	require.Equal(t, float64(2000), common.QuotaPerUnit)
	require.Equal(t, float64(1000), quote.QuotaPerUnit, "one sample must describe both the quote and its metadata")
	require.Equal(t, 500, quote.Preview.Estimate.QuotaAfterGroup, "1 unit * 0.5 USD * captured1000 * group1 = 500")
	require.False(t, quote.AdmissionReady)
	// A malformed node setting is an authority fault, not rejected user input.
	require.NoError(t, updateOptionMap("QuotaPerUnit", "NaN"))
	invalid, err := QuoteModelPricing(request)
	require.Nil(t, invalid)
	require.ErrorContains(t, err, "invalid node quota unit")
	require.NotErrorIs(t, err, ErrModelPricingValidation)
}

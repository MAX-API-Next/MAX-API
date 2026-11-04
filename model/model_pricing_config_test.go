// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func setupModelPricingTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pricing-test.sqlite")+"?_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &TaskPluginCatalogHead{}, &TaskPluginVersion{}))
	oldDB := DB
	DB = db
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	cache := map[string]string{}
	for key, values := range map[string]any{"ModelPrice": ratio_setting.GetModelPriceCopy(), "ModelRatio": ratio_setting.GetModelRatioCopy(), "CompletionRatio": ratio_setting.GetCompletionRatioCopy(), "CacheRatio": ratio_setting.GetCacheRatioCopy(), "CreateCacheRatio": ratio_setting.GetCreateCacheRatioCopy(), "ImageRatio": ratio_setting.GetImageRatioCopy(), "AudioRatio": ratio_setting.GetAudioRatioCopy(), "AudioCompletionRatio": ratio_setting.GetAudioCompletionRatioCopy(), "billing_setting.billing_mode": billing_setting.GetBillingModeCopy(), "billing_setting.billing_expr": billing_setting.GetBillingExprCopy(), billing_setting.PluginBillingExprOption: billing_setting.GetPluginBillingExprCopy()} {
		encoded, _ := common.Marshal(values)
		cache[key] = string(encoded)
		common.OptionMap[key] = string(encoded)
	}
	common.OptionMap["SystemName"] = "synthetic"
	common.OptionMap["GroupRatio"] = "{}"
	oldQPU := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() {
		for key, value := range cache {
			require.NoError(t, updateOptionMap(key, value))
		}
		DB = oldDB
		common.QuotaPerUnit = oldQPU
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return db
}
func pricingEntryForTest(t *testing.T, db *gorm.DB, name string) ModelPricingEntry {
	t.Helper()
	snapshot, err := getModelPricingSnapshot(db, []string{name})
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 1)
	return snapshot.Entries[0]
}
func pricingChangeForTest(entry ModelPricingEntry, values types.ModelPricingValues) types.ModelPricingChange {
	return types.ModelPricingChange{ModelName: entry.ModelName, ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Pricing: values}
}

func TestModelPricingLegacyFullMapReturnsOnlyChangedEntries(t *testing.T) {
	setupModelPricingTest(t)
	_, err := updateModelPricingOptions(map[string]string{"ModelPrice": `{"unchanged":2,"changed":3}`})
	require.NoError(t, err)
	result, err := updateModelPricingOptions(map[string]string{"ModelPrice": `{"unchanged":2,"changed":0,"added":4}`})
	require.NoError(t, err)
	names := []string{}
	for _, entry := range result.Entries {
		names = append(names, entry.ModelName)
	}
	require.Equal(t, []string{"added", "changed"}, names)
	result, err = updateModelPricingOptions(map[string]string{"ModelPrice": `{"unchanged":2,"changed":0,"added":4}`})
	require.NoError(t, err)
	require.Empty(t, result.Entries, "an unchanged full-map save needs no entry projection")
}

func TestModelPricingLegacyTieredReturnsOnlyChangedEntries(t *testing.T) {
	setupModelPricingTest(t)
	config := map[string]LegacyTieredPricingItem{
		"unchanged": {Enabled: true, Expr: "p"},
		"changed":   {Enabled: true, Expr: "p * 2"},
	}
	_, err := updateLegacyTieredPricing(config)
	require.NoError(t, err)
	config["changed"] = LegacyTieredPricingItem{Enabled: true, Expr: "p * 3"}
	result, err := updateLegacyTieredPricing(config)
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Equal(t, "changed", result.Entries[0].ModelName)
	result, err = updateLegacyTieredPricing(config)
	require.NoError(t, err)
	require.Empty(t, result.Entries)
	delete(config, "changed")
	result, err = updateLegacyTieredPricing(config)
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Equal(t, "changed", result.Entries[0].ModelName)
}

func TestModelPricingBillingMapsRejectNonStringsBeforeCommit(t *testing.T) {
	for _, key := range []string{"billing_setting.billing_mode", "billing_setting.billing_expr", billing_setting.PluginBillingExprOption} {
		for _, raw := range []string{`{"sample":1}`, `{"sample":null}`, `[]`} {
			t.Run(key+raw, func(t *testing.T) {
				db := setupModelPricingTest(t)
				err := UpdateOption(key, raw)
				require.Error(t, err)
				var count int64
				require.NoError(t, db.Model(&Option{}).Where(commonKeyCol+" = ?", key).Count(&count).Error)
				require.Zero(t, count, "invalid string maps must not commit even if cache publication would reject them")
			})
		}
	}
}

func TestModelPricingInvalidBillingEntryIsVisibleAndRepairable(t *testing.T) {
	db := setupModelPricingTest(t)
	require.NoError(t, db.Create(&Option{Key: "billing_setting.billing_expr", Value: `{"broken":1,"healthy":"p"}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "billing_setting.billing_mode", Value: `{"healthy":"tiered_expr"}`}).Error)
	snapshot, err := GetModelPricingSnapshot([]string{"broken", "healthy"})
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 2)
	require.NotEmpty(t, snapshot.Entries[0].ConfigurationError)
	require.Empty(t, snapshot.Entries[1].ConfigurationError)
	require.NotContains(t, snapshot.Entries[0].Configured, "billing_setting.billing_expr", "wrong-typed fields cannot break the strict read DTO")
	require.Equal(t, `{"broken":1,"healthy":"p"}`, snapshot.Options["billing_setting.billing_expr"])
	require.NoError(t, UpdateOption("billing_setting.billing_expr", `{"broken":"p * 2","healthy":"p"}`))
	require.Empty(t, pricingEntryForTest(t, db, "broken").ConfigurationError)
}

func TestModelPricingRepairDoesNotCommitOrPublishUnrelatedInvalidBillingRow(t *testing.T) {
	db := setupModelPricingTest(t)
	require.NoError(t, db.Create(&Option{Key: "billing_setting.billing_expr", Value: `{"repair":1}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "billing_setting.billing_mode", Value: `{"other":false}`}).Error)
	entry := pricingEntryForTest(t, db, "repair")
	result, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_expr": "p"})})
	require.NoError(t, err)
	require.True(t, result.Committed)
	require.False(t, result.CachePublished, "unrelated corrupt runtime map still requires repair; never claim full publication")
	require.Equal(t, "p", rawPricingObjectForTest(t, db, "billing_setting.billing_expr")["repair"])
	require.Equal(t, false, rawPricingObjectForTest(t, db, "billing_setting.billing_mode")["other"])
	require.Empty(t, pricingEntryForTest(t, db, "repair").ConfigurationError)
	require.NotEmpty(t, pricingEntryForTest(t, db, "other").ConfigurationError)
}

func TestModelPricingMalformedPluginValuesKeepRawAuthorityAndRejectQuotes(t *testing.T) {
	db := setupModelPricingTest(t)
	channel := pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	require.NoError(t, db.Create(&Option{Key: billing_setting.PluginBillingExprOption, Value: `{"bad::kling-v1":1,"stale::kling-v1":"0"}`}).Error)
	entry := pricingEntryForTest(t, db, "kling-v1")
	require.NotEmpty(t, entry.ConfigurationError)
	require.Equal(t, map[string]any{"stale": "0"}, entry.Configured[billing_setting.PluginBillingExprOption])
	require.Equal(t, float64(1), rawPricingObjectForTest(t, db, billing_setting.PluginBillingExprOption)["bad::kling-v1"])
	_, err := QuoteModelPricing(ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: fmt.Sprintf("channel:%d", channel.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Submit: types.TaskPricingFacts{"units": 1.0}})
	require.Error(t, err, "projection sanitization cannot make corrupt authority quotable")
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`})})
	require.NoError(t, err)
	after := pricingEntryForTest(t, db, "kling-v1")
	require.NotEqual(t, entry.Version, after.Version, "guards must cover raw corrupt values, not only typed projection")
	require.Empty(t, after.ConfigurationError)
}

func TestModelPricingPublicationCannotActivateAnUnpublishedExpression(t *testing.T) {
	setupModelPricingTest(t)
	require.NoError(t, updateOptionMap("billing_setting.billing_expr", `{"affected":"2"}`))
	require.NoError(t, updateOptionMap("billing_setting.billing_mode", `{"affected":"ratio"}`))
	err := publishModelPricingOptions(map[string]string{
		"billing_setting.billing_expr": `{"affected":"9","corrupt":1}`,
		"billing_setting.billing_mode": `{"affected":"tiered_expr"}`,
	})
	require.Error(t, err)
	require.Equal(t, "ratio", billing_setting.GetBillingModeCopy()["affected"], "must not activate the old cached expression 2 when the intended expression 9 failed publication")
	require.Equal(t, "2", billing_setting.GetBillingExprCopy()["affected"])
}
func rawPricingObjectForTest(t *testing.T, db *gorm.DB, key string) map[string]any {
	t.Helper()
	var row Option
	require.NoError(t, db.Where(commonKeyCol+" = ?", key).First(&row).Error)
	var values map[string]any
	require.NoError(t, common.UnmarshalJsonStr(row.Value, &values))
	return values
}
func pricingChannelForTest(t *testing.T, db *gorm.DB, kind int, name string) Channel {
	t.Helper()
	channel := Channel{Type: kind, Status: 1, Name: "synthetic pricing fixture", Key: "synthetic-not-a-provider-key", Models: name, Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	return channel
}

func TestModelPricingZeroAtomicBatchAndAuthoritativeRead(t *testing.T) {
	db := setupModelPricingTest(t)
	entry := pricingEntryForTest(t, db, "sample-a")
	result, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(0)})})
	require.NoError(t, err)
	require.True(t, result.Committed)
	require.True(t, result.CachePublished)
	require.Equal(t, float64(0), rawPricingObjectForTest(t, db, "ModelPrice")["sample-a"])
	common.OptionMapRWMutex.Lock()
	common.OptionMap["ModelPrice"] = `{"sample-a":999}`
	common.OptionMapRWMutex.Unlock()
	entry = pricingEntryForTest(t, db, "sample-a")
	require.Equal(t, float64(0), entry.Configured["ModelPrice"])
	b := pricingEntryForTest(t, db, "sample-b")
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(3)}), pricingChangeForTest(b, types.ModelPricingValues{"ModelPrice": float64(-1)})})
	require.Error(t, err)
	prices := rawPricingObjectForTest(t, db, "ModelPrice")
	require.Equal(t, float64(0), prices["sample-a"])
	_, exists := prices["sample-b"]
	require.False(t, exists)
	// A lost response is recoverable by DB read; replaying the old baseline conflicts.
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(2)})})
	require.NoError(t, err)
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(7)})})
	require.ErrorIs(t, err, ErrModelPricingConflict)
	require.Equal(t, float64(2), rawPricingObjectForTest(t, db, "ModelPrice")["sample-a"])
}

func TestModelPricingFailedFirstWriteLeavesNoRowsAndUpdateFaultRollsBack(t *testing.T) {
	db := setupModelPricingTest(t)
	entry := pricingEntryForTest(t, db, "sample")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": nil})})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&Option{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("pricing_fixture_fail", func(tx *gorm.DB) {
		if option, ok := tx.Statement.Dest.(*Option); ok && option.Key == "ModelRatio" {
			tx.AddError(errors.New("synthetic write failure"))
		}
	}))
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(1), "ModelRatio": float64(2)})})
	require.Error(t, err)
	require.NoError(t, db.Callback().Create().Remove("pricing_fixture_fail"))
	require.NoError(t, db.Model(&Option{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestModelPricingTaskProtectionAndLegacyReadInsideTransaction(t *testing.T) {
	db := setupModelPricingTest(t)
	pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	entry := pricingEntryForTest(t, db, "kling-v1")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`})})
	require.NoError(t, err)
	err = UpdateOption("billing_setting.billing_expr", `{"kling-v1":"0"}`)
	require.ErrorIs(t, err, ErrModelPricingLegacyWrite)
	err = UpdateOptionsBulk(map[string]string{"ModelPrice": `{"kling-v1":7}`, "SystemName": "must-not-write"})
	require.ErrorIs(t, err, ErrModelPricingLegacyWrite)
	var count int64
	require.NoError(t, db.Model(&Option{}).Where(commonKeyCol+" = ?", "SystemName").Count(&count).Error)
	require.Zero(t, count)
	require.Equal(t, `u("units") * 0.1`, rawPricingObjectForTest(t, db, "billing_setting.billing_expr")["kling-v1"])
	// Independent legacy models remain editable and cannot erase task prices.
	require.NoError(t, UpdateOption("ModelPrice", `{"ordinary":0}`))
	common.OptionMapRWMutex.Lock()
	common.OptionMap["billing_setting.billing_mode"] = `{"stale-cache":"tiered_expr"}`
	common.OptionMapRWMutex.Unlock()
	err = UpdateLegacyTieredPricing(map[string]LegacyTieredPricingItem{"ordinary": {Enabled: true, Expr: "p"}})
	require.ErrorIs(t, err, ErrModelPricingLegacyWrite) // full legacy save would erase protected kling
}

func TestModelPricingIndependentQuotesAndZeroSideEffects(t *testing.T) {
	db := setupModelPricingTest(t)
	for _, sample := range []struct {
		model            string
		kind             int
		expr             string
		submit, actual   types.TaskPricingFacts
		estimate, target int
	}{
		{"MiniMax-H3", constant.ChannelTypeMiniMax, `u("seconds") * 0.08 + u("input_video_seconds") * 0.04 + max(u("input_images") - 2,0) * 0.02`, types.TaskPricingFacts{"seconds": 5.0, "resolution": "768P", "input_images": 3.0, "input_video_seconds": 15.0}, types.TaskPricingFacts{"seconds": 5.0, "input_images": 3.0, "input_video_seconds": 7.5}, 1020, 720},
		{"kling-v1", constant.ChannelTypeKling, `u("units") * 0.1`, types.TaskPricingFacts{"units": 1.0}, types.TaskPricingFacts{"units": 0.6}, 100, 60},
		{"doubao-seedance-1-5-pro-251215", constant.ChannelTypeDoubaoVideo, `u("tokens") / 1000000 * (u("generate_audio") ? 4 : 2)`, types.TaskPricingFacts{"tokens": 4000000.0, "resolution": "720p", "generate_audio": false}, types.TaskPricingFacts{"tokens": 2000000.0}, 8000, 4000},
		{"doubao-seedance-2-0-260128", constant.ChannelTypeDoubaoVideo, `u("tokens") / 1000000 * (u("video_input") == "video" ? 2 : 3)`, types.TaskPricingFacts{"tokens": 3000000.0, "resolution": "4k", "video_input": "video"}, types.TaskPricingFacts{"tokens": 2000000.0}, 6000, 4000},
	} {
		t.Run(sample.model, func(t *testing.T) {
			channel := pricingChannelForTest(t, db, sample.kind, sample.model)
			entry := pricingEntryForTest(t, db, sample.model)
			at := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
			request := ModelPricingQuoteRequest{ModelName: sample.model, ContextID: fmt.Sprintf("channel:%d", channel.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", PricingAt: &at, Pricing: types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": sample.expr}, Submit: sample.submit, Actual: sample.actual}
			writes := 0
			callback := "quote_no_write_" + sample.model
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(*gorm.DB) { writes++ }))
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(*gorm.DB) { writes++ }))
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callback, func(*gorm.DB) { writes++ }))
			quote, err := QuoteModelPricing(request)
			require.NoError(t, db.Callback().Create().Remove(callback))
			require.NoError(t, db.Callback().Update().Remove(callback))
			require.NoError(t, db.Callback().Delete().Remove(callback))
			require.NoError(t, err)
			require.Zero(t, writes)
			require.False(t, quote.AdmissionReady)
			require.Equal(t, sample.estimate, quote.Preview.Estimate.QuotaAfterGroup)
			require.Equal(t, sample.target, quote.Preview.Final.QuotaAfterGroup)
			require.Equal(t, max(500, sample.estimate), quote.Preview.ReservedQuota)
			if sample.model == "kling-v1" {
				request.Actual = types.TaskPricingFacts{"units": float64(0)}
				zero, err := QuoteModelPricing(request)
				require.NoError(t, err)
				require.NotNil(t, zero.Preview.Final)
				require.Zero(t, zero.Preview.Final.QuotaAfterGroup)
			}
			request.Actual = types.TaskPricingFacts{}
			_, err = QuoteModelPricing(request)
			require.Error(t, err)
			request.Actual = sample.actual
			request.ContextID = "plugin:forged"
			_, err = QuoteModelPricing(request)
			require.Error(t, err)
		})
	}
}

func installPricingPluginForTest(t *testing.T, db *gorm.DB, version, unit string, previous []taskplugin.StoredPackage, revision int64) []taskplugin.StoredPackage {
	t.Helper()
	document := taskplugin.PackageDocument{FormatVersion: 1, Source: "synthetic metadata only; never executed", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "kling", Name: "Synthetic Kling", Version: version, Models: []string{"kling-v1"}, BaseURL: "https://example.com", AllowedHosts: []string{"example.com"}, UsageSchema: map[string]taskplugin.UsageField{"units": {Type: "number", Unit: unit}}}}
	data, err := common.Marshal(document)
	require.NoError(t, err)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	next := append([]taskplugin.StoredPackage(nil), previous...)
	for i := range next {
		next[i].Active = false
	}
	next = append(next, taskplugin.StoredPackage{Document: data, Digest: digest, Active: true, Enabled: true})
	_, err = NewTaskPluginCatalogStore(db).CompareAndSwap(t.Context(), revision, next)
	require.NoError(t, err)
	return next
}
func TestModelPricingSchemaDriftStaleSourceAndNodeCache(t *testing.T) {
	db := setupModelPricingTest(t)
	packages := installPricingPluginForTest(t, db, "1.0.0", "credit", nil, 0)
	entry := pricingEntryForTest(t, db, "kling-v1")
	draft := types.ModelPricingValues{billing_setting.PluginBillingExprOption: map[string]any{"kling": `u("units") * 0.1`}}
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, draft)})
	require.NoError(t, err)
	entry = pricingEntryForTest(t, db, "kling-v1")
	require.Len(t, entry.PluginVariants, 1)
	require.True(t, entry.PluginVariants[0].SchemaCompatible)
	require.False(t, entry.PluginVariants[0].ExecutionReady)
	_, err = QuoteModelPricing(ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: "plugin:kling", ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Submit: types.TaskPricingFacts{"units": 1.0}})
	require.Error(t, err) // schema alone cannot establish phases
	installPricingPluginForTest(t, db, "1.0.1", "second", packages, 1)
	after := pricingEntryForTest(t, db, "kling-v1")
	require.Equal(t, entry.Version, after.Version)
	require.NotEqual(t, entry.ContractDigest, after.ContractDigest)
	change := pricingChangeForTest(after, draft)
	change.ExpectedContractDigest = entry.ContractDigest
	_, err = UpdateModelPricing([]types.ModelPricingChange{change})
	require.ErrorIs(t, err, ErrModelPricingConflict)
	require.Equal(t, `u("units") * 0.1`, rawPricingObjectForTest(t, db, billing_setting.PluginBillingExprOption)["kling::kling-v1"])
}

func TestModelPricingMappedDependencyBindingAndReadFailure(t *testing.T) {
	db := setupModelPricingTest(t)
	channel := pricingChannelForTest(t, db, constant.ChannelTypeKling, "alias")
	mapping := `{"alias":"kling-v1"}`
	require.NoError(t, db.Model(&channel).Update("model_mapping", mapping).Error)
	entry := pricingEntryForTest(t, db, "alias")
	target := pricingEntryForTest(t, db, "kling-v1")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(target, types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`})})
	require.NoError(t, err)
	after := pricingEntryForTest(t, db, "alias")
	require.NotEqual(t, entry.Version, after.Version)
	require.NoError(t, db.Model(&channel).Update("status", 2).Error)
	bound := pricingEntryForTest(t, db, "alias")
	require.NotEqual(t, after.BindingVersion, bound.BindingVersion)
	_, err = UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(after, types.ModelPricingValues{"ModelPrice": float64(0)})})
	require.ErrorIs(t, err, ErrModelPricingConflict)
	require.NoError(t, db.Save(&Option{Key: "ModelPrice", Value: "{"}).Error)
	_, err = GetModelPricingSnapshot([]string{"alias"})
	require.Error(t, err) // must not fall back to node cache/default zero
}

func TestModelPricingCommittedCacheFailureIsNotRollback(t *testing.T) {
	db := setupModelPricingTest(t)
	result, err := mutateModelPricingWithPublisher(db, func(values pricingMaps, _ *pricingAuthority) ([]string, error) {
		values["ModelPrice"]["cache-fault"] = float64(2)
		return []string{"cache-fault"}, nil
	}, nil, func(map[string]string) error { return errors.New("synthetic cache publication failure") })
	require.NoError(t, err)
	require.True(t, result.Committed)
	require.False(t, result.CachePublished)
	require.Contains(t, result.Warning, "committed")
	require.Equal(t, float64(2), rawPricingObjectForTest(t, db, "ModelPrice")["cache-fault"])
	entry := pricingEntryForTest(t, db, "cache-fault")
	require.Equal(t, float64(2), entry.Configured["ModelPrice"])
}

func TestModelPricingPersistenceColumnsMatchSchema(t *testing.T) {
	option, err := schema.Parse(&Option{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	for _, column := range []string{"key", "value"} {
		require.NotNil(t, option.LookUpField(column), column)
	}
	// Pricing fields are keys INSIDE Option.Value, never columns on Option.
	require.Nil(t, option.LookUpField("ModelPrice"))
}

type pricingChildInput struct {
	Path      string
	Change    types.ModelPricingChange
	LegacyRaw string
}

func TestModelPricingConcurrentFirstWriteAcrossProcesses(t *testing.T) {
	db := setupModelPricingTest(t)
	entry := pricingEntryForTest(t, db, "concurrent")
	var databaseRows int64
	require.NoError(t, db.Model(&Option{}).Count(&databaseRows).Error)
	require.Zero(t, databaseRows)
	// Both processes receive the same pre-write baseline; neither shares the
	// production node mutex or cache with the other process.
	var databases []struct {
		File string `gorm:"column:file"`
	}
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&databases).Error)
	require.NotEmpty(t, databases[0].File)
	commands := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for index := 0; index < 2; index++ {
		input := pricingChildInput{Path: databases[0].File, Change: pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(index + 1)})}
		data, err := common.Marshal(input)
		require.NoError(t, err)
		file := filepath.Join(t.TempDir(), "request.json")
		require.NoError(t, os.WriteFile(file, data, 0600))
		command := exec.Command(os.Args[0], "-test.run=^TestModelPricingChildWriter$", "-test.v", "-test.timeout=90s")
		command.Env = append(os.Environ(), "MAX_API_PRICING_CHILD_REQUEST="+file)
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		require.NoError(t, command.Start())
		commands = append(commands, command)
		outputs = append(outputs, output)
	}
	committed, conflicts := 0, 0
	for index, command := range commands {
		err := command.Wait()
		t.Logf("independent process %d: %s", index, outputs[index])
		require.NoError(t, err)
		text := outputs[index].String()
		if strings.Contains(text, "PRICING_CHILD:committed") {
			committed++
		}
		if strings.Contains(text, "PRICING_CHILD:conflict") {
			conflicts++
		}
	}
	require.Equal(t, 1, committed)
	require.Equal(t, 1, conflicts)
	price := rawPricingObjectForTest(t, db, "ModelPrice")["concurrent"]
	require.Contains(t, []any{float64(1), float64(2)}, price)
	require.NoError(t, db.Model(&Option{}).Count(&databaseRows).Error)
	require.EqualValues(t, 2, databaseRows) // coordination + changed price map only
}
func TestModelPricingChildWriter(t *testing.T) {
	file := os.Getenv("MAX_API_PRICING_CHILD_REQUEST")
	if file == "" {
		t.Skip("parent executes two independent writer processes")
	}
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	var input pricingChildInput
	require.NoError(t, common.Unmarshal(data, &input))
	require.Equal(t, "pricing-test.sqlite", filepath.Base(input.Path))
	db, err := gorm.Open(sqlite.Open(input.Path+"?_pragma=busy_timeout(20000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	common.OptionMap = map[string]string{}
	if input.LegacyRaw != "" {
		DB = db
		common.OptionMap["ModelPrice"] = "{}"
		err = UpdateOption("ModelPrice", input.LegacyRaw)
	} else {
		_, err = updateModelPricing(db, []types.ModelPricingChange{input.Change})
	}
	if errors.Is(err, ErrModelPricingConflict) || errors.Is(err, ErrModelPricingLegacyWrite) {
		t.Log("PRICING_CHILD:conflict")
	} else {
		require.NoError(t, err)
		t.Log("PRICING_CHILD:committed")
	}
	sqlDB, _ := db.DB()
	require.NoError(t, sqlDB.Close())
}

func TestModelPricingLegacyAndPatchCompeteAcrossProcesses(t *testing.T) {
	db := setupModelPricingTest(t)
	pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	require.NoError(t, db.Create(&Option{Key: "ModelPrice", Value: `{"kling-v1":0}`}).Error)
	entry := pricingEntryForTest(t, db, "kling-v1")
	var databases []struct {
		File string `gorm:"column:file"`
	}
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&databases).Error)
	inputs := []pricingChildInput{
		{Path: databases[0].File, LegacyRaw: `{"kling-v1":1234}`},
		{Path: databases[0].File, Change: pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(2), "billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`})},
	}
	commands := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for _, input := range inputs {
		data, err := common.Marshal(input)
		require.NoError(t, err)
		file := filepath.Join(t.TempDir(), "request.json")
		require.NoError(t, os.WriteFile(file, data, 0600))
		command := exec.Command(os.Args[0], "-test.run=^TestModelPricingChildWriter$", "-test.v", "-test.timeout=90s")
		command.Env = append(os.Environ(), "MAX_API_PRICING_CHILD_REQUEST="+file)
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		require.NoError(t, command.Start())
		commands = append(commands, command)
		outputs = append(outputs, output)
	}
	committed, conflicts := 0, 0
	for index, command := range commands {
		err := command.Wait()
		t.Logf("legacy/CAS process %d: %s", index, outputs[index])
		require.NoError(t, err)
		if strings.Contains(outputs[index].String(), "PRICING_CHILD:committed") {
			committed++
		}
		if strings.Contains(outputs[index].String(), "PRICING_CHILD:conflict") {
			conflicts++
		}
	}
	require.Equal(t, 1, committed)
	require.Equal(t, 1, conflicts)
	price := rawPricingObjectForTest(t, db, "ModelPrice")["kling-v1"]
	require.Contains(t, []any{float64(1234), float64(2)}, price)
}

func TestModelPricingVersionNamespacesAndSelfUseDefault(t *testing.T) {
	db := setupModelPricingTest(t)
	entry := pricingEntryForTest(t, db, "self_use_default")
	_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(0)})})
	require.NoError(t, err)
	after := pricingEntryForTest(t, db, "self_use_default")
	require.NotEqual(t, entry.Version, after.Version)
	require.NoError(t, db.Create(&Option{Key: "SelfUseModeEnabled", Value: "true"}).Error)
	unknown := pricingEntryForTest(t, db, "synthetic-unknown")
	require.Equal(t, float64(37.5), unknown.Effective["ModelRatio"])
	require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": "SelfUseModeEnabled"}).Update("value", "false").Error)
	unknown = pricingEntryForTest(t, db, "synthetic-unknown")
	_, exists := unknown.Effective["ModelRatio"]
	require.False(t, exists)
}

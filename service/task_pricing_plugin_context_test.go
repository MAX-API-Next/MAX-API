// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApprovedTaskPricingPluginContextQuotesFreshAuthorityWithoutAdmission(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "approved-context.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Channel{}, &model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{}))
	previousDB, previousUnit := model.DB, common.QuotaPerUnit
	model.DB, common.QuotaPerUnit = db, 1000
	t.Cleanup(func() {
		model.DB, common.QuotaPerUnit = previousDB, previousUnit
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	setting, mapping := `{"task_pricing_profile":"new-api-task@1"}`, `{"alias":"kling-v1"}`
	require.NoError(t, db.Create(&model.Channel{Id: 902, Type: constant.ChannelTypeKling, Status: 1, Key: "synthetic", Models: "alias", Group: "default", Setting: &setting, ModelMapping: &mapping}).Error)
	for _, row := range []model.Option{
		{Key: "billing_setting.plugin_billing_expr", Value: `{"kling::alias":"u(\"units\") * 0.1"}`},
		{Key: "GroupRatio", Value: `{"default":1}`}, {Key: "PreConsumedQuota", Value: "700"},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	store := &failingPricingCatalogStore{CatalogStore: model.NewTaskPluginCatalogStore(db)}
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg, _, _ := pricingPackageTask(t, "1.0.0", "credit")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	c, _ := taskpricing.KnownContract("kling", "kling-v1")
	cap := 8.0 // Fixture bound only; production Kling contract remains uncapped.
	p := c.FieldPolicy["units"]
	p.Max = &cap
	c.FieldPolicy["units"] = p
	registry, err := taskpricing.NewPhaseRegistry([]taskpricing.PhaseApproval{{PluginKey: "kling", Version: "1.0.0", PackageDigest: pkg.Digest(),
		Model: "kling-v1", Action: "generate", Contract: c, Evidence: "isolated synthetic review/bound; not real upstream approval"}})
	require.NoError(t, err)
	request := model.TaskPricingShadowRequest{ModelName: "alias", ChannelID: 902, Group: "default"}
	actor := taskplugin.Actor{UserID: 901, TokenID: 903, ChannelID: 902, RequestID: "approved-context"}
	facts := types.TaskPricingFacts{"units": 6}
	writes := 0
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("approved_no_create", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("approved_no_update", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("approved_no_delete", func(*gorm.DB) { writes++ }))
	first, quote, err := QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, registry, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", facts)
	require.NoError(t, err)
	require.Equal(t, 600, quote.Estimate.QuotaAfterGroup)
	require.Equal(t, 800, quote.Maximum.QuotaAfterGroup)
	require.Equal(t, 800, quote.RequiredReservationQuota)
	require.False(t, quote.AdmissionReady)
	require.False(t, quote.ProviderCostProven)
	require.Zero(t, writes, "no task/reservation/cache/business writes")
	_, _, err = QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, nil, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", facts)
	require.Error(t, err)
	_, _, err = QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, registry, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "other", facts)
	require.Error(t, err, "a model approval does not approve another action")
	wrongActor := actor
	wrongActor.ChannelID++
	_, _, err = QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, registry, request, wrongActor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", facts)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	// A later call must reread the database; the old detached response stays old.
	require.NoError(t, db.Model(&model.Option{}).Where(map[string]any{"key": "billing_setting.plugin_billing_expr"}).Update("value", `{"kling::alias":"0"}`).Error)
	writes = 0
	second, free, err := QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, registry, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", types.TaskPricingFacts{"units": 0})
	require.NoError(t, err)
	require.Zero(t, free.Maximum.QuotaAfterGroup)
	require.Equal(t, 700, free.RequiredReservationQuota)
	require.NotEqual(t, first.Shadow.Guards.PriceVersion, second.Shadow.Guards.PriceVersion)
	require.Equal(t, `u("units") * 0.1`, first.Shadow.Selection.Expression)
	require.Zero(t, writes)
	// A stale routed mapping must not be accepted as the configured source.
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", request.ChannelID).Update("model_mapping", `{"alias":"other"}`).Error)
	_, _, err = QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, registry, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", facts)
	require.Error(t, err)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", request.ChannelID).Update("model_mapping", mapping).Error)
	store.fail = true
	observed, denied, err := QuoteApprovedTaskPricingPluginExposure(t.Context(), catalog, registry, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", facts)
	require.ErrorIs(t, err, taskplugin.ErrCatalogUnavailable)
	require.Nil(t, observed)
	require.Nil(t, denied)
	store.fail = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = QuoteApprovedTaskPricingPluginExposure(ctx, catalog, registry, request, actor, "POST", "/isolated/kling/jobs", "kling-v1", "generate", facts)
	require.ErrorIs(t, err, context.Canceled)
}

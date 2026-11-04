// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func pricingPackageTask(t *testing.T, version, unit string) (*taskplugin.Package, *model.Task, taskplugin.Actor) {
	t.Helper()
	document := taskplugin.PackageDocument{FormatVersion: 1, Source: "synthetic opaque package; no JS is executed", Manifest: taskplugin.Manifest{
		APIVersion: 1, Key: "kling", Name: "Synthetic Kling", Version: version, Models: []string{"kling-v1"},
		Routes:      []taskplugin.Route{{Method: "POST", Path: "/isolated/kling/jobs", Type: "submit", Decode: "submit", Render: "created"}},
		UsageSchema: map[string]taskplugin.UsageField{"units": {Type: "number", Unit: unit}}}}
	wire, err := common.Marshal(document)
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	contract, ok := taskpricing.KnownContract("kling", "kling-v1")
	require.True(t, ok)
	digest, err := taskpricing.ContractDigest(contract)
	require.NoError(t, err)
	guards := taskpricing.Guards{PriceVersion: "fixture-price", BindingVersion: "fixture-binding", ContractDigest: digest}
	actor := taskplugin.Actor{UserID: 901, TokenID: 903, ChannelID: 902, RequestID: "pricing-pin-request"}
	plan, err := taskpricing.Freeze(taskpricing.FreezeInput{
		PreviewInput: taskpricing.PreviewInput{Profile: taskpricing.NewAPITask,
			Prices: types.TaskPricingMaps{PluginBillingExpr: map[string]string{"kling::alias": `u("units") * 0.1`}},
			Source: taskpricing.Source{Driver: "task_plugin", PluginKey: "kling"}, OriginalModel: "alias", MappedModel: "kling-v1",
			Contract: contract, Expected: guards, Current: guards, Submit: types.TaskPricingFacts{"units": 4},
			PricingAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: 1.5, ReservationFloor: 700},
		ChannelID: actor.ChannelID, RequestID: actor.RequestID, Action: "generate", SourceID: "kling", PluginVersion: version, PackageDigest: pkg.Digest()})
	require.NoError(t, err)
	task := &model.Task{ID: 99, UserId: actor.UserID, ChannelId: actor.ChannelID, Action: "generate", Quota: 700,
		Properties:  model.Properties{OriginModelName: "alias", UpstreamModelName: "kling-v1"},
		PrivateData: model.TaskPrivateData{TokenId: actor.TokenID, BillingRequestId: actor.RequestID, BillingContext: &model.TaskBillingContext{TaskBillingPlan: plan}}}
	return pkg, task, actor
}

func pricingCatalogStore(t *testing.T) *model.TaskPluginCatalogStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pricing-pin.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return model.NewTaskPluginCatalogStore(db)
}

func TestTaskPricingPluginPinsActualPackageAcrossUpgradeAndReload(t *testing.T) {
	store := pricingCatalogStore(t)
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	first, task, actor := pricingPackageTask(t, "1.0.0", "credit")
	require.NoError(t, catalog.Install(t.Context(), first))
	require.NoError(t, BindTaskPricingPlugin(t.Context(), catalog, task, actor, "POST", "/isolated/kling/jobs"))
	require.Equal(t, first.Digest(), task.PrivateData.PluginBinding.PackageDigest)
	private, err := common.Marshal(task.PrivateData)
	require.NoError(t, err)
	task.PrivateData = model.TaskPrivateData{}
	require.NoError(t, common.Unmarshal(private, &task.PrivateData))
	second, _, _ := pricingPackageTask(t, "2.0.0", "credit")
	require.NoError(t, catalog.Install(t.Context(), second))
	restarted, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	session, err := ResolveTaskPricingPluginSession(t.Context(), restarted, task, actor)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", session.Binding().Version)
	_, err = ResolveTaskPluginSession(restarted.Host(), task)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected, "new facts must not enter the legacy session path")
	result := &relaycommon.TaskInfo{CompletionTokens: 123}
	for _, tc := range []struct {
		units float64
		quota int
	}{{2.5, 375}, {0, 0}} {
		require.NoError(t, ApplyTaskPricingPluginFacts(t.Context(), restarted, task, actor, result, types.TaskPricingFacts{"units": tc.units}))
		require.Equal(t, first.Digest(), result.PricingUsageEnvelope.PackageDigest)
		require.Equal(t, tc.units, result.PricingUsageEnvelope.Facts["units"])
		quote, err := taskpricing.QuoteFinal(task.PrivateData.BillingContext.TaskBillingPlan, result.PricingUsageEnvelope)
		require.NoError(t, err)
		// Independent amount: 2.5*.1*1000*1.5=375; explicit zero=0.
		require.Equal(t, tc.quota, quote.QuotaAfterGroup)
		require.EqualValues(t, 123, result.CompletionTokens, "credits never overwrite canonical tokens")
		require.Equal(t, 700, task.Quota, "attaching facts does not settle the reservation")
	}
	before, err := common.Marshal(result)
	require.NoError(t, err)
	for _, facts := range []types.TaskPricingFacts{nil, {}, {"units": -1}, {"units": "2.5"}, {"units": 2.5, "undeclared": 1}} {
		require.Error(t, ApplyTaskPricingPluginFacts(t.Context(), restarted, task, actor, result, facts))
		after, err := common.Marshal(result)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after), "invalid facts must preserve the prior result")
	}
	newTask := *task
	_, pending, _ := pricingPackageTask(t, "1.0.0", "credit")
	newTask.PrivateData = pending.PrivateData
	require.ErrorIs(t, BindTaskPricingPlugin(t.Context(), restarted, &newTask, actor, "POST", "/isolated/kling/jobs"), taskplugin.ErrBindingRejected, "active package2 cannot silently replace price-plan package1")
	require.NoError(t, catalog.SetEnabled(t.Context(), "kling", false))
	_, err = ResolveTaskPricingPluginSession(t.Context(), restarted, task, actor)
	require.NoError(t, err)
	require.ErrorIs(t, BindTaskPricingPlugin(t.Context(), restarted, &newTask, actor, "POST", "/isolated/kling/jobs"), taskplugin.ErrBindingRejected)
	require.NoError(t, catalog.Revoke(t.Context(), "kling", "1.0.0"))
	_, err = ResolveTaskPricingPluginSession(t.Context(), restarted, task, actor)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	missing, err := taskplugin.OpenCatalog(t.Context(), pricingCatalogStore(t))
	require.NoError(t, err)
	_, err = ResolveTaskPricingPluginSession(t.Context(), missing, task, actor)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
}

func TestTaskPricingPluginRejectsPackageSchemaAndIdentityMismatch(t *testing.T) {
	store := pricingCatalogStore(t)
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg, _, _ := pricingPackageTask(t, "1.0.0", "credit")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	for _, mutate := range []func(*model.Task, *taskplugin.Actor){
		func(_ *model.Task, a *taskplugin.Actor) { a.UserID++ },
		func(_ *model.Task, a *taskplugin.Actor) { a.TokenID++ },
		func(_ *model.Task, a *taskplugin.Actor) { a.ChannelID++ },
		func(_ *model.Task, a *taskplugin.Actor) { a.RequestID = "other" },
		func(task *model.Task, _ *taskplugin.Actor) { task.Action = "other" },
		func(task *model.Task, _ *taskplugin.Actor) { task.Properties.UpstreamModelName = "other" },
		func(task *model.Task, _ *taskplugin.Actor) {
			task.PrivateData.BillingContext.TaskBillingPlan.Profile = "unknown"
		},
		func(task *model.Task, _ *taskplugin.Actor) {
			task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan.PackageDigest = strings.Repeat("a", 64)
		},
	} {
		_, task, actor := pricingPackageTask(t, "1.0.0", "credit")
		mutate(task, &actor)
		require.Error(t, BindTaskPricingPlugin(t.Context(), catalog, task, actor, "POST", "/isolated/kling/jobs"))
		require.Nil(t, task.PrivateData.PluginBinding)
	}
	_, bound, owner := pricingPackageTask(t, "1.0.0", "credit")
	require.NoError(t, BindTaskPricingPlugin(t.Context(), catalog, bound, owner, "POST", "/isolated/kling/jobs"))
	private, err := common.Marshal(bound.PrivateData)
	require.NoError(t, err)
	for _, mutate := range []func(*model.Task, *taskplugin.Actor){
		func(_ *model.Task, a *taskplugin.Actor) { a.TokenID++ },
		func(task *model.Task, _ *taskplugin.Actor) { task.ID = 0 },
		func(task *model.Task, _ *taskplugin.Actor) { task.PrivateData.PluginBinding.Profile = "" },
		func(task *model.Task, _ *taskplugin.Actor) { task.PrivateData.PluginBinding.Version = "2.0.0" },
		func(task *model.Task, _ *taskplugin.Actor) {
			task.PrivateData.PluginBinding.ContractDigest = strings.Repeat("b", 64)
		},
		func(task *model.Task, _ *taskplugin.Actor) {
			task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan.ChannelID++
		},
	} {
		copy, actor := *bound, owner
		require.NoError(t, common.Unmarshal(private, &copy.PrivateData))
		mutate(&copy, &actor)
		session, err := ResolveTaskPricingPluginSession(t.Context(), catalog, &copy, actor)
		require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
		require.Nil(t, session, "reloaded metadata must be checked against both the frozen plan and host actor")
	}
	wrong, task, actor := pricingPackageTask(t, "2.0.0", "token")
	require.NoError(t, catalog.Install(t.Context(), wrong))
	require.ErrorIs(t, BindTaskPricingPlugin(t.Context(), catalog, task, actor, "POST", "/isolated/kling/jobs"), taskplugin.ErrBindingRejected)
	require.Nil(t, task.PrivateData.PluginBinding, "even the selected package identity cannot change credit into token")
	conflictManifest := pkg.Manifest()
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "different code, same key/version", Manifest: conflictManifest})
	require.NoError(t, err)
	conflict, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	require.Error(t, catalog.Install(t.Context(), conflict))
}

type failingPricingCatalogStore struct {
	taskplugin.CatalogStore
	fail bool
}

func (s *failingPricingCatalogStore) Load(ctx context.Context) (taskplugin.CatalogSnapshot, error) {
	if s.fail {
		return taskplugin.CatalogSnapshot{}, errors.New("synthetic read failure")
	}
	return s.CatalogStore.Load(ctx)
}

func TestTaskPricingPluginRefreshFailureAndCancellationHaveNoFallback(t *testing.T) {
	store := &failingPricingCatalogStore{CatalogStore: pricingCatalogStore(t)}
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg, task, actor := pricingPackageTask(t, "1.0.0", "credit")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, BindTaskPricingPlugin(t.Context(), catalog, task, actor, "POST", "/isolated/kling/jobs"))
	store.fail = true
	result := &relaycommon.TaskInfo{CompletionTokens: 17}
	require.ErrorIs(t, ApplyTaskPricingPluginFacts(t.Context(), catalog, task, actor, result, types.TaskPricingFacts{"units": 2.5}), taskplugin.ErrCatalogUnavailable)
	require.Nil(t, result.PricingUsageEnvelope)
	require.EqualValues(t, 17, result.CompletionTokens)
	_, err = catalog.Host().Resolve(*task.PrivateData.PluginBinding, actor)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected, "stale Host remains suspended until a successful refresh")
	store.fail = false
	_, err = ResolveTaskPricingPluginSession(t.Context(), catalog, task, actor)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = ResolveTaskPricingPluginSession(ctx, catalog, task, actor)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTaskPricingPluginFactsUseExistingSettlementExactlyOnce(t *testing.T) {
	task, adaptor := reserveExpressionTask(t) // existing isolated wallet/token reservation700
	catalog, err := taskplugin.OpenCatalog(t.Context(), pricingCatalogStore(t))
	require.NoError(t, err)
	pkg, _, _ := pricingPackageTask(t, "1.0.0", "credit")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	// The earlier harness uses a synthetic digest. Pin this test's real loaded
	// package before attaching or persisting its new binding; no code is run.
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	plan.ExpressionPlan.PackageDigest = pkg.Digest()
	actor := taskplugin.Actor{UserID: task.UserId, TokenID: task.PrivateData.TokenId, ChannelID: task.ChannelId, RequestID: task.PrivateData.BillingRequestId}
	require.NoError(t, BindTaskPricingPlugin(t.Context(), catalog, task, actor, "POST", "/isolated/kling/jobs"))
	require.NoError(t, ApplyTaskPricingPluginFacts(t.Context(), catalog, task, actor, adaptor.result, types.TaskPricingFacts{"units": 2.5}))
	task.IncludePrivateDataInUpdate()
	require.NoError(t, model.DB.Save(task).Error)
	stale := *task
	stored := pollExpressionFixture(t, task, adaptor)
	assertExpressionLedger(t, stored, 375) // 2.5*.1*1000*1.5=375; refund325 once
	stored = pollExpressionFixture(t, &stale, adaptor)
	assertExpressionLedger(t, stored, 375)
}

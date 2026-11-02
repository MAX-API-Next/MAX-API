// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type boundedPricingFixture struct {
	db       *gorm.DB
	catalog  *taskplugin.Catalog
	registry *taskpricing.PhaseRegistry
	approval taskpricing.PhaseApproval
	request  model.TaskPricingShadowRequest
	actor    taskplugin.Actor
	submit   types.TaskPricingFacts
}

func newBoundedPricingFixture(t *testing.T) *boundedPricingFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bounded-pricing.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Option{}, &model.Channel{}, &model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{},
		&model.Task{}, &model.User{}, &model.Token{}, &model.Log{}, &model.BillingLogReceipt{},
		&model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{},
		&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{},
	))
	oldDB, oldLog, oldUnit := model.DB, model.LOG_DB, common.QuotaPerUnit
	oldSQLite, oldRedis, oldBatch, oldConsumeLog := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.DB, model.LOG_DB, common.QuotaPerUnit = db, db, 1000
	common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.QuotaPerUnit = oldDB, oldLog, oldUnit
		common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsumeLog
		require.NoError(t, sqlDB.Close())
	})
	seedUser(t, 901, 3000)
	seedToken(t, 903, 901, "synthetic-bounded-token", 3000)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 903).Update("used_quota", 0).Error)
	setting, mapping := "{\"task_pricing_profile\":\"new-api-task@1\"}", "{\"alias\":\"MiniMax-H3\"}"
	require.NoError(t, db.Create(&model.Channel{Id: 902, Type: constant.ChannelTypeMiniMax, Status: 1, Key: "synthetic", Models: "alias", Group: "default", Setting: &setting, ModelMapping: &mapping}).Error)
	for _, row := range []model.Option{
		{Key: "billing_setting.plugin_billing_expr", Value: "{\"hailuo::alias\":\"u(\\\"seconds\\\") * 0.1\"}"},
		{Key: "GroupRatio", Value: "{\"default\":1}"}, {Key: "PreConsumedQuota", Value: "700"},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	c, ok := taskpricing.KnownContract("hailuo", "MiniMax-H3")
	require.True(t, ok)
	c, err = taskpricing.NormalizeContract(c)
	require.NoError(t, err)
	schema := map[string]taskplugin.UsageField{}
	for key, field := range c.UsageSchema {
		schema[key] = taskplugin.UsageField{Type: field.Type, Unit: field.Unit, Enum: field.Enum}
	}
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "isolated opaque package; no JavaScript execution",
		Manifest: taskplugin.Manifest{APIVersion: 1, Key: "hailuo", Name: "Bounded fixture", Version: "1.0.0", Models: []string{"MiniMax-H3"}, UsageSchema: schema,
			Routes: []taskplugin.Route{{Method: "POST", Path: "/isolated/hailuo/jobs", Type: "submit", Decode: "submit", Render: "created"}}}})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	catalog, err := taskplugin.OpenCatalog(t.Context(), model.NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	approval := taskpricing.PhaseApproval{PluginKey: "hailuo", Version: "1.0.0", PackageDigest: pkg.Digest(),
		Model: "MiniMax-H3", Action: "generate", Contract: c, Evidence: "isolated MAX H3 4..15s charge-domain fixture; not JS/provider-cost approval"}
	registry, err := taskpricing.NewPhaseRegistry([]taskpricing.PhaseApproval{approval})
	require.NoError(t, err)
	return &boundedPricingFixture{db: db, catalog: catalog, registry: registry, approval: approval,
		request: model.TaskPricingShadowRequest{ModelName: "alias", ChannelID: 902, Group: "default"},
		actor:   taskplugin.Actor{UserID: 901, TokenID: 903, ChannelID: 902, RequestID: "bounded-request"},
		submit:  types.TaskPricingFacts{"seconds": 6, "resolution": "768P", "input_images": 0, "input_video_seconds": 0}}
}

func (f *boundedPricingFixture) observe(t *testing.T) TaskPricingFreezeGuards {
	t.Helper()
	observed, quote, err := QuoteApprovedTaskPricingPluginExposure(t.Context(), f.catalog, f.registry, f.request, f.actor, "POST", "/isolated/hailuo/jobs", "MiniMax-H3", "generate", f.submit)
	require.NoError(t, err)
	require.False(t, quote.AdmissionReady)
	require.False(t, quote.ProviderCostProven)
	return TaskPricingFreezeGuards{Pricing: observed.Shadow.Guards, TermsVersion: observed.Shadow.TermsVersion}
}

func (f *boundedPricingFixture) freeze(t *testing.T, expected TaskPricingFreezeGuards) (*TaskPricingPluginPlan, error) {
	t.Helper()
	return FreezeApprovedTaskPricingPluginPlan(t.Context(), f.catalog, f.registry, f.request, f.actor, "POST", "/isolated/hailuo/jobs", "MiniMax-H3", "generate", expected, f.submit)
}

func (f *boundedPricingFixture) billingInfo(plan *types.TaskBillingPlan) (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/isolated-bounded-task", nil)
	c.Set(common.RequestIdKey, f.actor.RequestID)
	info := &relaycommon.RelayInfo{RequestId: f.actor.RequestID, UserId: f.actor.UserID, TokenId: f.actor.TokenID, TokenKey: "synthetic-bounded-token",
		OriginModelName: "alias", UsingGroup: "default", ForcePreConsume: true, StartTime: time.Now(),
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelId: 902, ChannelType: constant.ChannelTypeMiniMax},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"}, TaskBillingPlan: types.CloneTaskBillingPlan(plan),
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only", QuotaWarningThreshold: 1},
		PriceData:   types.PriceData{ModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: plan.GroupRatio}}}
	return c, info
}

func TestBoundedPricingFreezeRereadsGuardsWithoutWrites(t *testing.T) {
	for _, change := range []string{"none", "price", "floor", "ratio", "node-unit", "mapping", "approval", "disabled"} {
		t.Run(change, func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			expected := f.observe(t)
			switch change {
			case "price":
				require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "billing_setting.plugin_billing_expr").Update("value", "{\"hailuo::alias\":\"0\"}").Error)
			case "floor":
				require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "PreConsumedQuota").Update("value", "701").Error)
			case "ratio":
				require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "GroupRatio").Update("value", "{\"default\":2}").Error)
			case "node-unit":
				common.QuotaPerUnit = 2000
			case "mapping":
				require.NoError(t, f.db.Model(&model.Channel{}).Where("id = ?", 902).Update("model_mapping", "{\"alias\":\"other\"}").Error)
			case "approval":
				p := f.approval.Contract.FieldPolicy["seconds"]
				maximum := 14.0
				p.Max = &maximum
				f.approval.Contract.FieldPolicy["seconds"] = p
				var err error
				f.registry, err = taskpricing.NewPhaseRegistry([]taskpricing.PhaseApproval{f.approval})
				require.NoError(t, err)
			case "disabled":
				require.NoError(t, f.catalog.SetEnabled(t.Context(), "hailuo", false))
			}
			writes := 0
			require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("freeze-no-create", func(*gorm.DB) { writes++ }))
			require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("freeze-no-update", func(*gorm.DB) { writes++ }))
			prepared, err := f.freeze(t, expected)
			require.Zero(t, writes)
			if change != "none" {
				require.Error(t, err)
				require.Nil(t, prepared)
			} else {
				require.NoError(t, err)
				require.False(t, prepared.AdmissionReady)
				require.Equal(t, 600, prepared.Plan.EstimateQuota)
				require.Equal(t, 1500, prepared.Plan.ReserveQuota)
				require.NotEmpty(t, prepared.Binding.PhaseApprovalID)
			}
			require.EqualValues(t, 3000, getUserQuota(t, 901))
			require.Equal(t, 3000, getTokenRemainQuota(t, 903))
		})
	}
}

func TestBoundedPricingActualReservationReplaysAndSettlesFrozenPrice(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(fmt.Sprintf("free=%t", free), func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			reserve, target := 1500, 800
			if free {
				require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "GroupRatio").Update("value", "{\"default\":0}").Error)
				reserve, target = 0, 0
			}
			prepared, err := f.freeze(t, f.observe(t))
			require.NoError(t, err)
			c, info := f.billingInfo(prepared.Plan)
			require.Nil(t, PreConsumeBilling(c, reserve, info))
			require.Equal(t, reserve, info.Billing.GetPreConsumedQuota())
			require.EqualValues(t, 3000-reserve, getUserQuota(t, 901))
			// New session objects and concurrent invocations must resolve the
			// same durable request key, not issue a second reservation.
			var wg sync.WaitGroup
			errs := make(chan *types.MaxAPIError, 4)
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c, replay := f.billingInfo(prepared.Plan)
					errs <- PreConsumeBilling(c, reserve, replay)
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.Nil(t, err)
			}
			require.EqualValues(t, 3000-reserve, getUserQuota(t, 901))
			require.Equal(t, 3000-reserve, getTokenRemainQuota(t, 903))
			require.Zero(t, countLogs(t))
			task := makeServiceH3Task(t, reserve, prepared.Plan)
			task.Properties.OriginModelName, task.Properties.UpstreamModelName = "alias", "MiniMax-H3"
			task.Action, task.PrivateData.BillingRequestId = "generate", f.actor.RequestID
			task.PrivateData.BillingContext.OriginModelName = "alias"
			require.NoError(t, AttachTaskPluginBinding(f.catalog.Host(), task, prepared.Binding))
			persistTask(t, task)
			require.Nil(t, BuildTaskSubmissionSettlementEffect(c, info, reserve))
			require.NoError(t, info.Billing.Settle(reserve))
			// A later configured price must not reprice this persisted task.
			require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "billing_setting.plugin_billing_expr").Update("value", "{\"hailuo::alias\":\"99.0\"}").Error)
			var stored model.Task
			require.NoError(t, f.db.First(&stored, task.ID).Error)
			p := stored.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan
			result := &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), PricingUsageEnvelope: &types.TaskPricingUsageEnvelope{
				ProducerKind: "task_plugin", SourceID: "hailuo", PluginVersion: p.PluginVersion, PackageDigest: p.PackageDigest,
				ChannelID: 902, RequestID: f.actor.RequestID, Action: "generate", SchemaVersion: 1,
				ContractDigest: stored.PrivateData.BillingContext.TaskBillingPlan.UsageContractDigest,
				Stage:          types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
				Facts: types.TaskPricingFacts{"seconds": 8, "input_images": 0, "input_video_seconds": 0}}}
			adaptor := &expressionPollingFixture{result: result}
			ch := &model.Channel{Id: 902, Type: constant.ChannelTypeMiniMax, Key: "synthetic"}
			for i := 0; i < 2; i++ {
				require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, stored.GetUpstreamTaskID(), map[string]*model.Task{stored.GetUpstreamTaskID(): &stored}))
				require.NoError(t, f.db.First(&stored, task.ID).Error)
			}
			require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
			require.Equal(t, target, stored.Quota)
			require.EqualValues(t, 3000-target, getUserQuota(t, 901))
			require.Equal(t, 3000-target, getTokenRemainQuota(t, 903))
			require.Equal(t, target, getTokenUsedQuota(t, 903))
			var user model.User
			require.NoError(t, f.db.First(&user, 901).Error)
			require.EqualValues(t, target, user.UsedQuota)
			require.EqualValues(t, 1, user.RequestCount)
			var channel model.Channel
			require.NoError(t, f.db.First(&channel, 902).Error)
			require.EqualValues(t, target, channel.UsedQuota)
			require.EqualValues(t, 1, countLogs(t))
			var finalized model.BillingSettlement
			require.NoError(t, f.db.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&finalized).Error)
			require.EqualValues(t, target-reserve, finalized.AppliedFundingDelta)
			require.Equal(t, model.BillingSettlementStatusApplied, finalized.Status)
			require.Equal(t, model.BillingSettlementEffectApplied, finalized.EffectStatus)
		})
	}
}

func TestBoundedPricingFreezePreservesMappedDefaultAndExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		name, plugin, mode, expression, selectedModel, selectedPlugin string
		estimate, reserve                                             int
	}{
		{"plugin-mapped", "{\"hailuo::MiniMax-H3\":\"u(\\\"seconds\\\") * 0.1\"}", "{}", "{}", "MiniMax-H3", "hailuo", 600, 1500},
		{"default-original-zero", "{}", "{\"alias\":\"tiered_expr\"}", "{\"alias\":\"0\"}", "alias", "", 0, 700},
		{"default-mapped-zero", "{}", "{\"MiniMax-H3\":\"tiered_expr\"}", "{\"MiniMax-H3\":\"0\"}", "MiniMax-H3", "", 0, 700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "billing_setting.plugin_billing_expr").Update("value", tc.plugin).Error)
			require.NoError(t, f.db.Create(&model.Option{Key: "billing_setting.billing_mode", Value: tc.mode}).Error)
			require.NoError(t, f.db.Create(&model.Option{Key: "billing_setting.billing_expr", Value: tc.expression}).Error)
			prepared, err := f.freeze(t, f.observe(t))
			require.NoError(t, err)
			require.Equal(t, tc.selectedModel, prepared.Plan.ExpressionPlan.SelectedModel)
			require.Equal(t, tc.selectedPlugin, prepared.Plan.ExpressionPlan.SelectedPluginKey)
			require.Equal(t, "hailuo", prepared.Plan.ExpressionPlan.PluginKey, "inheriting default price must not change the actual producer")
			require.Equal(t, tc.estimate, prepared.Plan.EstimateQuota)
			require.Equal(t, tc.reserve, prepared.Plan.ReserveQuota)
		})
	}
}

// Lose only the receipt of the transaction that actually marks this request's
// reservation applied. Earlier record creation and later reconciliation reads
// remain real SQLite operations; no balance mutation is mocked.
type boundedReceiptPool struct {
	gorm.ConnPool
	operation string
	lost      atomic.Bool
}

func (p *boundedReceiptPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(gorm.TxBeginner).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &boundedReceiptTx{Tx: tx, pool: p}, nil
}

type boundedReceiptTx struct {
	*sql.Tx
	pool *boundedReceiptPool
}

func (tx *boundedReceiptTx) Commit() error {
	var applied int
	err := tx.QueryRowContext(context.Background(), "SELECT count(*) FROM billing_settlements WHERE operation_key = ? AND status = ?", tx.pool.operation, model.BillingSettlementStatusApplied).Scan(&applied)
	if err != nil {
		return err
	}
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if applied == 1 && tx.pool.lost.CompareAndSwap(false, true) {
		return errors.New("synthetic lost receipt after durable reservation commit")
	}
	return nil
}

func TestBoundedPricingLostCommitReceiptUsesRecordedReservation(t *testing.T) {
	f := newBoundedPricingFixture(t)
	prepared, err := f.freeze(t, f.observe(t))
	require.NoError(t, err)
	pool := &boundedReceiptPool{ConnPool: f.db.ConnPool, operation: "request:" + f.actor.RequestID + ":pre-consume"}
	// Keep normal per-query cloning. An Initialized global handle would reuse
	// statement conditions across the unrelated accounting lookups.
	faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: t.Context()})
	faultDB.ConnPool, faultDB.Statement.ConnPool = pool, pool
	model.DB = faultDB
	c, first := f.billingInfo(prepared.Plan)
	apiErr := PreConsumeBilling(c, 1500, first)
	model.DB = f.db
	require.NotNil(t, apiErr, "a lost commit receipt must remain a failure, never guessed success")
	require.True(t, pool.lost.Load(), "unexpected failure before the intended commit: %s", apiErr.Error())
	require.Nil(t, first.Billing)
	require.EqualValues(t, 1500, getUserQuota(t, 901))
	require.Equal(t, 1500, getTokenRemainQuota(t, 903))
	require.Zero(t, countLogs(t))
	var record model.BillingSettlement
	require.NoError(t, f.db.Where("operation_key = ?", pool.operation).First(&record).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
	require.EqualValues(t, 1500, record.AppliedFundingDelta)
	// Only after reading the original applied record, reconstruct the caller
	// with the SAME frozen identity/quantity. The existing operation returns it.
	c, recovered := f.billingInfo(prepared.Plan)
	require.Nil(t, PreConsumeBilling(c, prepared.Plan.ReserveQuota, recovered))
	require.Equal(t, 1500, recovered.Billing.GetPreConsumedQuota())
	require.EqualValues(t, 1500, getUserQuota(t, 901))
	require.Equal(t, 1500, getTokenRemainQuota(t, 903))
	var count int64
	require.NoError(t, f.db.Model(&model.BillingSettlement{}).Where("operation_key = ?", pool.operation).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestBoundedPricingFailedWalletLegRollsBackAndBlocksReplay(t *testing.T) {
	f := newBoundedPricingFixture(t)
	prepared, err := f.freeze(t, f.observe(t))
	require.NoError(t, err)
	failed := false
	require.NoError(t, f.db.Callback().Update().After("gorm:update").Register("bounded-wallet-leg-fault", func(tx *gorm.DB) {
		if !failed && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			failed = true
			tx.AddError(errors.New("synthetic failure after wallet SQL, before transaction commit"))
		}
	}))
	c, info := f.billingInfo(prepared.Plan)
	require.NotNil(t, PreConsumeBilling(c, 1500, info))
	require.True(t, failed)
	require.EqualValues(t, 3000, getUserQuota(t, 901))
	require.Equal(t, 3000, getTokenRemainQuota(t, 903))
	require.Equal(t, 0, getTokenUsedQuota(t, 903))
	require.Zero(t, countLogs(t))
	require.NoError(t, f.db.Callback().Update().Remove("bounded-wallet-leg-fault"))
	var record model.BillingSettlement
	require.NoError(t, f.db.Where("operation_key = ?", "request:"+f.actor.RequestID+":pre-consume").First(&record).Error)
	require.Equal(t, model.BillingSettlementStatusManual, record.Status)
	c, replay := f.billingInfo(prepared.Plan)
	apiErr := PreConsumeBilling(c, 1500, replay)
	require.NotNil(t, apiErr, "a manual funding record cannot be reopened by simply repeating the request")
	require.Contains(t, apiErr.Error(), model.ErrBillingSettlementManualReview.Error())
	require.EqualValues(t, 3000, getUserQuota(t, 901))
	require.Equal(t, 3000, getTokenRemainQuota(t, 903))
	var count int64
	require.NoError(t, f.db.Model(&model.BillingSettlement{}).Where("operation_key = ?", "request:"+f.actor.RequestID+":pre-consume").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

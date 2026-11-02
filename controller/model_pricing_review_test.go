// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Restore typed caches directly: the production legacy writer must refuse to
// erase protected task prices, even when a test fixture is cleaning up.
func preserveModelPricingControllerCaches(t *testing.T) {
	t.Helper()
	for _, cache := range []struct {
		get func() string
		set func(string) error
	}{
		{ratio_setting.ModelPrice2JSONString, ratio_setting.UpdateModelPriceByJSONString},
		{ratio_setting.ModelRatio2JSONString, ratio_setting.UpdateModelRatioByJSONString},
		{ratio_setting.CompletionRatio2JSONString, ratio_setting.UpdateCompletionRatioByJSONString},
		{ratio_setting.CacheRatio2JSONString, ratio_setting.UpdateCacheRatioByJSONString},
		{ratio_setting.CreateCacheRatio2JSONString, ratio_setting.UpdateCreateCacheRatioByJSONString},
		{ratio_setting.ImageRatio2JSONString, ratio_setting.UpdateImageRatioByJSONString},
		{ratio_setting.AudioRatio2JSONString, ratio_setting.UpdateAudioRatioByJSONString},
		{ratio_setting.AudioCompletionRatio2JSONString, ratio_setting.UpdateAudioCompletionRatioByJSONString},
	} {
		raw := cache.get()
		t.Cleanup(func() { require.NoError(t, cache.set(raw)) })
	}
	saved := map[string]string{}
	for key, value := range map[string]any{"billing_setting.billing_mode": billing_setting.GetBillingModeCopy(), "billing_setting.billing_expr": billing_setting.GetBillingExprCopy(), billing_setting.PluginBillingExprOption: billing_setting.GetPluginBillingExprCopy()} {
		data, err := common.Marshal(value)
		require.NoError(t, err)
		saved[key] = string(data)
	}
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
}

func TestModelPricingReviewMixedLegacySourcePreservesTextPricing(t *testing.T) {
	engine := setupModelPricingControllerTest(t)
	oldFloor, oldGroup := common.PreConsumedQuota, ratio_setting.GroupRatio2JSONString()
	common.PreConsumedQuota = 0
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		common.PreConsumedQuota = oldFloor
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroup))
	})
	name := "shared-alias"
	for _, channel := range []model.Channel{
		{Type: constant.ChannelTypeKling, Status: 1, Models: name, Group: "default", Key: "synthetic", ModelMapping: stringPointerForPricingReview(`{"shared-alias":"kling-v1"}`)},
		{Type: constant.ChannelTypeOpenAI, Status: 1, Models: name, Group: "default", Key: "synthetic", ModelMapping: stringPointerForPricingReview(`{"shared-alias":"gpt-4o"}`)},
	} {
		require.NoError(t, model.DB.Create(&channel).Error)
	}
	require.NoError(t, model.UpdateLegacyTieredPricing(map[string]model.LegacyTieredPricingItem{name: {Enabled: true, Expr: `p * 2`}}))
	entry := pricingControllerEntry(t, engine, name)
	var before []model.Option
	require.NoError(t, model.DB.Order("key").Find(&before).Error)
	draft := types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}
	response := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", map[string]any{"changes": []types.ModelPricingChange{controllerPricingChange(entry, draft)}})
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	response = pricingControllerRequest(t, engine, http.MethodPost, "/preview", map[string]any{"model_name": name, "pricing": draft})
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var after []model.Option
	require.NoError(t, model.DB.Order("key").Find(&after).Error)
	require.Equal(t, before, after)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{OriginModelName: name, UsingGroup: "default", UserGroup: "pricing-review-no-user-overrides", BillingRequestInput: &billingexpr.RequestInput{}}
	price, err := helper.ModelPriceHelper(ctx, info, 1_000_000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 2000, price.QuotaToPreConsume, "1M tokens * 2 / 1M * QPU1000, with the original group multiplier")
	require.Equal(t, `p * 2`, info.TieredBillingSnapshot.ExprString)
}

func TestModelPricingReviewLegacyTieredHTTPRejectsFirstTaskPrice(t *testing.T) {
	engine := setupModelPricingControllerTest(t)
	engine.PUT("/tiered", UpdateTieredBillingConfig)
	require.NoError(t, model.DB.Create(&model.Channel{Type: constant.ChannelTypeKling, Status: 1, Models: "kling-v1", Group: "default", Key: "synthetic"}).Error)
	var before []model.Option
	require.NoError(t, model.DB.Order("key").Find(&before).Error)
	response := pricingControllerRequest(t, engine, http.MethodPut, "/tiered", map[string]any{"config": map[string]any{"kling-v1": map[string]any{"enabled": true, "expr": `u("units") * 0.1`}}})
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var after []model.Option
	require.NoError(t, model.DB.Order("key").Find(&after).Error)
	require.Equal(t, before, after)
}

func stringPointerForPricingReview(s string) *string { return &s }

func TestModelPricingReviewRejectedLegacyRatiosPreserveCommittedCache(t *testing.T) {
	for _, cache := range []struct {
		key string
		get func() string
	}{
		{"ImageRatio", ratio_setting.ImageRatio2JSONString},
		{"AudioRatio", ratio_setting.AudioRatio2JSONString},
		{"AudioCompletionRatio", ratio_setting.AudioCompletionRatio2JSONString},
		{"CreateCacheRatio", ratio_setting.CreateCacheRatio2JSONString},
	} {
		t.Run(cache.key, func(t *testing.T) {
			engine := setupModelPricingControllerTest(t)
			require.NoError(t, model.DB.Create(&model.Channel{Type: constant.ChannelTypeKling, Status: 1, Models: "kling-v1", Group: "default", Key: "synthetic"}).Error)
			entry := pricingControllerEntry(t, engine, "kling-v1")
			draft := types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`, cache.key: float64(2)}
			response := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", map[string]any{"changes": []types.ModelPricingChange{controllerPricingChange(entry, draft)}})
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var stored model.Option
			require.NoError(t, model.DB.Where("key = ?", cache.key).First(&stored).Error)
			before := cache.get()
			response = pricingControllerRequest(t, engine, http.MethodPut, "/option", map[string]any{"key": cache.key, "value": `{"kling-v1":9}`})
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			var after model.Option
			require.NoError(t, model.DB.Where("key = ?", cache.key).First(&after).Error)
			require.Equal(t, stored.Value, after.Value, "a rejected legacy save must not change the database")
			var committedCache map[string]float64
			require.NoError(t, common.UnmarshalJsonStr(before, &committedCache))
			require.Equal(t, float64(2), committedCache["kling-v1"])
			require.JSONEq(t, before, cache.get(), "billing must retain the committed ratio after a rejected save")
		})
	}
}

func TestLegacyPricingCommittedCacheWarningRemainsAuditedSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		payload    map[string]any
		storedKey  string
		expected   string
	}{
		{"single", "/option", map[string]any{"key": "ModelPrice", "value": `{"cache-warning":0}`}, "ModelPrice", `{"cache-warning":0}`},
		{"pair", "/tiered", map[string]any{"options": map[string]string{"billing_setting.billing_mode": `{"cache-warning":"tiered_expr"}`, "billing_setting.billing_expr": `{"cache-warning":"p * 2"}`}}, "billing_setting.billing_mode", `{"cache-warning":"tiered_expr"}`},
		{"old-config", "/tiered", map[string]any{"config": map[string]any{"cache-warning": map[string]any{"enabled": true, "expr": "p * 2"}}}, "billing_setting.billing_mode", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := setupModelPricingControllerTest(t)
			engine.PUT("/tiered", UpdateTieredBillingConfig)
			// Leave an unrelated malformed authority row unchanged. The requested write
			// commits, but publishing that row into the typed runtime cache must fail.
			require.NoError(t, model.DB.Create(&model.Option{Key: "billing_setting.plugin_billing_expr", Value: `{"synthetic-plugin::unrelated-corrupt":false}`}).Error)
			response := pricingControllerRequest(t, engine, http.MethodPut, tc.path, tc.payload)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var envelope struct {
				Success        bool   `json:"success"`
				Committed      bool   `json:"committed"`
				CachePublished *bool  `json:"cache_published"`
				Message        string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
			require.True(t, envelope.Success, response.Body.String())
			require.True(t, envelope.Committed)
			require.NotNil(t, envelope.CachePublished)
			require.False(t, *envelope.CachePublished)
			require.Contains(t, envelope.Message, "configuration committed")
			var stored model.Option
			require.NoError(t, model.DB.Where("key = ?", tc.storedKey).First(&stored).Error)
			if tc.expected != "" {
				require.JSONEq(t, tc.expected, stored.Value)
			} else {
				require.Contains(t, stored.Value, `"cache-warning":"tiered_expr"`)
			}
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeManage).Find(&logs).Error)
			require.Len(t, logs, 1)
			require.Contains(t, logs[0].Other, `"action":"option.update"`)
			require.Contains(t, logs[0].Other, `"cache_published":false`)
		})
	}
}

// Inject an acknowledgement loss AFTER SQLite really commits the price. This
// wraps the existing transaction interface, never a production database.
type pricingReceiptLossPool struct {
	gorm.ConnPool
	lost atomic.Bool
}

func (p *pricingReceiptLossPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(gorm.TxBeginner).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &pricingReceiptLossTx{Tx: tx, pool: p}, nil
}

type pricingReceiptLossTx struct {
	*sql.Tx
	pool *pricingReceiptLossPool
}

func (tx *pricingReceiptLossTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.lost.CompareAndSwap(false, true) {
		return errors.New("synthetic lost pricing commit acknowledgement")
	}
	return nil
}
func TestModelPricingHTTPLostCommitReceiptRemainsUnknown(t *testing.T) {
	engine := setupModelPricingControllerTest(t)
	db := model.DB
	before := pricingControllerEntry(t, engine, "lost-receipt")
	payload := map[string]any{"changes": []types.ModelPricingChange{controllerPricingChange(before, types.ModelPricingValues{"ModelPrice": float64(0)})}}
	pool := &pricingReceiptLossPool{ConnPool: db.ConnPool}
	faultDB := db.Session(&gorm.Session{NewDB: true, Context: t.Context()})
	faultDB.ConnPool, faultDB.Statement.ConnPool = pool, pool
	model.DB = faultDB
	t.Cleanup(func() { model.DB = db })
	response := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", payload)
	model.DB = db
	require.True(t, pool.lost.Load(), response.Body.String())
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	// Independent database observation proves the uncertain response was NOT a
	// validation rejection or rollback; exact zero is the committed authority.
	var stored model.Option
	require.NoError(t, db.Where("key = ?", "ModelPrice").First(&stored).Error)
	var prices map[string]float64
	require.NoError(t, common.UnmarshalJsonStr(stored.Value, &prices))
	require.Equal(t, float64(0), prices["lost-receipt"])
	observed := pricingControllerEntry(t, engine, "lost-receipt")
	require.Equal(t, float64(0), observed.Configured["ModelPrice"])
	require.NotEqual(t, before.Version, observed.Version)
	replay := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", payload)
	require.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
}
func TestModelPricingHTTPDatabaseFailureIsNotValidation(t *testing.T) {
	for _, phase := range []string{"query", "write"} {
		t.Run(phase, func(t *testing.T) {
			engine := setupModelPricingControllerTest(t)
			db := model.DB
			before := pricingControllerEntry(t, engine, "db-failure")
			var rowsBefore []model.Option
			require.NoError(t, db.Order("key").Find(&rowsBefore).Error)
			failure := func(tx *gorm.DB) { tx.AddError(errors.New("synthetic pricing database failure")) }
			if phase == "query" {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("pricing_http_fault", failure))
				t.Cleanup(func() { _ = db.Callback().Query().Remove("pricing_http_fault") })
			} else {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("pricing_http_fault", failure))
				t.Cleanup(func() { _ = db.Callback().Create().Remove("pricing_http_fault") })
			}
			if phase == "query" {
				response := pricingControllerRequest(t, engine, http.MethodGet, "/pricing?model=db-failure", nil)
				require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
			}
			response := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", map[string]any{"changes": []types.ModelPricingChange{controllerPricingChange(before, types.ModelPricingValues{"ModelPrice": float64(0)})}})
			require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
			if phase == "query" {
				require.NoError(t, db.Callback().Query().Remove("pricing_http_fault"))
			} else {
				require.NoError(t, db.Callback().Create().Remove("pricing_http_fault"))
			}
			var rowsAfter []model.Option
			require.NoError(t, db.Order("key").Find(&rowsAfter).Error)
			require.Equal(t, rowsBefore, rowsAfter)
		})
	}
}
func TestModelPricingHTTPKnownValidationRetainsBadRequest(t *testing.T) {
	engine := setupModelPricingControllerTest(t)
	for _, payload := range []map[string]any{{"changes": []types.ModelPricingChange{}}, {"changes": []types.ModelPricingChange{controllerPricingChange(pricingControllerEntry(t, engine, "invalid-draft"), types.ModelPricingValues{"ModelPrice": float64(-1)})}}} {
		response := pricingControllerRequest(t, engine, http.MethodPatch, "/pricing", payload)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
}

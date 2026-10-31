// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"net/http"
	"net/http/httptest"
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

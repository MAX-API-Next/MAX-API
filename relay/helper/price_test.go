package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPreConsumedQuota := common.PreConsumedQuota
	common.PreConsumedQuota = 2_000
	t.Cleanup(func() { common.PreConsumedQuota = originalPreConsumedQuota })

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, 1500, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

func TestModelPriceHelperDefersAlphaSearchFloorUntilSurcharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalModelPrice := ratio_setting.ModelPrice2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrice))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})

	common.QuotaPerUnit = 1_000
	common.PreConsumedQuota = 500
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"alpha-floor-test":0.2}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "alpha-floor-test",
		UserGroup:       "default",
		UsingGroup:      "default",
	}

	priceData, err := ModelPriceHelper(ctx, info, 1, &types.TokenCountMeta{})

	require.NoError(t, err)
	require.Equal(t, 200, priceData.QuotaToPreConsume)
}

func TestModelPriceHelperPerCallRejectsUnpricedMJSunoTaskModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalSelfUseMode := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = true
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUseMode
	})

	for _, modelName := range []string{"mj_unmapped_action", "suno_unmapped_action"} {
		t.Run(modelName, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				OriginModelName: modelName,
				UserGroup:       "default",
				UsingGroup:      "default",
				ChannelMeta:     &relaycommon.ChannelMeta{},
				UserSetting: dto.UserSetting{
					AcceptUnsetRatioModel: true,
				},
			}

			_, err := ModelPriceHelperPerCall(ctx, info)

			require.Error(t, err)
			require.Contains(t, err.Error(), "not been priced")
		})
	}
}

func TestModelPriceHelperPerCallDefersStructuredMinimaxPricingToTaskPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalSelfUseMode := operation_setting.SelfUseModeEnabled
	originalModelPrice := ratio_setting.ModelPrice2JSONString()
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalRateCards, err := common.Marshal(task_billing_setting.GetRateCardsCopy())
	require.NoError(t, err)
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUseMode
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"task_billing_setting.rate_cards": string(originalRateCards),
		}))
	})

	operation_setting.SelfUseModeEnabled = false
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"task_billing_setting.rate_cards": `{"minimax/minimax-h3":{"vendor":"minimax","billing_type":"minimax","billing_config":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}}`,
	}))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "MiniMax-H3",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "MiniMax-H3",
		},
	}

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.Zero(t, priceData.Quota)
	require.Zero(t, priceData.QuotaToPreConsume)
	require.False(t, priceData.FreeModel)
	require.False(t, priceData.UsePrice)
	require.EqualValues(t, 1, priceData.GroupRatioInfo.GroupRatio)
}

func TestModelPriceHelperPerCallDoesNotUseH3PlanAfterMappingToLegacyModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalSelfUseMode := operation_setting.SelfUseModeEnabled
	originalModelPrice := ratio_setting.ModelPrice2JSONString()
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalRateCards, err := common.Marshal(task_billing_setting.GetRateCardsCopy())
	require.NoError(t, err)
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUseMode
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"task_billing_setting.rate_cards": string(originalRateCards),
		}))
	})

	operation_setting.SelfUseModeEnabled = false
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"task_billing_setting.rate_cards": `{"minimax/minimax-h3":{"vendor":"minimax","billing_type":"minimax","billing_config":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}}`,
	}))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "MiniMax-H3",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "MiniMax-Hailuo-2.3",
		},
	}

	_, err = ModelPriceHelperPerCall(ctx, info)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not been priced")
}

func TestModelPriceHelperPerCallDefersLegacyH3ProfilePricingToTaskPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const legacyH3ProfilesFixture = `{"minimax_h3_v2":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}`
	originalSelfUseMode := operation_setting.SelfUseModeEnabled
	originalModelPrice := ratio_setting.ModelPrice2JSONString()
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalRateCards, err := common.Marshal(task_billing_setting.GetRateCardsCopy())
	require.NoError(t, err)
	originalProfiles, err := common.Marshal(task_billing_setting.GetH3BillingProfilesCopy())
	require.NoError(t, err)
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUseMode
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"task_billing_setting.rate_cards":  string(originalRateCards),
			"task_billing_setting.h3_profiles": string(originalProfiles),
		}))
	})

	operation_setting.SelfUseModeEnabled = false
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"task_billing_setting.rate_cards":  `{}`,
		"task_billing_setting.h3_profiles": legacyH3ProfilesFixture,
	}))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "MiniMax-H3",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "MiniMax-H3",
		},
	}

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.Zero(t, priceData.Quota)
	require.Zero(t, priceData.QuotaToPreConsume)
	require.False(t, priceData.UsePrice)
}

func TestModelPriceHelperPerCallUsesDefaultTaskPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	quotaSetting := operation_setting.GetQuotaSetting()
	originalEnableFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		quotaSetting.EnableFreeModelPreConsume = originalEnableFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})
	common.QuotaPerUnit = 1000
	common.PreConsumedQuota = 0
	quotaSetting.EnableFreeModelPreConsume = true
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "suno_music",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.UsePrice)
	require.Equal(t, float64(1), priceData.GroupRatioInfo.GroupRatio)
	require.Equal(t, 0.1, priceData.ModelPrice)
	require.Equal(t, 100, priceData.Quota)
}

func TestModelPriceHelperPerCallSeparatesFinalAndPreConsumedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})

	common.QuotaPerUnit = 1000
	common.PreConsumedQuota = 1000
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "suno_music",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.False(t, priceData.FreeModel)
	// The final per-call price remains the model price; the configured value
	// is only the amount reserved before the upstream request.
	require.Equal(t, 100, priceData.Quota)
	require.Equal(t, 1000, priceData.QuotaToPreConsume)
}

func TestModelPriceHelperAppliesPreConsumedQuotaToFixedPriceModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalModelPrice := ratio_setting.ModelPrice2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrice))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})

	common.QuotaPerUnit = 1000
	common.PreConsumedQuota = 1000
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"fixed-preconsume-test":0.1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "fixed-preconsume-test",
		UserGroup:       "default",
		UsingGroup:      "default",
	}

	priceData, err := ModelPriceHelper(ctx, info, 1, &types.TokenCountMeta{})

	require.NoError(t, err)
	require.True(t, priceData.UsePrice)
	require.Equal(t, 1000, priceData.QuotaToPreConsume)
}

func TestModelPriceHelperUsesConfiguredPreConsumedQuotaAsPromptEstimateFloor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})

	common.QuotaPerUnit = 1
	common.PreConsumedQuota = 100
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ratio-preconsume-test":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	for _, test := range []struct {
		name        string
		prompt      int
		maxTokens   int
		expectedPre int
	}{
		{name: "below floor", prompt: 50, expectedPre: 100},
		{name: "above floor", prompt: 250, expectedPre: 250},
		{name: "includes completion estimate", prompt: 50, maxTokens: 100, expectedPre: 150},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: "ratio-preconsume-test",
				UserGroup:       "default",
				UsingGroup:      "default",
			}
			priceData, err := ModelPriceHelper(ctx, info, test.prompt, &types.TokenCountMeta{MaxTokens: test.maxTokens})

			require.NoError(t, err)
			require.False(t, priceData.UsePrice)
			// PreConsumedQuota is the minimum admission reservation, not an
			// extra token estimate that should be multiplied by the ratio.
			require.Equal(t, test.expectedPre, priceData.QuotaToPreConsume)
		})
	}
}

func TestAddNonNegativeIntsRejectsInvalidBillingEstimates(t *testing.T) {
	_, err := addNonNegativeInts(-1, 1)
	require.Error(t, err)

	maxInt := int(^uint(0) >> 1)
	_, err = addNonNegativeInts(maxInt, 1)
	require.Error(t, err)
}

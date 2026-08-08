package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
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

func TestModelPriceHelperPerCallUsesDefaultTaskPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
	})

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
	require.Equal(t, 100, priceData.Quota)
}

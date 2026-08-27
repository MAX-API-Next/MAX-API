package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/stretchr/testify/require"
)

func TestPrepareAlphaSearchPreConsumedQuotaAppliesFloorAfterSurcharge(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	common.QuotaPerUnit = 1_000
	common.PreConsumedQuota = 500

	toolPrices := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	originalPrices := make(map[string]float64, len(toolPrices.Prices))
	for key, value := range toolPrices.Prices {
		originalPrices[key] = value
	}
	toolPrices.Prices = map[string]float64{"web_search_preview": 10}
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		toolPrices.Prices = originalPrices
		operation_setting.RebuildToolPriceIndex()
	})

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "alpha-floor-test",
	}

	for _, test := range []struct {
		name         string
		baseQuota    int
		freeModel    bool
		expected     int
		expectedFree bool
	}{
		{name: "total remains below floor", baseQuota: 200, expected: 500},
		{name: "surcharge crosses floor", baseQuota: 495, expected: 505},
		{name: "tool surcharge makes free model billable", freeModel: true, expected: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			priceData, err := prepareAlphaSearchPreConsumedQuota(types.PriceData{
				FreeModel:         test.freeModel,
				QuotaToPreConsume: test.baseQuota,
				GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 1},
			}, info)

			require.NoError(t, err)
			require.Equal(t, test.expected, priceData.QuotaToPreConsume)
			require.Equal(t, test.expectedFree, priceData.FreeModel)
		})
	}
}

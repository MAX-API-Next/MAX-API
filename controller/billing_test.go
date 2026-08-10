package controller

import (
	"math"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestBillingQuotaDisplayAmountRejectsInvalidQuotaPerUnit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	for _, quotaPerUnit := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		t.Run("invalid_quota_per_unit", func(t *testing.T) {
			common.QuotaPerUnit = quotaPerUnit
			operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

			_, err := billingQuotaDisplayAmount(1000)

			require.Error(t, err)
		})
	}
}

func TestBillingQuotaDisplayAmountAllowsTokenDisplayWithInvalidQuotaPerUnit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	common.QuotaPerUnit = 0
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	amount, err := billingQuotaDisplayAmount(1000)

	require.NoError(t, err)
	require.Equal(t, 1000.0, amount)
}

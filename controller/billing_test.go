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

func TestBillingQuotaDisplayAmountRejectsInvalidUSDExchangeRate(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldUSDExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		operation_setting.USDExchangeRate = oldUSDExchangeRate
	})

	common.QuotaPerUnit = 500
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY

	tests := []struct {
		name            string
		usdExchangeRate float64
	}{
		{name: "zero", usdExchangeRate: 0},
		{name: "negative", usdExchangeRate: -1},
		{name: "nan", usdExchangeRate: math.NaN()},
		{name: "infinite", usdExchangeRate: math.Inf(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation_setting.USDExchangeRate = tt.usdExchangeRate

			_, err := billingQuotaDisplayAmount(1000)

			require.Error(t, err)
		})
	}
}

func TestBillingQuotaDisplayAmountAllowsValidUSDExchangeRate(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldUSDExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		operation_setting.USDExchangeRate = oldUSDExchangeRate
	})

	common.QuotaPerUnit = 500
	operation_setting.USDExchangeRate = 7.2
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY

	amount, err := billingQuotaDisplayAmount(1000)

	require.NoError(t, err)
	require.Equal(t, 14.4, amount)
}

func TestBillingSubscriptionDisplayAmountAllowsUnlimitedTokenWithInvalidQuotaPerUnit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	common.QuotaPerUnit = 0
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	amount, err := billingSubscriptionDisplayAmount(0, true)

	require.NoError(t, err)
	require.Equal(t, 100000000.0, amount)
}

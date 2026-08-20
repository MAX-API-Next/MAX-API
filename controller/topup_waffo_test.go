package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestWaffoCreditedQuotaMatchesPersistedAmount(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
	})

	common.QuotaPerUnit = 500000

	tests := []struct {
		name        string
		displayType string
		amount      int64
		wantStored  int64
		wantQuota   string
	}{
		{name: "currency", displayType: operation_setting.QuotaDisplayTypeUSD, amount: 3, wantStored: 3, wantQuota: "1500000"},
		{name: "token sub unit", displayType: operation_setting.QuotaDisplayTypeTokens, amount: 1, wantStored: 1, wantQuota: "500000"},
		{name: "token whole units", displayType: operation_setting.QuotaDisplayTypeTokens, amount: 1500000, wantStored: 3, wantQuota: "1500000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.displayType
			stored := normalizeWaffoTopUpAmount(tc.amount)
			require.Equal(t, tc.wantStored, stored)
			require.True(t, decimal.RequireFromString(tc.wantQuota).Equal(waffoCreditedQuota(tc.amount)))
		})
	}
}

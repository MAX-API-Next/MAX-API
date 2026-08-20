package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBuildWaffoTopUpQuoteKeepsPriceAndCreditConsistent(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	originalUnitPrice := setting.WaffoUnitPrice
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
		setting.WaffoUnitPrice = originalUnitPrice
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	common.QuotaPerUnit = 500000
	setting.WaffoUnitPrice = 1
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))

	tests := []struct {
		name        string
		displayType string
		amount      int64
		wantStored  int64
		wantQuota   string
		wantMoney   float64
	}{
		{name: "currency", displayType: operation_setting.QuotaDisplayTypeUSD, amount: 3, wantStored: 3, wantQuota: "1500000", wantMoney: 3},
		{name: "token sub unit", displayType: operation_setting.QuotaDisplayTypeTokens, amount: 1, wantStored: 1, wantQuota: "500000", wantMoney: 1},
		{name: "token whole units", displayType: operation_setting.QuotaDisplayTypeTokens, amount: 1500000, wantStored: 3, wantQuota: "1500000", wantMoney: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.displayType
			quote := buildWaffoTopUpQuote(tc.amount, "default")
			require.Equal(t, tc.wantStored, quote.amount)
			require.True(t, decimal.RequireFromString(tc.wantQuota).Equal(quote.creditedQuota))
			require.InDelta(t, tc.wantMoney, quote.payMoney, 0.000001)
		})
	}
}

func TestRequestWaffoAmountPricesSubUnitTokenFromCreditedQuota(t *testing.T) {
	db := setupWaffoPancakeControllerTestDB(t)
	originalUnitPrice := setting.WaffoUnitPrice
	originalMinTopUp := setting.WaffoMinTopUp
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		setting.WaffoUnitPrice = originalUnitPrice
		setting.WaffoMinTopUp = originalMinTopUp
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoUnitPrice = 1
	setting.WaffoMinTopUp = 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))

	user := &model.User{Id: 1301, Username: "waffo-token-user", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	payload, err := common.Marshal(WaffoPayRequest{Amount: 1})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/waffo/amount", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", user.Id)

	RequestWaffoAmount(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "success", response.Message)
	require.Equal(t, "1.00", response.Data)
}

func TestRequestWaffoPayPersistsSubUnitTokenQuoteConsistently(t *testing.T) {
	db := setupWaffoPancakeControllerTestDB(t)
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalUnitPrice := setting.WaffoUnitPrice
	originalMinTopUp := setting.WaffoMinTopUp
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoUnitPrice = originalUnitPrice
		setting.WaffoMinTopUp = originalMinTopUp
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = "api-key"
	setting.WaffoPrivateKey = "invalid-private-key"
	setting.WaffoPublicCert = "invalid-public-key"
	setting.WaffoUnitPrice = 1
	setting.WaffoMinTopUp = 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))

	user := &model.User{Id: 1302, Username: "waffo-order-token-user", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	payload, err := common.Marshal(WaffoPayRequest{Amount: 1})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/waffo/pay", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", user.Id)

	RequestWaffoPay(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"data":"支付配置错误"`)
	var topUps []model.TopUp
	require.NoError(t, db.Find(&topUps).Error)
	require.Len(t, topUps, 1)
	require.Equal(t, int64(1), topUps[0].Amount)
	require.InDelta(t, 1, topUps[0].Money, 0.000001)
	require.Equal(t, common.TopUpStatusFailed, topUps[0].Status)
}

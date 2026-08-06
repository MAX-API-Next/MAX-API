package model

import (
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    int64(quota),
	}
	require.NoError(t, DB.Create(user).Error)
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func countTopUpsForPaymentGuardTest(t *testing.T, tradeNo string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", tradeNo).Count(&count).Error)
	return count
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.EqualValues(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder("sub-guard-order", `{"provider":"epay"}`, PaymentProviderEpay, "alipay")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	topUp := GetTopUpByTradeNo("sub-guard-order")
	assert.Nil(t, topUp)
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := ExpireSubscriptionOrder("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}

func TestCompleteSubscriptionOrder_IdempotentDoesNotDuplicateSideEffects(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 404, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 501)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-idempotent-order", 404, plan.Id, PaymentProviderStripe)

	require.NoError(t, CompleteSubscriptionOrder("sub-idempotent-order", `{"event":"first"}`, PaymentProviderStripe, "card"))
	require.NoError(t, CompleteSubscriptionOrder("sub-idempotent-order", `{"event":"second"}`, PaymentProviderStripe, "card"))

	order := GetSubscriptionOrderByTradeNo("sub-idempotent-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	assert.Equal(t, "card", order.PaymentMethod)
	assert.Equal(t, `{"event":"first"}`, order.ProviderPayload)
	assert.EqualValues(t, 1, countUserSubscriptionsForPaymentGuardTest(t, 404))
	assert.EqualValues(t, 1, countTopUpsForPaymentGuardTest(t, "sub-idempotent-order"))
}

func TestPurchaseSubscriptionWithBalance_InsufficientQuotaDoesNotOverdraw(t *testing.T) {
	truncateTables(t)

	plan := insertSubscriptionPlanForPaymentGuardTest(t, 601)
	requiredQuota, err := calcSubscriptionBalanceQuota(plan.PriceAmount)
	require.NoError(t, err)
	require.Greater(t, requiredQuota, 0)
	insertUserForPaymentGuardTest(t, 505, requiredQuota-1)

	err = PurchaseSubscriptionWithBalance(505, plan.Id)
	require.Error(t, err)
	assert.EqualValues(t, requiredQuota-1, getUserQuotaForPaymentGuardTest(t, 505))
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 505))
}

func TestPurchaseSubscriptionWithBalanceRetriesCommittedUserCacheInvalidation(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 506, 10)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 602)
	plan.PriceAmount = 0
	plan.UpgradeGroup = "premium"
	require.NoError(t, DB.Model(plan).Select("price_amount", "upgrade_group").Updates(plan).Error)

	client, _ := useFailingCacheMutationRedis(t, 1)
	cacheUserForRetryTest(t, client, User{
		Id: 506, Username: "payment_guard_user", Group: "default", Quota: 10, Status: common.UserStatusEnabled,
	})

	require.NoError(t, PurchaseSubscriptionWithBalance(506, plan.Id))
	requireCacheKeyDeletedEventually(t, client, getUserCacheKey(506))
	var stored User
	require.NoError(t, DB.First(&stored, 506).Error)
	assert.Equal(t, "premium", stored.Group)
}

func TestRedeem_UsedCodeDoesNotDoubleCredit(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 606, 0)
	redemption := &Redemption{
		Key:         "redeem-guard-code",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       123,
		CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, DB.Create(redemption).Error)

	quota, err := Redeem("redeem-guard-code", 606)
	require.NoError(t, err)
	assert.Equal(t, 123, quota)

	quota, err = Redeem("redeem-guard-code", 606)
	require.ErrorIs(t, err, ErrRedeemFailed)
	assert.Zero(t, quota)
	assert.EqualValues(t, 123, getUserQuotaForPaymentGuardTest(t, 606))

	var reloaded Redemption
	require.NoError(t, DB.Where("id = ?", redemption.Id).First(&reloaded).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, reloaded.Status)
	assert.Equal(t, 606, reloaded.UsedUserId)
}

func TestEnsureUserUpdateMatchedTx_AllowsNoopRowsAffectedWhenUserExists(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 607, 0)
	missingErr := errors.New("missing user")

	err := DB.Transaction(func(tx *gorm.DB) error {
		return ensureUserUpdateMatchedTx(tx, &gorm.DB{RowsAffected: 0}, 607, missingErr)
	})
	require.NoError(t, err)
}

func TestEnsureUserUpdateMatchedTx_ReturnsMissingWhenUserAbsent(t *testing.T) {
	truncateTables(t)

	missingErr := errors.New("missing user")

	err := DB.Transaction(func(tx *gorm.DB) error {
		return ensureUserUpdateMatchedTx(tx, &gorm.DB{RowsAffected: 0}, 608, missingErr)
	})
	require.ErrorIs(t, err, missingErr)
}

func TestRedeemRejectsNonPositiveQuotaWithoutUsingCode(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 609, 0)
	redemption := &Redemption{
		Key:         "redeem-zero-quota",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       0,
		CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	require.NoError(t, DB.Model(redemption).Update("quota", 0).Error)

	quota, err := Redeem("redeem-zero-quota", 609)
	require.ErrorIs(t, err, ErrRedeemFailed)
	assert.Zero(t, quota)
	assert.EqualValues(t, 0, getUserQuotaForPaymentGuardTest(t, 609))

	var reloaded Redemption
	require.NoError(t, DB.Where("id = ?", redemption.Id).First(&reloaded).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, reloaded.Status)
	assert.Zero(t, reloaded.UsedUserId)
}

func TestRechargeCreemRejectsZeroQuotaBeforeCompletingOrder(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 610, 0)
	topUp := &TopUp{
		UserId:          610,
		Amount:          0,
		Money:           9.99,
		TradeNo:         "creem-zero-quota",
		PaymentMethod:   PaymentProviderCreem,
		PaymentProvider: PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	err := RechargeCreem("creem-zero-quota", "", "", "127.0.0.1")
	require.Error(t, err)
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "creem-zero-quota"))
	assert.EqualValues(t, 0, getUserQuotaForPaymentGuardTest(t, 610))
}

func TestRechargeRejectsPaidAmountMismatchBeforeCompletingOrder(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 613, 0)
	topUp := &TopUp{
		UserId:          613,
		Amount:          10,
		Money:           9.99,
		TradeNo:         "stripe-amount-mismatch",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	err := Recharge("stripe-amount-mismatch", "cus_test", "127.0.0.1",
		PaymentValidationFromMinorUnits(998, "USD", "USD", false))
	require.ErrorIs(t, err, ErrPaymentAmountMismatch)
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "stripe-amount-mismatch"))
	assert.EqualValues(t, 0, getUserQuotaForPaymentGuardTest(t, 613))
}

func TestRechargeEpayCompletesOrderAndCreditsQuotaAtomically(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 614, 0)
	topUp := &TopUp{
		UserId:          614,
		Amount:          3,
		Money:           6.66,
		TradeNo:         "epay-atomic-success",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, RechargeEpay("epay-atomic-success", "wxpay", "127.0.0.1",
		PaymentValidationFromMajorString("6.66", "", "", false)))

	reloaded := GetTopUpByTradeNo("epay-atomic-success")
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.Equal(t, "wxpay", reloaded.PaymentMethod)
	assert.EqualValues(t, int64(3*common.QuotaPerUnit), getUserQuotaForPaymentGuardTest(t, 614))
}

func TestRechargeEpayRejectsPaidAmountMismatchBeforeCompletingOrder(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 615, 0)
	topUp := &TopUp{
		UserId:          615,
		Amount:          3,
		Money:           6.66,
		TradeNo:         "epay-amount-mismatch",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	err := RechargeEpay("epay-amount-mismatch", "alipay", "127.0.0.1",
		PaymentValidationFromMajorString("6.65", "", "", false))
	require.ErrorIs(t, err, ErrPaymentAmountMismatch)
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "epay-amount-mismatch"))
	assert.EqualValues(t, 0, getUserQuotaForPaymentGuardTest(t, 615))
}

func TestCompleteSubscriptionOrderRejectsPaidCurrencyMismatch(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 616, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 616)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-currency-mismatch", 616, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder("sub-currency-mismatch", `{"provider":"stripe"}`, PaymentProviderStripe, "card",
		PaymentValidationFromMinorUnits(999, "EUR", "USD", false))
	require.ErrorIs(t, err, ErrPaymentCurrencyMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-currency-mismatch")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 616))
}

func TestRechargeCreemSkipsDuplicateCustomerEmailBinding(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&User{
		Id:       611,
		Username: "creem-email-owner",
		Email:    "taken@example.com",
		AffCode:  "creem611",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&User{
		Id:       612,
		Username: "creem-empty-email",
		AffCode:  "creem612",
		Status:   common.UserStatusEnabled,
	}).Error)
	insertTopUpForPaymentGuardTest(t, "creem-duplicate-email", 612, PaymentProviderCreem)

	err := RechargeCreem("creem-duplicate-email", " Taken@Example.COM ", "", "127.0.0.1")
	require.NoError(t, err)

	var got User
	require.NoError(t, DB.First(&got, 612).Error)
	assert.Empty(t, got.Email)
	assert.Empty(t, got.NormalizedEmail)
	assert.EqualValues(t, 2, got.Quota)
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "creem-duplicate-email"))

	count, err := CountUsersByEmail("taken@example.com")
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestRefundSubscriptionPreConsume_IdempotentDoesNotDoubleRefund(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 707, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 701)
	sub := &UserSubscription{
		UserId:      707,
		PlanId:      plan.Id,
		AmountTotal: 100,
		AmountUsed:  50,
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
		Status:      "active",
		Source:      "order",
	}
	require.NoError(t, DB.Create(sub).Error)
	record := &SubscriptionPreConsumeRecord{
		RequestId:          "refund-guard-request",
		UserId:             707,
		UserSubscriptionId: sub.Id,
		PreConsumed:        30,
		Status:             "consumed",
	}
	require.NoError(t, DB.Create(record).Error)

	require.NoError(t, RefundSubscriptionPreConsume("refund-guard-request"))
	require.NoError(t, RefundSubscriptionPreConsume("refund-guard-request"))

	var reloadedSub UserSubscription
	require.NoError(t, DB.Where("id = ?", sub.Id).First(&reloadedSub).Error)
	assert.EqualValues(t, 20, reloadedSub.AmountUsed)

	var reloadedRecord SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("id = ?", record.Id).First(&reloadedRecord).Error)
	assert.Equal(t, "refunded", reloadedRecord.Status)
}

func TestPostConsumeUserSubscriptionDelta_ReturnsAppliedNegativeDelta(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 808, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 801)
	sub := &UserSubscription{
		UserId:      808,
		PlanId:      plan.Id,
		AmountTotal: 100,
		AmountUsed:  5,
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
		Status:      "active",
		Source:      "order",
	}
	require.NoError(t, DB.Create(sub).Error)

	applied, err := PostConsumeUserSubscriptionDelta(sub.Id, -10)
	require.NoError(t, err)
	assert.EqualValues(t, -5, applied)

	var reloadedSub UserSubscription
	require.NoError(t, DB.Where("id = ?", sub.Id).First(&reloadedSub).Error)
	assert.EqualValues(t, 0, reloadedSub.AmountUsed)
}

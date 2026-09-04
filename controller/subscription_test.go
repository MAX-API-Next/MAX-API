package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	appi18n "github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

type subscriptionAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func denyPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func newSubscriptionTestContext(t *testing.T, target string, body any, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	return ctx, recorder
}

func decodeSubscriptionAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) subscriptionAPIResponse {
	t.Helper()
	var response subscriptionAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestAdminResetUserSubscriptionsByPlanRequiresPaymentCompliance(t *testing.T) {
	require.NoError(t, appi18n.Init())
	denyPaymentComplianceForTest(t)

	ctx, recorder := newSubscriptionTestContext(t,
		"/api/user/1/subscription/reset",
		AdminResetSubscriptionRequest{PlanId: 1},
		gin.Params{{Key: "id", Value: "1"}},
	)
	AdminResetUserSubscriptionsByPlan(ctx)

	response := decodeSubscriptionAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)
}

func TestAdminResetPlanSubscriptionsRequiresPaymentCompliance(t *testing.T) {
	require.NoError(t, appi18n.Init())
	denyPaymentComplianceForTest(t)

	ctx, recorder := newSubscriptionTestContext(t,
		"/api/subscription/plan/1/reset",
		AdminResetSubscriptionRequest{},
		gin.Params{{Key: "id", Value: "1"}},
	)
	AdminResetPlanSubscriptions(ctx)

	response := decodeSubscriptionAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)
}

func TestSubscriptionPlanUpdateMapUsesOnlyModelColumns(t *testing.T) {
	parsed, err := schema.Parse(&model.SubscriptionPlan{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	columns := make(map[string]struct{}, len(parsed.DBNames))
	for _, name := range parsed.DBNames {
		columns[name] = struct{}{}
	}
	allowBalance := false
	allowWallet := false
	updates := subscriptionPlanUpdateMap(model.SubscriptionPlan{
		Title: "plan", AllowBalancePay: &allowBalance, AllowWalletOverflow: &allowWallet,
		DowngradeGroup: "default",
	})
	for key := range updates {
		if _, ok := columns[key]; !ok {
			t.Fatalf("subscription plan update key %q is not a model column", key)
		}
	}
	require.Contains(t, updates, "allow_wallet_overflow")
	require.Contains(t, updates, "downgrade_group")
}

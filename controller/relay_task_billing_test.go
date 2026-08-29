package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingTaskBillingSettler struct {
	err error
}

func (s *failingTaskBillingSettler) Settle(int) error         { return s.err }
func (s *failingTaskBillingSettler) Refund(*gin.Context)      {}
func (s *failingTaskBillingSettler) NeedsRefund() bool        { return true }
func (s *failingTaskBillingSettler) GetPreConsumedQuota() int { return 10 }
func (s *failingTaskBillingSettler) Reserve(int) error        { return nil }

type recordingSelectedGroupBillingSettler struct {
	preConsumed int
	reserves    []int
}

func (s *recordingSelectedGroupBillingSettler) Settle(int) error    { return nil }
func (s *recordingSelectedGroupBillingSettler) Refund(*gin.Context) {}
func (s *recordingSelectedGroupBillingSettler) NeedsRefund() bool   { return true }
func (s *recordingSelectedGroupBillingSettler) GetPreConsumedQuota() int {
	return s.preConsumed
}
func (s *recordingSelectedGroupBillingSettler) Reserve(targetQuota int) error {
	s.reserves = append(s.reserves, targetQuota)
	if targetQuota > s.preConsumed {
		s.preConsumed = targetQuota
	}
	return nil
}

func TestFinalizeTaskSubmissionDoesNotWriteSuccessAfterSettlementFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	settlementErr := errors.New("user quota is not enough")
	info := &relaycommon.RelayInfo{
		UserId:  71,
		TokenId: 72,
		Billing: &failingTaskBillingSettler{err: settlementErr},
	}
	result := &relay.TaskSubmitResult{
		Quota: 20,
		Task:  &model.Task{TaskID: "task-settlement-review"},
	}
	writeCount := 0

	taskErr := finalizeTaskSubmission(c, info, result, "", func() error {
		writeCount++
		return nil
	})

	require.NotNil(t, taskErr)
	assert.Equal(t, constant.MjBillingSettlementPending, taskErr.Code)
	assert.Equal(t, http.StatusConflict, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, map[string]string{"task_id": "task-settlement-review"}, taskErr.Data)
	assert.Zero(t, writeCount)
}

func TestPendingTaskSettlementDoesNotMarkAcceptedTaskAsManualFailure(t *testing.T) {
	taskErr := &dto.TaskError{Code: constant.MjBillingSettlementPending}

	assert.False(t, shouldMarkTaskSubmitNeedsReview(taskErr, true, true))
	assert.True(t, shouldMarkTaskSubmitNeedsReview(taskErr, false, true))
	assert.True(t, shouldMarkTaskSubmitNeedsReview(taskErr, true, false))
	assert.True(t, shouldMarkTaskSubmitNeedsReview(&dto.TaskError{Code: "persist_task_failed"}, true, true))
}

func TestMidjourneyRelayErrorStatusCodeReportsSettlementPendingAsConflict(t *testing.T) {
	assert.Equal(t, http.StatusConflict, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{
		Code:        constant.MjRequestError,
		Description: constant.MjBillingSettlementPending,
	}))
	assert.Equal(t, http.StatusTooManyRequests, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{Code: 30}))
	assert.Equal(t, http.StatusBadRequest, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{Code: constant.MjRequestError}))
}

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

func TestPrepareBillingForSelectedGroupRepricesAlphaSearchCrossGroupRetry(t *testing.T) {
	const modelName = "alpha-cross-group-retry"
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	common.QuotaPerUnit = 1_000
	common.PreConsumedQuota = 2_500
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"alpha-cross-group-retry":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"initial":1,"retry":3}`))

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
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		toolPrices.Prices = originalPrices
		operation_setting.RebuildToolPriceIndex()
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "retry")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "retry")
	billing := &recordingSelectedGroupBillingSettler{preConsumed: 2_500}
	info := &relaycommon.RelayInfo{
		RelayMode:             relayconstant.RelayModeAlphaSearch,
		OriginModelName:       modelName,
		UsingGroup:            "initial",
		Billing:               billing,
		FinalPreConsumedQuota: 2_500,
	}

	require.Nil(t, prepareBillingForSelectedGroup(c, info, 1_000, &types.TokenCountMeta{}))

	require.Equal(t, "retry", info.UsingGroup)
	require.Equal(t, 3.0, info.PriceData.GroupRatioInfo.GroupRatio)
	require.Equal(t, 3_030, info.PriceData.QuotaToPreConsume)
	require.Equal(t, []int{3_030}, billing.reserves)
	require.Equal(t, 3_030, info.FinalPreConsumedQuota)
}

func TestPrepareBillingForSelectedGroupClearsFallbackQuotaForFreeRetry(t *testing.T) {
	const modelName = "free-cross-group-retry"
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	quotaSetting := operation_setting.GetQuotaSetting()
	originalFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"free-cross-group-retry":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"initial":1,"free":0}`))
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = originalFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "free")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "free")
	billing := &recordingSelectedGroupBillingSettler{preConsumed: 2_500}
	info := &relaycommon.RelayInfo{
		OriginModelName:       modelName,
		UsingGroup:            "initial",
		Billing:               billing,
		FinalPreConsumedQuota: 2_500,
	}

	require.Nil(t, prepareBillingForSelectedGroup(c, info, 1_000, &types.TokenCountMeta{}))

	require.Equal(t, "free", info.UsingGroup)
	require.True(t, info.PriceData.FreeModel)
	require.Zero(t, info.FinalPreConsumedQuota)
	require.Same(t, billing, info.Billing)
	require.Equal(t, 2_500, billing.GetPreConsumedQuota())
	require.Empty(t, billing.reserves)
}

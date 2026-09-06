package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsUnregisteredKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{"SystemName": "MAX API"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(`{"key":"unexpected.option","value":"x"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateOption(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPreviewH3BillingReturnsDraftQuotesWithoutMutatingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	originalProfiles := task_billing_setting.GetH3BillingProfilesCopy()
	profile, ok := task_billing_setting.GetH3BillingProfileCopy(task_billing_setting.H3BillingProfileKey)
	require.True(t, ok)
	profile.OutputUnitPrice["768P"] = "0.10"
	profile.InputVideoUnitPrice["768P"] = "0.10"
	request := H3BillingPreviewRequest{
		Profile:               *profile,
		Resolution:            "768P",
		OutputDurationSeconds: 5,
		InputVideoCount:       1,
		GroupRatio:            1,
		Actual: &H3BillingPreviewActual{
			OutputDurationMs:     testInt64Pointer(5_000),
			InputVideoDurationMs: testInt64Pointer(7_500),
			InputImageCount:      testInt64Pointer(0),
		},
	}
	body, err := common.Marshal(request)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/option/h3_billing/preview", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PreviewH3Billing(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                                  `json:"success"`
		Data    task_billing_setting.H3BillingPreview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, 2000, response.Data.Reserve.Quota)
	require.Equal(t, 1250, response.Data.Final.Quota)
	require.Equal(t, 750, *response.Data.RefundQuota)
	require.Equal(t, originalProfiles, task_billing_setting.GetH3BillingProfilesCopy())
}

func TestPreviewH3BillingRejectsInvalidDraft(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile, ok := task_billing_setting.GetH3BillingProfileCopy(task_billing_setting.H3BillingProfileKey)
	require.True(t, ok)
	profile.InputAudioUnitPrice = "0.01"
	body, err := common.Marshal(H3BillingPreviewRequest{
		Profile:               *profile,
		Resolution:            "768P",
		OutputDurationSeconds: 5,
		GroupRatio:            1,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/option/h3_billing/preview", bytes.NewReader(body))

	PreviewH3Billing(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.Contains(t, recorder.Body.String(), "input_audio_unit_price must be zero")
}

func TestPreviewH3BillingDerivesActualUsageCompleteness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile, ok := task_billing_setting.GetH3BillingProfileCopy(task_billing_setting.H3BillingProfileKey)
	require.True(t, ok)

	tests := []struct {
		name            string
		actual          *H3BillingPreviewActual
		wantSuccess     bool
		wantFinal       bool
		wantMessagePart string
	}{
		{
			name:        "empty actual is treated as absent",
			actual:      &H3BillingPreviewActual{},
			wantSuccess: true,
		},
		{
			name: "missing output duration is partial",
			actual: &H3BillingPreviewActual{
				InputImageCount: testInt64Pointer(0),
			},
			wantMessagePart: "not automatically billable: partial",
		},
		{
			name: "missing image count is partial",
			actual: &H3BillingPreviewActual{
				OutputDurationMs: testInt64Pointer(5_000),
			},
			wantMessagePart: "not automatically billable: partial",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := common.Marshal(H3BillingPreviewRequest{
				Profile:               *profile,
				Resolution:            "768P",
				OutputDurationSeconds: 5,
				GroupRatio:            1,
				Actual:                test.actual,
			})
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/option/h3_billing/preview", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			PreviewH3Billing(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool                                   `json:"success"`
				Message string                                 `json:"message"`
				Data    *task_billing_setting.H3BillingPreview `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, test.wantSuccess, response.Success)
			if test.wantSuccess {
				require.NotNil(t, response.Data)
				require.Equal(t, test.wantFinal, response.Data.Final != nil)
				return
			}
			require.Contains(t, response.Message, test.wantMessagePart)
		})
	}
}

func testInt64Pointer(value int64) *int64 {
	return &value
}

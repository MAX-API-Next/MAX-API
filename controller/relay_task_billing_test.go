package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
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
	assert.Equal(t, "billing_settlement_pending", taskErr.Code)
	assert.Equal(t, http.StatusConflict, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, map[string]string{"task_id": "task-settlement-review"}, taskErr.Data)
	assert.Zero(t, writeCount)
}

func TestMidjourneyRelayErrorStatusCodeReportsSettlementPendingAsConflict(t *testing.T) {
	assert.Equal(t, http.StatusConflict, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{
		Code:        constant.MjRequestError,
		Description: constant.MjBillingSettlementPending,
	}))
	assert.Equal(t, http.StatusTooManyRequests, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{Code: 30}))
	assert.Equal(t, http.StatusBadRequest, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{Code: constant.MjRequestError}))
}

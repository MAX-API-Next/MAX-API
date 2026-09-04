package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayTaskSubmitAttemptAdvancesToolUsageRetryIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("platform", "invalid-test-platform")
	info := &relaycommon.RelayInfo{
		OriginModelName: "task-test",
		RetryIndex:      0,
		ToolUsage:       relaycommon.NewToolUsageLedger("task-test"),
	}
	info.ToolUsage.BeginAttempt(0)

	_, taskErr := relayTaskSubmitAttempt(c, info, 1)
	require.NotNil(t, taskErr)
	require.Equal(t, 1, info.RetryIndex)
	require.Equal(t, 1, info.ToolUsageSnapshot().Attempt)
}

func TestShouldRetryEmptyResponseRequiresFeatureFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	oldRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
		operation_setting.AutomaticRetryStatusCodeRanges = oldRetryRanges
	})

	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: http.StatusBadGateway, End: http.StatusBadGateway}}
	err := types.NewOpenAIError(errors.New("empty completion"), types.ErrorCodeEmptyCompletion, http.StatusBadGateway)

	common.EmptyCompletionRetryEnabled = false
	require.False(t, shouldRetry(c, err, 1))

	common.EmptyCompletionRetryEnabled = true
	require.True(t, shouldRetry(c, err, 1))
}

func TestShouldRetryEmptyResponseRespectsStatusCodePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	oldRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
		operation_setting.AutomaticRetryStatusCodeRanges = oldRetryRanges
	})

	common.EmptyCompletionRetryEnabled = true
	operation_setting.AutomaticRetryStatusCodeRanges = nil
	err := types.NewOpenAIError(errors.New("empty completion"), types.ErrorCodeEmptyCompletion, http.StatusBadGateway)

	require.False(t, shouldRetry(c, err, 1))
}

func TestShouldRetryEmptyResponseDisabledBeatsInvalidStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
	})

	common.EmptyCompletionRetryEnabled = false
	err := types.NewOpenAIError(errors.New("empty completion"), types.ErrorCodeEmptyCompletion, 0)

	require.False(t, shouldRetry(c, err, 1))
}

func TestShouldRetryGenericEmptyResponseDoesNotRequireEmptyCompletionFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	oldEmptyCompletionRetryEnabled := common.EmptyCompletionRetryEnabled
	oldRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled = oldEmptyCompletionRetryEnabled
		operation_setting.AutomaticRetryStatusCodeRanges = oldRetryRanges
	})

	common.EmptyCompletionRetryEnabled = false
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: http.StatusInternalServerError, End: http.StatusInternalServerError}}
	err := types.NewOpenAIError(errors.New("empty response"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)

	require.True(t, shouldRetry(c, err, 1))
}

func TestShouldRetryTaskRelaySkipsLocalServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{
		StatusCode: http.StatusInternalServerError,
		LocalError: true,
	}, 1))
}

func TestShouldRetryTaskRelayDoesNotEnableRetryThroughStatusMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{
		StatusCode:         http.StatusServiceUnavailable,
		UpstreamStatusCode: http.StatusBadRequest,
	}, 1))
}

func TestShouldRetryTaskRelayAllowsStatusMappingToSuppressRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, shouldRetryTaskRelay(c, 1, &dto.TaskError{
		StatusCode:         http.StatusBadRequest,
		UpstreamStatusCode: http.StatusServiceUnavailable,
	}, 1))
}

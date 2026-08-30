package relay

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/doubao"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingTaskReservationBilling struct {
	preConsumed int
	reserveTo   []int
	settleTo    []int
}

func (b *recordingTaskReservationBilling) Settle(actualQuota int) error {
	b.settleTo = append(b.settleTo, actualQuota)
	b.preConsumed = actualQuota
	return nil
}
func (b *recordingTaskReservationBilling) Refund(*gin.Context)      {}
func (b *recordingTaskReservationBilling) NeedsRefund() bool        { return true }
func (b *recordingTaskReservationBilling) GetPreConsumedQuota() int { return b.preConsumed }
func (b *recordingTaskReservationBilling) Reserve(targetQuota int) error {
	b.reserveTo = append(b.reserveTo, targetQuota)
	if targetQuota > b.preConsumed {
		b.preConsumed = targetQuota
	}
	return nil
}

func TestReserveTaskQuotaUsesActualReservationAndExtendsRetries(t *testing.T) {
	billing := &recordingTaskReservationBilling{preConsumed: 100}
	info := &relaycommon.RelayInfo{
		Billing: billing,
		PriceData: types.PriceData{
			FreeModel: false,
		},
	}

	reserved, taskErr := reserveTaskQuota(nil, info, 250)

	require.Nil(t, taskErr)
	assert.Equal(t, 250, reserved)
	assert.Equal(t, []int{250}, billing.reserveTo)
}

func TestReserveTaskQuotaPreservesPaidReservationUntilFreeRetrySettlement(t *testing.T) {
	billing := &recordingTaskReservationBilling{preConsumed: 100}
	info := &relaycommon.RelayInfo{
		Billing: billing,
		PriceData: types.PriceData{
			FreeModel: true,
		},
	}

	reserved, taskErr := reserveTaskQuota(nil, info, 0)

	require.Nil(t, taskErr)
	assert.Equal(t, 100, reserved)
	assert.Same(t, billing, info.Billing)
	assert.Empty(t, billing.reserveTo)

	require.NoError(t, info.Billing.Settle(0))
	assert.Equal(t, []int{0}, billing.settleTo)
	assert.Zero(t, info.Billing.GetPreConsumedQuota())
}

func TestReserveTaskQuotaReturnsZeroForUnreservedFreeTask(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			FreeModel: true,
		},
	}

	reserved, taskErr := reserveTaskQuota(nil, info, 250)

	require.Nil(t, taskErr)
	assert.Zero(t, reserved)
	assert.Nil(t, info.Billing)
}

func withRelayTaskRateCards(t *testing.T, cards map[string]task_billing_setting.RateCard) {
	t.Helper()
	original := task_billing_setting.GetRateCardsCopy()
	originalData, err := common.Marshal(original)
	require.NoError(t, err)
	data, err := common.Marshal(cards)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"task_billing_setting.rate_cards": string(data),
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"task_billing_setting.rate_cards": string(originalData),
		}))
	})
}

func withRelayTaskQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	original := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() {
		common.QuotaPerUnit = original
	})
}

func TestPrepareTaskSubmitRequestBodyMakesParamOverrideVisibleToBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"test",
		"metadata":{"resolution":"720p"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"path":  "resolution",
						"mode":  "set",
						"value": "1080p",
					},
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"resolution": "720p",
		},
	})

	requestBody, taskErr := prepareTaskSubmitRequestBody(c, info, &doubao.TaskAdaptor{})

	require.Nil(t, taskErr)
	body, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"resolution":"1080p"`)

	ratios := (&doubao.TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 51.0/46.0, ratios["video_input"], 1e-9)
}

func TestPrepareTaskSubmitRequestBodyMakesMultipartParamOverrideVisibleToBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`--boundary--`))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"path":  "resolution",
						"mode":  "set",
						"value": "1080p",
					},
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
		Metadata: map[string]interface{}{
			"resolution": "720p",
		},
	})

	requestBody, taskErr := prepareTaskSubmitRequestBody(c, info, &doubao.TaskAdaptor{})

	require.Nil(t, taskErr)
	body, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"resolution":"1080p"`)

	finalBody, ok := relaycommon.GetTaskSubmitRequestBody(c)
	require.True(t, ok)
	assert.Contains(t, string(finalBody), `"resolution":"1080p"`)
	ratios := (&doubao.TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, 51.0/46.0, ratios["video_input"], 1e-9)
}

func TestEstimateTaskBillingFallsBackToGenericRateCard(t *testing.T) {
	withRelayTaskQuotaPerUnit(t, 1000)
	withRelayTaskRateCards(t, map[string]task_billing_setting.RateCard{
		"custom-video-model": {
			Vendor:          "custom",
			Unit:            "second",
			QuantityField:   "duration",
			DefaultQuantity: 5,
			Strict:          true,
			Defaults: map[string]string{
				"resolution":      "720p",
				"has_audio":       "false",
				"has_video_input": "false",
			},
			Rows: []task_billing_setting.RateCardRow{
				{
					ID: "720p_no_audio",
					Match: map[string]string{
						"resolution": "720p",
						"has_audio":  "false",
					},
					UnitPrice: 0.5,
				},
			},
		},
	})

	duration := 6
	audio := false
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "custom-video-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "custom-video-model"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:           "custom-video-model",
		DurationSeconds: &duration,
		Resolution:      "720p",
		GenerateAudio:   &audio,
	})

	got, err := estimateTaskBilling(c, info, &doubao.TaskAdaptor{}, constant.TaskPlatform("custom-video"))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "custom-video-model", got.RuleKey)
	assert.Equal(t, "720p_no_audio", got.RowID)
	assert.InDelta(t, 6.0, got.Quantity, 1e-9)
	assert.Equal(t, 3000, got.Quota)
}

func TestRecalcQuotaFromRatiosUsesRawEstimateBeforePreConsumeFloor(t *testing.T) {
	const estimatedQuota = 200
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			Quota:             estimatedQuota,
			QuotaToPreConsume: 500,
			OtherRatios:       map[string]float64{"duration": 2},
		},
	}

	for _, tc := range []struct {
		name     string
		ratio    float64
		expected int
	}{
		{name: "adjusted below estimate and floor", ratio: 1, expected: 100},
		{name: "adjusted above reservation", ratio: 6, expected: 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := recalcQuotaFromRatios(info, estimatedQuota, map[string]float64{"duration": tc.ratio})
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestPrepareTaskSubmitRequestBodyParamOverrideReturnErrorIsLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"test"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode": "return_error",
						"value": map[string]interface{}{
							"message":     "forced bad request by param override",
							"status_code": 422,
							"code":        "forced_bad_request",
							"skip_retry":  true,
						},
					},
				},
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "test",
	})

	_, taskErr := prepareTaskSubmitRequestBody(c, info, &doubao.TaskAdaptor{})

	require.NotNil(t, taskErr)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, http.StatusUnprocessableEntity, taskErr.StatusCode)
	assert.Equal(t, "forced_bad_request", taskErr.Code)
	assert.Equal(t, "forced bad request by param override", taskErr.Message)
}

func TestMapUpstreamTaskErrorAppliesMappingWithoutChangingLocalClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("status_code_mapping", `{"400":"503"}`)
	taskErr := &dto.TaskError{
		StatusCode: http.StatusBadRequest,
		LocalError: true,
	}

	got := mapUpstreamTaskError(c, taskErr)

	require.Same(t, taskErr, got)
	assert.Equal(t, http.StatusServiceUnavailable, got.StatusCode)
	assert.Equal(t, http.StatusBadRequest, got.UpstreamStatusCode)
	assert.True(t, got.LocalError)
}

func TestTaskResponseBufferDefersHeadersStatusAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	originalWriter := c.Writer
	bufferedWriter := newTaskResponseBuffer(originalWriter)
	c.Writer = bufferedWriter

	c.Header("X-Task-Result", "buffered")
	c.Status(http.StatusAccepted)
	_, err := c.Writer.WriteString(`{"id":"task_public"}`)
	require.NoError(t, err)

	require.False(t, originalWriter.Written())
	require.Empty(t, recorder.Body.String())
	require.Empty(t, recorder.Header().Get("X-Task-Result"))

	snapshot := bufferedWriter.snapshot()
	c.Writer = originalWriter
	require.NoError(t, snapshot.writeTo(c))
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Equal(t, "buffered", recorder.Header().Get("X-Task-Result"))
	require.JSONEq(t, `{"id":"task_public"}`, recorder.Body.String())
}

func TestTaskResponseSnapshotClearsHeadersRemovedFromBuffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Header("X-Stale", "stale")
	originalWriter := c.Writer
	bufferedWriter := newTaskResponseBuffer(originalWriter)
	c.Writer = bufferedWriter

	bufferedWriter.Header().Del("X-Stale")
	bufferedWriter.Header().Set("X-Fresh", "fresh")
	_, err := c.Writer.WriteString(`{"ok":true}`)
	require.NoError(t, err)

	snapshot := bufferedWriter.snapshot()
	c.Writer = originalWriter
	require.NoError(t, snapshot.writeTo(c))
	require.Empty(t, recorder.Header().Get("X-Stale"))
	require.Equal(t, "fresh", recorder.Header().Get("X-Fresh"))
}

func TestSetTaskOtherRatioHeadersUsesFinalRatios(t *testing.T) {
	header := http.Header{}

	setTaskOtherRatioHeaders(header, map[string]float64{"duration": 1.25})
	setTaskOtherRatioHeaders(header, map[string]float64{"duration": 2})

	require.JSONEq(t, `{"duration":2}`, header.Get("X-Max-Api-Other-Ratios"))
	require.JSONEq(t, `{"duration":2}`, header.Get("X-New-Api-Other-Ratios"))
}

func TestTaskPersistenceErrorReturnsSafeClientMessage(t *testing.T) {
	taskErr := TaskPersistenceError(errors.New("sql: failed near secret_table"), "persist_task_failed", "failed to persist task")

	require.NotNil(t, taskErr)
	assert.Equal(t, "persist_task_failed", taskErr.Code)
	assert.Equal(t, "failed to persist task", taskErr.Message)
	assert.NotContains(t, taskErr.Message, "secret_table")
	assert.NotContains(t, taskErr.Error.Error(), "secret_table")
}

func TestTerminalTaskStatusClassification(t *testing.T) {
	require.True(t, isTerminalTaskStatus(model.TaskStatusSuccess))
	require.True(t, isTerminalTaskStatus(model.TaskStatusFailure))
	require.False(t, isTerminalTaskStatus(model.TaskStatusInProgress))
	require.False(t, isTerminalTaskStatus(model.TaskStatusNotStart))
}

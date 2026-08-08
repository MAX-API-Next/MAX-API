package model

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withLogAuditSettings(t *testing.T, requestEnabled bool, responseEnabled bool) {
	t.Helper()
	prevRequest := common.LogRequestContentEnabled
	prevResponse := common.LogResponseContentEnabled
	common.LogRequestContentEnabled = requestEnabled
	common.LogResponseContentEnabled = responseEnabled
	t.Cleanup(func() {
		common.LogRequestContentEnabled = prevRequest
		common.LogResponseContentEnabled = prevResponse
	})
}

func TestRecordTaskBillingLogOnceUsesReceiptClaimForIdempotency(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&BillingLogReceipt{}).Error)
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&BillingLogReceipt{}).Error)
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	params := RecordTaskBillingLogParams{
		UserId:  1,
		LogType: LogTypeSystem,
		Content: "idempotent task billing effect",
	}
	require.NoError(t, RecordTaskBillingLogOnce("task:test:effect", params))
	require.NoError(t, RecordTaskBillingLogOnce("task:test:effect", params))

	var logCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&logCount).Error)
	require.EqualValues(t, 1, logCount)

	var receipt BillingLogReceipt
	require.NoError(t, LOG_DB.Where("operation_key = ?", "task:test:effect").Take(&receipt).Error)
	require.NotEmpty(t, receipt.ClaimToken)

	require.NoError(t, LOG_DB.Create(&BillingLogReceipt{
		OperationKey: "task:legacy:effect",
		ClaimToken:   "",
		CreatedAt:    time.Now().Unix(),
	}).Error)
	require.NoError(t, RecordTaskBillingLogOnce("task:legacy:effect", params))
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&logCount).Error)
	require.EqualValues(t, 1, logCount)
}

func markRetryLogBackfillCompletedForTest(t *testing.T) {
	t.Helper()
	markerKey := logRetryMarkerBackfillCompletionKey()
	require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	require.NoError(t, markLogRetryMarkerBackfillCompleted())
	t.Cleanup(func() {
		require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	})
}

func resetQuotaDataCacheForTest(t *testing.T) {
	t.Helper()
	resetLogQuotaDataShutdownForTest(t)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
	})
}

func resetLogQuotaDataShutdownForTest(t *testing.T) {
	t.Helper()
	logQuotaDataShutdownMu.Lock()
	logQuotaDataShutdownStarted = false
	logQuotaDataShutdownMu.Unlock()
	t.Cleanup(func() {
		logQuotaDataShutdownMu.Lock()
		logQuotaDataShutdownStarted = false
		logQuotaDataShutdownMu.Unlock()
	})
}

func newLogTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func TestRecordConsumeLogSanitizesPersistedContent(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})

	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	content := "line1\r\nline2\tline3\x00" + strings.Repeat("x", common.PersistedLogContentLimit+1)
	RecordConsumeLog(newLogTestContext(), 7, RecordConsumeLogParams{
		ModelName: "gpt-test",
		Quota:     1,
		Content:   content,
	})

	var log Log
	require.NoError(t, LOG_DB.Last(&log).Error)
	require.True(t, utf8.ValidString(log.Content))
	require.NotContains(t, log.Content, "\r")
	require.NotContains(t, log.Content, "\n")
	require.NotContains(t, log.Content, "\t")
	require.NotContains(t, log.Content, "\x00")
	require.Contains(t, log.Content, "line1  line2 line3")
	require.True(t, strings.HasSuffix(log.Content, "... [truncated]"))
	require.Equal(t, common.PersistedLogContentLimit+utf8.RuneCountInString("... [truncated]"), utf8.RuneCountInString(log.Content))
}

func TestBillingSettlementEffectPayloadSanitizesContent(t *testing.T) {
	payload, err := billingSettlementEffectPayload(&BillingSettlementEffect{
		LogType: LogTypeConsume,
		Content: "effect\r\ncontent\x00" + strings.Repeat("x", common.PersistedLogContentLimit+1),
	})
	require.NoError(t, err)

	effect, err := decodeBillingSettlementEffect(payload)
	require.NoError(t, err)
	require.NotNil(t, effect)
	require.NotContains(t, effect.Content, "\r")
	require.NotContains(t, effect.Content, "\n")
	require.NotContains(t, effect.Content, "\x00")
	require.True(t, strings.HasSuffix(effect.Content, "... [truncated]"))
}

func TestMarkTaskSubmitFailedSanitizesFailReason(t *testing.T) {
	require.NoError(t, DB.Where("1 = 1").Delete(&Task{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&Task{}).Error)
	})

	task := &Task{
		TaskID:     "sanitize-fail-reason",
		Status:     TaskStatusSubmitted,
		SubmitTime: time.Now().Unix(),
	}
	require.NoError(t, DB.Create(task).Error)

	reason := "upstream\r\nfailed\x00" + strings.Repeat("x", common.PersistedLogContentLimit+1)
	require.NoError(t, MarkTaskSubmitFailed(task.ID, reason))

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.EqualValues(t, TaskStatusFailure, reloaded.Status)
	require.NotContains(t, reloaded.FailReason, "\r")
	require.NotContains(t, reloaded.FailReason, "\n")
	require.NotContains(t, reloaded.FailReason, "\x00")
	require.True(t, strings.HasSuffix(reloaded.FailReason, "... [truncated]"))
}

func TestLogQuotaFilterIndexes(t *testing.T) {
	db := newRetryBackfillTestDB(t, &Log{})

	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_quota"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_type_quota_created_at"))
}

func TestLogSchemaDoesNotAddBillingSettlementIdempotencyColumn(t *testing.T) {
	db := newRetryBackfillTestDB(t, &Log{})

	require.False(t, db.Migrator().HasColumn(&Log{}, "billing_settlement_operation_key"))
}

func TestGetAllLogsRetryFilter(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	logs := createRetryFilterLogs(t)

	got, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.NoError(t, err)
	require.EqualValues(t, 5, total)
	require.Len(t, got, 5)
	require.ElementsMatch(t, []int{logs[0].Id, logs[1].Id, logs[4].Id, logs[5].Id, logs[6].Id}, []int{got[0].Id, got[1].Id, got[2].Id, got[3].Id, got[4].Id})
}

func TestGetUserLogsRetryFilter(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	logs := createRetryFilterLogs(t)

	got, total, err := GetUserLogs(LogQueryParams{UserId: 1, LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.NoError(t, err)
	require.EqualValues(t, 4, total)
	require.Len(t, got, 4)
	gotLogIds := []int{got[0].LogId, got[1].LogId, got[2].LogId, got[3].LogId}
	require.ElementsMatch(t, []int{logs[0].Id, logs[1].Id, logs[5].Id, logs[6].Id}, gotLogIds)
	require.NotContains(t, gotLogIds, logs[4].Id)
	for _, log := range got {
		require.Equal(t, 1, log.UserId)
	}
}

func TestSumUsedQuotaRetryFilter(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	createRetryFilterLogs(t)

	stat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry})
	require.NoError(t, err)
	require.Equal(t, 1550, stat.Quota)
	require.Equal(t, 4, stat.Rpm)
	require.Equal(t, 96, stat.Tpm)
}

func TestSumUsedQuotaRetryFilterIgnoresNonConsumeQuota(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	createRetryFilterLogs(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           1,
		CreatedAt:        time.Now().Unix(),
		Type:             LogTypeTopup,
		Quota:            999,
		PromptTokens:     1000,
		CompletionTokens: 1000,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}).Error)

	stat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry})
	require.NoError(t, err)
	require.Equal(t, 1550, stat.Quota)
	require.Equal(t, 4, stat.Rpm)
	require.Equal(t, 96, stat.Tpm)
}

func TestGetAllLogsRetrySubtypeFilters(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	logs := createRetryFilterLogs(t)

	errorLogs, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterErrorRetry, Num: 10})
	require.NoError(t, err)
	require.EqualValues(t, 4, total)
	require.Len(t, errorLogs, 4)
	require.ElementsMatch(t, []int{logs[0].Id, logs[4].Id, logs[5].Id, logs[6].Id}, []int{errorLogs[0].Id, errorLogs[1].Id, errorLogs[2].Id, errorLogs[3].Id})

	emptyLogs, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterEmptyRetry, Num: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, emptyLogs, 2)
	require.ElementsMatch(t, []int{logs[1].Id, logs[6].Id}, []int{emptyLogs[0].Id, emptyLogs[1].Id})
}

func TestSumUsedQuotaRetrySubtypeFilters(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	createRetryFilterLogs(t)

	errorStat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterErrorRetry})
	require.NoError(t, err)
	require.Equal(t, 1350, errorStat.Quota)
	require.Equal(t, 3, errorStat.Rpm)
	require.Equal(t, 81, errorStat.Tpm)

	emptyStat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterEmptyRetry})
	require.NoError(t, err)
	require.Equal(t, 550, emptyStat.Quota)
	require.Equal(t, 2, emptyStat.Rpm)
	require.Equal(t, 40, emptyStat.Tpm)
}

func TestSumUsedQuotaAppliesExplicitLogType(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	createRetryFilterLogs(t)

	stat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeError})
	require.NoError(t, err)
	require.Equal(t, 0, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 0, stat.Tpm)
}

func TestSumUsedQuotaKeepsRpmTpmLiveForHistoricalWindow(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	now := time.Now().Unix()
	logs := []Log{
		{
			UserId:           1,
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Quota:            100,
			PromptTokens:     3,
			CompletionTokens: 7,
		},
		{
			UserId:           1,
			CreatedAt:        now - 86400,
			Type:             LogTypeConsume,
			Quota:            200,
			PromptTokens:     11,
			CompletionTokens: 13,
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	stat, err := SumUsedQuota(LogQueryParams{
		LogType:        LogTypeUnknown,
		StartTimestamp: now - 90000,
		EndTimestamp:   now - 80000,
	})

	require.NoError(t, err)
	require.Equal(t, 200, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 10, stat.Tpm)
}

func TestLogQuotaFilters(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	now := time.Now().Unix()
	logs := []Log{
		{
			UserId:           1,
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Quota:            0,
			PromptTokens:     1,
			CompletionTokens: 2,
		},
		{
			UserId:           1,
			CreatedAt:        now - 20,
			Type:             LogTypeConsume,
			Quota:            -50,
			PromptTokens:     3,
			CompletionTokens: 4,
		},
		{
			UserId:           1,
			CreatedAt:        now - 30,
			Type:             LogTypeConsume,
			Quota:            100,
			PromptTokens:     5,
			CompletionTokens: 6,
		},
		{
			UserId:           2,
			CreatedAt:        now - 40,
			Type:             LogTypeConsume,
			Quota:            -75,
			PromptTokens:     7,
			CompletionTokens: 8,
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	zeroLogs, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeConsume, Num: 10, QuotaFilter: LogQuotaFilterZero})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, zeroLogs, 1)
	require.Equal(t, logs[0].Id, zeroLogs[0].Id)

	negativeLogs, total, err := GetUserLogs(LogQueryParams{UserId: 1, LogType: LogTypeConsume, Num: 10, QuotaFilter: LogQuotaFilterNegative})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, negativeLogs, 1)
	require.Equal(t, logs[1].Id, negativeLogs[0].LogId)

	abnormalStat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, QuotaFilter: LogQuotaFilterAbnormal})
	require.NoError(t, err)
	require.Equal(t, -125, abnormalStat.Quota)
	require.Equal(t, 3, abnormalStat.Rpm)
	require.Equal(t, 25, abnormalStat.Tpm)
}

func TestRetryFilterIgnoresNestedRetryMarker(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})
	markRetryLogBackfillCompletedForTest(t)

	logs := []Log{
		{
			UserId:           1,
			CreatedAt:        time.Now().Unix() - 10,
			Type:             LogTypeConsume,
			Quota:            300,
			PromptTokens:     3,
			CompletionTokens: 7,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"request_content": `user prompt literally contains "retry_log":true`,
					"retry_log":       true,
				},
			}),
		},
		{
			UserId:           1,
			CreatedAt:        time.Now().Unix() - 20,
			Type:             LogTypeConsume,
			Quota:            200,
			PromptTokens:     5,
			CompletionTokens: 11,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log": true,
			}),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	got, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, got, 1)
	require.Equal(t, logs[1].Id, got[0].Id)

	stat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry})
	require.NoError(t, err)
	require.Equal(t, 200, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 16, stat.Tpm)
}

func TestRetryFilterReturnsReadinessErrorBeforeBackfillCompletion(t *testing.T) {
	markerKey := logRetryMarkerBackfillCompletionKey()
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
		require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	})

	logs := []Log{
		{
			UserId:           1,
			CreatedAt:        time.Now().Unix() - 10,
			Type:             LogTypeConsume,
			Quota:            100,
			PromptTokens:     3,
			CompletionTokens: 5,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log": true,
			}),
		},
		{
			UserId:           1,
			CreatedAt:        time.Now().Unix() - 20,
			Type:             LogTypeConsume,
			Quota:            200,
			PromptTokens:     7,
			CompletionTokens: 11,
			Other: common.MapToJsonStr(map[string]interface{}{
				"empty_retry": true,
			}),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").Updates(map[string]interface{}{
		"is_retry":       false,
		"is_error_retry": false,
		"is_empty_retry": false,
	}).Error)

	got, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.ErrorIs(t, err, ErrLogRetryMarkerBackfillIncomplete)
	require.Nil(t, got)
	require.Zero(t, total)

	var reloaded []Log
	require.NoError(t, LOG_DB.Order("id asc").Find(&reloaded).Error)
	require.Len(t, reloaded, 2)
	require.False(t, reloaded[0].IsRetry)
	require.False(t, reloaded[0].IsErrorRetry)
	require.False(t, reloaded[0].IsEmptyRetry)
	require.False(t, reloaded[1].IsRetry)
	require.False(t, reloaded[1].IsErrorRetry)
	require.False(t, reloaded[1].IsEmptyRetry)
}

func TestRetryFilterUsesIsRetryAfterBackfillCompletion(t *testing.T) {
	markerKey := logRetryMarkerBackfillCompletionKey()
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
		require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	})

	log := Log{
		UserId:    1,
		CreatedAt: time.Now().Unix() - 10,
		Type:      LogTypeConsume,
		Quota:     100,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}
	require.NoError(t, LOG_DB.Create(&log).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("id = ?", log.Id).UpdateColumn("is_retry", false).Error)
	require.NoError(t, markLogRetryMarkerBackfillCompleted())

	got, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.NoError(t, err)
	require.EqualValues(t, 0, total)
	require.Empty(t, got)

	var reloaded Log
	require.NoError(t, LOG_DB.First(&reloaded, log.Id).Error)
	require.False(t, reloaded.IsRetry)
}

func TestRetryFilterReadPathsReturnReadinessError(t *testing.T) {
	originalEnsure := ensureLogRetryMarkerBackfillCompletedForRead
	expectedErr := errors.New("readiness unavailable")
	ensureLogRetryMarkerBackfillCompletedForRead = func() error {
		return expectedErr
	}
	t.Cleanup(func() {
		ensureLogRetryMarkerBackfillCompletedForRead = originalEnsure
	})

	got, total, err := GetAllLogs(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, got)
	require.Zero(t, total)

	got, total, err = GetUserLogs(LogQueryParams{UserId: 1, LogType: LogTypeUnknown, LogFilter: LogFilterRetry, Num: 10})
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, got)
	require.Zero(t, total)

	stat, err := SumUsedQuota(LogQueryParams{LogType: LogTypeUnknown, LogFilter: LogFilterRetry})
	require.ErrorIs(t, err, expectedErr)
	require.Zero(t, stat)
}

func TestBackfillLogRetryMarkerUsesTopLevelMarkersOnly(t *testing.T) {
	markerKey := logRetryMarkerBackfillCompletionKey()
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
		require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	})

	logs := []Log{
		{
			UserId: 1,
			Type:   LogTypeConsume,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log": true,
			}),
		},
		{
			UserId: 1,
			Type:   LogTypeConsume,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"retry_log": true,
				},
			}),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").UpdateColumn("is_retry", false).Error)

	require.NoError(t, backfillLogRetryMarker())

	var reloaded []Log
	require.NoError(t, LOG_DB.Order("id asc").Find(&reloaded).Error)
	require.Len(t, reloaded, 2)
	require.True(t, reloaded[0].IsRetry)
	require.True(t, reloaded[0].IsErrorRetry)
	require.False(t, reloaded[0].IsEmptyRetry)
	require.False(t, reloaded[1].IsRetry)
	require.False(t, reloaded[1].IsErrorRetry)
	require.False(t, reloaded[1].IsEmptyRetry)
}

func TestBackfillLogRetryMarkerSkipsAfterCompletionMarker(t *testing.T) {
	markerKey := logRetryMarkerBackfillCompletionKey()
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
		require.NoError(t, DB.Where(commonKeyCol+" = ?", markerKey).Delete(&Option{}).Error)
	})

	log := Log{
		UserId: 1,
		Type:   LogTypeConsume,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}
	require.NoError(t, LOG_DB.Create(&log).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").UpdateColumn("is_retry", false).Error)
	require.NoError(t, backfillLogRetryMarker())

	var marker Option
	require.NoError(t, DB.First(&marker, commonKeyCol+" = ?", markerKey).Error)
	require.Equal(t, "true", marker.Value)

	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").UpdateColumn("is_retry", false).Error)
	require.NoError(t, backfillLogRetryMarker())

	var reloaded Log
	require.NoError(t, LOG_DB.First(&reloaded, log.Id).Error)
	require.False(t, reloaded.IsRetry)
}

func TestBackfillLogRetryMarkerCompletionIsScopedToLogDBIdentity(t *testing.T) {
	originalDB := DB
	originalLOGDB := LOG_DB
	originalLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLOGDB
		common.LogSqlType = originalLogSQLType
		initCol()
	})

	mainDB := newRetryBackfillTestDB(t, &Option{})
	logOneDB := newRetryBackfillTestDB(t, &Log{})
	logTwoDB := newRetryBackfillTestDB(t, &Log{})

	DB = mainDB
	common.LogSqlType = common.DatabaseTypeSQLite

	t.Setenv("LOG_SQL_DSN", "sqlite://log-one")
	initCol()
	LOG_DB = logOneDB
	logOne := Log{
		UserId: 1,
		Type:   LogTypeConsume,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}
	require.NoError(t, LOG_DB.Create(&logOne).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").UpdateColumn("is_retry", false).Error)
	require.NoError(t, backfillLogRetryMarker())

	var reloaded Log
	require.NoError(t, logOneDB.First(&reloaded, logOne.Id).Error)
	require.True(t, reloaded.IsRetry)

	require.NoError(t, os.Setenv("LOG_SQL_DSN", "sqlite://log-two"))
	initCol()
	LOG_DB = logTwoDB
	logTwo := Log{
		UserId: 1,
		Type:   LogTypeConsume,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}
	require.NoError(t, LOG_DB.Create(&logTwo).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").UpdateColumn("is_retry", false).Error)

	require.NoError(t, backfillLogRetryMarker())

	reloaded = Log{}
	require.NoError(t, logTwoDB.First(&reloaded, logTwo.Id).Error)
	require.True(t, reloaded.IsRetry)
}

func TestMigrateLOGDBSchedulesRetryMarkerBackfillWithoutRunningInline(t *testing.T) {
	originalDB := DB
	originalLOGDB := LOG_DB
	originalRunner := logRetryMarkerBackfillAsyncRunner
	originalLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLOGDB
		logRetryMarkerBackfillAsyncRunner = originalRunner
		common.LogSqlType = originalLogSQLType
		initCol()
	})

	mainDB := newRetryBackfillTestDB(t, &Option{})
	logDB := newRetryBackfillTestDB(t)

	DB = mainDB
	LOG_DB = logDB
	common.LogSqlType = common.DatabaseTypeSQLite
	t.Setenv("LOG_SQL_DSN", "sqlite://async-backfill")
	initCol()

	var scheduled []func()
	logRetryMarkerBackfillAsyncRunner = func(fn func()) {
		scheduled = append(scheduled, fn)
	}

	require.NoError(t, migrateLOGDB())
	require.Len(t, scheduled, 1)

	log := Log{
		UserId: 1,
		Type:   LogTypeConsume,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}
	require.NoError(t, LOG_DB.Create(&log).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("1 = 1").UpdateColumn("is_retry", false).Error)

	var reloaded Log
	require.NoError(t, LOG_DB.First(&reloaded, log.Id).Error)
	require.False(t, reloaded.IsRetry)

	scheduled[0]()

	reloaded = Log{}
	require.NoError(t, LOG_DB.First(&reloaded, log.Id).Error)
	require.True(t, reloaded.IsRetry)
}

func TestScheduleLogRetryMarkerBackfillSkipsCompletedMarker(t *testing.T) {
	originalDB := DB
	originalLOGDB := LOG_DB
	originalRunner := logRetryMarkerBackfillAsyncRunner
	originalLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLOGDB
		logRetryMarkerBackfillAsyncRunner = originalRunner
		common.LogSqlType = originalLogSQLType
		initCol()
	})

	mainDB := newRetryBackfillTestDB(t, &Option{})
	logDB := newRetryBackfillTestDB(t, &Log{})

	DB = mainDB
	LOG_DB = logDB
	common.LogSqlType = common.DatabaseTypeSQLite
	t.Setenv("LOG_SQL_DSN", "sqlite://completed-backfill")
	initCol()
	require.NoError(t, markLogRetryMarkerBackfillCompleted())

	called := false
	logRetryMarkerBackfillAsyncRunner = func(fn func()) {
		called = true
	}

	scheduleLogRetryMarkerBackfill()
	require.False(t, called)
}

func TestLogRetryMarkerCompletionKeyUsesHashedLogDBIdentity(t *testing.T) {
	originalLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		common.LogSqlType = originalLogSQLType
	})

	common.LogSqlType = common.DatabaseTypeMySQL
	t.Setenv("LOG_SQL_DSN", "user:secret@tcp(log-one:3306)/logs")
	firstKey := logRetryMarkerBackfillCompletionKey()
	require.NotEqual(t, logRetryMarkerBackfillOptionKey, firstKey)
	require.NotContains(t, firstKey, "secret")
	require.NotContains(t, firstKey, "log-one")

	require.NoError(t, os.Setenv("LOG_SQL_DSN", "user:secret@tcp(log-two:3306)/logs"))
	secondKey := logRetryMarkerBackfillCompletionKey()
	require.NotEqual(t, firstKey, secondKey)
}

func TestLogRetryMarkerIsRecomputedWhenOtherChanges(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})

	log := Log{
		UserId: 1,
		Type:   LogTypeConsume,
		Other: common.MapToJsonStr(map[string]interface{}{
			"retry_log": true,
		}),
	}
	require.NoError(t, LOG_DB.Create(&log).Error)
	require.True(t, log.IsRetry)

	log.Other = common.MapToJsonStr(map[string]interface{}{
		"admin_info": map[string]interface{}{
			"use_channel": []string{"1"},
		},
	})
	require.NoError(t, LOG_DB.Save(&log).Error)

	var reloaded Log
	require.NoError(t, LOG_DB.First(&reloaded, log.Id).Error)
	require.False(t, reloaded.IsRetry)
}

func TestRecordConsumeLogQueuesQuotaDataAsync(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})
	resetQuotaDataCacheForTest(t)

	originalDataExportEnabled := common.DataExportEnabled
	originalRunner := logQuotaDataAsyncRunner
	common.DataExportEnabled = true
	var queued []func()
	executed := false
	logQuotaDataAsyncRunner = func(fn func()) {
		queued = append(queued, func() {
			executed = true
			fn()
		})
	}
	t.Cleanup(func() {
		common.DataExportEnabled = originalDataExportEnabled
		logQuotaDataAsyncRunner = originalRunner
	})

	RecordConsumeLog(newLogTestContext(), 7, RecordConsumeLogParams{
		ModelName:        "gpt-test",
		Quota:            42,
		PromptTokens:     3,
		CompletionTokens: 5,
		Group:            "default",
		TokenId:          11,
		ChannelId:        13,
	})

	require.Len(t, queued, 1)
	require.False(t, executed)
	require.Empty(t, CacheQuotaData)

	queued[0]()
	require.True(t, executed)
	require.Len(t, CacheQuotaData, 1)
}

func TestRecordTaskBillingLogQueuesQuotaDataAsync(t *testing.T) {
	require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Where("1 = 1").Delete(&Log{}).Error)
	})
	resetQuotaDataCacheForTest(t)

	originalDataExportEnabled := common.DataExportEnabled
	originalRunner := logQuotaDataAsyncRunner
	common.DataExportEnabled = true
	var queued []func()
	logQuotaDataAsyncRunner = func(fn func()) {
		queued = append(queued, fn)
	}
	t.Cleanup(func() {
		common.DataExportEnabled = originalDataExportEnabled
		logQuotaDataAsyncRunner = originalRunner
	})

	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:    7,
		LogType:   LogTypeConsume,
		Content:   "task billing",
		ChannelId: 13,
		ModelName: "task-test",
		Quota:     42,
		TokenId:   11,
		Group:     "default",
	})

	require.Len(t, queued, 1)
	require.Empty(t, CacheQuotaData)

	queued[0]()
	require.Len(t, CacheQuotaData, 1)
}

func TestWaitPendingLogQuotaDataDrainsEnqueuedWork(t *testing.T) {
	resetQuotaDataCacheForTest(t)

	originalRunner := logQuotaDataAsyncRunner
	release := make(chan struct{})
	logQuotaDataAsyncRunner = func(fn func()) {
		go func() {
			<-release
			fn()
		}()
	}
	t.Cleanup(func() {
		logQuotaDataAsyncRunner = originalRunner
	})

	enqueueLogQuotaData(QuotaDataLogParams{
		UserID:    7,
		Username:  "quota-wait",
		ModelName: "gpt-test",
		Quota:     42,
		CreatedAt: time.Now().Unix(),
	})

	waitDone := make(chan struct{})
	go func() {
		WaitPendingLogQuotaData()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Fatal("wait returned before queued quota data ran")
	case <-time.After(10 * time.Millisecond):
	}

	close(release)

	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued quota data")
	}
	require.Len(t, CacheQuotaData, 1)
}

func TestEnqueueLogQuotaDataAfterShutdownPersistsSynchronously(t *testing.T) {
	resetQuotaDataCacheForTest(t)
	require.NoError(t, DB.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	require.NoError(t, DB.Where("1 = 1").Delete(&QuotaData{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&QuotaData{}).Error)
	})

	originalRunner := logQuotaDataAsyncRunner
	runnerCalled := false
	logQuotaDataAsyncRunner = func(fn func()) {
		runnerCalled = true
	}
	logQuotaDataShutdownMu.Lock()
	logQuotaDataShutdownStarted = true
	logQuotaDataShutdownMu.Unlock()
	t.Cleanup(func() {
		logQuotaDataAsyncRunner = originalRunner
	})

	enqueueLogQuotaData(QuotaDataLogParams{
		UserID:    7,
		Username:  "quota-shutdown",
		ModelName: "gpt-test",
		Quota:     42,
		CreatedAt: time.Now().Unix(),
	})

	require.False(t, runnerCalled)
	require.Empty(t, CacheQuotaData)

	var stored QuotaData
	require.NoError(t, DB.Where("user_id = ? AND username = ? AND model_name = ?", 7, "quota-shutdown", "gpt-test").First(&stored).Error)
	require.Equal(t, 1, stored.Count)
	require.Equal(t, 42, stored.Quota)
}

func newRetryBackfillTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.AutoMigrate(models...))

	return db
}

func createRetryFilterLogs(t *testing.T) []Log {
	t.Helper()
	markRetryLogBackfillCompletedForTest(t)

	now := time.Now().Unix()
	logs := []Log{
		{
			UserId:           1,
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Quota:            100,
			PromptTokens:     5,
			CompletionTokens: 10,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log": true,
				"admin_info": map[string]interface{}{
					"use_channel": []string{"1", "2"},
				},
			}),
		},
		{
			UserId:           1,
			CreatedAt:        now - 20,
			Type:             LogTypeConsume,
			Quota:            200,
			PromptTokens:     7,
			CompletionTokens: 8,
			Other: common.MapToJsonStr(map[string]interface{}{
				"empty_retry": true,
				"admin_info": map[string]interface{}{
					"use_channel": []string{"3"},
				},
			}),
		},
		{
			UserId:           1,
			CreatedAt:        now - 30,
			Type:             LogTypeConsume,
			Quota:            400,
			PromptTokens:     11,
			CompletionTokens: 13,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"use_channel": []string{"4"},
				},
				"empty_output_types": []string{"message", "function_call"},
			}),
		},
		{
			UserId:           2,
			CreatedAt:        now - 40,
			Type:             LogTypeConsume,
			Quota:            800,
			PromptTokens:     17,
			CompletionTokens: 19,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"use_channel": []string{"5"},
				},
			}),
		},
		{
			UserId:           2,
			CreatedAt:        now - 50,
			Type:             LogTypeConsume,
			Quota:            900,
			PromptTokens:     23,
			CompletionTokens: 18,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log": true,
				"admin_info": map[string]interface{}{
					"use_channel": []string{"6", "7"},
				},
			}),
		},
		{
			UserId:           1,
			CreatedAt:        now - 5,
			Type:             LogTypeError,
			Quota:            0,
			PromptTokens:     0,
			CompletionTokens: 0,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log": true,
				"admin_info": map[string]interface{}{
					"use_channel": []string{"8"},
				},
			}),
		},
		{
			UserId:           1,
			CreatedAt:        now - 55,
			Type:             LogTypeConsume,
			Quota:            350,
			PromptTokens:     12,
			CompletionTokens: 13,
			Other: common.MapToJsonStr(map[string]interface{}{
				"retry_log":   true,
				"empty_retry": true,
				"admin_info": map[string]interface{}{
					"use_channel": []string{"9", "10"},
				},
			}),
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	return logs
}

func TestFormatUserLogDetailExposesOnlyUserAuditContent(t *testing.T) {
	withLogAuditSettings(t, true, true)

	log := &Log{
		Id:          42,
		ChannelName: "secret channel",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"request_content":            "user prompt",
				"request_content_truncated":  true,
				"response_content":           "model answer",
				"response_content_truncated": true,
				"local_count_tokens":         true,
				"use_channel":                []int{1, 2},
			},
			"audit_info": map[string]interface{}{
				"request_content": "stale audit content",
			},
			"stream_status": map[string]interface{}{
				"status": "error",
			},
		}),
	}

	formatUserLog(log)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, 42, log.Id)
	require.Empty(t, log.ChannelName)
	require.NotContains(t, other, "admin_info")
	streamStatus, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "error", streamStatus["status"])

	auditInfo, ok := other["audit_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "user prompt", auditInfo["request_content"])
	require.Equal(t, true, auditInfo["request_content_truncated"])
	require.Equal(t, "model answer", auditInfo["response_content"])
	require.Equal(t, true, auditInfo["response_content_truncated"])
	require.NotContains(t, auditInfo, "local_count_tokens")
	require.NotContains(t, auditInfo, "use_channel")
}

func TestFormatUserLogsStripsAuditContentFromList(t *testing.T) {
	withLogAuditSettings(t, true, true)

	logs := []*Log{
		{
			Id: 42,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"request_content":            "user prompt",
					"request_content_truncated":  true,
					"response_content":           "model answer",
					"response_content_truncated": true,
					"local_count_tokens":         true,
					"use_channel":                []int{1, 2},
				},
			}),
		},
	}

	formatUserLogs(logs, 10)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, 42, logs[0].LogId)
	require.Equal(t, 11, logs[0].Id)
	require.NotContains(t, other, "admin_info")

	auditInfo, ok := other["audit_info"].(map[string]interface{})
	require.True(t, ok)
	require.NotContains(t, auditInfo, "request_content")
	require.NotContains(t, auditInfo, "response_content")
	require.Equal(t, true, auditInfo["request_content_truncated"])
	require.Equal(t, true, auditInfo["response_content_truncated"])
}

func TestStripLogAuditContentKeepsAdminMetadata(t *testing.T) {
	log := &Log{
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"request_content":            "user prompt",
				"request_content_truncated":  true,
				"response_content":           "model answer",
				"response_content_truncated": true,
				"local_count_tokens":         true,
				"use_channel":                []int{1, 2},
			},
		}),
	}

	stripLogAuditContent(log)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.NotContains(t, adminInfo, "request_content")
	require.NotContains(t, adminInfo, "response_content")
	require.Equal(t, true, adminInfo["request_content_truncated"])
	require.Equal(t, true, adminInfo["response_content_truncated"])
	require.Equal(t, true, adminInfo["local_count_tokens"])
	require.Contains(t, adminInfo, "use_channel")
}

func TestFormatUserLogsHidesAuditContentWhenDisabled(t *testing.T) {
	withLogAuditSettings(t, false, false)

	logs := []*Log{
		{
			Id: 42,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"request_content":  "user prompt",
					"response_content": "model answer",
				},
			}),
		},
	}

	formatUserLogs(logs, 0)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, 42, logs[0].LogId)
	require.NotContains(t, other, "admin_info")
	require.NotContains(t, other, "audit_info")
}

func TestSumUsedTokenUsesPortableAggregate(t *testing.T) {
	require.Equal(t, "COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0)", sumUsedTokenSelect)
}

package model

import (
	"context"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelPerformanceTestDB(t *testing.T, retryMetricsReady bool) *gorm.DB {
	t.Helper()

	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres, previousMySQL := common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL
	previousLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = previousSQLite, previousPostgres, previousMySQL
		common.LogSqlType = previousLogSQLType
		initCol()
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = true, false, false
	common.LogSqlType = common.DatabaseTypeSQLite
	initCol()
	require.NoError(t, db.AutoMigrate(&Log{}, &Channel{}, &Option{}))
	if retryMetricsReady {
		require.NoError(t, markLogRetryMarkerBackfillCompleted())
	}
	return db
}

func TestInitColUsesIndependentLogDatabaseDialect(t *testing.T) {
	previousSQLite, previousPostgres, previousMySQL := common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL
	previousLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = previousSQLite, previousPostgres, previousMySQL
		common.LogSqlType = previousLogSQLType
		initCol()
	})
	t.Setenv("LOG_SQL_DSN", "separate-log-database")

	common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = false, true, false
	common.LogSqlType = common.DatabaseTypeMySQL
	initCol()
	require.Equal(t, `"group"`, commonGroupCol)
	require.Equal(t, "`group`", logGroupCol)
	require.Equal(t, "`key`", logKeyCol)

	common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = false, false, true
	common.LogSqlType = common.DatabaseTypePostgreSQL
	initCol()
	require.Equal(t, "`group`", commonGroupCol)
	require.Equal(t, `"group"`, logGroupCol)
	require.Equal(t, `"key"`, logKeyCol)
}

func TestQueryLegacyChannelPerformanceExcludesNonAttemptBillingLogs(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, true)

	now := int64(1_700_000_000)
	require.NoError(t, db.Create(&Channel{Id: 7, Name: "Primary", Type: 1, Status: 1, ResponseTime: 240, TestTime: now}).Error)
	require.NoError(t, db.Create(&[]Log{
		{CreatedAt: now - 20, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 120, UseTime: 9, Other: `{"retry_log":true}`},
		{CreatedAt: now - 10, Type: LogTypeError, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 999, UseTime: 1},
		{CreatedAt: now - 9, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 50, UseTime: 5, Content: "Violation fee charged", Other: `{"violation_fee":true}`},
		{CreatedAt: now - 8, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 40, Other: `{"task_id":"task-1","pre_consumed_quota":10,"actual_quota":15}`},
		{CreatedAt: now - 7, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 80, Other: `{"is_task":true}`},
		{CreatedAt: now - 5, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 70, TokenName: "模型测试"},
		{CreatedAt: now - 4, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 60, Content: "模型测试"},
	}).Error)

	result, err := QueryLegacyChannelPerformance(context.Background(), ChannelPerformanceQuery{
		StartAt: now - 100,
		EndAt:   now,
		Limit:   10,
	})
	require.NoError(t, err)
	require.True(t, result.RetryMetricsAvailable)
	require.Equal(t, LegacyRetryMetricStateAvailable, result.RetryMetricState)
	require.Len(t, result.Rows, 1)
	row := result.Rows[0]
	require.Equal(t, 7, row.ChannelID)
	require.Equal(t, "alpha", row.ModelName)
	require.Equal(t, "prod", row.EffectiveGroup)
	require.EqualValues(t, 3, row.ObservedCount)
	require.EqualValues(t, 2, row.ConsumeLogCount)
	require.EqualValues(t, 1, row.ErrorLogCount)
	require.EqualValues(t, 200, row.ConsumedQuota)
	require.EqualValues(t, 1, row.RetryLogCount)
	require.EqualValues(t, 1, row.LatencySampleCount)
	require.EqualValues(t, 1, row.TotalUseTimeSeconds)
	require.EqualValues(t, 3, result.Summary.ObservedCount)
	require.EqualValues(t, 2, result.Summary.ConsumeLogCount)
	require.EqualValues(t, 1, result.Summary.ErrorLogCount)
	require.EqualValues(t, 200, result.Summary.ConsumedQuota)
	require.EqualValues(t, 1, result.Summary.LatencySampleCount)
	require.EqualValues(t, 1, result.Summary.TotalUseTimeSeconds)
	require.EqualValues(t, 1, result.Summary.ChannelCount)
	require.EqualValues(t, now-7, result.Summary.LastObservedAt)

	metadata, err := GetChannelPerformanceChannels(context.Background(), []int{7})
	require.NoError(t, err)
	require.Equal(t, "Primary", metadata[7].Name)
	require.Equal(t, 240, metadata[7].ResponseTime)
}

func TestQueryLegacyChannelPerformanceSummaryIsNotLimitedWithRows(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, true)
	now := int64(1_700_000_000)
	logs := make([]Log, 0, 3)
	for channelID := 1; channelID <= 3; channelID++ {
		logs = append(logs, Log{
			CreatedAt: now - int64(channelID), Type: LogTypeConsume,
			ModelName: "alpha", ChannelId: channelID, Group: "prod", Quota: channelID * 10, UseTime: channelID,
		})
	}
	require.NoError(t, db.Create(&logs).Error)

	result, err := QueryLegacyChannelPerformance(context.Background(), ChannelPerformanceQuery{
		StartAt: now - 100,
		EndAt:   now,
		Limit:   1,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 2, "the row query should retain one extra row for truncation detection")
	require.EqualValues(t, 3, result.Summary.ChannelCount)
	require.EqualValues(t, 3, result.Summary.ObservedCount)
	require.EqualValues(t, 3, result.Summary.ConsumeLogCount)
	require.EqualValues(t, 60, result.Summary.ConsumedQuota)
	require.EqualValues(t, 3, result.Summary.LatencySampleCount)
	require.EqualValues(t, 6, result.Summary.TotalUseTimeSeconds)
	require.EqualValues(t, now-1, result.Summary.LastObservedAt)
}

func TestQueryLegacyChannelPerformancePreservesSignedConsumeQuota(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, true)
	now := int64(1_700_000_000)
	require.NoError(t, db.Create(&[]Log{
		{CreatedAt: now - 3, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 100},
		{CreatedAt: now - 2, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 0},
		{CreatedAt: now - 1, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: -25},
		{CreatedAt: now - 1, Type: LogTypeError, ModelName: "alpha", ChannelId: 7, Group: "prod", Quota: 500},
	}).Error)

	result, err := QueryLegacyChannelPerformance(context.Background(), ChannelPerformanceQuery{
		StartAt: now - 100,
		EndAt:   now,
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.EqualValues(t, 75, result.Rows[0].ConsumedQuota)
	require.EqualValues(t, 75, result.Summary.ConsumedQuota)
}

func TestQueryLegacyChannelPerformanceMarksRetryMetricsUnavailableBeforeBackfill(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, false)
	now := int64(1_700_000_000)
	log := Log{
		CreatedAt: now - 1, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 7,
		Group: "prod", UseTime: 5, Other: `{"retry_log":true}`,
	}
	require.NoError(t, db.Create(&log).Error)
	require.NoError(t, db.Model(&Log{}).Where("id = ?", log.Id).Updates(map[string]interface{}{
		"is_retry": false, "is_error_retry": false, "is_empty_retry": false,
	}).Error)

	result, err := QueryLegacyChannelPerformance(context.Background(), ChannelPerformanceQuery{
		StartAt: now - 100,
		EndAt:   now,
		Limit:   10,
	})
	require.NoError(t, err)
	require.False(t, result.RetryMetricsAvailable)
	require.Equal(t, LegacyRetryMetricStatePending, result.RetryMetricState)
	require.Len(t, result.Rows, 1)
	require.Zero(t, result.Rows[0].RetryLogCount)
	require.Zero(t, result.Rows[0].LatencySampleCount)
	require.Zero(t, result.Rows[0].TotalUseTimeSeconds)
	require.Zero(t, result.Summary.RetryLogCount)
	require.Zero(t, result.Summary.LatencySampleCount)
	require.Zero(t, result.Summary.TotalUseTimeSeconds)
}

func TestQueryLegacyChannelPerformanceReturnsZeroSummaryWithoutRows(t *testing.T) {
	setupChannelPerformanceTestDB(t, true)

	result, err := QueryLegacyChannelPerformance(context.Background(), ChannelPerformanceQuery{
		StartAt: 1_700_000_000,
		EndAt:   1_700_000_100,
		Limit:   10,
	})
	require.NoError(t, err)
	require.Empty(t, result.Rows)
	require.Zero(t, result.Summary.ChannelCount)
	require.Zero(t, result.Summary.ObservedCount)
	require.Zero(t, result.Summary.ConsumedQuota)
	require.Zero(t, result.Summary.LatencySampleCount)
	require.Zero(t, result.Summary.LastObservedAt)
}

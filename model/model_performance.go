package model

import (
	"context"
	"fmt"
)

// ModelPerformanceQuery describes the bounded, read-only legacy log query
// used by Smart Operations Center to aggregate production evidence by model.
type ModelPerformanceQuery struct {
	StartAt   int64
	EndAt     int64
	Hours     int
	Limit     int
	ModelName string
	Group     string
}

type LegacyModelPerformanceRow struct {
	ModelName           string `gorm:"column:model_name"`
	ChannelCount        int64  `gorm:"column:channel_count"`
	ObservedCount       int64  `gorm:"column:observed_count"`
	ConsumeLogCount     int64  `gorm:"column:consume_log_count"`
	ErrorLogCount       int64  `gorm:"column:error_log_count"`
	ConsumedQuota       int64  `gorm:"column:consumed_quota"`
	RetryLogCount       int64  `gorm:"column:retry_log_count"`
	LatencySampleCount  int64  `gorm:"column:latency_sample_count"`
	TotalUseTimeSeconds int64  `gorm:"column:total_use_time_seconds"`
	LastObservedAt      int64  `gorm:"column:last_observed_at"`
}

type LegacyModelPerformanceSummary struct {
	ModelCount          int64 `gorm:"column:model_count"`
	ChannelCount        int64 `gorm:"column:channel_count"`
	ObservedCount       int64 `gorm:"column:observed_count"`
	ConsumeLogCount     int64 `gorm:"column:consume_log_count"`
	ErrorLogCount       int64 `gorm:"column:error_log_count"`
	ConsumedQuota       int64 `gorm:"column:consumed_quota"`
	RetryLogCount       int64 `gorm:"column:retry_log_count"`
	LatencySampleCount  int64 `gorm:"column:latency_sample_count"`
	TotalUseTimeSeconds int64 `gorm:"column:total_use_time_seconds"`
	LastObservedAt      int64 `gorm:"column:last_observed_at"`
}

type LegacyModelPerformanceResult struct {
	Rows                  []LegacyModelPerformanceRow
	Summary               LegacyModelPerformanceSummary
	RetryMetricsAvailable bool
	RetryMetricState      string
}

// QueryLegacyModelPerformance reads existing log rows and aggregates them by
// model. It intentionally shares the channel-performance eligibility rules so
// both views exclude probes, billing adjustments and known non-attempt logs in
// the same way.
func QueryLegacyModelPerformance(ctx context.Context, query ModelPerformanceQuery) (LegacyModelPerformanceResult, error) {
	if LOG_DB == nil {
		return LegacyModelPerformanceResult{}, fmt.Errorf("log database is not initialized")
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}

	groupColumn := logGroupCol
	if groupColumn == "" {
		groupColumn = commonGroupCol
	}

	retryMetricState := legacyChannelPerformanceRetryMetricState()
	retryMetricsAvailable := retryMetricState == LegacyRetryMetricStateAvailable
	rowSelect, rowArgs := legacyModelPerformanceSelect(true, retryMetricsAvailable)
	baseQuery := ChannelPerformanceQuery{
		StartAt:   query.StartAt,
		EndAt:     query.EndAt,
		ModelName: query.ModelName,
		Group:     query.Group,
	}

	var rows []LegacyModelPerformanceRow
	err := legacyChannelPerformanceQuery(ctx, baseQuery, groupColumn).
		Select(rowSelect, rowArgs...).
		Group("model_name").
		Order("observed_count DESC, last_observed_at DESC, model_name ASC").
		Limit(limit + 1).
		Scan(&rows).Error
	if err != nil {
		return LegacyModelPerformanceResult{}, err
	}

	summarySelect, summaryArgs := legacyModelPerformanceSelect(false, retryMetricsAvailable)
	var summary LegacyModelPerformanceSummary
	if err := legacyChannelPerformanceQuery(ctx, baseQuery, groupColumn).
		Select(summarySelect, summaryArgs...).
		Scan(&summary).Error; err != nil {
		return LegacyModelPerformanceResult{}, err
	}

	return LegacyModelPerformanceResult{
		Rows:                  rows,
		Summary:               summary,
		RetryMetricsAvailable: retryMetricsAvailable,
		RetryMetricState:      retryMetricState,
	}, nil
}

func legacyModelPerformanceSelect(includeModel bool, retryMetricsAvailable bool) (string, []any) {
	prefix := "COUNT(DISTINCT model_name) AS model_count, COUNT(DISTINCT channel_id) AS channel_count, "
	if includeModel {
		prefix = "model_name, COUNT(DISTINCT channel_id) AS channel_count, "
	}

	retryExpr := "0 AS retry_log_count, "
	args := []any{LogTypeConsume, LogTypeError, LogTypeConsume}
	if retryMetricsAvailable {
		retryExpr = "SUM(CASE WHEN is_retry = ? OR is_error_retry = ? OR is_empty_retry = ? THEN 1 ELSE 0 END) AS retry_log_count, "
		args = append(args, true, true, true)
	}

	// Keep quota on the same eligible-log projection as channel performance;
	// this operational total intentionally excludes accounting-only rows.
	selectClause := prefix +
		"COUNT(*) AS observed_count, " +
		"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS consume_log_count, " +
		"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS error_log_count, " +
		"SUM(CASE WHEN type = ? THEN quota ELSE 0 END) AS consumed_quota, " +
		retryExpr
	if !retryMetricsAvailable {
		return selectClause +
			"0 AS total_use_time_seconds, " +
			"0 AS latency_sample_count, " +
			"MAX(created_at) AS last_observed_at", args
	}

	args = append(args,
		LogTypeConsume, legacyTaskSubmissionPattern, false, false, false,
		LogTypeConsume, legacyTaskSubmissionPattern, false, false, false,
	)
	return selectClause +
		"SUM(CASE WHEN (type <> ? OR other IS NULL OR other NOT LIKE ?) AND is_retry = ? AND is_error_retry = ? AND is_empty_retry = ? THEN use_time ELSE 0 END) AS total_use_time_seconds, " +
		"SUM(CASE WHEN (type <> ? OR other IS NULL OR other NOT LIKE ?) AND is_retry = ? AND is_error_retry = ? AND is_empty_retry = ? THEN 1 ELSE 0 END) AS latency_sample_count, " +
		"MAX(created_at) AS last_observed_at", args
}

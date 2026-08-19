package model

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const (
	LegacyRetryMetricStateAvailable   = "available"
	LegacyRetryMetricStatePending     = "pending"
	LegacyRetryMetricStateUnavailable = "unavailable"
)

const (
	legacyViolationFeePattern   = `%"violation_fee":true%`
	legacyTaskAdjustmentPattern = `%"pre_consumed_quota":%`
	legacyTaskSubmissionPattern = `%"is_task":true%`
	legacyViolationFeeContent   = "Violation fee charged"
)

// ChannelPerformanceQuery describes the bounded, read-only legacy log query
// used by Smart Operations Center. It deliberately does not model a Relay
// Attempt; a future Attempt reader can implement the service seam separately.
type ChannelPerformanceQuery struct {
	StartAt   int64
	EndAt     int64
	Hours     int
	Limit     int
	ChannelID int
	ModelName string
	Group     string
}

// LegacyChannelPerformanceRow is an aggregate projection from LOG_DB. Keep
// this projection narrow so channel credentials, prompts and responses never
// enter the operations surface.
type LegacyChannelPerformanceRow struct {
	ChannelID           int    `gorm:"column:channel_id"`
	ModelName           string `gorm:"column:model_name"`
	EffectiveGroup      string `gorm:"column:effective_group"`
	ObservedCount       int64  `gorm:"column:observed_count"`
	ConsumeLogCount     int64  `gorm:"column:consume_log_count"`
	ErrorLogCount       int64  `gorm:"column:error_log_count"`
	ConsumedQuota       int64  `gorm:"column:consumed_quota"`
	RetryLogCount       int64  `gorm:"column:retry_log_count"`
	LatencySampleCount  int64  `gorm:"column:latency_sample_count"`
	TotalUseTimeSeconds int64  `gorm:"column:total_use_time_seconds"`
	LastObservedAt      int64  `gorm:"column:last_observed_at"`
}

type LegacyChannelPerformanceSummary struct {
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

type LegacyChannelPerformanceResult struct {
	Rows                  []LegacyChannelPerformanceRow
	Summary               LegacyChannelPerformanceSummary
	RetryMetricsAvailable bool
	RetryMetricState      string
}

// ChannelPerformanceChannel is the safe metadata projection joined in the
// application after LOG_DB aggregation. It intentionally excludes Key,
// BaseURL, overrides, settings and all other secret-bearing fields.
type ChannelPerformanceChannel struct {
	ID           int    `gorm:"column:id"`
	Name         string `gorm:"column:name"`
	Type         int    `gorm:"column:type"`
	Status       int    `gorm:"column:status"`
	ResponseTime int    `gorm:"column:response_time"`
	TestTime     int64  `gorm:"column:test_time"`
}

// QueryLegacyChannelPerformance reads only existing log rows. The bounded row
// query returns one extra row for truncation detection, while the independent
// summary query always covers every matching eligible log.
func QueryLegacyChannelPerformance(ctx context.Context, query ChannelPerformanceQuery) (LegacyChannelPerformanceResult, error) {
	if LOG_DB == nil {
		return LegacyChannelPerformanceResult{}, fmt.Errorf("log database is not initialized")
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}

	// group is reserved by all supported databases. logGroupCol is selected by
	// initCol for the configured LOG_DB dialect.
	groupColumn := logGroupCol
	if groupColumn == "" {
		groupColumn = commonGroupCol
	}

	retryMetricState := legacyChannelPerformanceRetryMetricState()
	retryMetricsAvailable := retryMetricState == LegacyRetryMetricStateAvailable
	rowSelect, rowArgs := legacyChannelPerformanceSelect(groupColumn, true, retryMetricsAvailable)

	var rows []LegacyChannelPerformanceRow
	err := legacyChannelPerformanceQuery(ctx, query, groupColumn).
		Select(rowSelect, rowArgs...).
		Group("channel_id, model_name, " + groupColumn).
		Order("observed_count DESC, last_observed_at DESC").
		Limit(limit + 1).
		Scan(&rows).Error
	if err != nil {
		return LegacyChannelPerformanceResult{}, err
	}

	summarySelect, summaryArgs := legacyChannelPerformanceSelect(groupColumn, false, retryMetricsAvailable)
	var summary LegacyChannelPerformanceSummary
	if err := legacyChannelPerformanceQuery(ctx, query, groupColumn).
		Select(summarySelect, summaryArgs...).
		Scan(&summary).Error; err != nil {
		return LegacyChannelPerformanceResult{}, err
	}

	return LegacyChannelPerformanceResult{
		Rows:                  rows,
		Summary:               summary,
		RetryMetricsAvailable: retryMetricsAvailable,
		RetryMetricState:      retryMetricState,
	}, nil
}

func legacyChannelPerformanceRetryMetricState() string {
	if err := ensureLogRetryMarkerBackfillCompletedForRead(); err == nil {
		return LegacyRetryMetricStateAvailable
	} else if errors.Is(err, ErrLogRetryMarkerBackfillIncomplete) {
		return LegacyRetryMetricStatePending
	}
	return LegacyRetryMetricStateUnavailable
}

func legacyChannelPerformanceQuery(ctx context.Context, query ChannelPerformanceQuery, groupColumn string) *gorm.DB {
	tx := LOG_DB.WithContext(ctx).Table("logs")
	tx = tx.Where("created_at >= ? AND created_at <= ?", query.StartAt, query.EndAt)
	tx = tx.Where("type IN ?", []int{LogTypeConsume, LogTypeError})
	tx = tx.Where("channel_id > ?", 0).Where("model_name <> ?", "")
	// Channel tests are probes, not production traffic. Billing-only consume
	// records are accounting evidence, not successful Relay attempts. Keep the
	// accounting rows intact and exclude them only from this read projection.
	tx = tx.Where("(content IS NULL OR content <> ?)", "模型测试").
		Where("(token_name IS NULL OR token_name <> ?)", "模型测试").
		Where(
			"(type <> ? OR ((other IS NULL OR (other NOT LIKE ? AND other NOT LIKE ?)) AND (content IS NULL OR content <> ?)))",
			LogTypeConsume,
			legacyViolationFeePattern,
			legacyTaskAdjustmentPattern,
			legacyViolationFeeContent,
		)
	if query.ChannelID > 0 {
		tx = tx.Where("channel_id = ?", query.ChannelID)
	}
	if query.ModelName != "" {
		tx = tx.Where("model_name = ?", query.ModelName)
	}
	if query.Group != "" {
		tx = tx.Where(groupColumn+" = ?", query.Group)
	}
	return tx
}

func legacyChannelPerformanceSelect(groupColumn string, includeDimensions bool, retryMetricsAvailable bool) (string, []any) {
	prefix := "COUNT(DISTINCT channel_id) AS channel_count, "
	if includeDimensions {
		prefix = fmt.Sprintf("channel_id, model_name, %s AS effective_group, ", groupColumn)
	}

	retryExpr := "0 AS retry_log_count, "
	args := []any{LogTypeConsume, LogTypeError, LogTypeConsume}
	if retryMetricsAvailable {
		retryExpr = "SUM(CASE WHEN is_retry = ? OR is_error_retry = ? OR is_empty_retry = ? THEN 1 ELSE 0 END) AS retry_log_count, "
		args = append(args, true, true, true)
	}
	// consumed_quota is an operational projection over the same eligible
	// Consume logs as the performance row. It is not a complete billing ledger
	// total because known accounting-only adjustments are excluded upstream.
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

// GetChannelPerformanceChannels returns only the metadata required to label
// aggregate rows. It is intentionally a separate query because LOG_DB may be
// configured as a different database from the primary application database.
func GetChannelPerformanceChannels(ctx context.Context, ids []int) (map[int]ChannelPerformanceChannel, error) {
	result := make(map[int]ChannelPerformanceChannel, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if DB == nil {
		return nil, fmt.Errorf("primary database is not initialized")
	}

	var channels []ChannelPerformanceChannel
	err := DB.WithContext(ctx).Model(&Channel{}).
		Select("id, name, type, status, response_time, test_time").
		Where("id IN ?", ids).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		result[channel.ID] = channel
	}
	return result, nil
}

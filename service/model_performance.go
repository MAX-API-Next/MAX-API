package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	perfmetrics "github.com/MAX-API-Next/MAX-API/pkg/perf_metrics"
)

var ErrInvalidModelPerformanceQuery = errors.New("invalid model performance query")

const modelPerformanceDetailHours = 24

type ModelPerformanceQuery = model.ModelPerformanceQuery

type ModelPerformanceTimeRange struct {
	StartAt int64 `json:"start_at"`
	EndAt   int64 `json:"end_at"`
	Hours   int   `json:"hours"`
}

type ModelPerformanceSummaryStats struct {
	ModelCount          int      `json:"model_count"`
	ChannelCount        int      `json:"channel_count"`
	ObservedCount       int64    `json:"observed_count"`
	ConsumeLogCount     int64    `json:"consume_log_count"`
	ErrorLogCount       int64    `json:"error_log_count"`
	ConsumedQuota       int64    `json:"consumed_quota"`
	RetryLogCount       *int64   `json:"retry_log_count"`
	LatencySampleCount  int64    `json:"latency_sample_count"`
	ObservedSuccessRate *float64 `json:"observed_success_rate"`
	AvgLoggedLatencyMs  *float64 `json:"avg_logged_latency_ms"`
	LastObservedAt      int64    `json:"last_observed_at"`
}

type ModelPerformanceItem struct {
	ModelName           string   `json:"model_name"`
	ChannelCount        int      `json:"channel_count"`
	ObservedCount       int64    `json:"observed_count"`
	ConsumeLogCount     int64    `json:"consume_log_count"`
	ErrorLogCount       int64    `json:"error_log_count"`
	ConsumedQuota       int64    `json:"consumed_quota"`
	RetryLogCount       *int64   `json:"retry_log_count"`
	LatencySampleCount  int64    `json:"latency_sample_count"`
	ObservedSuccessRate *float64 `json:"observed_success_rate"`
	AvgLoggedLatencyMs  *float64 `json:"avg_logged_latency_ms"`
	AvgTps              *float64 `json:"avg_tps"`
	LastObservedAt      int64    `json:"last_observed_at"`
	QualityFlags        []string `json:"quality_flags"`
}

type ModelPerformanceSummary struct {
	StorageMode  string                         `json:"storage_mode"`
	Source       string                         `json:"source"`
	Partial      bool                           `json:"partial"`
	QualityFlags []string                       `json:"quality_flags"`
	TimeRange    ModelPerformanceTimeRange      `json:"time_range"`
	Summary      ModelPerformanceSummaryStats   `json:"summary"`
	Items        []ModelPerformanceItem         `json:"items"`
	Throughput   ModelPerformanceThroughputInfo `json:"throughput"`
	Truncated    bool                           `json:"truncated"`
	GeneratedAt  int64                          `json:"generated_at"`
}

type ModelPerformanceReader interface {
	Summary(context.Context, ModelPerformanceQuery) (ModelPerformanceSummary, error)
}

type legacyModelPerformanceReader struct{}

type modelPerformanceThroughputSnapshot struct {
	State    perfmetrics.CollectionState
	Coverage perfmetrics.QueryCoverage
	ByModel  map[string]float64
}

type ModelPerformanceThroughputInfo struct {
	CollectionState perfmetrics.CollectionState `json:"collection_state"`
	Coverage        perfmetrics.QueryCoverage   `json:"coverage"`
}

func (legacyModelPerformanceReader) Summary(ctx context.Context, query ModelPerformanceQuery) (ModelPerformanceSummary, error) {
	query, rangeWasClamped, err := normalizeModelPerformanceQuery(query)
	if err != nil {
		return ModelPerformanceSummary{}, err
	}

	legacyResult, err := model.QueryLegacyModelPerformance(ctx, query)
	if err != nil {
		return ModelPerformanceSummary{}, err
	}
	rows := legacyResult.Rows
	truncated := len(rows) > query.Limit
	if truncated {
		rows = rows[:query.Limit]
	}
	throughput := queryModelPerformanceThroughput(ctx, query)

	return buildLegacyModelPerformanceSummary(
		query,
		rangeWasClamped,
		legacyResult,
		rows,
		truncated,
		common.LogConsumeEnabled,
		constant.ErrorLogEnabled,
		throughput,
		time.Now().Unix(),
	), nil
}

func buildLegacyModelPerformanceSummary(
	query ModelPerformanceQuery,
	rangeWasClamped bool,
	legacyResult model.LegacyModelPerformanceResult,
	rows []model.LegacyModelPerformanceRow,
	truncated bool,
	consumeLogsEnabled bool,
	errorLogsEnabled bool,
	throughput modelPerformanceThroughputSnapshot,
	generatedAt int64,
) ModelPerformanceSummary {
	qualityFlags := []string{
		"legacy",
		"partial",
		"weak_correlation",
		"heuristic_outcome",
		"coarse_latency",
		"non_attempt_logs_excluded",
		"incomplete_errors_possible",
	}
	if rangeWasClamped {
		qualityFlags = append(qualityFlags, "time_range_clamped")
	}
	if truncated {
		qualityFlags = append(qualityFlags, "truncated")
	}
	switch legacyResult.RetryMetricState {
	case model.LegacyRetryMetricStatePending:
		qualityFlags = append(qualityFlags, "retry_backfill_pending")
	case model.LegacyRetryMetricStateUnavailable:
		qualityFlags = append(qualityFlags, "retry_metrics_unavailable")
	}
	if !consumeLogsEnabled {
		qualityFlags = append(qualityFlags, "consume_logs_disabled")
	}
	if !errorLogsEnabled {
		qualityFlags = append(qualityFlags, "error_logs_disabled")
	}
	switch throughput.State {
	case perfmetrics.CollectionStatePartial:
		qualityFlags = append(qualityFlags, "throughput_partial")
	case perfmetrics.CollectionStateDisabled:
		qualityFlags = append(qualityFlags, "throughput_collection_disabled")
	case perfmetrics.CollectionStateNoSamples:
		qualityFlags = append(qualityFlags, "throughput_no_samples")
	case perfmetrics.CollectionStateQueryFailed:
		qualityFlags = append(qualityFlags, "throughput_query_failed")
	}
	if throughput.Coverage.Approximate && throughput.State != perfmetrics.CollectionStateNoSamples && throughput.State != perfmetrics.CollectionStateQueryFailed {
		qualityFlags = append(qualityFlags, "throughput_window_approximate")
	}

	allowSuccessRate := consumeLogsEnabled && errorLogsEnabled
	items := make([]ModelPerformanceItem, 0, len(rows))
	for _, row := range rows {
		item := ModelPerformanceItem{
			ModelName:          row.ModelName,
			ChannelCount:       int(row.ChannelCount),
			ObservedCount:      row.ObservedCount,
			ConsumeLogCount:    row.ConsumeLogCount,
			ErrorLogCount:      row.ErrorLogCount,
			ConsumedQuota:      row.ConsumedQuota,
			LatencySampleCount: row.LatencySampleCount,
			LastObservedAt:     row.LastObservedAt,
			QualityFlags:       append([]string(nil), qualityFlags...),
		}
		if legacyResult.RetryMetricsAvailable {
			item.RetryLogCount = int64Pointer(row.RetryLogCount)
		}
		if item.LatencySampleCount > 0 {
			item.AvgLoggedLatencyMs = floatPointer(float64(row.TotalUseTimeSeconds*1000) / float64(item.LatencySampleCount))
		}
		if avgTps, ok := throughput.ByModel[row.ModelName]; ok {
			item.AvgTps = floatPointer(avgTps)
		}
		if allowSuccessRate && item.ObservedCount > 0 {
			item.ObservedSuccessRate = floatPointer(float64(item.ConsumeLogCount) / float64(item.ObservedCount) * 100)
		}
		items = append(items, item)
	}

	summary := ModelPerformanceSummaryStats{
		ModelCount:         int(legacyResult.Summary.ModelCount),
		ChannelCount:       int(legacyResult.Summary.ChannelCount),
		ObservedCount:      legacyResult.Summary.ObservedCount,
		ConsumeLogCount:    legacyResult.Summary.ConsumeLogCount,
		ErrorLogCount:      legacyResult.Summary.ErrorLogCount,
		ConsumedQuota:      legacyResult.Summary.ConsumedQuota,
		LatencySampleCount: legacyResult.Summary.LatencySampleCount,
		LastObservedAt:     legacyResult.Summary.LastObservedAt,
	}
	if legacyResult.RetryMetricsAvailable {
		summary.RetryLogCount = int64Pointer(legacyResult.Summary.RetryLogCount)
	}
	if allowSuccessRate && summary.ObservedCount > 0 {
		summary.ObservedSuccessRate = floatPointer(float64(summary.ConsumeLogCount) / float64(summary.ObservedCount) * 100)
	}
	if summary.LatencySampleCount > 0 {
		summary.AvgLoggedLatencyMs = floatPointer(float64(legacyResult.Summary.TotalUseTimeSeconds*1000) / float64(summary.LatencySampleCount))
	}

	return ModelPerformanceSummary{
		StorageMode:  "legacy_log",
		Source:       "log_db",
		Partial:      true,
		QualityFlags: qualityFlags,
		TimeRange: ModelPerformanceTimeRange{
			StartAt: query.StartAt,
			EndAt:   query.EndAt,
			Hours:   int((query.EndAt - query.StartAt) / int64(time.Hour/time.Second)),
		},
		Summary: summary,
		Items:   items,
		Throughput: ModelPerformanceThroughputInfo{
			CollectionState: throughput.State,
			Coverage:        throughput.Coverage,
		},
		Truncated:   truncated,
		GeneratedAt: generatedAt,
	}
}

var queryModelPerformanceSummaryRange = perfmetrics.QuerySummaryAllRangeDetailedContext

func queryModelPerformanceThroughput(ctx context.Context, query ModelPerformanceQuery) modelPerformanceThroughputSnapshot {
	groups := []string(nil)
	if query.Group != "" {
		groups = []string{query.Group}
	}
	result, err := queryModelPerformanceSummaryRange(ctx, query.StartAt, query.EndAt, groups)
	if err != nil {
		logger.LogError(ctx, "failed to query model performance throughput: "+err.Error())
		return modelPerformanceThroughputSnapshot{State: perfmetrics.CollectionStateQueryFailed}
	}
	byModel := make(map[string]float64, len(result.Models))
	for _, summary := range result.Models {
		if query.ModelName != "" && summary.ModelName != query.ModelName {
			continue
		}
		byModel[summary.ModelName] = summary.AvgTps
	}
	return modelPerformanceThroughputSnapshot{
		State:    collectionStateForModelFilter(result.CollectionState, query.ModelName, byModel),
		Coverage: result.Coverage,
		ByModel:  byModel,
	}
}

func collectionStateForModelFilter(
	state perfmetrics.CollectionState,
	modelName string,
	byModel map[string]float64,
) perfmetrics.CollectionState {
	if modelName == "" {
		return state
	}
	if _, hasSamples := byModel[modelName]; hasSamples {
		return state
	}
	switch state {
	case perfmetrics.CollectionStateDisabled:
		return perfmetrics.CollectionStateDisabled
	case perfmetrics.CollectionStatePartial:
		return perfmetrics.CollectionStatePartial
	case perfmetrics.CollectionStateQueryFailed:
		return perfmetrics.CollectionStateQueryFailed
	default:
		return perfmetrics.CollectionStateNoSamples
	}
}

var defaultModelPerformanceReader ModelPerformanceReader = legacyModelPerformanceReader{}

func GetModelPerformance(ctx context.Context, query ModelPerformanceQuery) (ModelPerformanceSummary, error) {
	return defaultModelPerformanceReader.Summary(ctx, query)
}

func GetModelPerformanceDetail(ctx context.Context, modelName string) (perfmetrics.DetailedQueryResult, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return perfmetrics.DetailedQueryResult{}, fmt.Errorf("%w: model is required", ErrInvalidModelPerformanceQuery)
	}
	return perfmetrics.QueryDetailedContext(ctx, perfmetrics.QueryParams{
		Model: modelName,
		Hours: modelPerformanceDetailHours,
	})
}

func normalizeModelPerformanceQuery(query ModelPerformanceQuery) (ModelPerformanceQuery, bool, error) {
	normalized, rangeWasClamped, err := normalizePerformanceQuery(
		query.StartAt,
		query.EndAt,
		query.Hours,
		query.Limit,
		ErrInvalidModelPerformanceQuery,
	)
	if err != nil {
		return query, false, err
	}
	query.StartAt = normalized.StartAt
	query.EndAt = normalized.EndAt
	query.Hours = normalized.Hours
	query.Limit = normalized.Limit
	return query, rangeWasClamped, nil
}

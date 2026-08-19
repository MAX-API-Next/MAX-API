package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
)

var ErrInvalidChannelPerformanceQuery = errors.New("invalid channel performance query")

const (
	defaultChannelPerformanceHours = 1
	channelPerformanceDetailHours  = 24
	maxChannelPerformanceHours     = 168
	defaultChannelPerformanceLimit = 100
	maxChannelPerformanceLimit     = 200
)

type ChannelPerformanceQuery = model.ChannelPerformanceQuery

type ChannelPerformanceTimeRange struct {
	StartAt int64 `json:"start_at"`
	EndAt   int64 `json:"end_at"`
	Hours   int   `json:"hours"`
}

type ChannelPerformanceSummaryStats struct {
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

type ChannelPerformanceItem struct {
	ChannelID           int      `json:"channel_id"`
	ChannelName         string   `json:"channel_name"`
	ChannelType         *int     `json:"channel_type"`
	ChannelStatus       *int     `json:"channel_status"`
	ModelName           string   `json:"model_name"`
	EffectiveGroup      string   `json:"effective_group"`
	ObservedCount       int64    `json:"observed_count"`
	ConsumeLogCount     int64    `json:"consume_log_count"`
	ErrorLogCount       int64    `json:"error_log_count"`
	ConsumedQuota       int64    `json:"consumed_quota"`
	RetryLogCount       *int64   `json:"retry_log_count"`
	LatencySampleCount  int64    `json:"latency_sample_count"`
	ObservedSuccessRate *float64 `json:"observed_success_rate"`
	AvgLoggedLatencyMs  *float64 `json:"avg_logged_latency_ms"`
	LastObservedAt      int64    `json:"last_observed_at"`
	ProbeLatencyMs      *int     `json:"probe_latency_ms"`
	ProbeTestTime       *int64   `json:"probe_test_time"`
	QualityFlags        []string `json:"quality_flags"`
}

type ChannelPerformanceSummary struct {
	StorageMode  string                         `json:"storage_mode"`
	Source       string                         `json:"source"`
	Partial      bool                           `json:"partial"`
	QualityFlags []string                       `json:"quality_flags"`
	TimeRange    ChannelPerformanceTimeRange    `json:"time_range"`
	Summary      ChannelPerformanceSummaryStats `json:"summary"`
	Items        []ChannelPerformanceItem       `json:"items"`
	Truncated    bool                           `json:"truncated"`
	GeneratedAt  int64                          `json:"generated_at"`
}

// ChannelPerformanceReader is deliberately read-only and narrow. Future
// Attempt/Evidence implementations can replace the legacy source without
// changing the controller or the Smart Operations Center UI contract.
type ChannelPerformanceReader interface {
	Summary(context.Context, ChannelPerformanceQuery) (ChannelPerformanceSummary, error)
}

type legacyChannelPerformanceReader struct{}

func (legacyChannelPerformanceReader) Summary(ctx context.Context, query ChannelPerformanceQuery) (ChannelPerformanceSummary, error) {
	query, rangeWasClamped, err := normalizeChannelPerformanceQuery(query)
	if err != nil {
		return ChannelPerformanceSummary{}, err
	}

	legacyResult, err := model.QueryLegacyChannelPerformance(ctx, query)
	if err != nil {
		return ChannelPerformanceSummary{}, err
	}
	rows := legacyResult.Rows
	limit := query.Limit
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	ids := make([]int, 0, len(rows))
	seen := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ChannelID]; ok {
			continue
		}
		seen[row.ChannelID] = struct{}{}
		ids = append(ids, row.ChannelID)
	}
	channels, err := model.GetChannelPerformanceChannels(ctx, ids)
	if err != nil {
		return ChannelPerformanceSummary{}, err
	}
	return buildLegacyChannelPerformanceSummary(
		query,
		rangeWasClamped,
		legacyResult,
		rows,
		channels,
		truncated,
		common.LogConsumeEnabled,
		constant.ErrorLogEnabled,
		time.Now().Unix(),
	), nil
}

func buildLegacyChannelPerformanceSummary(
	query ChannelPerformanceQuery,
	rangeWasClamped bool,
	legacyResult model.LegacyChannelPerformanceResult,
	rows []model.LegacyChannelPerformanceRow,
	channels map[int]model.ChannelPerformanceChannel,
	truncated bool,
	consumeLogsEnabled bool,
	errorLogsEnabled bool,
	generatedAt int64,
) ChannelPerformanceSummary {
	qualityFlags := []string{
		"legacy",
		"partial",
		"weak_correlation",
		"heuristic_outcome",
		"coarse_latency",
		"probe_separate",
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

	allowSuccessRate := consumeLogsEnabled && errorLogsEnabled
	items := make([]ChannelPerformanceItem, 0, len(rows))
	for _, row := range rows {
		metadata, metadataFound := channels[row.ChannelID]
		itemFlags := append([]string(nil), qualityFlags...)
		item := ChannelPerformanceItem{
			ChannelID:          row.ChannelID,
			ChannelName:        fmt.Sprintf("Channel #%d", row.ChannelID),
			ModelName:          row.ModelName,
			EffectiveGroup:     row.EffectiveGroup,
			ObservedCount:      row.ObservedCount,
			ConsumeLogCount:    row.ConsumeLogCount,
			ErrorLogCount:      row.ErrorLogCount,
			ConsumedQuota:      row.ConsumedQuota,
			LatencySampleCount: row.LatencySampleCount,
			LastObservedAt:     row.LastObservedAt,
			QualityFlags:       itemFlags,
		}
		if legacyResult.RetryMetricsAvailable {
			item.RetryLogCount = int64Pointer(row.RetryLogCount)
		}
		if metadataFound {
			item.ChannelName = metadata.Name
			item.ChannelType = intPointer(metadata.Type)
			item.ChannelStatus = intPointer(metadata.Status)
			if metadata.ResponseTime > 0 {
				item.ProbeLatencyMs = intPointer(metadata.ResponseTime)
			}
			if metadata.TestTime > 0 {
				item.ProbeTestTime = int64Pointer(metadata.TestTime)
			}
		} else {
			item.QualityFlags = append(item.QualityFlags, "metadata_missing")
		}
		if item.ProbeLatencyMs == nil {
			item.QualityFlags = append(item.QualityFlags, "probe_unavailable")
		}
		if item.LatencySampleCount > 0 {
			item.AvgLoggedLatencyMs = floatPointer(float64(row.TotalUseTimeSeconds*1000) / float64(item.LatencySampleCount))
		}
		if allowSuccessRate && item.ObservedCount > 0 {
			item.ObservedSuccessRate = floatPointer(float64(item.ConsumeLogCount) / float64(item.ObservedCount) * 100)
		}
		items = append(items, item)
	}

	summary := ChannelPerformanceSummaryStats{
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

	return ChannelPerformanceSummary{
		StorageMode:  "legacy_log",
		Source:       "log_db",
		Partial:      true,
		QualityFlags: qualityFlags,
		TimeRange: ChannelPerformanceTimeRange{
			StartAt: query.StartAt,
			EndAt:   query.EndAt,
			Hours:   int((query.EndAt - query.StartAt) / int64(time.Hour/time.Second)),
		},
		Summary:     summary,
		Items:       items,
		Truncated:   truncated,
		GeneratedAt: generatedAt,
	}
}

var defaultChannelPerformanceReader ChannelPerformanceReader = legacyChannelPerformanceReader{}

func GetChannelPerformance(ctx context.Context, query ChannelPerformanceQuery) (ChannelPerformanceSummary, error) {
	return defaultChannelPerformanceReader.Summary(ctx, query)
}

// GetChannelPerformanceDetail returns a channel-scoped, read-only 24-hour
// log projection. Keeping this contract separate from the list filters avoids
// accidental automatic refreshes and lets a future Attempt-backed reader
// replace the legacy source without changing the administrator API.
func GetChannelPerformanceDetail(ctx context.Context, channelID int) (ChannelPerformanceSummary, error) {
	if channelID <= 0 {
		return ChannelPerformanceSummary{}, fmt.Errorf("%w: channel_id must be greater than zero", ErrInvalidChannelPerformanceQuery)
	}
	return defaultChannelPerformanceReader.Summary(ctx, ChannelPerformanceQuery{
		ChannelID: channelID,
		Hours:     channelPerformanceDetailHours,
		Limit:     maxChannelPerformanceLimit,
	})
}

func normalizeChannelPerformanceQuery(query ChannelPerformanceQuery) (ChannelPerformanceQuery, bool, error) {
	now := time.Now().Unix()
	rangeWasClamped := false
	if query.EndAt <= 0 {
		query.EndAt = now
	}
	if query.StartAt <= 0 {
		hours := query.Hours
		if hours <= 0 {
			hours = defaultChannelPerformanceHours
		}
		if hours > maxChannelPerformanceHours {
			hours = maxChannelPerformanceHours
			rangeWasClamped = true
		}
		query.Hours = hours
		query.StartAt = query.EndAt - int64(hours)*int64(time.Hour/time.Second)
	}
	if query.EndAt <= query.StartAt {
		return query, false, fmt.Errorf("%w: end must be greater than start", ErrInvalidChannelPerformanceQuery)
	}
	if query.Limit <= 0 {
		query.Limit = defaultChannelPerformanceLimit
	}
	if query.Limit > maxChannelPerformanceLimit {
		query.Limit = maxChannelPerformanceLimit
	}
	maxWindow := int64(maxChannelPerformanceHours) * int64(time.Hour/time.Second)
	if query.EndAt-query.StartAt > maxWindow {
		query.StartAt = query.EndAt - maxWindow
		rangeWasClamped = true
	}
	return query, rangeWasClamped, nil
}

func intPointer(value int) *int { return &value }

func int64Pointer(value int64) *int64 { return &value }

func floatPointer(value float64) *float64 { return &value }

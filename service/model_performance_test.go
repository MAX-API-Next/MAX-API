package service

import (
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	perfmetrics "github.com/MAX-API-Next/MAX-API/pkg/perf_metrics"
	"github.com/stretchr/testify/require"
)

func TestNormalizeModelPerformanceQueryUsesOneHourDefaultAndClampsWindow(t *testing.T) {
	before := time.Now().Unix()
	query, clamped, err := normalizeModelPerformanceQuery(ModelPerformanceQuery{})
	after := time.Now().Unix()

	require.NoError(t, err)
	require.False(t, clamped)
	require.GreaterOrEqual(t, query.EndAt, before)
	require.LessOrEqual(t, query.EndAt, after)
	require.EqualValues(t, time.Hour/time.Second, query.EndAt-query.StartAt)

	query, clamped, err = normalizeModelPerformanceQuery(ModelPerformanceQuery{Hours: 37})
	require.NoError(t, err)
	require.False(t, clamped)
	require.EqualValues(t, 37*time.Hour/time.Second, query.EndAt-query.StartAt)

	query, clamped, err = normalizeModelPerformanceQuery(ModelPerformanceQuery{Hours: 999})
	require.NoError(t, err)
	require.True(t, clamped)
	require.EqualValues(t, maxChannelPerformanceHours*time.Hour/time.Second, query.EndAt-query.StartAt)
}

func TestNormalizeModelPerformanceQueryRejectsReversedRange(t *testing.T) {
	_, _, err := normalizeModelPerformanceQuery(ModelPerformanceQuery{StartAt: 20, EndAt: 10})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidModelPerformanceQuery))
}

func TestBuildLegacyModelPerformanceSummaryPreservesGlobalSummaryWhenRowsTruncated(t *testing.T) {
	result := buildLegacyModelPerformanceSummary(
		ModelPerformanceQuery{StartAt: 1_700_000_000, EndAt: 1_700_003_600, Limit: 1},
		false,
		model.LegacyModelPerformanceResult{
			Rows: []model.LegacyModelPerformanceRow{
				{ModelName: "alpha"},
				{ModelName: "beta"},
			},
			Summary: model.LegacyModelPerformanceSummary{
				ModelCount:          2,
				ChannelCount:        3,
				ObservedCount:       5,
				ConsumeLogCount:     3,
				ErrorLogCount:       2,
				ConsumedQuota:       900,
				RetryLogCount:       1,
				LatencySampleCount:  4,
				TotalUseTimeSeconds: 3,
				LastObservedAt:      1_700_003_500,
			},
			RetryMetricsAvailable: true,
			RetryMetricState:      model.LegacyRetryMetricStateAvailable,
		},
		[]model.LegacyModelPerformanceRow{{
			ModelName:           "alpha",
			ChannelCount:        2,
			ObservedCount:       3,
			ConsumeLogCount:     2,
			ErrorLogCount:       1,
			ConsumedQuota:       500,
			RetryLogCount:       1,
			LatencySampleCount:  2,
			TotalUseTimeSeconds: 1,
			LastObservedAt:      1_700_003_400,
		}},
		true,
		true,
		true,
		modelPerformanceThroughputSnapshot{
			State:   perfmetrics.CollectionStateAvailable,
			ByModel: map[string]float64{"alpha": 42.5},
		},
		1_700_003_600,
	)

	require.True(t, result.Truncated)
	require.Len(t, result.Items, 1)
	require.Contains(t, result.QualityFlags, "truncated")
	require.Equal(t, 2, result.Summary.ModelCount)
	require.Equal(t, 3, result.Summary.ChannelCount)
	require.EqualValues(t, 5, result.Summary.ObservedCount)
	require.EqualValues(t, 900, result.Summary.ConsumedQuota)
	require.NotNil(t, result.Summary.ObservedSuccessRate)
	require.InDelta(t, 60, *result.Summary.ObservedSuccessRate, 0.001)
	require.NotNil(t, result.Summary.AvgLoggedLatencyMs)
	require.InDelta(t, 750, *result.Summary.AvgLoggedLatencyMs, 0.001)
	require.NotNil(t, result.Items[0].RetryLogCount)
	require.EqualValues(t, 500, result.Items[0].ConsumedQuota)
	require.EqualValues(t, 1, *result.Items[0].RetryLogCount)
	require.NotNil(t, result.Items[0].AvgTps)
	require.InDelta(t, 42.5, *result.Items[0].AvgTps, 0.001)
}

func TestBuildLegacyModelPerformanceSummaryDoesNotInventMetricsWhenUnavailable(t *testing.T) {
	result := buildLegacyModelPerformanceSummary(
		ModelPerformanceQuery{StartAt: 1, EndAt: 3_601, Limit: 10},
		false,
		model.LegacyModelPerformanceResult{
			Summary:          model.LegacyModelPerformanceSummary{ObservedCount: 1, ConsumeLogCount: 1},
			RetryMetricState: model.LegacyRetryMetricStatePending,
		},
		[]model.LegacyModelPerformanceRow{{
			ModelName: "alpha", ObservedCount: 1, ConsumeLogCount: 1,
		}},
		false,
		true,
		true,
		modelPerformanceThroughputSnapshot{State: perfmetrics.CollectionStateQueryFailed},
		3_601,
	)

	require.Nil(t, result.Summary.RetryLogCount)
	require.Nil(t, result.Items[0].RetryLogCount)
	require.Nil(t, result.Items[0].AvgLoggedLatencyMs)
	require.Nil(t, result.Items[0].AvgTps)
	require.Contains(t, result.QualityFlags, "retry_backfill_pending")
	require.Contains(t, result.QualityFlags, "throughput_query_failed")
}

func TestBuildLegacyModelPerformanceSummaryDistinguishesThroughputStates(t *testing.T) {
	tests := []struct {
		name  string
		state perfmetrics.CollectionState
		flag  string
	}{
		{name: "collection disabled", state: perfmetrics.CollectionStateDisabled, flag: "throughput_collection_disabled"},
		{name: "no samples", state: perfmetrics.CollectionStateNoSamples, flag: "throughput_no_samples"},
		{name: "query failed", state: perfmetrics.CollectionStateQueryFailed, flag: "throughput_query_failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := buildLegacyModelPerformanceSummary(
				ModelPerformanceQuery{StartAt: 1, EndAt: 3_601, Limit: 10},
				false,
				model.LegacyModelPerformanceResult{},
				nil,
				false,
				true,
				true,
				modelPerformanceThroughputSnapshot{State: test.state},
				3_601,
			)

			require.Contains(t, result.QualityFlags, test.flag)
			require.Equal(t, test.state, result.Throughput.CollectionState)
		})
	}
}

func TestQueryModelPerformanceThroughputMarksQueryFailure(t *testing.T) {
	originalQuery := queryModelPerformanceSummaryRange
	queryModelPerformanceSummaryRange = func(int64, int64, []string) (perfmetrics.DetailedSummaryAllResult, error) {
		return perfmetrics.DetailedSummaryAllResult{}, errors.New("database unavailable")
	}
	t.Cleanup(func() {
		queryModelPerformanceSummaryRange = originalQuery
	})

	snapshot := queryModelPerformanceThroughput(ModelPerformanceQuery{StartAt: 1, EndAt: 3_601})

	require.Equal(t, perfmetrics.CollectionStateQueryFailed, snapshot.State)
	require.Empty(t, snapshot.ByModel)
}

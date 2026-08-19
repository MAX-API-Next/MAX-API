package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
)

type capturingChannelPerformanceReader struct {
	query ChannelPerformanceQuery
}

func (reader *capturingChannelPerformanceReader) Summary(_ context.Context, query ChannelPerformanceQuery) (ChannelPerformanceSummary, error) {
	reader.query = query
	return ChannelPerformanceSummary{TimeRange: ChannelPerformanceTimeRange{Hours: query.Hours}}, nil
}

func TestNormalizeChannelPerformanceQueryBoundsWindowAndLimit(t *testing.T) {
	end := int64(1_700_000_000)
	query, clamped, err := normalizeChannelPerformanceQuery(ChannelPerformanceQuery{
		StartAt: end - int64(30*24*time.Hour/time.Second),
		EndAt:   end,
		Limit:   999,
	})

	require.NoError(t, err)
	require.True(t, clamped)
	require.Equal(t, maxPerformanceLimit, query.Limit)
	require.EqualValues(t, maxPerformanceHours, (query.EndAt-query.StartAt)/int64(time.Hour/time.Second))
}

func TestNormalizeChannelPerformanceQueryUsesServerOwnedHourWindow(t *testing.T) {
	before := time.Now().Unix()
	query, clamped, err := normalizeChannelPerformanceQuery(ChannelPerformanceQuery{})
	after := time.Now().Unix()

	require.NoError(t, err)
	require.False(t, clamped)
	require.GreaterOrEqual(t, query.EndAt, before)
	require.LessOrEqual(t, query.EndAt, after)
	require.EqualValues(t, time.Hour/time.Second, query.EndAt-query.StartAt)

	query, clamped, err = normalizeChannelPerformanceQuery(ChannelPerformanceQuery{Hours: 37})
	require.NoError(t, err)
	require.False(t, clamped)
	require.EqualValues(t, 37*time.Hour/time.Second, query.EndAt-query.StartAt)

	query, clamped, err = normalizeChannelPerformanceQuery(ChannelPerformanceQuery{Hours: 999})
	require.NoError(t, err)
	require.True(t, clamped)
	require.EqualValues(t, maxPerformanceHours*time.Hour/time.Second, query.EndAt-query.StartAt)
}

func TestNormalizeChannelPerformanceQueryRejectsReversedRange(t *testing.T) {
	_, _, err := normalizeChannelPerformanceQuery(ChannelPerformanceQuery{
		StartAt: 20,
		EndAt:   10,
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidChannelPerformanceQuery))
}

func TestGetChannelPerformanceDetailUsesFixedChannelWindow(t *testing.T) {
	previousReader := defaultChannelPerformanceReader
	reader := &capturingChannelPerformanceReader{}
	defaultChannelPerformanceReader = reader
	t.Cleanup(func() { defaultChannelPerformanceReader = previousReader })

	result, err := GetChannelPerformanceDetail(context.Background(), 17)

	require.NoError(t, err)
	require.Equal(t, 17, reader.query.ChannelID)
	require.Equal(t, channelPerformanceDetailHours, reader.query.Hours)
	require.Equal(t, maxPerformanceLimit, reader.query.Limit)
	require.Equal(t, channelPerformanceDetailHours, result.TimeRange.Hours)
}

func TestGetChannelPerformanceDetailRejectsInvalidChannel(t *testing.T) {
	_, err := GetChannelPerformanceDetail(context.Background(), 0)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidChannelPerformanceQuery)
}

func TestBuildLegacyChannelPerformanceSummaryPreservesFullSummaryContract(t *testing.T) {
	retryCount := int64(2)
	result := buildLegacyChannelPerformanceSummary(
		ChannelPerformanceQuery{StartAt: 1_700_000_000, EndAt: 1_700_003_600, Limit: 1},
		false,
		model.LegacyChannelPerformanceResult{
			Rows: []model.LegacyChannelPerformanceRow{
				{ChannelID: 7, ModelName: "alpha", EffectiveGroup: "prod"},
				{ChannelID: 8, ModelName: "beta", EffectiveGroup: "prod"},
			},
			Summary: model.LegacyChannelPerformanceSummary{
				ChannelCount:        2,
				ObservedCount:       5,
				ConsumeLogCount:     3,
				ErrorLogCount:       2,
				ConsumedQuota:       900,
				RetryLogCount:       retryCount,
				LatencySampleCount:  4,
				TotalUseTimeSeconds: 3,
				LastObservedAt:      1_700_003_500,
			},
			RetryMetricsAvailable: true,
			RetryMetricState:      model.LegacyRetryMetricStateAvailable,
		},
		[]model.LegacyChannelPerformanceRow{
			{
				ChannelID:           7,
				ModelName:           "alpha",
				EffectiveGroup:      "prod",
				ObservedCount:       3,
				ConsumeLogCount:     2,
				ErrorLogCount:       1,
				ConsumedQuota:       500,
				RetryLogCount:       1,
				LatencySampleCount:  2,
				TotalUseTimeSeconds: 1,
				LastObservedAt:      1_700_003_400,
			},
		},
		map[int]model.ChannelPerformanceChannel{
			7: {ID: 7, Name: "Primary", Type: 1, Status: 1},
		},
		true,
		true,
		true,
		1_700_003_600,
	)

	require.True(t, result.Truncated)
	require.Len(t, result.Items, 1)
	require.Contains(t, result.QualityFlags, "truncated")
	require.Equal(t, 2, result.Summary.ChannelCount, "summary must not be reduced to the limited row set")
	require.EqualValues(t, 5, result.Summary.ObservedCount)
	require.EqualValues(t, 3, result.Summary.ConsumeLogCount)
	require.EqualValues(t, 900, result.Summary.ConsumedQuota)
	require.NotNil(t, result.Summary.RetryLogCount)
	require.EqualValues(t, retryCount, *result.Summary.RetryLogCount)
	require.NotNil(t, result.Summary.ObservedSuccessRate)
	require.InDelta(t, 60, *result.Summary.ObservedSuccessRate, 0.001)
	require.NotNil(t, result.Summary.AvgLoggedLatencyMs)
	require.InDelta(t, 750, *result.Summary.AvgLoggedLatencyMs, 0.001, "latency must use the explicit sample count")
	require.EqualValues(t, 1_700_003_500, result.Summary.LastObservedAt)
	require.EqualValues(t, 1_700_003_600, result.GeneratedAt)

	item := result.Items[0]
	require.Equal(t, "Primary", item.ChannelName)
	require.NotNil(t, item.ObservedSuccessRate)
	require.EqualValues(t, 500, item.ConsumedQuota)
	require.InDelta(t, 66.666, *item.ObservedSuccessRate, 0.01)
	require.NotNil(t, item.AvgLoggedLatencyMs)
	require.InDelta(t, 500, *item.AvgLoggedLatencyMs, 0.001)
}

func TestBuildLegacyChannelPerformanceSummaryDoesNotInventRetryMetrics(t *testing.T) {
	result := buildLegacyChannelPerformanceSummary(
		ChannelPerformanceQuery{StartAt: 1, EndAt: 3_601, Limit: 10},
		false,
		model.LegacyChannelPerformanceResult{
			Summary:          model.LegacyChannelPerformanceSummary{ObservedCount: 1, ConsumeLogCount: 1},
			RetryMetricState: model.LegacyRetryMetricStatePending,
		},
		[]model.LegacyChannelPerformanceRow{{
			ChannelID: 1, ModelName: "alpha", ObservedCount: 1, ConsumeLogCount: 1,
		}},
		nil,
		false,
		true,
		true,
		3_601,
	)

	require.Nil(t, result.Summary.RetryLogCount)
	require.Nil(t, result.Items[0].RetryLogCount)
	require.Contains(t, result.QualityFlags, "retry_backfill_pending")
	require.Contains(t, result.Items[0].QualityFlags, "retry_backfill_pending")
}

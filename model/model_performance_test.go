package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryLegacyModelPerformanceAggregatesAcrossChannels(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, true)
	now := int64(1_700_000_000)

	require.NoError(t, db.Create(&[]Log{
		{CreatedAt: now - 20, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 1, Group: "prod", Quota: 120, UseTime: 2},
		{CreatedAt: now - 19, Type: LogTypeError, ModelName: "alpha", ChannelId: 1, Group: "prod", Quota: 999},
		{CreatedAt: now - 18, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 2, Group: "prod", Quota: 80, UseTime: 4, Other: `{"retry_log":true}`},
		{CreatedAt: now - 10, Type: LogTypeConsume, ModelName: "beta", ChannelId: 2, Group: "prod", Quota: 50, UseTime: 3},
		{CreatedAt: now - 9, Type: LogTypeConsume, ModelName: "ignored", ChannelId: 0, Group: "prod", Quota: 1000, UseTime: 8},
	}).Error)

	result, err := QueryLegacyModelPerformance(context.Background(), ModelPerformanceQuery{
		StartAt: now - 100,
		EndAt:   now,
		Limit:   10,
	})
	require.NoError(t, err)
	require.True(t, result.RetryMetricsAvailable)
	require.Len(t, result.Rows, 2)

	alpha := result.Rows[0]
	require.Equal(t, "alpha", alpha.ModelName)
	require.EqualValues(t, 2, alpha.ChannelCount)
	require.EqualValues(t, 3, alpha.ObservedCount)
	require.EqualValues(t, 2, alpha.ConsumeLogCount)
	require.EqualValues(t, 1, alpha.ErrorLogCount)
	require.EqualValues(t, 200, alpha.ConsumedQuota)
	require.EqualValues(t, 1, alpha.RetryLogCount)
	require.EqualValues(t, 2, alpha.LatencySampleCount)
	require.EqualValues(t, 2, alpha.TotalUseTimeSeconds)
	require.EqualValues(t, now-18, alpha.LastObservedAt)

	require.Equal(t, "beta", result.Rows[1].ModelName)
	require.EqualValues(t, 1, result.Rows[1].ChannelCount)
	require.EqualValues(t, 1, result.Rows[1].ObservedCount)
	require.EqualValues(t, 50, result.Rows[1].ConsumedQuota)
	require.EqualValues(t, 1, result.Rows[1].LatencySampleCount)

	require.EqualValues(t, 2, result.Summary.ModelCount)
	require.EqualValues(t, 2, result.Summary.ChannelCount)
	require.EqualValues(t, 4, result.Summary.ObservedCount)
	require.EqualValues(t, 3, result.Summary.ConsumeLogCount)
	require.EqualValues(t, 1, result.Summary.ErrorLogCount)
	require.EqualValues(t, 250, result.Summary.ConsumedQuota)
	require.EqualValues(t, 1, result.Summary.RetryLogCount)
	require.EqualValues(t, 3, result.Summary.LatencySampleCount)
	require.EqualValues(t, now-10, result.Summary.LastObservedAt)
}

func TestQueryLegacyModelPerformanceSummaryIsNotLimitedWithRows(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, true)
	now := int64(1_700_000_000)
	logs := []Log{
		{CreatedAt: now - 1, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 1, Group: "prod", Quota: 10},
		{CreatedAt: now - 2, Type: LogTypeConsume, ModelName: "beta", ChannelId: 2, Group: "prod", Quota: 20},
	}
	require.NoError(t, db.Create(&logs).Error)

	result, err := QueryLegacyModelPerformance(context.Background(), ModelPerformanceQuery{
		StartAt: now - 100,
		EndAt:   now,
		Limit:   1,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 2, "the row query should retain one extra row for truncation detection")
	require.EqualValues(t, 2, result.Summary.ModelCount)
	require.EqualValues(t, 2, result.Summary.ObservedCount)
	require.EqualValues(t, 30, result.Summary.ConsumedQuota)
}

func TestQueryLegacyModelPerformanceSupportsModelAndGroupFilters(t *testing.T) {
	db := setupChannelPerformanceTestDB(t, true)
	now := int64(1_700_000_000)
	require.NoError(t, db.Create(&[]Log{
		{CreatedAt: now - 2, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 1, Group: "prod"},
		{CreatedAt: now - 1, Type: LogTypeConsume, ModelName: "alpha", ChannelId: 2, Group: "staging"},
	}).Error)

	result, err := QueryLegacyModelPerformance(context.Background(), ModelPerformanceQuery{
		StartAt:   now - 100,
		EndAt:     now,
		Limit:     10,
		ModelName: "alpha",
		Group:     "prod",
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.EqualValues(t, 1, result.Rows[0].ChannelCount)
	require.EqualValues(t, 1, result.Summary.ModelCount)
	require.EqualValues(t, 1, result.Summary.ChannelCount)
}

package perfmetrics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/perf_metrics_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuerySummaryAllRangeReportsBucketCoverageAndGroupFilter(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.RDB = nil
	common.RedisEnabled = false
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		hotBuckets = sync.Map{}
	})
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "alpha", Group: "prod", BucketTs: 1_000, RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 400, OutputTokens: 100, GenerationMs: 2_000},
		{ModelName: "alpha", Group: "prod", BucketTs: 2_000, RequestCount: 1, SuccessCount: 0, TotalLatencyMs: 300, OutputTokens: 50, GenerationMs: 1_000},
		{ModelName: "alpha", Group: "staging", BucketTs: 1_500, RequestCount: 10, SuccessCount: 10, TotalLatencyMs: 100, OutputTokens: 1_000, GenerationMs: 1_000},
		{ModelName: "beta", Group: "prod", BucketTs: 2_600, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 50, OutputTokens: 100, GenerationMs: 1_000},
	}).Error)

	result, err := QuerySummaryAllRangeDetailed(900, 2_500, []string{"prod"})
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	require.Equal(t, "alpha", result.Models[0].ModelName)
	require.EqualValues(t, 3, result.Models[0].RequestCount)
	require.EqualValues(t, 233, result.Models[0].AvgLatencyMs)
	require.InDelta(t, 66.67, result.Models[0].SuccessRate, 0.001)
	require.InDelta(t, 50, result.Models[0].AvgTps, 0.001)
	require.Equal(t, CollectionStateAvailable, result.CollectionState)
	require.EqualValues(t, 900, result.Coverage.RequestedStartAt)
	require.EqualValues(t, 2_500, result.Coverage.RequestedEndAt)
	require.EqualValues(t, 1_000, result.Coverage.BucketStartAt)
	require.EqualValues(t, 2_500, result.Coverage.BucketEndAt)
	require.EqualValues(t, 3_600, result.Coverage.BucketSeconds)
	require.True(t, result.Coverage.Approximate)
	require.Equal(t, CoverageGranularityUnknown, result.Coverage.GranularityState)
}

func TestBuildQueryResultUsesRawCountersForModelAggregate(t *testing.T) {
	result := buildQueryResult("alpha", map[bucketKey]counters{
		{model: "alpha", group: "small", bucketTs: 3_600}: {
			requestCount:   1,
			successCount:   0,
			totalLatencyMs: 1_000,
			ttftSumMs:      100,
			ttftCount:      1,
			outputTokens:   10,
			generationMs:   1_000,
		},
		{model: "alpha", group: "large", bucketTs: 3_600}: {
			requestCount:   99,
			successCount:   99,
			totalLatencyMs: 9_900,
			ttftSumMs:      4_950,
			ttftCount:      99,
			outputTokens:   990,
			generationMs:   9_900,
		},
	})

	require.Len(t, result.Groups, 2)
	require.EqualValues(t, 109, result.Summary.AvgLatencyMs)
	require.EqualValues(t, 50, result.Summary.AvgTtftMs)
	require.InDelta(t, 99, result.Summary.SuccessRate, 0.001)
	require.InDelta(t, 91.743, result.Summary.AvgTps, 0.001)
	require.Len(t, result.Summary.Series, 1)
	require.InDelta(t, 99, result.Summary.Series[0].SuccessRate, 0.001)
}

func TestBuildQueryResultMergesSameGroupBucketAcrossNodes(t *testing.T) {
	result := buildQueryResult("alpha", map[bucketKey]counters{
		{model: "alpha", group: "prod", node: "node-a", bucketTs: 3_600}: {
			requestCount:   2,
			successCount:   2,
			totalLatencyMs: 200,
			ttftSumMs:      80,
			ttftCount:      2,
			outputTokens:   20,
			generationMs:   1_000,
		},
		{model: "alpha", group: "prod", node: "node-b", bucketTs: 3_600}: {
			requestCount:   3,
			successCount:   2,
			totalLatencyMs: 600,
			ttftSumMs:      120,
			ttftCount:      3,
			outputTokens:   30,
			generationMs:   1_500,
		},
	})

	require.Len(t, result.Groups, 1)
	require.EqualValues(t, 160, result.Groups[0].AvgLatencyMs)
	require.EqualValues(t, 40, result.Groups[0].AvgTtftMs)
	require.InDelta(t, 80, result.Groups[0].SuccessRate, 0.001)
	require.InDelta(t, 80, result.Summary.SuccessRate, 0.001)
}

func TestAtomicBucketCloseAndDrainRejectsLateSample(t *testing.T) {
	bucket := &atomicBucket{}
	require.True(t, bucket.add(Sample{Success: true, LatencyMs: 100}))
	drained := bucket.closeAndDrain()
	require.EqualValues(t, 1, drained.requestCount)
	require.False(t, bucket.add(Sample{Success: true, LatencyMs: 50}))
	require.Zero(t, bucket.snapshot().requestCount)
}

func TestFlushOnceDrainsAcceptedBucketsWhenCollectionIsDisabled(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	setting := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	previousSetting := *setting
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	model.DB = db
	common.RDB = nil
	common.RedisEnabled = false
	*setting = previousSetting
	setting.Enabled = false
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		*setting = previousSetting
		hotBuckets = sync.Map{}
	})

	key := bucketKey{
		model:         "alpha",
		group:         "prod",
		node:          "node-a",
		bucketTs:      bucketStart(time.Now().Unix()) - 3_600,
		bucketSeconds: 3_600,
	}
	bucket := &atomicBucket{}
	require.True(t, bucket.add(Sample{Success: true, LatencyMs: 125}))
	hotBuckets.Store(key, bucket)

	flushOnce()

	rows, err := model.GetPerfMetrics(key.model, key.group, key.bucketTs, key.bucketTs)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].RequestCount)
}

func TestAtomicBucketSnapshotWaitsForLifecycleLock(t *testing.T) {
	bucket := &atomicBucket{}
	bucket.mu.Lock()
	done := make(chan struct{})
	go func() {
		_ = bucket.snapshot()
		close(done)
	}()

	select {
	case <-done:
		bucket.mu.Unlock()
		t.Fatal("snapshot returned while the bucket lifecycle lock was held")
	case <-time.After(50 * time.Millisecond):
		bucket.mu.Unlock()
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("snapshot did not resume after the lifecycle lock was released")
	}
}

func TestAtomicBucketCloseWaitsForInFlightRedisProjection(t *testing.T) {
	bucket := &atomicBucket{}
	value, version, added := bucket.addForProjection(Sample{Success: true, LatencyMs: 100})
	require.True(t, added)
	require.EqualValues(t, 1, value.requestCount)

	drained := make(chan counters, 1)
	go func() {
		drained <- bucket.closeAndDrain()
	}()
	select {
	case <-drained:
		t.Fatal("bucket drained before its in-flight Redis projection completed")
	case <-time.After(50 * time.Millisecond):
	}

	require.False(t, bucket.finishProjection(version, true))
	select {
	case result := <-drained:
		require.EqualValues(t, 1, result.requestCount)
		require.EqualValues(t, 1, result.successCount)
	case <-time.After(time.Second):
		t.Fatal("bucket did not drain after the Redis projection completed")
	}
}

func TestRemoveClosedBucketDoesNotDeleteReplacement(t *testing.T) {
	hotBuckets = sync.Map{}
	key := bucketKey{model: "alpha", group: "prod", node: "node", bucketTs: 3_600, bucketSeconds: 3_600}
	closed := &atomicBucket{}
	require.True(t, closed.add(Sample{Success: true}))
	closed.closeAndDrain()
	replacement := &atomicBucket{}
	require.True(t, replacement.add(Sample{Success: true}))
	hotBuckets.Store(key, replacement)
	t.Cleanup(func() { hotBuckets = sync.Map{} })

	removeClosedBucket(key)

	actual, ok := hotBuckets.Load(key)
	require.True(t, ok)
	require.Same(t, replacement, actual)
}

func TestQueryDetailedExcludesInactiveGroupsBeforeAggregate(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	model.DB = db
	common.RDB = nil
	common.RedisEnabled = false
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		hotBuckets = sync.Map{}
	})

	bucketSeconds := int64(3_600)
	bucketTs := bucketStartFor(time.Now().Unix(), bucketSeconds)
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "alpha", Group: "prod", BucketTs: bucketTs, RequestCount: 99, SuccessCount: 99, TotalLatencyMs: 9_900},
		{ModelName: "alpha", Group: "removed", BucketTs: bucketTs, RequestCount: 1, SuccessCount: 0, TotalLatencyMs: 1_000},
	}).Error)

	result, err := QueryDetailed(QueryParams{
		Model:         "alpha",
		Hours:         1,
		AllowedGroups: []string{"prod"},
	})

	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "prod", result.Groups[0].Group)
	require.InDelta(t, 100, result.Summary.SuccessRate, 0.001)
	require.EqualValues(t, 100, result.Summary.AvgLatencyMs)
}

func TestQueryDetailedMarksRedisFailureAsPartialWhenDatabaseHasSamples(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Close())
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		hotBuckets = sync.Map{}
	})

	bucketTs := bucketStart(time.Now().Unix())
	require.NoError(t, db.Create(&model.PerfMetric{
		ModelName: "alpha", Group: "prod", BucketTs: bucketTs,
		RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 100,
	}).Error)

	result, err := QueryDetailed(QueryParams{Model: "alpha", Hours: 1})

	require.NoError(t, err)
	require.Equal(t, CollectionStatePartial, result.CollectionState)
	require.Len(t, result.Groups, 1)
}

func TestQueryDetailedMarksRedisFailureAsQueryFailedWithoutSamples(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Close())
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		hotBuckets = sync.Map{}
	})

	result, err := QueryDetailed(QueryParams{Model: "alpha", Hours: 1})

	require.NoError(t, err)
	require.Equal(t, CollectionStateQueryFailed, result.CollectionState)
	require.Empty(t, result.Groups)
}

func TestQuerySummaryAllRangeDetailedMarksEmptyWindowAsNoSamples(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.RDB = nil
	common.RedisEnabled = false
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		hotBuckets = sync.Map{}
	})
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))

	result, err := QuerySummaryAllRangeDetailed(900, 2_500, nil)

	require.NoError(t, err)
	require.Empty(t, result.Models)
	require.Equal(t, CollectionStateNoSamples, result.CollectionState)
	require.EqualValues(t, 900, result.Coverage.RequestedStartAt)
	require.EqualValues(t, 2_500, result.Coverage.RequestedEndAt)
	require.Zero(t, result.Coverage.BucketStartAt)
	require.Zero(t, result.Coverage.BucketEndAt)
	require.False(t, result.Coverage.Approximate)
}

func TestResolveCollectionStateDistinguishesNormalOperationalStates(t *testing.T) {
	require.Equal(t, CollectionStateDisabled, resolveCollectionState(false, true, activeCollectionHealthy))
	require.Equal(t, CollectionStateDisabled, resolveCollectionState(false, false, activeCollectionHealthy))
	require.Equal(t, CollectionStateNoSamples, resolveCollectionState(true, false, activeCollectionHealthy))
	require.Equal(t, CollectionStateAvailable, resolveCollectionState(true, true, activeCollectionHealthy))
	require.Equal(t, CollectionStatePartial, resolveCollectionState(true, true, activeCollectionFailed))
	require.Equal(t, CollectionStateQueryFailed, resolveCollectionState(true, false, activeCollectionFailed))
}

func TestQuerySummaryAllRangeDetailedMergesRedisBucketsAcrossNodesWithoutLocalDoubleCounting(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousNodeName := common.NodeName
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.NodeName = previousNodeName
		hotBuckets = sync.Map{}
		_ = redisClient.Close()
	})

	common.NodeName = "perf-node-a"
	Record(Sample{Model: "alpha", Group: "prod", LatencyMs: 100, Success: true})
	common.NodeName = "perf-node-b"
	Record(Sample{Model: "alpha", Group: "prod", LatencyMs: 300, Success: false})

	now := time.Now().Unix()
	result, err := QuerySummaryAllRangeDetailed(now-3600, now+1, []string{"prod"})

	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	require.EqualValues(t, 2, result.Models[0].RequestCount)
	require.EqualValues(t, 200, result.Models[0].AvgLatencyMs)
	require.InDelta(t, 50, result.Models[0].SuccessRate, 0.001)
}

func TestRecoverRedisBucketsForNodeRestoresFlushStateAfterRestart(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousNodeName := common.NodeName
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	common.NodeName = "stable-perf-node"
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.NodeName = previousNodeName
		hotBuckets = sync.Map{}
		_ = redisClient.Close()
	})

	Record(Sample{Model: "alpha", Group: "prod", LatencyMs: 125, Success: true})
	hotBuckets = sync.Map{}

	recovered, err := recoverRedisBucketsForNode()

	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	requestCount := int64(0)
	hotBuckets.Range(func(_, value any) bool {
		requestCount += value.(*atomicBucket).snapshot().requestCount
		return true
	})
	require.EqualValues(t, 1, requestCount)
}

func TestRecoveredCompletedRedisBucketFlushesToDatabaseAndCleansActiveState(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousNodeName := common.NodeName
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	common.NodeName = "stable-flush-node"
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.NodeName = previousNodeName
		hotBuckets = sync.Map{}
		_ = redisClient.Close()
	})

	bucketSeconds := int64(3600)
	key := bucketKey{
		model:         "alpha",
		group:         "prod",
		node:          common.NodeName,
		bucketTs:      bucketStart(time.Now().Unix()) - bucketSeconds,
		bucketSeconds: bucketSeconds,
	}
	require.True(t, recordRedis(key, counters{
		requestCount:   1,
		successCount:   1,
		totalLatencyMs: 250,
	}))
	recovered, err := recoverRedisBucketsForNode()
	require.NoError(t, err)
	require.Equal(t, 1, recovered)

	flushCompletedBuckets()

	rows, err := model.GetPerfMetrics(key.model, key.group, key.bucketTs, key.bucketTs)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].RequestCount)
	require.EqualValues(t, 1, rows[0].SuccessCount)
	require.EqualValues(t, 250, rows[0].TotalLatencyMs)
	require.EqualValues(t, 0, redisClient.Exists(context.Background(), redisBucketKey(key)).Val())
	_, hot := hotBuckets.Load(key)
	require.False(t, hot)
}

func TestRecoveredCommittedRedisBucketDoesNotIncrementDatabaseTwice(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousNodeName := common.NodeName
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	common.NodeName = "stable-idempotent-node"
	hotBuckets = sync.Map{}
	committedBuckets = sync.Map{}
	resetProjectionHealthForTest()
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.NodeName = previousNodeName
		hotBuckets = sync.Map{}
		committedBuckets = sync.Map{}
		resetProjectionHealthForTest()
		_ = redisClient.Close()
	})

	key := bucketKey{
		model:         "alpha",
		group:         "prod",
		node:          common.NodeName,
		bucketTs:      bucketStart(time.Now().Unix()) - 3600,
		bucketSeconds: 3600,
	}
	value := counters{requestCount: 1, successCount: 1, totalLatencyMs: 100}
	require.True(t, recordRedis(key, value))
	applied, err := persistBucket(key, value)
	require.NoError(t, err)
	require.True(t, applied)
	reapplied, err := persistBucket(key, value)
	require.NoError(t, err)
	require.False(t, reapplied, "the durable receipt must make a repeated commit a no-op")

	// Simulate termination after the database transaction commits but before
	// the Redis bucket and process-local receipt cache are cleaned.
	hotBuckets = sync.Map{}
	committedBuckets = sync.Map{}
	recovered, err := recoverRedisBucketsForNode()
	require.NoError(t, err)
	require.Zero(t, recovered)
	require.EqualValues(t, 0, redisClient.Exists(context.Background(), redisBucketKey(key)).Val())

	flushCompletedBuckets()

	rows, err := model.GetPerfMetrics(key.model, key.group, key.bucketTs, key.bucketTs)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].RequestCount)
	require.EqualValues(t, 1, rows[0].SuccessCount)
	require.EqualValues(t, 100, rows[0].TotalLatencyMs)
}

func TestRedisProjectionGapIsVisibleAcrossNodesAfterRedisRecovers(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousNodeName := common.NodeName
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	common.NodeName = "projection-node-a"
	hotBuckets = sync.Map{}
	committedBuckets = sync.Map{}
	resetProjectionHealthForTest()
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.NodeName = previousNodeName
		hotBuckets = sync.Map{}
		committedBuckets = sync.Map{}
		resetProjectionHealthForTest()
		_ = redisClient.Close()
	})

	redisServer.Close()
	Record(Sample{Model: "alpha", Group: "prod", Success: true, LatencyMs: 100})
	hotBuckets = sync.Map{} // another process cannot access node A's local bucket
	require.NoError(t, redisServer.Restart())
	require.NoError(t, publishProjectionHealth())
	resetProjectionHealthForTest() // simulate the independent node B process

	common.NodeName = "projection-node-b"
	Record(Sample{Model: "alpha", Group: "prod", Success: true, LatencyMs: 200})
	result, err := QueryDetailed(QueryParams{Model: "alpha", Hours: 1})
	require.NoError(t, err)
	require.Equal(t, CollectionStatePartial, result.CollectionState)
}

func TestRedisAbsoluteSnapshotIsIdempotentWhenAWriteIsRetried(t *testing.T) {
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		_ = redisClient.Close()
	})

	key := bucketKey{model: "alpha", group: "prod", node: "node-a", bucketTs: bucketStart(time.Now().Unix()), bucketSeconds: 3600}
	value := counters{requestCount: 1, successCount: 1, totalLatencyMs: 100}
	require.True(t, recordRedis(key, value))
	require.True(t, recordRedis(key, value))

	fields, err := redisClient.HGetAll(context.Background(), redisBucketKey(key)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, parseRedisInt(fields["req"]))
	require.EqualValues(t, 1, parseRedisInt(fields["ok"]))
	require.EqualValues(t, 100, parseRedisInt(fields["lat"]))
}

func TestModelQueryUsesModelScopedRedisIndex(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		_ = redisClient.Close()
	})

	now := time.Now().Unix()
	alphaKey := bucketKey{model: "alpha", group: "prod", node: "node-a", bucketTs: bucketStart(now), bucketSeconds: 3600}
	betaKey := bucketKey{model: "beta", group: "prod", node: "node-b", bucketTs: bucketStart(now), bucketSeconds: 3600}
	require.True(t, recordRedis(alphaKey, counters{requestCount: 1, successCount: 1}))
	require.True(t, recordRedis(betaKey, counters{requestCount: 1, successCount: 1}))
	require.NoError(t, redisClient.Del(context.Background(), redisActiveBucketIndex).Err())

	values, err := loadRedisActiveBuckets(context.Background(), activeBucketFilter{
		model:   "alpha",
		startTs: alphaKey.bucketTs,
		endTs:   now,
	})
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Contains(t, values, alphaKey)
}

func TestLoadRedisActiveBucketsSkipsReceiptLookupForOpenBuckets(t *testing.T) {
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousLookup := getPerfMetricFlushReceiptKeys
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	common.RedisEnabled = true
	lookupCalls := 0
	getPerfMetricFlushReceiptKeys = func(context.Context, []string) (map[string]struct{}, error) {
		lookupCalls++
		return map[string]struct{}{}, nil
	}
	t.Cleanup(func() {
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		getPerfMetricFlushReceiptKeys = previousLookup
		_ = redisClient.Close()
	})

	now := time.Now().Unix()
	key := bucketKey{
		model:         "alpha",
		group:         "prod",
		node:          "node-a",
		bucketTs:      bucketStartFor(now, 3_600),
		bucketSeconds: 3_600,
	}
	require.True(t, recordRedis(key, counters{requestCount: 1, successCount: 1}))

	values, err := loadRedisActiveBuckets(context.Background(), activeBucketFilter{
		model:   "alpha",
		startTs: key.bucketTs,
		endTs:   now,
	})

	require.NoError(t, err)
	require.Contains(t, values, key)
	require.Zero(t, lookupCalls)
}

func TestClaimProjectionNodeDetectsDuplicateOwnerAndRefreshesTTL(t *testing.T) {
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		_ = redisClient.Close()
	})

	ctx := context.Background()
	owned, err := claimProjectionNode(ctx, "shared-node", "process-a")
	require.NoError(t, err)
	require.True(t, owned)
	owned, err = claimProjectionNode(ctx, "shared-node", "process-b")
	require.NoError(t, err)
	require.False(t, owned)
	owned, err = claimProjectionNode(ctx, "shared-node", "process-a")
	require.NoError(t, err)
	require.True(t, owned)
	require.Greater(t, redisClient.TTL(ctx, redisProjectionNodeClaimKey("shared-node")).Val(), time.Duration(0))
}

func TestMarkRedisBucketFlushedPreservesExpiryWhenHashIsMissing(t *testing.T) {
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		_ = redisClient.Close()
	})

	ctx := context.Background()
	require.NoError(t, markRedisBucketFlushed(ctx, "perf:v2:bucket:missing"))
	require.Equal(t, "1", redisClient.HGet(ctx, "perf:v2:bucket:missing", "flushed").Val())
	require.Greater(t, redisClient.TTL(ctx, "perf:v2:bucket:missing").Val(), time.Duration(0))
}

func TestRecoveredProjectionHealthClearsWhenNoBucketCanOwnTheGap(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousNodeName := common.NodeName
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	model.DB = db
	common.RDB = redisClient
	common.RedisEnabled = true
	common.NodeName = "restored-health-node"
	resetProjectionHealthForTest()
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		common.NodeName = previousNodeName
		resetProjectionHealthForTest()
		_ = redisClient.Close()
	})

	ctx := context.Background()
	require.NoError(t, redisClient.HSet(ctx, redisProjectionHealthKey(common.NodeName), map[string]interface{}{
		"degraded":       1,
		"degraded_until": time.Now().Add(time.Hour).Unix(),
	}).Err())
	require.NoError(t, restoreProjectionHealth())
	require.True(t, localProjectionDegraded())

	recovered, err := recoverRedisBucketsForNode()

	require.NoError(t, err)
	require.Zero(t, recovered)
	require.False(t, localProjectionDegraded())
}

func TestLoadProjectionHealthIgnoresExpiredDegradedMarker(t *testing.T) {
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
		_ = redisClient.Close()
	})

	ctx := context.Background()
	healthKey := redisProjectionHealthKey("stale-node")
	require.NoError(t, redisClient.HSet(ctx, healthKey, map[string]interface{}{
		"degraded":       1,
		"degraded_until": time.Now().Add(-time.Minute).Unix(),
	}).Err())
	require.NoError(t, redisClient.ZAdd(ctx, redisProjectionHealthIndex, &redis.Z{
		Score:  float64(time.Now().Add(time.Hour).Unix()),
		Member: healthKey,
	}).Err())

	degraded, err := loadProjectionHealth(ctx)

	require.NoError(t, err)
	require.False(t, degraded)
}

func TestQueryDetailedContextHonorsRequestCancellation(t *testing.T) {
	previousDB := model.DB
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	model.DB = db
	common.RDB = nil
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = QueryDetailedContext(ctx, QueryParams{Model: "alpha", Hours: 1})

	require.ErrorIs(t, err, context.Canceled)
}

func TestBuildCoverageClampsKnownBucketEndToRequestedEnd(t *testing.T) {
	coverage := buildCoverage(2_400, 2_500, map[bucketKey]counters{
		{model: "alpha", group: "prod", bucketTs: 2_480, bucketSeconds: 60}: {
			requestCount: 1,
		},
	}, 60)

	require.EqualValues(t, 2_500, coverage.BucketEndAt)
	require.EqualValues(t, 60, coverage.BucketSeconds)
	require.Equal(t, CoverageGranularityKnown, coverage.GranularityState)
	require.True(t, coverage.Approximate)
}

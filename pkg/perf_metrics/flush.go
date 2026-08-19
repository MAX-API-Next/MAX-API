package perfmetrics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/perf_metrics_setting"
)

func flushLoop() {
	for {
		interval := perf_metrics_setting.GetFlushIntervalMinutes()
		time.Sleep(time.Duration(interval) * time.Minute)
		flushOnce()
	}
}

func flushOnce() {
	setting := perf_metrics_setting.GetSetting()
	// Disabling collection stops new samples at Record, but buckets that
	// were already accepted still need to be drained and committed. Skipping
	// the flush here would strand those samples in memory and leave committed
	// Redis snapshots around until their TTL expires.
	flushCompletedBuckets()
	cleanupCommittedRedisBuckets()
	cleanupExpiredMetrics(setting.RetentionDays)
}

func flushCompletedBuckets() {
	now := time.Now().Unix()
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		bucketSeconds := k.bucketSeconds
		if bucketSeconds <= 0 {
			bucketSeconds = perf_metrics_setting.GetBucketSeconds()
		}
		if bucketSeconds <= 0 {
			bucketSeconds = 3600
		}
		if k.bucketTs+bucketSeconds > now {
			return true
		}

		bucket := value.(*atomicBucket)
		drained := bucket.closeAndDrain()
		hotBuckets.CompareAndDelete(k, bucket)
		if drained.requestCount == 0 {
			cleanupFlushedBucket(k)
			return true
		}

		_, err := persistBucket(k, drained)
		if err != nil {
			requeueCounters(k, drained)
			common.SysError(fmt.Sprintf("failed to flush perf metric bucket model=%s group=%s bucket=%d: %s", k.model, k.group, k.bucketTs, err.Error()))
			return true
		}

		committedBuckets.Store(k, struct{}{})
		cleanupFlushedBucket(k)
		return true
	})
}

func persistBucket(key bucketKey, value counters) (bool, error) {
	return model.CommitPerfMetricBucket(perfMetricReceiptKey(key), &model.PerfMetric{
		ModelName:      key.model,
		Group:          key.group,
		BucketTs:       key.bucketTs,
		RequestCount:   value.requestCount,
		SuccessCount:   value.successCount,
		TotalLatencyMs: value.totalLatencyMs,
		TtftSumMs:      value.ttftSumMs,
		TtftCount:      value.ttftCount,
		OutputTokens:   value.outputTokens,
		GenerationMs:   value.generationMs,
	})
}

func perfMetricReceiptKey(key bucketKey) string {
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", key.node, key.model, key.group, key.bucketTs, key.bucketSeconds)
	return fmt.Sprintf("%x", common.Sha256Raw([]byte(identity)))
}

func requeueCounters(key bucketKey, value counters) {
	for {
		actual, _ := hotBuckets.LoadOrStore(key, &atomicBucket{})
		if actual.(*atomicBucket).addCounters(value) {
			return
		}
		hotBuckets.CompareAndDelete(key, actual)
	}
}

func cleanupFlushedBucket(key bucketKey) {
	if err := deleteRedisBucket(key); err != nil {
		common.SysError(fmt.Sprintf("failed to cleanup flushed perf metric bucket model=%s group=%s bucket=%d: %s", key.model, key.group, key.bucketTs, err.Error()))
		return
	}
	committedBuckets.Delete(key)
	clearProjectionGap(key)
	removeClosedBucket(key)
}

func cleanupCommittedRedisBuckets() {
	committedBuckets.Range(func(key, _ any) bool {
		if err := deleteRedisBucket(key.(bucketKey)); err == nil {
			committedBuckets.Delete(key)
			clearProjectionGap(key.(bucketKey))
			removeClosedBucket(key.(bucketKey))
		}
		return true
	})
}

func removeClosedBucket(key bucketKey) {
	value, ok := hotBuckets.Load(key)
	if !ok {
		return
	}
	bucket := value.(*atomicBucket)
	bucket.mu.Lock()
	closed := bucket.closed
	bucket.mu.Unlock()
	if closed {
		hotBuckets.CompareAndDelete(key, bucket)
	}
}

func deleteRedisBucket(key bucketKey) error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	redisKey := redisBucketKey(key)
	if err := common.RDB.HSet(ctx, redisKey, "flushed", 1).Err(); err != nil {
		return err
	}
	pipe := common.RDB.TxPipeline()
	pipe.ZRem(ctx, redisActiveBucketIndex, redisKey)
	pipe.ZRem(ctx, redisNodeBucketIndex(key.node), redisKey)
	pipe.ZRem(ctx, redisModelBucketIndex(key.model), redisKey)
	pipe.Del(ctx, redisKey)
	_, err := pipe.Exec(ctx)
	return err
}

func cleanupExpiredMetrics(retentionDays int) {
	receiptCutoff := time.Now().Add(-redisBucketTTL - time.Hour).Unix()
	if err := model.DeletePerfMetricFlushReceiptsBefore(receiptCutoff); err != nil {
		common.SysError("failed to cleanup performance metric flush receipts: " + err.Error())
	}
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := model.DeletePerfMetricsBefore(cutoff); err != nil {
		common.SysError("failed to cleanup expired perf metrics: " + err.Error())
	}
}

func redisCounters(values map[string]string) counters {
	return counters{
		requestCount:   parseRedisInt(values["req"]),
		successCount:   parseRedisInt(values["ok"]),
		totalLatencyMs: parseRedisInt(values["lat"]),
		ttftSumMs:      parseRedisInt(values["ttft"]),
		ttftCount:      parseRedisInt(values["ttft_n"]),
		outputTokens:   parseRedisInt(values["out"]),
		generationMs:   parseRedisInt(values["gen_ms"]),
	}
}

func parseRedisInt(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

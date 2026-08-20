package perfmetrics

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/perf_metrics_setting"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var hotBuckets sync.Map

// committedBuckets suppresses a just-committed Redis snapshot in this
// process until cleanup succeeds. The durable flush receipt is the
// cross-restart guard; this local marker keeps the normal DB-success/
// Redis-cleanup window from being queried twice before cleanup finishes.
var committedBuckets sync.Map

// projectionGapBuckets tracks active local buckets whose latest absolute
// snapshot could not be published to Redis. The node health projection makes
// this quality loss visible to queries served by other instances after Redis
// recovers.
var projectionGapBuckets sync.Map
var projectionGapCount atomic.Int64
var projectionDegradedUntil atomic.Int64
var restoredProjectionDegraded atomic.Bool
var projectionNodeClaimConflict atomic.Bool
var projectionNodeClaimToken = uuid.NewString()

var getPerfMetricFlushReceiptKeys = model.GetPerfMetricFlushReceiptKeysContext

const (
	redisActiveBucketIndex     = "perf:v2:active"
	redisProjectionHealthIndex = "perf:v2:projection-health"
	redisBucketTTL             = 48 * time.Hour
	projectionHealthInterval   = 5 * time.Second
	projectionHealthyTTL       = 30 * time.Second
	projectionNodeClaimTTL     = 30 * time.Second
)

var redisBucketSnapshotScript = redis.NewScript(`
redis.call('HSET', KEYS[1],
  'model', ARGV[1],
  'group', ARGV[2],
  'node', ARGV[3],
  'bucket_ts', ARGV[4],
  'bucket_seconds', ARGV[5])
local names = {'req', 'ok', 'lat', 'ttft', 'ttft_n', 'out', 'gen_ms'}
for i = 1, #names do
  local incoming = tonumber(ARGV[i + 5]) or 0
  local current = tonumber(redis.call('HGET', KEYS[1], names[i]) or '0')
  if incoming > current then
    redis.call('HSET', KEYS[1], names[i], incoming)
  end
end
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[13]))
redis.call('ZADD', KEYS[2], tonumber(ARGV[14]), KEYS[1])
redis.call('ZADD', KEYS[3], tonumber(ARGV[14]), KEYS[1])
redis.call('ZADD', KEYS[4], tonumber(ARGV[14]), KEYS[1])
redis.call('EXPIRE', KEYS[2], tonumber(ARGV[15]))
redis.call('EXPIRE', KEYS[3], tonumber(ARGV[15]))
redis.call('EXPIRE', KEYS[4], tonumber(ARGV[15]))
return 1
`)

var redisNodeClaimScript = redis.NewScript(`
local owner = redis.call('GET', KEYS[1])
if not owner then
  redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2], 'NX')
  owner = redis.call('GET', KEYS[1])
end
if owner == ARGV[1] then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  return 1
end
return 0
`)

// seriesSchema is a stable client cache/schema marker. Do not change it when
// hiding fields or making response-only privacy hardening changes.
const seriesSchema = "dbcd0a3c01b55203"

func Init() {
	if common.RedisEnabled && common.RDB != nil {
		if currentNodeName() == "unnamed" {
			common.SysError("performance metrics Redis recovery requires a unique stable NODE_NAME per running instance")
		}
		maintainProjectionNodeClaim()
	}
	if err := restoreProjectionHealth(); err != nil {
		common.SysError("failed to restore performance projection health: " + err.Error())
	}
	if recovered, err := recoverRedisBucketsForNode(); err != nil {
		common.SysError("failed to recover active performance metric buckets from Redis: " + err.Error())
	} else if recovered > 0 {
		common.SysLog(fmt.Sprintf("recovered %d active performance metric buckets from Redis", recovered))
	}
	if err := publishProjectionHealth(); err != nil && common.RedisEnabled {
		common.SysError("failed to publish initial performance projection health: " + err.Error())
	}
	go flushLoop()
	go projectionHealthLoop()
}

func RecordRelaySample(info *relaycommon.RelayInfo, success bool, outputTokens int64) {
	if info == nil {
		return
	}
	now := time.Now()
	hasTtft := info.IsStream && info.HasSendResponse()
	ttftMs := int64(0)
	if hasTtft {
		ttftMs = info.FirstResponseTime.Sub(info.StartTime).Milliseconds()
	}
	latencyMs := now.Sub(info.StartTime).Milliseconds()
	generationMs := latencyMs
	if hasTtft {
		generationMs = now.Sub(info.FirstResponseTime).Milliseconds()
	}
	if generationMs <= 0 {
		generationMs = latencyMs
	}
	Record(Sample{
		Model:        info.OriginModelName,
		Group:        info.UsingGroup,
		LatencyMs:    latencyMs,
		TtftMs:       ttftMs,
		HasTtft:      hasTtft,
		Success:      success,
		OutputTokens: outputTokens,
		GenerationMs: generationMs,
	})
}

func Record(sample Sample) {
	setting := perf_metrics_setting.GetSetting()
	if !setting.Enabled || sample.Model == "" {
		return
	}
	if sample.Group == "" {
		sample.Group = "default"
	}
	if sample.LatencyMs < 0 {
		sample.LatencyMs = 0
	}

	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	now := time.Now().Unix()
	key := bucketKey{
		model:         sample.Model,
		group:         sample.Group,
		node:          currentNodeName(),
		bucketTs:      bucketStartFor(now, bucketSeconds),
		bucketSeconds: bucketSeconds,
	}
	projectToRedis := common.RedisEnabled && common.RDB != nil
	for {
		actual, _ := hotBuckets.LoadOrStore(key, &atomicBucket{})
		bucket := actual.(*atomicBucket)
		if !projectToRedis {
			if bucket.add(sample) {
				break
			}
			hotBuckets.CompareAndDelete(key, actual)
			continue
		}
		snapshot, version, added := bucket.addForProjection(sample)
		if added {
			success := recordRedis(key, snapshot)
			setProjectionGap(key, bucket.finishProjection(version, success))
			break
		}
		hotBuckets.CompareAndDelete(key, actual)
	}
}

func Query(params QueryParams) (QueryResult, error) {
	return QueryContext(context.Background(), params)
}

func QueryContext(ctx context.Context, params QueryParams) (QueryResult, error) {
	result, err := QueryDetailedContext(ctx, params)
	if err != nil {
		return QueryResult{}, err
	}
	return result.QueryResult, nil
}

func QueryDetailed(params QueryParams) (DetailedQueryResult, error) {
	return QueryDetailedContext(context.Background(), params)
}

func QueryDetailedContext(ctx context.Context, params QueryParams) (DetailedQueryResult, error) {
	setting := perf_metrics_setting.GetSetting()
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if params.Hours <= 0 {
		params.Hours = 24
	}
	if params.Hours > 24*30 {
		params.Hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(params.Hours)*3600

	merged := map[bucketKey]counters{}
	metricStartTs := bucketStartFor(startTs, bucketSeconds)
	rows, err := model.GetPerfMetricsContext(ctx, params.Model, params.Group, metricStartTs, endTs)
	if err != nil {
		return DetailedQueryResult{}, err
	}
	for _, row := range rows {
		if !matchesAllowedGroup(row.Group, params.AllowedGroups) {
			continue
		}
		mergeCounters(merged, bucketKey{
			model:         row.ModelName,
			group:         row.Group,
			bucketTs:      row.BucketTs,
			bucketSeconds: 0,
		}, counters{
			requestCount:   row.RequestCount,
			successCount:   row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs,
			ttftSumMs:      row.TtftSumMs,
			ttftCount:      row.TtftCount,
			outputTokens:   row.OutputTokens,
			generationMs:   row.GenerationMs,
		})
	}

	activeState := mergeActiveBuckets(ctx, merged, activeBucketFilter{
		model:         params.Model,
		group:         params.Group,
		allowedGroups: allowedGroupSet(params.AllowedGroups),
		startTs:       metricStartTs,
		endTs:         endTs,
	})

	queryResult := buildQueryResult(params.Model, merged)
	return DetailedQueryResult{
		QueryResult:     queryResult,
		Coverage:        buildCoverage(startTs, endTs, merged, bucketSeconds),
		CollectionState: resolveCollectionState(setting.Enabled, len(queryResult.Groups) > 0, activeState),
	}, nil
}

func QuerySummaryAll(hours int, groups []string) (SummaryAllResult, error) {
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	return QuerySummaryAllRange(startTs, endTs, groups)
}

// QuerySummaryAllRange aggregates bucketed model metrics whose bucket start is
// inside the caller-owned time range. Call QuerySummaryAllRangeDetailed when
// an operational client also needs the requested range, selected bucket
// coverage, bucket size, and collection state.
func QuerySummaryAllRange(startTs int64, endTs int64, groups []string) (SummaryAllResult, error) {
	result, err := QuerySummaryAllRangeDetailedContext(context.Background(), startTs, endTs, groups)
	if err != nil {
		return SummaryAllResult{}, err
	}
	return SummaryAllResult{Models: result.Models}, nil
}

func QuerySummaryAllRangeDetailed(startTs int64, endTs int64, groups []string) (DetailedSummaryAllResult, error) {
	return QuerySummaryAllRangeDetailedContext(context.Background(), startTs, endTs, groups)
}

func QuerySummaryAllRangeDetailedContext(ctx context.Context, startTs int64, endTs int64, groups []string) (DetailedSummaryAllResult, error) {
	setting := perf_metrics_setting.GetSetting()
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if endTs <= startTs {
		return DetailedSummaryAllResult{
			Coverage: QueryCoverage{
				RequestedStartAt: startTs,
				RequestedEndAt:   endTs,
				BucketSeconds:    bucketSeconds,
				GranularityState: CoverageGranularityUnknown,
			},
			CollectionState: resolveCollectionState(setting.Enabled, false, activeCollectionHealthy),
		}, nil
	}
	allowedGroups := allowedGroupSet(groups)

	metricStartTs := bucketStartFor(startTs, bucketSeconds)
	rows, err := model.GetPerfMetricsSummaryAllContext(ctx, metricStartTs, endTs, groups)
	if err != nil {
		return DetailedSummaryAllResult{}, err
	}

	totals := map[string]counters{}
	coverageBuckets := map[bucketKey]struct{}{}
	for _, row := range rows {
		totals[row.ModelName] = counters{
			requestCount:   row.RequestCount,
			successCount:   row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs,
			outputTokens:   row.OutputTokens,
			generationMs:   row.GenerationMs,
		}
		coverageBuckets[bucketKey{model: row.ModelName, bucketTs: row.MinBucketTs}] = struct{}{}
		coverageBuckets[bucketKey{model: row.ModelName, bucketTs: row.MaxBucketTs}] = struct{}{}
	}

	active := map[bucketKey]counters{}
	activeState := mergeActiveBuckets(ctx, active, activeBucketFilter{
		allowedGroups: allowedGroups,
		startTs:       metricStartTs,
		endTs:         endTs,
	})
	for k, snap := range active {
		if snap.requestCount == 0 {
			continue
		}
		cur := totals[k.model]
		cur.requestCount += snap.requestCount
		cur.successCount += snap.successCount
		cur.totalLatencyMs += snap.totalLatencyMs
		cur.outputTokens += snap.outputTokens
		cur.generationMs += snap.generationMs
		totals[k.model] = cur
		coverageBuckets[k] = struct{}{}
	}

	models := make([]ModelSummary, 0, len(totals))
	for name, total := range totals {
		if total.requestCount == 0 {
			continue
		}
		avgLatency := total.totalLatencyMs / total.requestCount
		successRate := float64(total.successCount) / float64(total.requestCount) * 100
		avgTps := 0.0
		if total.generationMs > 0 {
			avgTps = float64(total.outputTokens) / (float64(total.generationMs) / 1000.0)
		}
		models = append(models, ModelSummary{
			ModelName:    name,
			AvgLatencyMs: avgLatency,
			SuccessRate:  math.Round(successRate*100) / 100,
			AvgTps:       math.Round(avgTps*100) / 100,
			RequestCount: total.requestCount,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].RequestCount > models[j].RequestCount
	})

	coverage := buildCoverageFromBucketSet(startTs, endTs, coverageBuckets, bucketSeconds)
	return DetailedSummaryAllResult{
		Models:          models,
		Coverage:        coverage,
		CollectionState: resolveCollectionState(setting.Enabled, len(models) > 0, activeState),
	}, nil
}

func allowedGroupSet(groups []string) map[string]struct{} {
	if groups == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowed[group] = struct{}{}
	}
	return allowed
}

func bucketStartFor(ts int64, bucketSeconds int64) int64 {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	return ts - (ts % bucketSeconds)
}

func bucketStart(ts int64) int64 {
	return bucketStartFor(ts, perf_metrics_setting.GetBucketSeconds())
}

func matchesAllowedGroup(group string, groups []string) bool {
	if groups == nil {
		return true
	}
	for _, allowed := range groups {
		if group == allowed {
			return true
		}
	}
	return false
}

func mergeCounters(merged map[bucketKey]counters, key bucketKey, value counters) {
	if value.requestCount == 0 {
		return
	}
	current := merged[key]
	current.requestCount += value.requestCount
	current.successCount += value.successCount
	current.totalLatencyMs += value.totalLatencyMs
	current.ttftSumMs += value.ttftSumMs
	current.ttftCount += value.ttftCount
	current.outputTokens += value.outputTokens
	current.generationMs += value.generationMs
	merged[key] = current
}

func buildQueryResult(modelName string, merged map[bucketKey]counters) QueryResult {
	groupBuckets := map[string]map[int64]counters{}
	for key, value := range merged {
		if value.requestCount == 0 {
			continue
		}
		if _, ok := groupBuckets[key.group]; !ok {
			groupBuckets[key.group] = map[int64]counters{}
		}
		bucket := groupBuckets[key.group][key.bucketTs]
		bucket.requestCount += value.requestCount
		bucket.successCount += value.successCount
		bucket.totalLatencyMs += value.totalLatencyMs
		bucket.ttftSumMs += value.ttftSumMs
		bucket.ttftCount += value.ttftCount
		bucket.outputTokens += value.outputTokens
		bucket.generationMs += value.generationMs
		groupBuckets[key.group][key.bucketTs] = bucket
	}

	groups := make([]string, 0, len(groupBuckets))
	for group := range groupBuckets {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	results := make([]GroupResult, 0, len(groups))
	for _, group := range groups {
		buckets := groupBuckets[group]
		timestamps := make([]int64, 0, len(buckets))
		for ts := range buckets {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i] < timestamps[j]
		})

		total := counters{}
		series := make([]BucketPoint, 0, len(timestamps))
		for _, ts := range timestamps {
			value := buckets[ts]
			total.requestCount += value.requestCount
			total.successCount += value.successCount
			total.totalLatencyMs += value.totalLatencyMs
			total.ttftSumMs += value.ttftSumMs
			total.ttftCount += value.ttftCount
			total.outputTokens += value.outputTokens
			total.generationMs += value.generationMs
			series = append(series, bucketPoint(ts, value))
		}

		results = append(results, GroupResult{
			Group:        group,
			AvgTtftMs:    avg(total.ttftSumMs, total.ttftCount),
			AvgLatencyMs: avg(total.totalLatencyMs, total.requestCount),
			SuccessRate:  successRate(total),
			AvgTps:       avgTps(total),
			Series:       series,
		})
	}

	return QueryResult{
		ModelName:    modelName,
		SeriesSchema: seriesSchema,
		Summary:      buildAggregateResult(merged),
		Groups:       results,
	}
}

func buildAggregateResult(merged map[bucketKey]counters) AggregateResult {
	total := counters{}
	buckets := map[int64]counters{}
	for key, value := range merged {
		if value.requestCount == 0 {
			continue
		}
		total.requestCount += value.requestCount
		total.successCount += value.successCount
		total.totalLatencyMs += value.totalLatencyMs
		total.ttftSumMs += value.ttftSumMs
		total.ttftCount += value.ttftCount
		total.outputTokens += value.outputTokens
		total.generationMs += value.generationMs
		bucket := buckets[key.bucketTs]
		bucket.requestCount += value.requestCount
		bucket.successCount += value.successCount
		bucket.totalLatencyMs += value.totalLatencyMs
		bucket.ttftSumMs += value.ttftSumMs
		bucket.ttftCount += value.ttftCount
		bucket.outputTokens += value.outputTokens
		bucket.generationMs += value.generationMs
		buckets[key.bucketTs] = bucket
	}

	timestamps := make([]int64, 0, len(buckets))
	for ts := range buckets {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	series := make([]BucketPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		series = append(series, bucketPoint(ts, buckets[ts]))
	}

	return AggregateResult{
		AvgTtftMs:    avg(total.ttftSumMs, total.ttftCount),
		AvgLatencyMs: avg(total.totalLatencyMs, total.requestCount),
		SuccessRate:  successRate(total),
		AvgTps:       avgTps(total),
		Series:       series,
	}
}

func buildCoverage(startTs int64, endTs int64, merged map[bucketKey]counters, bucketSeconds int64) QueryCoverage {
	buckets := make(map[bucketKey]struct{}, len(merged))
	for key, value := range merged {
		if value.requestCount > 0 {
			buckets[key] = struct{}{}
		}
	}
	return buildCoverageFromBucketSet(startTs, endTs, buckets, bucketSeconds)
}

func buildCoverageFromBucketSet(startTs int64, endTs int64, buckets map[bucketKey]struct{}, bucketSeconds int64) QueryCoverage {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	coverage := QueryCoverage{
		RequestedStartAt: startTs,
		RequestedEndAt:   endTs,
		BucketSeconds:    bucketSeconds,
		GranularityState: CoverageGranularityUnknown,
	}
	if len(buckets) == 0 {
		return coverage
	}
	minTs, maxEndTs := int64(0), int64(0)
	hasBucket := false
	hasUnknown := false
	knownGranularities := map[int64]struct{}{}
	for key := range buckets {
		ts := key.bucketTs
		if !hasBucket || ts < minTs {
			minTs = ts
		}
		span := key.bucketSeconds
		if span <= 0 {
			hasUnknown = true
			span = bucketSeconds
		} else {
			knownGranularities[span] = struct{}{}
		}
		if end := ts + span; !hasBucket || end > maxEndTs {
			maxEndTs = end
		}
		hasBucket = true
	}
	switch {
	case hasUnknown && len(knownGranularities) == 0:
		coverage.GranularityState = CoverageGranularityUnknown
	case hasUnknown || len(knownGranularities) > 1:
		coverage.GranularityState = CoverageGranularityMixed
	default:
		coverage.GranularityState = CoverageGranularityKnown
		for known := range knownGranularities {
			coverage.BucketSeconds = known
		}
	}
	coverage.BucketStartAt = minTs
	coverage.BucketEndAt = min(maxEndTs, endTs)
	coverage.Approximate = minTs != startTs || coverage.BucketEndAt != endTs || coverage.GranularityState != CoverageGranularityKnown
	return coverage
}

type activeCollectionHealth int

const (
	activeCollectionHealthy activeCollectionHealth = iota
	activeCollectionFailed
)

func resolveCollectionState(enabled bool, hasSamples bool, activeState activeCollectionHealth) CollectionState {
	if !enabled {
		return CollectionStateDisabled
	}
	if activeState == activeCollectionFailed && !hasSamples {
		return CollectionStateQueryFailed
	}
	if activeState != activeCollectionHealthy && hasSamples {
		return CollectionStatePartial
	}
	if !hasSamples {
		return CollectionStateNoSamples
	}
	return CollectionStateAvailable
}

func bucketPoint(ts int64, value counters) BucketPoint {
	return BucketPoint{
		Ts:           ts,
		AvgTtftMs:    avg(value.ttftSumMs, value.ttftCount),
		AvgLatencyMs: avg(value.totalLatencyMs, value.requestCount),
		SuccessRate:  successRate(value),
		AvgTps:       avgTps(value),
	}
}

func avg(sum int64, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return sum / count
}

func successRate(value counters) float64 {
	if value.requestCount <= 0 {
		return 0
	}
	return float64(value.successCount) / float64(value.requestCount) * 100
}

func avgTps(value counters) float64 {
	if value.outputTokens <= 0 || value.generationMs <= 0 {
		return 0
	}
	return float64(value.outputTokens) / (float64(value.generationMs) / 1000)
}

type activeBucketFilter struct {
	model         string
	group         string
	allowedGroups map[string]struct{}
	startTs       int64
	endTs         int64
}

func mergeActiveBuckets(ctx context.Context, merged map[bucketKey]counters, filter activeBucketFilter) activeCollectionHealth {
	if !common.RedisEnabled || common.RDB == nil {
		mergeMemoryBuckets(merged, &hotBuckets, filter, false)
		return activeCollectionHealthy
	}

	redisBuckets, err := loadRedisActiveBuckets(ctx, filter)
	if err != nil {
		mergeMemoryBuckets(merged, &hotBuckets, filter, false)
		return activeCollectionFailed
	}
	for key, value := range redisBuckets {
		mergeCounters(merged, key, value)
	}
	// The local bucket mirrors the same node-specific Redis bucket. Merge its
	// monotonic counters by maximum rather than summing, so an ambiguous Redis
	// write acknowledgement cannot double count the same sample.
	mergeMemoryBuckets(merged, &hotBuckets, filter, true)
	if localProjectionDegraded() {
		return activeCollectionFailed
	}
	degraded, err := loadProjectionHealth(ctx)
	if err != nil || degraded {
		return activeCollectionFailed
	}
	return activeCollectionHealthy
}

func mergeMemoryBuckets(merged map[bucketKey]counters, buckets *sync.Map, filter activeBucketFilter, mergeReplica bool) {
	buckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if !matchesActiveBucket(k, filter) {
			return true
		}
		snapshot := value.(*atomicBucket).snapshot()
		if mergeReplica {
			mergeCounterReplica(merged, k, snapshot)
		} else {
			mergeCounters(merged, k, snapshot)
		}
		return true
	})
}

func mergeCounterReplica(merged map[bucketKey]counters, key bucketKey, value counters) {
	if value.requestCount == 0 {
		return
	}
	current := merged[key]
	current.requestCount = max(current.requestCount, value.requestCount)
	current.successCount = max(current.successCount, value.successCount)
	current.totalLatencyMs = max(current.totalLatencyMs, value.totalLatencyMs)
	current.ttftSumMs = max(current.ttftSumMs, value.ttftSumMs)
	current.ttftCount = max(current.ttftCount, value.ttftCount)
	current.outputTokens = max(current.outputTokens, value.outputTokens)
	current.generationMs = max(current.generationMs, value.generationMs)
	merged[key] = current
}

func matchesActiveBucket(key bucketKey, filter activeBucketFilter) bool {
	if key.bucketTs < filter.startTs || key.bucketTs > filter.endTs {
		return false
	}
	if filter.model != "" && key.model != filter.model {
		return false
	}
	if filter.group != "" && key.group != filter.group {
		return false
	}
	if filter.allowedGroups != nil {
		_, ok := filter.allowedGroups[key.group]
		return ok
	}
	return true
}

func recordRedis(key bucketKey, value counters) bool {
	if !common.RedisEnabled || common.RDB == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	redisKey := redisBucketKey(key)
	expiresAt := time.Now().Add(redisBucketTTL).Unix()
	if err := redisBucketSnapshotScript.Run(ctx, common.RDB, []string{
		redisKey,
		redisActiveBucketIndex,
		redisNodeBucketIndex(key.node),
		redisModelBucketIndex(key.model),
	},
		key.model,
		key.group,
		key.node,
		key.bucketTs,
		key.bucketSeconds,
		value.requestCount,
		value.successCount,
		value.totalLatencyMs,
		value.ttftSumMs,
		value.ttftCount,
		value.outputTokens,
		value.generationMs,
		int64(redisBucketTTL/time.Second),
		expiresAt,
		int64((redisBucketTTL+time.Hour)/time.Second),
	).Err(); err != nil {
		return false
	}
	return true
}

func loadRedisActiveBuckets(parent context.Context, filter activeBucketFilter) (map[bucketKey]counters, error) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	index := redisActiveBucketIndex
	if filter.model != "" {
		index = redisModelBucketIndex(filter.model)
	}
	keys, err := activeRedisKeys(ctx, index)
	if err != nil {
		return nil, err
	}
	values, err := redisBucketValues(ctx, keys)
	if err != nil {
		return nil, err
	}
	type decodedBucket struct {
		key        bucketKey
		value      counters
		receiptKey string
	}
	decoded := make([]decodedBucket, 0, len(values))
	receiptKeys := make([]string, 0, len(values))
	now := time.Now().Unix()
	for _, fields := range values {
		key, value, ok := decodeRedisBucket(fields)
		if !ok || !matchesActiveBucket(key, filter) {
			continue
		}
		if _, suppressed := committedBuckets.Load(key); suppressed {
			continue
		}
		bucket := decodedBucket{key: key, value: value}
		if key.bucketSeconds > 0 && key.bucketTs+key.bucketSeconds <= now {
			bucket.receiptKey = perfMetricReceiptKey(key)
			receiptKeys = append(receiptKeys, bucket.receiptKey)
		}
		decoded = append(decoded, bucket)
	}
	receipts := map[string]struct{}{}
	if len(receiptKeys) > 0 {
		var err error
		receipts, err = getPerfMetricFlushReceiptKeys(ctx, receiptKeys)
		if err != nil {
			return nil, err
		}
	}
	merged := make(map[bucketKey]counters, len(decoded))
	for _, bucket := range decoded {
		if bucket.receiptKey != "" {
			if _, committed := receipts[bucket.receiptKey]; committed {
				continue
			}
		}
		mergeCounters(merged, bucket.key, bucket.value)
	}
	return merged, nil
}

func redisBucketKey(key bucketKey) string {
	identity := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%s\x00%s\x00%d",
		key.node,
		key.model,
		key.group,
		key.bucketSeconds,
	)))
	return fmt.Sprintf("perf:v2:bucket:%x:%d", identity[:12], key.bucketTs)
}

func redisNodeBucketIndex(node string) string {
	identity := sha256.Sum256([]byte(node))
	return fmt.Sprintf("perf:v2:node:%x", identity[:12])
}

func redisModelBucketIndex(modelName string) string {
	identity := sha256.Sum256([]byte(modelName))
	return fmt.Sprintf("perf:v2:model:%x", identity[:12])
}

func redisProjectionHealthKey(node string) string {
	identity := sha256.Sum256([]byte(node))
	return fmt.Sprintf("perf:v2:health:%x", identity[:12])
}

func redisProjectionNodeClaimKey(node string) string {
	identity := sha256.Sum256([]byte(node))
	return fmt.Sprintf("perf:v2:node-claim:%x", identity[:12])
}

func setProjectionGap(key bucketKey, incomplete bool) {
	if incomplete {
		if _, loaded := projectionGapBuckets.LoadOrStore(key, struct{}{}); !loaded {
			projectionGapCount.Add(1)
		}
		degradedUntil := time.Now().Add(redisBucketTTL).Unix()
		for {
			current := projectionDegradedUntil.Load()
			if current >= degradedUntil || projectionDegradedUntil.CompareAndSwap(current, degradedUntil) {
				break
			}
		}
		return
	}
	clearProjectionGap(key)
}

func clearProjectionGap(key bucketKey) {
	if _, loaded := projectionGapBuckets.LoadAndDelete(key); loaded {
		if projectionGapCount.Add(-1) == 0 {
			projectionDegradedUntil.Store(0)
		}
	}
}

func localProjectionDegraded() bool {
	return projectionGapCount.Load() > 0 || projectionDegradedUntil.Load() > time.Now().Unix()
}

func projectionHealthLoop() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ticker := time.NewTicker(projectionHealthInterval)
	defer ticker.Stop()
	for range ticker.C {
		maintainProjectionNodeClaim()
		if err := publishProjectionHealth(); err != nil && common.RedisEnabled {
			common.SysError("failed to publish performance projection health: " + err.Error())
		}
	}
}

func maintainProjectionNodeClaim() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	owned, err := claimProjectionNode(ctx, currentNodeName(), projectionNodeClaimToken)
	if err != nil {
		common.SysError("failed to refresh performance metrics NODE_NAME claim: " + err.Error())
		return
	}
	if !owned {
		if projectionNodeClaimConflict.CompareAndSwap(false, true) {
			common.SysError(fmt.Sprintf("duplicate NODE_NAME %q detected for performance metrics; use a unique stable NODE_NAME per running instance", currentNodeName()))
		}
		return
	}
	if projectionNodeClaimConflict.CompareAndSwap(true, false) {
		common.SysLog(fmt.Sprintf("performance metrics NODE_NAME claim recovered for %q", currentNodeName()))
	}
}

func claimProjectionNode(ctx context.Context, node string, token string) (bool, error) {
	if node == "" || token == "" {
		return false, fmt.Errorf("node name and claim token are required")
	}
	result, err := redisNodeClaimScript.Run(
		ctx,
		common.RDB,
		[]string{redisProjectionNodeClaimKey(node)},
		token,
		int64(projectionNodeClaimTTL/time.Second),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func publishProjectionHealth() error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	degraded := localProjectionDegraded()
	ttl := projectionHealthyTTL
	if degraded {
		ttl = redisBucketTTL
	}
	expiresAt := time.Now().Add(ttl).Unix()
	key := redisProjectionHealthKey(currentNodeName())
	pipe := common.RDB.TxPipeline()
	pipe.HSet(ctx, key, map[string]interface{}{
		"node":           currentNodeName(),
		"degraded":       boolToRedisInt(degraded),
		"degraded_until": projectionDegradedUntil.Load(),
		"updated_at":     time.Now().Unix(),
	})
	pipe.Expire(ctx, key, ttl)
	pipe.ZAdd(ctx, redisProjectionHealthIndex, &redis.Z{Score: float64(expiresAt), Member: key})
	pipe.Expire(ctx, redisProjectionHealthIndex, redisBucketTTL+time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

func restoreProjectionHealth() error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fields, err := common.RDB.HGetAll(ctx, redisProjectionHealthKey(currentNodeName())).Result()
	if err != nil {
		return err
	}
	degradedUntil := parseRedisInt(fields["degraded_until"])
	if fields["degraded"] == "1" && degradedUntil > time.Now().Unix() {
		projectionDegradedUntil.Store(degradedUntil)
		restoredProjectionDegraded.Store(true)
	}
	return nil
}

func resetProjectionHealthForTest() {
	projectionGapBuckets = sync.Map{}
	projectionGapCount.Store(0)
	projectionDegradedUntil.Store(0)
	restoredProjectionDegraded.Store(false)
	projectionNodeClaimConflict.Store(false)
}

func loadProjectionHealth(parent context.Context) (bool, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	keys, err := activeRedisKeys(ctx, redisProjectionHealthIndex)
	if err != nil {
		return false, err
	}
	values, err := redisBucketValues(ctx, keys)
	if err != nil {
		return false, err
	}
	now := time.Now().Unix()
	for _, fields := range values {
		if fields["degraded"] == "1" && parseRedisInt(fields["degraded_until"]) > now {
			return true, nil
		}
	}
	return false, nil
}

func boolToRedisInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func currentNodeName() string {
	if common.NodeName != "" {
		return common.NodeName
	}
	return "unnamed"
}

func activeRedisKeys(ctx context.Context, index string) ([]string, error) {
	now := time.Now().Unix()
	pipe := common.RDB.TxPipeline()
	pipe.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(now, 10))
	keysCmd := pipe.ZRangeByScore(ctx, index, &redis.ZRangeBy{Min: strconv.FormatInt(now+1, 10), Max: "+inf"})
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return keysCmd.Val(), nil
}

func redisBucketValues(ctx context.Context, keys []string) ([]map[string]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := common.RDB.Pipeline()
	commands := make([]*redis.StringStringMapCmd, 0, len(keys))
	for _, key := range keys {
		commands = append(commands, pipe.HGetAll(ctx, key))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	values := make([]map[string]string, 0, len(commands))
	for _, command := range commands {
		if fields := command.Val(); len(fields) > 0 && fields["flushed"] != "1" {
			values = append(values, fields)
		}
	}
	return values, nil
}

func decodeRedisBucket(fields map[string]string) (bucketKey, counters, bool) {
	key := bucketKey{
		model:         fields["model"],
		group:         fields["group"],
		node:          fields["node"],
		bucketTs:      parseRedisInt(fields["bucket_ts"]),
		bucketSeconds: parseRedisInt(fields["bucket_seconds"]),
	}
	value := redisCounters(fields)
	return key, value, key.model != "" && key.group != "" && key.node != "" && key.bucketTs > 0 && value.requestCount > 0
}

func recoverRedisBucketsForNode() (int, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return 0, nil
	}
	node := currentNodeName()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	keys, err := activeRedisKeys(ctx, redisNodeBucketIndex(node))
	if err != nil {
		return 0, err
	}
	values, err := redisBucketValues(ctx, keys)
	if err != nil {
		return 0, err
	}
	type recoveredBucket struct {
		key   bucketKey
		value counters
	}
	candidates := make([]recoveredBucket, 0, len(values))
	receiptKeys := make([]string, 0, len(values))
	for _, fields := range values {
		key, value, ok := decodeRedisBucket(fields)
		if !ok || key.node != node {
			continue
		}
		candidates = append(candidates, recoveredBucket{key: key, value: value})
		receiptKeys = append(receiptKeys, perfMetricReceiptKey(key))
	}
	receipts, err := getPerfMetricFlushReceiptKeys(ctx, receiptKeys)
	if err != nil {
		return 0, err
	}
	restoreGapOwnership := restoredProjectionDegraded.Swap(false)
	recovered := 0
	for _, candidate := range candidates {
		if _, committed := receipts[perfMetricReceiptKey(candidate.key)]; committed {
			if err := deleteRedisBucket(candidate.key); err != nil {
				committedBuckets.Store(candidate.key, struct{}{})
			}
			continue
		}
		bucket := &atomicBucket{}
		if !bucket.addCounters(candidate.value) {
			common.SysError(fmt.Sprintf("failed to restore performance metric bucket model=%s group=%s bucket=%d", candidate.key.model, candidate.key.group, candidate.key.bucketTs))
			continue
		}
		hotBuckets.Store(candidate.key, bucket)
		if restoreGapOwnership {
			setProjectionGap(candidate.key, true)
		}
		recovered++
	}
	if restoreGapOwnership && recovered == 0 {
		projectionDegradedUntil.Store(0)
	}
	return recovered, nil
}

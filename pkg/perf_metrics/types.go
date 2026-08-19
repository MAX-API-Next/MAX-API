package perfmetrics

import (
	"sync"
	"sync/atomic"
)

type Store interface {
	Record(sample Sample)
	Query(params QueryParams) (QueryResult, error)
}

type Sample struct {
	Model        string
	Group        string
	LatencyMs    int64
	TtftMs       int64
	HasTtft      bool
	Success      bool
	OutputTokens int64
	GenerationMs int64
}

type QueryParams struct {
	Model         string
	Group         string
	Hours         int
	AllowedGroups []string
}

type BucketPoint struct {
	Ts           int64   `json:"ts"`
	AvgTtftMs    int64   `json:"avg_ttft_ms"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
}

// AggregateResult is the model-level weighted aggregate. It is derived from
// the stored counters, not by averaging already-aggregated group values.
type AggregateResult struct {
	AvgTtftMs    int64         `json:"avg_ttft_ms"`
	AvgLatencyMs int64         `json:"avg_latency_ms"`
	SuccessRate  float64       `json:"success_rate"`
	AvgTps       float64       `json:"avg_tps"`
	Series       []BucketPoint `json:"series"`
}

type QueryCoverage struct {
	RequestedStartAt int64               `json:"requested_start_at"`
	RequestedEndAt   int64               `json:"requested_end_at"`
	BucketStartAt    int64               `json:"bucket_start_at"`
	BucketEndAt      int64               `json:"bucket_end_at"`
	BucketSeconds    int64               `json:"bucket_seconds"`
	GranularityState CoverageGranularity `json:"granularity_state"`
	Approximate      bool                `json:"approximate"`
}

type CoverageGranularity string

const (
	CoverageGranularityKnown   CoverageGranularity = "known"
	CoverageGranularityUnknown CoverageGranularity = "unknown"
	CoverageGranularityMixed   CoverageGranularity = "mixed"
)

type CollectionState string

const (
	CollectionStateAvailable   CollectionState = "available"
	CollectionStatePartial     CollectionState = "partial"
	CollectionStateDisabled    CollectionState = "collection_disabled"
	CollectionStateNoSamples   CollectionState = "no_samples"
	CollectionStateQueryFailed CollectionState = "query_failed"
)

type GroupResult struct {
	Group        string        `json:"group"`
	AvgTtftMs    int64         `json:"avg_ttft_ms"`
	AvgLatencyMs int64         `json:"avg_latency_ms"`
	SuccessRate  float64       `json:"success_rate"`
	AvgTps       float64       `json:"avg_tps"`
	Series       []BucketPoint `json:"series"`
}

type QueryResult struct {
	ModelName    string          `json:"model_name"`
	SeriesSchema string          `json:"series_schema"`
	Summary      AggregateResult `json:"summary"`
	Groups       []GroupResult   `json:"groups"`
}

// DetailedQueryResult is used by administrator-facing Smart Operations
// endpoints. The public pricing projection remains QueryResult-compatible;
// these fields make bucket coverage and collection state explicit for
// operational decisions.
type DetailedQueryResult struct {
	QueryResult
	Coverage        QueryCoverage   `json:"coverage"`
	CollectionState CollectionState `json:"collection_state"`
}

type ModelSummary struct {
	ModelName    string  `json:"model_name"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
	RequestCount int64   `json:"-"`
}

type SummaryAllResult struct {
	Models []ModelSummary `json:"models"`
}

type DetailedSummaryAllResult struct {
	Models          []ModelSummary  `json:"models"`
	Coverage        QueryCoverage   `json:"coverage"`
	CollectionState CollectionState `json:"collection_state"`
}

type bucketKey struct {
	model         string
	group         string
	node          string
	bucketTs      int64
	bucketSeconds int64
}

type counters struct {
	requestCount   int64
	successCount   int64
	totalLatencyMs int64
	ttftSumMs      int64
	ttftCount      int64
	outputTokens   int64
	generationMs   int64
}

type atomicBucket struct {
	mu             sync.Mutex
	projectionWG   sync.WaitGroup
	closed         bool
	version        uint64
	projected      uint64
	requestCount   atomic.Int64
	successCount   atomic.Int64
	totalLatencyMs atomic.Int64
	ttftSumMs      atomic.Int64
	ttftCount      atomic.Int64
	outputTokens   atomic.Int64
	generationMs   atomic.Int64
}

func (b *atomicBucket) add(sample Sample) bool {
	_, _, added := b.addSample(sample, false)
	return added
}

func (b *atomicBucket) addForProjection(sample Sample) (counters, uint64, bool) {
	return b.addSample(sample, true)
}

func (b *atomicBucket) addSample(sample Sample, project bool) (counters, uint64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return counters{}, 0, false
	}
	b.requestCount.Add(1)
	if sample.Success {
		b.successCount.Add(1)
	}
	if sample.LatencyMs > 0 {
		b.totalLatencyMs.Add(sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		b.ttftSumMs.Add(sample.TtftMs)
		b.ttftCount.Add(1)
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		b.outputTokens.Add(sample.OutputTokens)
		b.generationMs.Add(sample.GenerationMs)
	}
	if !project {
		return counters{}, 0, true
	}
	b.version++
	b.projectionWG.Add(1)
	return b.snapshotLocked(), b.version, true
}

func (b *atomicBucket) snapshot() counters {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshotLocked()
}

func (b *atomicBucket) snapshotLocked() counters {
	return counters{
		requestCount:   b.requestCount.Load(),
		successCount:   b.successCount.Load(),
		totalLatencyMs: b.totalLatencyMs.Load(),
		ttftSumMs:      b.ttftSumMs.Load(),
		ttftCount:      b.ttftCount.Load(),
		outputTokens:   b.outputTokens.Load(),
		generationMs:   b.generationMs.Load(),
	}
}

func (b *atomicBucket) finishProjection(version uint64, success bool) bool {
	b.mu.Lock()
	if success && version > b.projected {
		b.projected = version
	}
	incomplete := b.projected < b.version
	b.mu.Unlock()
	b.projectionWG.Done()
	return incomplete
}

func (b *atomicBucket) closeAndDrain() counters {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return counters{}
	}
	b.closed = true
	b.mu.Unlock()

	// Every sample that entered this bucket has either completed its shared
	// Redis projection or recorded a projection gap before the durable flush
	// proceeds. This closes the late-write-after-cleanup race.
	b.projectionWG.Wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	return counters{
		requestCount:   b.requestCount.Swap(0),
		successCount:   b.successCount.Swap(0),
		totalLatencyMs: b.totalLatencyMs.Swap(0),
		ttftSumMs:      b.ttftSumMs.Swap(0),
		ttftCount:      b.ttftCount.Swap(0),
		outputTokens:   b.outputTokens.Swap(0),
		generationMs:   b.generationMs.Swap(0),
	}
}

func (b *atomicBucket) addCounters(c counters) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	if c.requestCount != 0 {
		b.requestCount.Add(c.requestCount)
	}
	if c.successCount != 0 {
		b.successCount.Add(c.successCount)
	}
	if c.totalLatencyMs != 0 {
		b.totalLatencyMs.Add(c.totalLatencyMs)
	}
	if c.ttftSumMs != 0 {
		b.ttftSumMs.Add(c.ttftSumMs)
	}
	if c.ttftCount != 0 {
		b.ttftCount.Add(c.ttftCount)
	}
	if c.outputTokens != 0 {
		b.outputTokens.Add(c.outputTokens)
	}
	if c.generationMs != 0 {
		b.generationMs.Add(c.generationMs)
	}
	return true
}

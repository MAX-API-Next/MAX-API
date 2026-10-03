package common

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	channelHealthRuntimeKeyPrefix     = "maxapi:channel-timeout:v1:observation:"
	channelHealthRuntimeTTL           = 24 * time.Hour
	channelHealthObservationTimeout   = 100 * time.Millisecond
	channelHealthObservationQueueSize = 256
)

var channelHealthObservationQueue = make(chan ChannelHealthObservation, channelHealthObservationQueueSize)

var channelHealthObservationWorkerOnce sync.Once

func startChannelHealthObservationWorker() {
	channelHealthObservationWorkerOnce.Do(func() {
		go func() {
			for observation := range channelHealthObservationQueue {
				// The worker is deliberately best-effort. A slow or unavailable
				// Redis instance must not remain on a relay response path.
				_ = RecordChannelHealthObservation(observation)
			}
		}()
	})
}

// EnqueueChannelHealthObservation schedules a best-effort diagnostic write.
// It never waits for Redis and drops the observation when the bounded queue is
// full, preserving the upstream response path as fail-open.
func EnqueueChannelHealthObservation(observation ChannelHealthObservation) {
	if !observation.PolicyEnabled || observation.ChannelID <= 0 || observation.Event == "" || !RedisEnabled || RDB == nil {
		return
	}
	startChannelHealthObservationWorker()
	select {
	case channelHealthObservationQueue <- observation:
	default:
		// A full diagnostics queue is equivalent to an unavailable optional
		// projection. The request path remains unaffected.
	}
}

// ChannelHealthObservation is the minimal, non-sensitive runtime fact emitted
// by a selected upstream channel attempt. It deliberately carries no request
// or response body and no provider credential.
type ChannelHealthObservation struct {
	ChannelID        int
	Event            string
	PolicyEnabled    bool
	ObservedAt       time.Time
	AttemptLatencyMS int64
	Stream           bool
	RetryIndex       int
}

// ChannelHealthRuntimeTransition is a small process-local bridge from the
// relay/runtime package to Smart Operations. The runtime state itself remains
// in Redis; this hook only lets the existing notification projector react
// immediately without introducing a package cycle.
type ChannelHealthRuntimeTransition struct {
	ChannelID       int
	Transition      string
	Penalty         int64
	PenaltyUntil    time.Time
	RuntimeDisabled bool
	DisabledUntil   time.Time
	ObservedAt      time.Time
}

var channelHealthRuntimeTransitionObserver struct {
	sync.RWMutex
	fn func(ChannelHealthRuntimeTransition)
}

func SetChannelHealthRuntimeTransitionObserver(observer func(ChannelHealthRuntimeTransition)) {
	channelHealthRuntimeTransitionObserver.Lock()
	channelHealthRuntimeTransitionObserver.fn = observer
	channelHealthRuntimeTransitionObserver.Unlock()
}

func PublishChannelHealthRuntimeTransition(transition ChannelHealthRuntimeTransition) {
	channelHealthRuntimeTransitionObserver.RLock()
	observer := channelHealthRuntimeTransitionObserver.fn
	channelHealthRuntimeTransitionObserver.RUnlock()
	if observer != nil {
		observer(transition)
	}
}

func isChannelHealthTimeoutEvent(event string) bool {
	switch event {
	case "transport_timeout", "stream_idle_timeout", "request_timeout",
		"streaming_first_result_timeout", "non_streaming_response_timeout":
		return true
	default:
		return false
	}
}

func channelHealthRuntimeKey(channelID int) string {
	return fmt.Sprintf("%s%d", channelHealthRuntimeKeyPrefix, channelID)
}

// RecordChannelHealthObservation writes a bounded runtime projection. Redis
// failure is returned to the caller, which must treat it as diagnostic-only
// and never let it change relay behavior.
func RecordChannelHealthObservation(observation ChannelHealthObservation) error {
	if observation.ChannelID <= 0 || observation.Event == "" {
		return errors.New("channel health observation requires channel id and event")
	}
	if !RedisEnabled || RDB == nil {
		return nil
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now()
	}
	if observation.AttemptLatencyMS < 0 {
		observation.AttemptLatencyMS = 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelHealthObservationTimeout)
	defer cancel()
	key := channelHealthRuntimeKey(observation.ChannelID)
	fields := map[string]interface{}{
		"channel_id":           strconv.Itoa(observation.ChannelID),
		"last_event":           observation.Event,
		"last_observed_at":     observation.ObservedAt.UTC().Format(time.RFC3339Nano),
		"last_attempt_latency": strconv.FormatInt(observation.AttemptLatencyMS, 10),
		"last_stream":          strconv.FormatBool(observation.Stream),
		"last_retry_index":     strconv.Itoa(observation.RetryIndex),
	}

	pipe := RDB.TxPipeline()
	pipe.HIncrBy(ctx, key, "observations", 1)
	if isChannelHealthTimeoutEvent(observation.Event) {
		pipe.HIncrBy(ctx, key, "timeouts", 1)
	}
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, channelHealthRuntimeTTL)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("record channel health observation: %w", err)
	}
	return nil
}

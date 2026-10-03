package common

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestRecordChannelHealthObservationStoresBoundedRuntimeProjection(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := RDB, RedisEnabled
	RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	RedisEnabled = true
	t.Cleanup(func() {
		_ = RDB.Close()
		RDB, RedisEnabled = oldRDB, oldEnabled
	})

	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	require.NoError(t, RecordChannelHealthObservation(ChannelHealthObservation{
		ChannelID:        936,
		Event:            "first_response",
		ObservedAt:       now,
		AttemptLatencyMS: 1200,
		Stream:           true,
		RetryIndex:       1,
	}))
	require.NoError(t, RecordChannelHealthObservation(ChannelHealthObservation{
		ChannelID:        936,
		Event:            "stream_idle_timeout",
		ObservedAt:       now.Add(time.Second),
		AttemptLatencyMS: 2200,
		Stream:           true,
		RetryIndex:       1,
	}))

	fields, err := RDB.HGetAll(context.Background(), channelHealthRuntimeKey(936)).Result()
	require.NoError(t, err)
	require.Equal(t, "2", fields["observations"])
	require.Equal(t, "1", fields["timeouts"])
	require.Equal(t, "stream_idle_timeout", fields["last_event"])
	require.Equal(t, "2200", fields["last_attempt_latency"])
	require.Equal(t, "true", fields["last_stream"])
	require.Equal(t, "1", fields["last_retry_index"])
	require.Equal(t, "936", fields["channel_id"])
}

func TestRecordChannelHealthObservationCountsCanonicalTimeoutKinds(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := RDB, RedisEnabled
	RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	RedisEnabled = true
	t.Cleanup(func() {
		_ = RDB.Close()
		RDB, RedisEnabled = oldRDB, oldEnabled
	})

	for _, event := range []string{"streaming_first_result_timeout", "non_streaming_response_timeout"} {
		require.NoError(t, RecordChannelHealthObservation(ChannelHealthObservation{
			ChannelID: 936,
			Event:     event,
		}))
	}
	fields, err := RDB.HGetAll(context.Background(), channelHealthRuntimeKey(936)).Result()
	require.NoError(t, err)
	require.Equal(t, "2", fields["timeouts"])
}

func TestRecordChannelHealthObservationSkipsRedisWhenDisabled(t *testing.T) {
	oldRDB, oldEnabled := RDB, RedisEnabled
	RDB = nil
	RedisEnabled = false
	t.Cleanup(func() {
		RDB, RedisEnabled = oldRDB, oldEnabled
	})

	require.NoError(t, RecordChannelHealthObservation(ChannelHealthObservation{
		ChannelID: 1,
		Event:     "first_response",
	}))
}

func TestEnqueueChannelHealthObservationSkipsDisabledPolicy(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := RDB, RedisEnabled
	RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	RedisEnabled = true
	t.Cleanup(func() {
		_ = RDB.Close()
		RDB, RedisEnabled = oldRDB, oldEnabled
	})

	EnqueueChannelHealthObservation(ChannelHealthObservation{
		ChannelID: 937,
		Event:     "first_response",
	})
	time.Sleep(50 * time.Millisecond)
	count, err := RDB.Exists(context.Background(), channelHealthRuntimeKey(937)).Result()
	require.NoError(t, err)
	require.Zero(t, count)
}

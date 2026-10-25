package channelhealth

import (
	"context"
	"io"
	"maps"
	"net"
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

type displayReadHook struct {
	keys []string
}

func (hook *displayReadHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	hook.keys = append(hook.keys, cmd.Name())
	return ctx, nil
}

func (hook *displayReadHook) AfterProcess(context.Context, redis.Cmder) error { return nil }

func (hook *displayReadHook) BeforeProcessPipeline(ctx context.Context, commands []redis.Cmder) (context.Context, error) {
	for _, command := range commands {
		hook.keys = append(hook.keys, command.Args()[1].(string))
	}
	return ctx, nil
}

func (hook *displayReadHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func TestLoadRuntimeStatesStrictIsReadOnlyAndKeepsRoutingCompatibility(t *testing.T) {
	withRuntimeRedis(t)
	ctx := context.Background()
	now := time.Now().UTC()
	state := emptyRuntimeState(1)
	state.Penalty = 1
	state.PenaltyUntil = now.Add(time.Minute)
	state.LastObservedAt = now
	require.NoError(t, maxcommon.RDB.HSet(ctx, RuntimeStateKey(1), encodeRuntimeState(state)).Err())
	require.NoError(t, maxcommon.RDB.HSet(ctx, RuntimeStateKey(3), map[string]interface{}{
		"penalty": "broken", "runtime_disabled": "false",
	}).Err())
	before, err := maxcommon.RDB.HGetAll(ctx, RuntimeStateKey(1)).Result()
	require.NoError(t, err)
	hook := &displayReadHook{}
	maxcommon.RDB.AddHook(hook)

	results, err := LoadRuntimeStatesStrict(ctx, []int{1, 2, 3, 1, 0, -1})
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.ElementsMatch(t, []string{RuntimeStateKey(1), RuntimeStateKey(2), RuntimeStateKey(3)}, hook.keys)
	require.NoError(t, results[1].Err)
	require.Equal(t, int64(1), results[1].State.Penalty)
	require.Equal(t, state.PenaltyUntil, results[1].State.PenaltyUntil)
	require.NoError(t, results[2].Err)
	require.Zero(t, results[2].State.Penalty)
	require.ErrorIs(t, results[3].Err, ErrInvalidRuntimeState)

	// The routing loader intentionally retains its pre-existing tolerant behavior.
	routing, err := LoadRuntimeStates(ctx, []int{3})
	require.NoError(t, err)
	require.Zero(t, routing[3].Penalty)
	after, err := maxcommon.RDB.HGetAll(ctx, RuntimeStateKey(1)).Result()
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.EqualValues(t, 0, maxcommon.RDB.Exists(ctx, RuntimeStateKey(2)).Val())
}

func TestLoadRuntimeStatesStrictValidatesCorruptionPerChannel(t *testing.T) {
	withRuntimeRedis(t)
	valid := map[string]string{"penalty": "1", "runtime_disabled": "false"}
	for name, change := range map[string]func(map[string]string){
		"missing penalty": func(fields map[string]string) { delete(fields, "penalty") },
		"missing flag":    func(fields map[string]string) { delete(fields, "runtime_disabled") },
		"invalid flag":    func(fields map[string]string) { fields["runtime_disabled"] = "TRUE" },
		"negative":        func(fields map[string]string) { fields["penalty"] = "-1" },
		"overflow":        func(fields map[string]string) { fields["penalty"] = "9223372036854775808" },
		"bad deadline":    func(fields map[string]string) { fields["penalty_until"] = "tomorrow" },
		"bad observation": func(fields map[string]string) { fields["last_observed_at"] = "broken" },
		"bad counter":     func(fields map[string]string) { fields["timeout_count_streaming"] = "broken" },
		"wrong channel":   func(fields map[string]string) { fields["channel_id"] = "99" },
		"bad mode":        func(fields map[string]string) { fields["last_timeout_mode"] = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			fields := maps.Clone(valid)
			change(fields)
			require.NoError(t, maxcommon.RDB.Del(context.Background(), RuntimeStateKey(1)).Err())
			require.NoError(t, maxcommon.RDB.HSet(context.Background(), RuntimeStateKey(1), fields).Err())
			results, err := LoadRuntimeStatesStrict(context.Background(), []int{1, 2})
			require.NoError(t, err)
			require.ErrorIs(t, results[1].Err, ErrInvalidRuntimeState)
			require.NoError(t, results[2].Err)
		})
	}
	// Legacy states may lack fields added later; unknown fields are forward compatible.
	valid["future_field"] = "opaque"
	require.NoError(t, maxcommon.RDB.Del(context.Background(), RuntimeStateKey(1)).Err())
	require.NoError(t, maxcommon.RDB.HSet(context.Background(), RuntimeStateKey(1), valid).Err())
	results, err := LoadRuntimeStatesStrict(context.Background(), []int{1})
	require.NoError(t, err)
	require.NoError(t, results[1].Err)
}

func TestLoadRuntimeStatesStrictDoesNotReturnPartialBatchOnRedisError(t *testing.T) {
	withRuntimeRedis(t)
	require.NoError(t, maxcommon.RDB.Set(context.Background(), RuntimeStateKey(2), "wrong-type", 0).Err())
	results, err := LoadRuntimeStatesStrict(context.Background(), []int{1, 2})
	require.Error(t, err)
	require.Empty(t, results)
}

func TestLoadRuntimeStatesStrictHonorsDeadline(t *testing.T) {
	oldClient, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	go func() { _, _ = io.Copy(io.Discard, serverConn) }()
	client := redis.NewClient(&redis.Options{
		Dialer:      func(context.Context, string, string) (net.Conn, error) { return clientConn, nil },
		MaxRetries:  -1,
		ReadTimeout: time.Second,
	})
	maxcommon.RDB, maxcommon.RedisEnabled = client, true
	t.Cleanup(func() {
		_ = client.Close()
		maxcommon.RDB, maxcommon.RedisEnabled = oldClient, oldEnabled
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	results, err := LoadRuntimeStatesStrict(ctx, []int{1})
	require.Error(t, err)
	require.Empty(t, results)
	require.Less(t, time.Since(started), time.Second)
}

package channelhealth

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func testRuntimeSetting() *operation_setting.MonitorSetting {
	return &operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled:        true,
		StreamingFirstResultTimeoutSeconds: 10,
		PriorityDeduction:                  10,
		PenaltyCooldownSeconds:             60,
		RecoveryMode:                       operation_setting.RecoveryModeAutomatic,
		ExtendOnRepeatTimeout:              true,
		TimeoutAutoDisableEnabled:          true,
		TimeoutAutoDisableCount:            2,
		TimeoutAutoDisableWindowSeconds:    3600,
		TimeoutAutoDisableCountScope:       operation_setting.TimeoutAutoDisableCountScopeSameMode,
		TimeoutAutoDisableDurationSeconds:  1800,
		TimeoutAutoDisableRecoveryMode:     operation_setting.TimeoutAutoDisableRecoveryProbe,
	}
}

func withRuntimeRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	maxcommon.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	maxcommon.RedisEnabled = true
	t.Cleanup(func() {
		_ = maxcommon.RDB.Close()
		maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled
	})
}

func TestEvaluateTimeoutGuardAppliesPenaltyAndDeduplicates(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	now := time.Now()

	first, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "attempt-1",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now,
	}, setting)
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.Equal(t, "priority_demotion_firing", first.Transition)
	require.Equal(t, int64(10), first.State.Penalty)
	require.Equal(t, int64(1), first.State.TimeoutCount[RequestModeStreaming])

	duplicate, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "attempt-1",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now.Add(time.Second),
	}, setting)
	require.NoError(t, err)
	require.True(t, duplicate.Deduplicated)
	require.Equal(t, int64(1), duplicate.State.TimeoutCount[RequestModeStreaming])
}

func TestEvaluateTimeoutGuardSkipsInactiveEvidenceWrite(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.StreamingFirstResultTimeoutSeconds = 0

	result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:   936,
		AttemptID:   "inactive-stream-success",
		RequestMode: RequestModeStreaming,
		TimeoutKind: TimeoutKindFirstResponse,
		Success:     true,
		AutoBan:     true,
		ObservedAt:  time.Now(),
	}, setting)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.False(t, result.Deduplicated)

	exists, err := maxcommon.RDB.Exists(context.Background(), RuntimeStateKey(936)).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
	indexed, err := maxcommon.RDB.SIsMember(context.Background(), runtimeStateIndexKey, "936").Result()
	require.NoError(t, err)
	require.False(t, indexed)
}

func TestEvaluateTimeoutGuardRetriesConcurrentRedisTransactions(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.TimeoutAutoDisableCount = 100
	const attempts = 8
	errorsCh := make(chan error, attempts)
	var waitGroup sync.WaitGroup
	waitGroup.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func() {
			defer waitGroup.Done()
			_, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
				ChannelID:       936,
				AttemptID:       "concurrent-attempt-" + strconv.Itoa(index),
				RequestMode:     RequestModeStreaming,
				TimeoutKind:     TimeoutKindStreamingFirstResult,
				TimeoutEligible: true,
				AutoBan:         true,
				ObservedAt:      time.Now(),
			}, setting)
			errorsCh <- err
		}()
	}
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}

	state, err := LoadRuntimeState(context.Background(), 936)
	require.NoError(t, err)
	require.Equal(t, int64(attempts), state.TimeoutCount[RequestModeStreaming])
}

func TestEvaluateTimeoutGuardBoundsUnavailableRedis(t *testing.T) {
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		MaxRetries:   0,
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
	})
	maxcommon.RDB = client
	maxcommon.RedisEnabled = true
	t.Cleanup(func() {
		_ = client.Close()
		maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled
	})

	started := time.Now()
	_, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "unavailable-redis",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
	}, testRuntimeSetting())
	require.Error(t, err)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestEvaluateTimeoutGuardAutoDisablesAfterConfiguredCount(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	now := time.Now()
	for _, id := range []string{"attempt-1", "attempt-2"} {
		result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
			ChannelID:       936,
			AttemptID:       id,
			RequestMode:     RequestModeStreaming,
			TimeoutKind:     TimeoutKindStreamingFirstResult,
			TimeoutEligible: true,
			AutoBan:         true,
			ObservedAt:      now,
		}, setting)
		require.NoError(t, err)
		if id == "attempt-2" {
			require.Equal(t, "timeout_auto_disabled", result.Transition)
			require.True(t, result.State.RuntimeDisabled)
			require.WithinDuration(t, now.Add(30*time.Minute), result.State.DisabledUntil, time.Second)
			require.Zero(t, result.State.Penalty)
		}
	}
}

func TestEvaluateTimeoutGuardAdvancedGateUsesSuccessfulSamplesAndRatio(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.TimeoutAutoDisableMinimumSamples = 3
	setting.TimeoutAutoDisableRatioPercent = 50
	now := time.Now()
	for _, id := range []string{"success-1", "success-2", "success-3", "timeout-1", "timeout-2"} {
		evidence := AttemptEvidence{
			ChannelID:   936,
			AttemptID:   id,
			RequestMode: RequestModeStreaming,
			AutoBan:     true,
			ObservedAt:  now,
		}
		if strings.HasPrefix(id, "success") {
			evidence.Success = true
			evidence.TimeoutKind = TimeoutKindFirstResponse
		} else {
			evidence.TimeoutEligible = true
			evidence.TimeoutKind = TimeoutKindStreamingFirstResult
		}
		result, err := EvaluateTimeoutGuard(context.Background(), evidence, setting)
		require.NoError(t, err)
		if id == "timeout-2" {
			require.False(t, result.State.RuntimeDisabled)
			require.Equal(t, int64(2), result.State.TimeoutCount[RequestModeStreaming])
			require.Equal(t, int64(5), result.State.SampleCount[RequestModeStreaming])
		}
	}

	result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "timeout-3",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now,
	}, setting)
	require.NoError(t, err)
	require.True(t, result.State.RuntimeDisabled)
	require.Equal(t, "timeout_auto_disabled", result.Transition)
	require.Equal(t, int64(3), result.State.TimeoutCount[RequestModeStreaming])
	require.Equal(t, int64(6), result.State.SampleCount[RequestModeStreaming])
}

func TestEvaluateTimeoutGuardSuccessfulSampleIsNoOpWhenAdvancedGateDisabled(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.TimeoutAutoDisableRatioPercent = 0
	result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:   936,
		AttemptID:   "success-without-gate",
		RequestMode: RequestModeStreaming,
		TimeoutKind: TimeoutKindFirstResponse,
		Success:     true,
		AutoBan:     true,
	}, setting)
	require.NoError(t, err)
	require.False(t, result.Applied)
	keys, err := maxcommon.RDB.Keys(context.Background(), "maxapi:channel-timeout:v1:*").Result()
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestEvaluateTimeoutGuardPreservesLastTimeoutModeAcrossSuccessfulSamples(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.TimeoutAutoDisableMinimumSamples = 1
	setting.TimeoutAutoDisableRatioPercent = 50
	now := time.Now()

	_, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "streaming-timeout",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now,
	}, setting)
	require.NoError(t, err)

	result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:   936,
		AttemptID:   "streaming-success",
		RequestMode: RequestModeStreaming,
		TimeoutKind: TimeoutKindFirstResponse,
		Success:     true,
		AutoBan:     true,
		ObservedAt:  now.Add(time.Second),
	}, setting)
	require.NoError(t, err)
	require.Equal(t, RequestModeStreaming, result.State.LastTimeoutMode)
	require.Equal(t, TimeoutKindFirstResponse, result.State.LastEvent)
}

func TestEvaluateTimeoutGuardIsNoOpWhenPoliciesAreDisabled(t *testing.T) {
	withRuntimeRedis(t)
	setting := &operation_setting.MonitorSetting{}
	result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "disabled-policy-attempt",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
	}, setting)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.False(t, result.Deduplicated)
	keys, err := maxcommon.RDB.Keys(context.Background(), "maxapi:channel-timeout:v1:*").Result()
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestEffectivePriorityExpiresAndClampsAtZero(t *testing.T) {
	state := RuntimeState{Penalty: 20, PenaltyUntil: time.Now().Add(time.Minute)}
	require.Equal(t, int64(80), EffectivePriority(100, state, time.Now()))
	require.Equal(t, int64(100), EffectivePriority(100, state, time.Now().Add(2*time.Minute)))
	require.Equal(t, int64(0), EffectivePriority(10, state, time.Now()))
	require.Equal(t, int64(-25), EffectivePriority(-5, state, time.Now()))
}

func TestEvaluateTimeoutGuardPreservesPenaltyDeadlineWhenRepeatExtensionDisabled(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.ExtendOnRepeatTimeout = false
	setting.TimeoutAutoDisableCount = 100
	now := time.Now()

	first, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "non-extending-first",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now,
	}, setting)
	require.NoError(t, err)

	second, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "non-extending-repeat",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now.Add(10 * time.Second),
	}, setting)
	require.NoError(t, err)
	require.WithinDuration(t, first.State.PenaltyUntil, second.State.PenaltyUntil, time.Millisecond)
}

func TestEvaluateTimeoutGuardIgnoresTimeoutObservedBeforeRecovery(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	staleObservedAt := time.Now().Add(-time.Second)

	first, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "stale-before-recovery",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      staleObservedAt,
	}, setting)
	require.NoError(t, err)
	require.True(t, first.Applied)

	recovered, err := RestoreRuntimeState(context.Background(), 936)
	require.NoError(t, err)
	require.True(t, recovered.Applied)
	require.Equal(t, "manual_recovery", recovered.State.LastEvent)

	stale, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "stale-after-recovery",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      staleObservedAt,
	}, setting)
	require.NoError(t, err)
	require.False(t, stale.Applied)
	require.False(t, stale.Deduplicated)
	require.Equal(t, "manual_recovery", stale.State.LastEvent)
	require.Zero(t, stale.State.Penalty)
	require.False(t, stale.State.RuntimeDisabled)
}

func TestEvaluateTimeoutGuardKeepsRecoveryBoundaryAfterNewSample(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.TimeoutAutoDisableMinimumSamples = 10
	oldEvidence := AttemptEvidence{
		ChannelID: 936, AttemptID: "initial", RequestMode: RequestModeStreaming,
		TimeoutKind: TimeoutKindStreamingFirstResult, TimeoutEligible: true, AutoBan: true,
		ObservedAt: time.Now().Add(-time.Second),
	}
	_, err := EvaluateTimeoutGuard(context.Background(), oldEvidence, setting)
	require.NoError(t, err)
	_, err = RestoreRuntimeState(context.Background(), 936)
	require.NoError(t, err)
	newSample, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID: 936, AttemptID: "new-success", RequestMode: RequestModeStreaming,
		TimeoutKind: TimeoutKindFirstResponse, Success: true, AutoBan: true, ObservedAt: time.Now(),
	}, setting)
	require.NoError(t, err)
	oldEvidence.AttemptID = "delayed-old-timeout"
	stale, err := EvaluateTimeoutGuard(context.Background(), oldEvidence, setting)
	require.NoError(t, err)
	require.False(t, stale.Applied)
	require.Equal(t, newSample.State.Generation, stale.State.Generation)
	require.True(t, newSample.State.LastObservedAt.Equal(stale.State.LastObservedAt))
	require.Zero(t, stale.State.TimeoutCount[RequestModeStreaming])
	require.Zero(t, stale.State.Penalty)
}

func TestEvaluateTimeoutGuardTTLIncludesActiveDeadlines(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(disabled), func(t *testing.T) {
			withRuntimeRedis(t)
			setting := testRuntimeSetting()
			setting.PenaltyCooldownSeconds = 30 * 24 * 3600
			setting.TimeoutAutoDisableDurationSeconds = 30 * 24 * 3600
			setting.TimeoutAutoDisableCount = 1
			setting.TimeoutAutoDisableEnabled = disabled
			result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
				ChannelID: 936, AttemptID: "long-deadline", RequestMode: RequestModeStreaming,
				TimeoutKind: TimeoutKindStreamingFirstResult, TimeoutEligible: true, AutoBan: true,
				ObservedAt: time.Now(),
			}, setting)
			require.NoError(t, err)
			deadline := result.State.PenaltyUntil
			if disabled {
				deadline = result.State.DisabledUntil
			}
			ttl, err := maxcommon.RDB.TTL(context.Background(), RuntimeStateKey(936)).Result()
			require.NoError(t, err)
			require.Greater(t, ttl, time.Until(deadline))
		})
	}
}

func TestStreamIdleTimeoutDoesNotCountOrDemote(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	result, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "attempt-idle",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamIdle,
		TimeoutEligible: true,
		AutoBan:         true,
	}, setting)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Zero(t, result.State.Penalty)
	require.Zero(t, result.State.TimeoutCount[RequestModeStreaming])
}

func TestListRuntimeStatesRebuildsTheSmartOpsProjection(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	now := time.Now()
	_, err := EvaluateTimeoutGuard(context.Background(), AttemptEvidence{
		ChannelID:       936,
		AttemptID:       "restart-visible-attempt",
		RequestMode:     RequestModeStreaming,
		TimeoutKind:     TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      now,
	}, setting)
	require.NoError(t, err)

	states, err := ListRuntimeStates(context.Background())
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.Equal(t, 936, states[0].ChannelID)
	require.Equal(t, int64(10), states[0].Penalty)
	require.Equal(t, int64(1), states[0].TimeoutCount[RequestModeStreaming])
	indexed, err := maxcommon.RDB.SIsMember(context.Background(), runtimeStateIndexKey, "936").Result()
	require.NoError(t, err)
	require.True(t, indexed)
}

func TestListRuntimeStatesMarksLegacyScanComplete(t *testing.T) {
	withRuntimeRedis(t)
	key := RuntimeStateKey(937)
	_, err := maxcommon.RDB.HSet(context.Background(), key, map[string]interface{}{
		"channel_id": "937",
		"penalty":    "5",
	}).Result()
	require.NoError(t, err)

	states, err := ListRuntimeStates(context.Background())
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.Equal(t, 937, states[0].ChannelID)
	migrated, err := maxcommon.RDB.Exists(context.Background(), runtimeStateIndexMigratedKey).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), migrated)
	require.NoError(t, maxcommon.RDB.SRem(context.Background(), runtimeStateIndexKey, "937").Err())

	// A stray legacy-shaped key does not restart a full keyspace scan after the
	// migration marker has been recorded.
	_, err = maxcommon.RDB.HSet(context.Background(), RuntimeStateKey(938), map[string]interface{}{
		"channel_id": "938",
		"penalty":    "7",
	}).Result()
	require.NoError(t, err)
	states, err = ListRuntimeStates(context.Background())
	require.NoError(t, err)
	require.Empty(t, states)
}

func TestListRuntimeStatesCompletesLegacyMigrationWithIndexedMembers(t *testing.T) {
	withRuntimeRedis(t)
	for channelID, penalty := range map[int]string{937: "5", 938: "7"} {
		_, err := maxcommon.RDB.HSet(context.Background(), RuntimeStateKey(channelID), map[string]interface{}{
			"channel_id": strconv.Itoa(channelID),
			"penalty":    penalty,
		}).Result()
		require.NoError(t, err)
	}
	require.NoError(t, maxcommon.RDB.SAdd(context.Background(), runtimeStateIndexKey, "937", "939").Err())

	states, err := ListRuntimeStates(context.Background())
	require.NoError(t, err)
	require.Len(t, states, 2)
	migrated, err := maxcommon.RDB.Exists(context.Background(), runtimeStateIndexMigratedKey).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), migrated)
	indexed, err := maxcommon.RDB.SIsMember(context.Background(), runtimeStateIndexKey, "938").Result()
	require.NoError(t, err)
	require.True(t, indexed)
	indexed, err = maxcommon.RDB.SIsMember(context.Background(), runtimeStateIndexKey, "939").Result()
	require.NoError(t, err)
	require.False(t, indexed)
}

func TestRecoverRuntimeStateRequiresConfiguredSuccessfulProbes(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	setting.TimeoutAutoDisableSuccessfulProbeCount = 2
	now := time.Now().Add(-time.Minute)
	key := RuntimeStateKey(936)
	_, err := maxcommon.RDB.HSet(context.Background(), key, map[string]interface{}{
		"channel_id":              "936",
		"runtime_disabled":        "true",
		"disabled_until":          now.Format(time.RFC3339Nano),
		"active_source":           "repeated_timeout",
		"generation":              "1",
		"timeout_count_streaming": "2",
		"sample_count_streaming":  "4",
		"window_start_streaming":  now.Add(-time.Minute).Format(time.RFC3339Nano),
	}).Result()
	require.NoError(t, err)

	first, err := RecoverRuntimeState(context.Background(), 936, "probe-1", true, setting)
	require.NoError(t, err)
	require.False(t, first.Applied)
	require.True(t, first.State.RuntimeDisabled)
	require.Equal(t, int64(1), first.State.RecoveryProbeSuccessCount)

	second, err := RecoverRuntimeState(context.Background(), 936, "probe-2", true, setting)
	require.NoError(t, err)
	require.True(t, second.Applied)
	require.Equal(t, "runtime_recovered", second.Transition)
	require.False(t, second.State.RuntimeDisabled)
	require.Zero(t, second.State.RecoveryProbeSuccessCount)
	require.Zero(t, second.State.TimeoutCount[RequestModeStreaming])
	require.Zero(t, second.State.SampleCount[RequestModeStreaming])
	require.True(t, second.State.WindowStartedAt[RequestModeStreaming].IsZero())
}

func TestRecoverRuntimeStateFailedProbeKeepsChannelDisabled(t *testing.T) {
	withRuntimeRedis(t)
	setting := testRuntimeSetting()
	now := time.Now().Add(-time.Minute)
	key := RuntimeStateKey(936)
	_, err := maxcommon.RDB.HSet(context.Background(), key, map[string]interface{}{
		"channel_id":       "936",
		"runtime_disabled": "true",
		"disabled_until":   now.Format(time.RFC3339Nano),
		"active_source":    "repeated_timeout",
	}).Result()
	require.NoError(t, err)

	result, err := RecoverRuntimeState(context.Background(), 936, "probe-failed", false, setting)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.True(t, result.State.RuntimeDisabled)
	require.Zero(t, result.State.RecoveryProbeSuccessCount)
	require.Equal(t, "recovery_probe_failed", result.State.LastEvent)
}

func TestRestoreRuntimeStateDoesNotTouchDatabaseState(t *testing.T) {
	withRuntimeRedis(t)
	key := RuntimeStateKey(936)
	_, err := maxcommon.RDB.HSet(context.Background(), key, map[string]interface{}{
		"channel_id":              "936",
		"runtime_disabled":        "true",
		"active_source":           "repeated_timeout",
		"penalty":                 "10",
		"timeout_count_streaming": "2",
		"sample_count_streaming":  "4",
		"window_start_streaming":  time.Now().Add(-time.Minute).Format(time.RFC3339Nano),
	}).Result()
	require.NoError(t, err)

	result, err := RestoreRuntimeState(context.Background(), 936)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, "runtime_recovered", result.Transition)
	require.False(t, result.State.RuntimeDisabled)
	require.Zero(t, result.State.Penalty)
	require.Zero(t, result.State.TimeoutCount[RequestModeStreaming])
	require.Zero(t, result.State.SampleCount[RequestModeStreaming])
	require.True(t, result.State.WindowStartedAt[RequestModeStreaming].IsZero())
}

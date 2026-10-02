package common

import (
	"context"
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoRecordsOneFirstResponsePerChannelAttempt(t *testing.T) {
	oldRecorder := recordChannelHealthObservation
	t.Cleanup(func() { recordChannelHealthObservation = oldRecorder })

	var observations []maxcommon.ChannelHealthObservation
	recordChannelHealthObservation = func(observation maxcommon.ChannelHealthObservation) error {
		observations = append(observations, observation)
		return nil
	}

	start := time.Now().Add(-2 * time.Second)
	info := &RelayInfo{
		StartTime:               start,
		IsStream:                true,
		RetryIndex:              2,
		isFirstResponse:         true,
		channelAttemptStartTime: start,
		ChannelMeta:             &ChannelMeta{ChannelId: 936},
	}
	info.SetFirstResponseTime()
	info.SetFirstResponseTime()
	info.RecordChannelTimeout("stream_idle_timeout")

	require.Len(t, observations, 2)
	require.Equal(t, "first_response", observations[0].Event)
	require.Equal(t, "stream_idle_timeout", observations[1].Event)
	require.Equal(t, 936, observations[0].ChannelID)
	require.Equal(t, 2, observations[0].RetryIndex)
	require.True(t, observations[0].AttemptLatencyMS >= 0)
}

func TestRelayInfoFirstResponseEvaluationHasBoundedGrace(t *testing.T) {
	info := &RelayInfo{}
	finish := info.BeginFirstResponseEvaluation()

	require.False(t, info.FirstResponseDeadlineExpired())
	info.SetFirstResponseTime()
	require.False(t, info.FirstResponseDeadlineExpired())
	finish()
	require.False(t, info.FirstResponseDeadlineExpired())

	pending := &RelayInfo{}
	finishPending := pending.BeginFirstResponseEvaluation()
	t.Cleanup(finishPending)
	time.Sleep(firstResponseEvaluationGrace + 10*time.Millisecond)
	require.True(t, pending.FirstResponseDeadlineExpired())
}

func TestRelayInfoTransportTimeoutHonorsConfiguredThreshold(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	maxcommon.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	maxcommon.RedisEnabled = true
	t.Cleanup(func() {
		_ = maxcommon.RDB.Close()
		maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled
	})

	oldSetting := *operation_setting.GetMonitorSetting()
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled:        true,
		StreamingFirstResultTimeoutSeconds: 2,
		PriorityDeduction:                  10,
		PenaltyCooldownSeconds:             60,
	}
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldSetting })

	start := time.Now().Add(-100 * time.Millisecond)
	info := &RelayInfo{
		StartTime:               start,
		IsStream:                true,
		ChannelMeta:             &ChannelMeta{ChannelId: 936, ChannelAutoBan: true},
		channelAttemptStartTime: start,
	}
	info.RecordChannelTimeout("transport_timeout")
	time.Sleep(50 * time.Millisecond)

	state, err := channelhealth.LoadRuntimeState(context.Background(), 936)
	require.NoError(t, err)
	require.Zero(t, state.Penalty)
	require.Zero(t, state.TimeoutCount[channelhealth.RequestModeStreaming])
}

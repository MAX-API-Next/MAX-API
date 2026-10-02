package common

import (
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
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

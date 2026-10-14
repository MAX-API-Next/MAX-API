package common

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestFirstResultKeepsFirstFrameMetricAndAttemptSignalsSeparate(t *testing.T) {
	start := time.Now().Add(-time.Second)
	info := &RelayInfo{StartTime: start, isFirstResponse: true, channelFirstResponseSignal: make(chan struct{})}
	info.EnableFirstResultTracking()
	info.SetFirstResponseTime()
	frameTime := info.FirstResponseTime
	require.True(t, info.HasRecordedChannelFirstResponse())
	require.False(t, info.HasRecordedChannelFirstResult())
	require.True(t, info.FirstResultDeadlineExpired())
	select {
	case <-info.FirstResultSignal():
		t.Fatal("metadata closed the output signal")
	default:
	}
	finish := info.BeginFirstResponseEvaluation()
	require.False(t, info.FirstResultDeadlineExpired())
	finish()
	require.True(t, info.FirstResultDeadlineExpired())
	info.SetFirstResultTime()
	resultTime := info.FirstResultTime
	info.SetFirstResultTime()
	require.Equal(t, frameTime, info.FirstResponseTime)
	require.Equal(t, resultTime, info.FirstResultTime)
	require.False(t, resultTime.Before(frameTime))
	require.False(t, info.FirstResultDeadlineExpired())
	select {
	case <-info.FirstResultSignal():
	default:
		t.Fatal("delivered output did not close the result signal")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info.InitChannelMeta(c)
	info.EnableFirstResultTracking()
	require.False(t, info.HasRecordedChannelFirstResponse())
	require.False(t, info.HasRecordedChannelFirstResult())
	require.True(t, info.FirstResultDeadlineExpired(), "retry gets a fresh attempt budget")
	require.Equal(t, frameTime, info.FirstResponseTime, "request latency stays compatible")
	select {
	case <-info.FirstResultSignal():
		t.Fatal("retry reused a closed signal")
	default:
	}
}

func TestStrictFirstResultHealthSamplesExcludeMetadata(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	maxcommon.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	maxcommon.RedisEnabled = true
	t.Cleanup(func() { _ = maxcommon.RDB.Close(); maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled })
	oldSetting := *operation_setting.GetMonitorSetting()
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		TimeoutAutoDisableEnabled: true, TimeoutAutoDisableMinimumSamples: 2,
		StreamingFirstResultTimeoutSeconds: 1,
	}
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldSetting })
	info := &RelayInfo{IsStream: true, RequestId: "first-result-health", channelAttemptStartTime: time.Now(),
		ChannelMeta: &ChannelMeta{ChannelId: 936, ChannelAutoBan: true}}
	info.EnableFirstResultTracking()
	info.SetFirstResponseTime()
	assertFirstResultHealthCondition(t, false, func() bool {
		state, err := channelhealth.LoadRuntimeState(context.Background(), 936)
		require.NoError(t, err)
		return state.SampleCount[channelhealth.RequestModeStreaming] != 0
	}, 150*time.Millisecond)
	info.SetFirstResultTime()
	info.SetFirstResultTime()
	assertFirstResultHealthCondition(t, true, func() bool {
		state, err := channelhealth.LoadRuntimeState(context.Background(), 936)
		require.NoError(t, err)
		return state.SampleCount[channelhealth.RequestModeStreaming] == 1
	}, time.Second)
}

func TestStrictFirstResultTimeoutClassification(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	maxcommon.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	maxcommon.RedisEnabled = true
	t.Cleanup(func() { _ = maxcommon.RDB.Close(); maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled })
	oldSetting := *operation_setting.GetMonitorSetting()
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled: true, StreamingFirstResultTimeoutSeconds: 1,
		PriorityDeduction: 10, PenaltyCooldownSeconds: 60,
	}
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldSetting })
	for idx, test := range []struct {
		event            string
		output, penalize bool
	}{
		{"transport_timeout", false, true},
		{"transport_timeout", true, false},
		{"stream_idle_timeout", false, false},
		{"stream_idle_timeout", true, false},
	} {
		t.Run(test.event+map[bool]string{false: "/metadata", true: "/output"}[test.output], func(t *testing.T) {
			channelID := 1936 + idx
			info := &RelayInfo{IsStream: true, RequestId: "classified-first-result", channelAttemptStartTime: time.Now().Add(-2 * time.Second),
				ChannelMeta: &ChannelMeta{ChannelId: channelID, ChannelAutoBan: true}}
			info.EnableFirstResultTracking()
			info.SetFirstResponseTime()
			if test.output {
				info.SetFirstResultTime()
			}
			info.RecordChannelTimeout(test.event)
			penaltyApplied := func() bool {
				state, err := channelhealth.LoadRuntimeState(context.Background(), channelID)
				require.NoError(t, err)
				return state.Penalty == 10
			}
			if test.penalize {
				assertFirstResultHealthCondition(t, true, penaltyApplied, time.Second)
			} else {
				assertFirstResultHealthCondition(t, false, penaltyApplied, 150*time.Millisecond)
			}
		})
	}
}

// Run polling on the test goroutine: testify's asynchronous Never/Eventually
// callbacks can outlive the deadline and access t or Redis after cleanup.
func assertFirstResultHealthCondition(t *testing.T, expected bool, condition func() bool, waitFor time.Duration) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		if condition() {
			require.True(t, expected, "unexpected channel health state within the observation window")
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.False(t, expected, "expected channel health state was not observed")
}

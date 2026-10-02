package model

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

func withChannelRoutingTestState(t *testing.T, setting operation_setting.MonitorSetting) {
	t.Helper()
	oldSetting := *operation_setting.GetMonitorSetting()
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	server := miniredis.RunT(t)
	maxcommon.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	maxcommon.RedisEnabled = true
	*operation_setting.GetMonitorSetting() = setting
	t.Cleanup(func() {
		_ = maxcommon.RDB.Close()
		maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled
		*operation_setting.GetMonitorSetting() = oldSetting
	})
}

func TestAbilityRoutingUsesEffectivePriorityWithoutChangingBasePriority(t *testing.T) {
	setting := operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled:        true,
		StreamingFirstResultTimeoutSeconds: 1,
		PriorityDeduction:                  20,
		PenaltyCooldownSeconds:             60,
		RecoveryMode:                       operation_setting.RecoveryModeAutomatic,
	}
	withChannelRoutingTestState(t, setting)
	_, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
		ChannelID:       1,
		AttemptID:       "routing-penalty",
		RequestMode:     channelhealth.RequestModeStreaming,
		TimeoutKind:     channelhealth.TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
		ObservedAt:      time.Now(),
	}, &setting)
	require.NoError(t, err)

	baseHigh, baseLow := int64(100), int64(90)
	abilities := []Ability{
		{ChannelId: 1, Priority: &baseHigh, Weight: 100},
		{ChannelId: 2, Priority: &baseLow, Weight: 100},
	}
	selected, ok, err := selectChannelIdFromAbilities(abilities, 0, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, selected)
	selected, ok, err = selectChannelIdFromAbilities(abilities, 1, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, selected)
	require.Equal(t, int64(100), *abilities[0].Priority)
}

func TestAbilityRoutingExcludesRuntimeDisabledChannel(t *testing.T) {
	setting := operation_setting.MonitorSetting{
		TimeoutAutoDisableEnabled:          true,
		StreamingFirstResultTimeoutSeconds: 1,
		TimeoutAutoDisableCount:            2,
		TimeoutAutoDisableWindowSeconds:    3600,
		TimeoutAutoDisableDurationSeconds:  1800,
		TimeoutAutoDisableRecoveryMode:     operation_setting.TimeoutAutoDisableRecoveryProbe,
	}
	withChannelRoutingTestState(t, setting)
	for _, attemptID := range []string{"routing-disable-1", "routing-disable-2"} {
		_, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
			ChannelID:       1,
			AttemptID:       attemptID,
			RequestMode:     channelhealth.RequestModeStreaming,
			TimeoutKind:     channelhealth.TimeoutKindStreamingFirstResult,
			TimeoutEligible: true,
			AutoBan:         true,
		}, &setting)
		require.NoError(t, err)
	}

	baseHigh, baseLow := int64(100), int64(90)
	selected, ok, err := selectChannelIdFromAbilities([]Ability{
		{ChannelId: 1, Priority: &baseHigh, Weight: 100},
		{ChannelId: 2, Priority: &baseLow, Weight: 100},
	}, 0, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, selected)
}

func TestAbilityRoutingFailsOpenWhenRedisUnavailable(t *testing.T) {
	oldSetting := *operation_setting.GetMonitorSetting()
	oldRDB, oldEnabled := maxcommon.RDB, maxcommon.RedisEnabled
	t.Cleanup(func() {
		maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled
		*operation_setting.GetMonitorSetting() = oldSetting
	})
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled:        true,
		StreamingFirstResultTimeoutSeconds: 1,
		PriorityDeduction:                  20,
	}
	maxcommon.RDB = nil
	maxcommon.RedisEnabled = false

	baseHigh, baseLow := int64(100), int64(90)
	selected, ok, err := selectChannelIdFromAbilities([]Ability{
		{ChannelId: 1, Priority: &baseHigh, Weight: 100},
		{ChannelId: 2, Priority: &baseLow, Weight: 100},
	}, 0, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, selected)
}

func TestAbilityRoutingFailsOpenWhenRedisDisconnects(t *testing.T) {
	oldSetting := *operation_setting.GetMonitorSetting()
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
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled:        true,
		StreamingFirstResultTimeoutSeconds: 1,
		PriorityDeduction:                  20,
	}
	t.Cleanup(func() {
		_ = client.Close()
		maxcommon.RDB, maxcommon.RedisEnabled = oldRDB, oldEnabled
		*operation_setting.GetMonitorSetting() = oldSetting
	})

	baseHigh, baseLow := int64(100), int64(90)
	selected, ok, err := selectChannelIdFromAbilities([]Ability{
		{ChannelId: 1, Priority: &baseHigh, Weight: 100},
		{ChannelId: 2, Priority: &baseLow, Weight: 100},
	}, 0, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, selected)
}

func TestCachedRoutingCandidatesApplyRuntimeState(t *testing.T) {
	setting := operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled:        true,
		StreamingFirstResultTimeoutSeconds: 1,
		PriorityDeduction:                  20,
		PenaltyCooldownSeconds:             60,
		RecoveryMode:                       operation_setting.RecoveryModeAutomatic,
	}
	withChannelRoutingTestState(t, setting)
	_, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
		ChannelID:       1,
		AttemptID:       "cached-routing-penalty",
		RequestMode:     channelhealth.RequestModeStreaming,
		TimeoutKind:     channelhealth.TimeoutKindStreamingFirstResult,
		TimeoutEligible: true,
		AutoBan:         true,
	}, &setting)
	require.NoError(t, err)

	baseHigh, baseLow := int64(100), int64(90)
	candidates := buildChannelRoutingCandidates([]*Channel{
		{Id: 1, Priority: &baseHigh},
		{Id: 2, Priority: &baseLow},
	})
	require.Len(t, candidates, 2)
	require.Equal(t, int64(80), candidates[0].priority)
	require.Equal(t, int64(90), candidates[1].priority)
}

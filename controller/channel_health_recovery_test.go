package controller

import (
	"context"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestSelectChannelsForRuntimeRecoveryOnlyProbesDueEligibleChannels(t *testing.T) {
	server := miniredis.RunT(t)
	previousRedis, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	previousSetting := *operation_setting.GetMonitorSetting()
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB, common.RedisEnabled = previousRedis, previousEnabled
		*operation_setting.GetMonitorSetting() = previousSetting
	})
	setting := operation_setting.GetMonitorSetting()
	setting.TimeoutAutoDisableEnabled = true
	setting.TimeoutAutoDisableRecoveryMode = operation_setting.TimeoutAutoDisableRecoveryProbe
	setting.TimeoutAutoDisableDurationSeconds = 60

	now := time.Now()
	for id, until := range map[int]time.Time{
		1: now.Add(-time.Minute),
		2: now.Add(time.Hour),
		3: now.Add(-time.Minute),
	} {
		err := common.RDB.HSet(context.Background(), channelhealth.RuntimeStateKey(id), map[string]interface{}{
			"runtime_disabled": "true",
			"disabled_until":   until.UTC().Format(time.RFC3339Nano),
		}).Err()
		require.NoError(t, err)
	}
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
		{Id: 4, Status: common.ChannelStatusAutoDisabled},
	}
	selected := selectChannelsForRuntimeRecovery(channels)
	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 4, selected[1].Id)

	setting.TimeoutAutoDisableRecoveryMode = operation_setting.TimeoutAutoDisableRecoveryManual
	selected = selectChannelsForRuntimeRecovery(channels)
	require.Len(t, selected, 1)
	require.Equal(t, 4, selected[0].Id)
}

func TestRuntimeRecoveryDoesNotReenableLegacySQLAutoDisabledChannel(t *testing.T) {
	previousAutomaticEnable := common.AutomaticEnableChannelEnabled
	common.AutomaticEnableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = previousAutomaticEnable })

	require.False(t, shouldEnableLegacyChannelAfterTest(
		true,
		nil,
		false,
		nil,
		common.ChannelStatusAutoDisabled,
	))
	require.True(t, shouldEnableLegacyChannelAfterTest(
		false,
		nil,
		false,
		nil,
		common.ChannelStatusAutoDisabled,
	))
}

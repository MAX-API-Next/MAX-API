package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorSettingTimeoutDefaults(t *testing.T) {
	setting := GetMonitorSetting()
	require.False(t, setting.AutoPriorityDemotionEnabled)
	require.Equal(t, 5, setting.TimeoutAutoDisableCount)
	require.Equal(t, 3600, setting.TimeoutAutoDisableWindowSeconds)
	require.Equal(t, TimeoutAutoDisableCountScopeSameMode, setting.TimeoutAutoDisableCountScope)
	require.Equal(t, TimeoutAutoDisableRecoveryProbe, setting.TimeoutAutoDisableRecoveryMode)
	require.Equal(t, RecoveryModeAutomatic, setting.RecoveryMode)
	require.False(t, setting.ChannelTimeoutNotificationEnabled)
}

func TestValidateMonitorSettingOption(t *testing.T) {
	require.NoError(t, ValidateMonitorSettingOption(
		"monitor_setting.streaming_first_result_timeout_seconds", "15",
	))
	require.NoError(t, ValidateMonitorSettingOption(
		"monitor_setting.timeout_auto_disable_ratio_percent", "50",
	))
	require.Error(t, ValidateMonitorSettingOption(
		"monitor_setting.timeout_auto_disable_count", "1",
	))
	require.Error(t, ValidateMonitorSettingOption(
		"monitor_setting.timeout_auto_disable_count_scope", "invalid",
	))
	require.Error(t, ValidateMonitorSettingOption(
		"monitor_setting.priority_deduction", "-1",
	))
}

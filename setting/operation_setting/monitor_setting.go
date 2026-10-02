package operation_setting

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/setting/config"
)

type MonitorSetting struct {
	AutoTestChannelEnabled                 bool    `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes                 float64 `json:"auto_test_channel_minutes"`
	ChannelTestMode                        string  `json:"channel_test_mode"`
	AutoPriorityDemotionEnabled            bool    `json:"auto_priority_demotion_enabled"`
	StreamingFirstResultTimeoutSeconds     int     `json:"streaming_first_result_timeout_seconds"`
	NonStreamingResponseTimeoutSeconds     int     `json:"non_streaming_response_timeout_seconds"`
	PriorityDeduction                      int     `json:"priority_deduction"`
	TimeoutAutoDisableEnabled              bool    `json:"timeout_auto_disable_enabled"`
	TimeoutAutoDisableCount                int     `json:"timeout_auto_disable_count"`
	TimeoutAutoDisableWindowSeconds        int     `json:"timeout_auto_disable_window_seconds"`
	TimeoutAutoDisableCountScope           string  `json:"timeout_auto_disable_count_scope"`
	TimeoutAutoDisableMinimumSamples       int     `json:"timeout_auto_disable_minimum_samples"`
	TimeoutAutoDisableRatioPercent         float64 `json:"timeout_auto_disable_ratio_percent"`
	TimeoutAutoDisableDurationSeconds      int     `json:"timeout_auto_disable_duration_seconds"`
	TimeoutAutoDisableRecoveryMode         string  `json:"timeout_auto_disable_recovery_mode"`
	TimeoutAutoDisableSuccessfulProbeCount int     `json:"timeout_auto_disable_successful_probe_count"`
	PenaltyCooldownSeconds                 int     `json:"penalty_cooldown_seconds"`
	RecoveryMode                           string  `json:"recovery_mode"`
	ExtendOnRepeatTimeout                  bool    `json:"extend_on_repeat_timeout"`
	ChannelTimeoutNotificationEnabled      bool    `json:"channel_timeout_notification_enabled"`
	AlertRepeatIntervalSeconds             int     `json:"alert_repeat_interval_seconds"`
}

const (
	ChannelTestModeScheduledAll    = "scheduled_all"
	ChannelTestModePassiveRecovery = "passive_recovery"
)

// 默认配置
var monitorSetting = MonitorSetting{
	AutoTestChannelEnabled:             false,
	AutoTestChannelMinutes:             10,
	ChannelTestMode:                    ChannelTestModeScheduledAll,
	AutoPriorityDemotionEnabled:        false,
	StreamingFirstResultTimeoutSeconds: 0,
	NonStreamingResponseTimeoutSeconds: 0,
	PriorityDeduction:                  0,
	TimeoutAutoDisableEnabled:          false,
	TimeoutAutoDisableCount:            5,
	TimeoutAutoDisableWindowSeconds:    3600,
	TimeoutAutoDisableCountScope:       TimeoutAutoDisableCountScopeSameMode,
	TimeoutAutoDisableMinimumSamples:   0,
	// Advanced sample/ratio gates are opt-in. Keeping the ratio at zero by
	// default preserves the low-write count-only mode until an administrator
	// explicitly configures a sample gate.
	TimeoutAutoDisableRatioPercent:         0,
	TimeoutAutoDisableDurationSeconds:      1800,
	TimeoutAutoDisableRecoveryMode:         TimeoutAutoDisableRecoveryProbe,
	TimeoutAutoDisableSuccessfulProbeCount: 1,
	PenaltyCooldownSeconds:                 600,
	RecoveryMode:                           RecoveryModeAutomatic,
	ExtendOnRepeatTimeout:                  true,
	// External delivery is opt-in. Smart Operations still keeps the in-process
	// and Redis-backed activity projection when this switch is false.
	ChannelTimeoutNotificationEnabled: false,
	AlertRepeatIntervalSeconds:        900,
}

const (
	TimeoutAutoDisableCountScopeSameMode = "same_mode"
	TimeoutAutoDisableCountScopeCombined = "combined"
	TimeoutAutoDisableRecoveryProbe      = "probe_then_automatic"
	TimeoutAutoDisableRecoveryManual     = "manual"
	RecoveryModeAutomatic                = "automatic"
	RecoveryModeManual                   = "manual"
)

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("monitor_setting", &monitorSetting)
}

// Normalize applies compatibility defaults after a persisted configuration
// update. Keeping this separate from GetMonitorSetting makes reads side-effect
// free for concurrent relay requests.
func (setting *MonitorSetting) Normalize() {
	if setting == nil {
		return
	}
	if setting.ChannelTestMode != ChannelTestModePassiveRecovery {
		setting.ChannelTestMode = ChannelTestModeScheduledAll
	}
	if setting.TimeoutAutoDisableCountScope != TimeoutAutoDisableCountScopeCombined {
		setting.TimeoutAutoDisableCountScope = TimeoutAutoDisableCountScopeSameMode
	}
	if setting.TimeoutAutoDisableRecoveryMode != TimeoutAutoDisableRecoveryManual {
		setting.TimeoutAutoDisableRecoveryMode = TimeoutAutoDisableRecoveryProbe
	}
	if setting.RecoveryMode != RecoveryModeManual {
		setting.RecoveryMode = RecoveryModeAutomatic
	}
}

func GetMonitorSetting() *MonitorSetting {
	frequencyValue, hasFrequency := os.LookupEnv("CHANNEL_TEST_FREQUENCY")
	enabledValue, hasEnabled := os.LookupEnv("CHANNEL_TEST_ENABLED")
	if !hasFrequency && !hasEnabled {
		return &monitorSetting
	}

	effective := monitorSetting
	if frequencyValue != "" {
		frequency, err := strconv.Atoi(frequencyValue)
		if err == nil && frequency > 0 {
			effective.AutoTestChannelEnabled = true
			effective.AutoTestChannelMinutes = float64(frequency)
			effective.ChannelTestMode = ChannelTestModeScheduledAll
		}
	}
	if hasEnabled {
		parsed, err := strconv.ParseBool(enabledValue)
		if err == nil {
			effective.AutoTestChannelEnabled = parsed
		}
	}
	effective.Normalize()
	return &effective
}

// ValidateMonitorSettingOption validates one persisted option without
// requiring a database schema change. Cross-field validation belongs to the
// future atomic policy update endpoint.
func ValidateMonitorSettingOption(key, value string) error {
	const prefix = "monitor_setting."
	if !strings.HasPrefix(key, prefix) {
		return nil
	}
	field := strings.TrimPrefix(key, prefix)
	switch field {
	case "auto_priority_demotion_enabled", "timeout_auto_disable_enabled",
		"extend_on_repeat_timeout", "channel_timeout_notification_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be boolean", key)
		}
	case "streaming_first_result_timeout_seconds", "non_streaming_response_timeout_seconds":
		return validateMonitorInt(key, value, 0, 86400)
	case "priority_deduction":
		return validateMonitorInt(key, value, 0, 1<<31-1)
	case "timeout_auto_disable_count":
		return validateMonitorInt(key, value, 2, 1<<31-1)
	case "timeout_auto_disable_window_seconds":
		return validateMonitorInt(key, value, 60, 2592000)
	case "timeout_auto_disable_minimum_samples":
		return validateMonitorInt(key, value, 0, 1<<31-1)
	case "timeout_auto_disable_duration_seconds":
		return validateMonitorInt(key, value, 0, 604800)
	case "timeout_auto_disable_successful_probe_count":
		return validateMonitorInt(key, value, 1, 10)
	case "penalty_cooldown_seconds", "alert_repeat_interval_seconds":
		return validateMonitorInt(key, value, 0, 2592000)
	case "timeout_auto_disable_ratio_percent":
		ratio, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(ratio) || ratio < 0 || ratio > 100 {
			return fmt.Errorf("%s must be between 0 and 100", key)
		}
	case "timeout_auto_disable_count_scope":
		if value != TimeoutAutoDisableCountScopeSameMode && value != TimeoutAutoDisableCountScopeCombined {
			return fmt.Errorf("%s has an invalid value", key)
		}
	case "timeout_auto_disable_recovery_mode":
		if value != TimeoutAutoDisableRecoveryProbe && value != TimeoutAutoDisableRecoveryManual {
			return fmt.Errorf("%s has an invalid value", key)
		}
	case "recovery_mode":
		if value != RecoveryModeAutomatic && value != RecoveryModeManual {
			return fmt.Errorf("%s has an invalid value", key)
		}
	}
	return nil
}

func validateMonitorInt(key, value string, lo, hi int) error {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < int64(lo) || parsed > int64(hi) {
		return fmt.Errorf("%s must be an integer between %d and %d", key, lo, hi)
	}
	return nil
}

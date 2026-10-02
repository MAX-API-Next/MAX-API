package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func withSmartOpsDeliveryRedis(t *testing.T) {
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

func TestSmartOpsAlertDeliveryStateIsTraceableInRedis(t *testing.T) {
	withSmartOpsDeliveryRedis(t)
	alert := SmartOpsAlert{Key: "channel_timeout_auto_disabled:936", Node: "node-a", Status: smartOpsAlertStatusFiring}

	recordSmartOpsAlertDeliveryState(alert, "queued", 0, "")
	queued := alert
	loadSmartOpsAlertDeliveryState(&queued)
	require.Equal(t, "queued", queued.DeliveryStatus)
	require.Zero(t, queued.DeliveryAttempts)

	recordSmartOpsAlertDeliveryState(alert, "failed", 3, "temporary delivery failure")
	failed := alert
	loadSmartOpsAlertDeliveryState(&failed)
	require.Equal(t, "failed", failed.DeliveryStatus)
	require.Equal(t, 3, failed.DeliveryAttempts)
	require.Equal(t, "temporary delivery failure", failed.DeliveryError)
	require.False(t, failed.DeliveryUpdatedAt.IsZero())
}

func TestSmartOpsAlertDeliveryProjectionRejectsOlderStatus(t *testing.T) {
	withSmartOpsDeliveryRedis(t)
	eventAt := time.Now().UTC().Truncate(time.Millisecond)
	alert := SmartOpsAlert{
		Key:        "channel_timeout_auto_disabled:936",
		Node:       "node-a",
		Status:     smartOpsAlertStatusFiring,
		ObservedAt: eventAt,
	}

	recordSmartOpsAlertDeliveryState(alert, "sent", 1, "")
	recordSmartOpsAlertDeliveryState(alert, "queued", 0, "")
	recordSmartOpsAlertDeliveryState(alert, "failed", 1, "late queue projection")

	state := alert
	loadSmartOpsAlertDeliveryState(&state)
	require.Equal(t, "sent", state.DeliveryStatus)
	require.Equal(t, 1, state.DeliveryAttempts)
}

func TestDeliverSmartOpsAlertRecordsSentDeliveryState(t *testing.T) {
	withSmartOpsDeliveryRedis(t)
	originalDelays := smartOpsAlertRetryDelays
	originalLoader := smartOpsAlertLoadRecipients
	originalLimit := smartOpsAlertCheckNotificationLimit
	originalSender := smartOpsAlertSendToRecipient
	t.Cleanup(func() {
		smartOpsAlertRetryDelays = originalDelays
		smartOpsAlertLoadRecipients = originalLoader
		smartOpsAlertCheckNotificationLimit = originalLimit
		smartOpsAlertSendToRecipient = originalSender
	})

	smartOpsAlertRetryDelays = []time.Duration{0}
	smartOpsAlertLoadRecipients = func() ([]smartOpsAlertRecipient, error) {
		return []smartOpsAlertRecipient{{ID: 1}}, nil
	}
	smartOpsAlertCheckNotificationLimit = func(int, string) (bool, error) { return true, nil }
	smartOpsAlertSendToRecipient = func(smartOpsAlertRecipient, dto.Notify) error { return nil }

	alert := SmartOpsAlert{Key: "channel_timeout_priority_demotion:936", Node: "node-a", Status: smartOpsAlertStatusFiring}
	require.NoError(t, deliverSmartOpsAlertNotification(alert))
	loadSmartOpsAlertDeliveryState(&alert)
	require.Equal(t, "sent", alert.DeliveryStatus)
	require.Equal(t, 1, alert.DeliveryAttempts)
}

func TestDeliverSmartOpsAlertHonorsSharedRepeatInterval(t *testing.T) {
	withSmartOpsDeliveryRedis(t)
	originalSetting := *operation_setting.GetMonitorSetting()
	originalDelays := smartOpsAlertRetryDelays
	originalLoader := smartOpsAlertLoadRecipients
	originalLimit := smartOpsAlertCheckNotificationLimit
	originalSender := smartOpsAlertSendToRecipient
	t.Cleanup(func() {
		*operation_setting.GetMonitorSetting() = originalSetting
		smartOpsAlertRetryDelays = originalDelays
		smartOpsAlertLoadRecipients = originalLoader
		smartOpsAlertCheckNotificationLimit = originalLimit
		smartOpsAlertSendToRecipient = originalSender
	})

	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AlertRepeatIntervalSeconds: 60,
	}
	smartOpsAlertRetryDelays = []time.Duration{0}
	smartOpsAlertLoadRecipients = func() ([]smartOpsAlertRecipient, error) {
		return []smartOpsAlertRecipient{{ID: 1, Email: "admin@example.com"}}, nil
	}
	smartOpsAlertCheckNotificationLimit = func(int, string) (bool, error) { return true, nil }
	sendCount := 0
	smartOpsAlertSendToRecipient = func(smartOpsAlertRecipient, dto.Notify) error {
		sendCount++
		return nil
	}

	alert := SmartOpsAlert{
		Key:       "channel_timeout_priority_demotion:936",
		Node:      "node-a",
		Status:    smartOpsAlertStatusFiring,
		Component: "channel",
	}
	require.NoError(t, deliverSmartOpsAlertNotification(alert))
	require.NoError(t, deliverSmartOpsAlertNotification(alert))
	require.Equal(t, 1, sendCount)
	alertState := alert
	loadSmartOpsAlertDeliveryState(&alertState)
	require.Equal(t, "skipped_repeat", alertState.DeliveryStatus)
}

func TestDeliverSmartOpsAlertReleasesRepeatIntervalAfterFailure(t *testing.T) {
	withSmartOpsDeliveryRedis(t)
	originalSetting := *operation_setting.GetMonitorSetting()
	originalDelays := smartOpsAlertRetryDelays
	originalLoader := smartOpsAlertLoadRecipients
	originalLimit := smartOpsAlertCheckNotificationLimit
	originalSender := smartOpsAlertSendToRecipient
	t.Cleanup(func() {
		*operation_setting.GetMonitorSetting() = originalSetting
		smartOpsAlertRetryDelays = originalDelays
		smartOpsAlertLoadRecipients = originalLoader
		smartOpsAlertCheckNotificationLimit = originalLimit
		smartOpsAlertSendToRecipient = originalSender
	})

	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AlertRepeatIntervalSeconds: 60,
	}
	smartOpsAlertRetryDelays = nil
	smartOpsAlertLoadRecipients = func() ([]smartOpsAlertRecipient, error) {
		return []smartOpsAlertRecipient{{ID: 1, Email: "admin@example.com"}}, nil
	}
	smartOpsAlertCheckNotificationLimit = func(int, string) (bool, error) { return true, nil }
	sendCount := 0
	smartOpsAlertSendToRecipient = func(smartOpsAlertRecipient, dto.Notify) error {
		sendCount++
		if sendCount == 1 {
			return fmt.Errorf("temporary delivery failure")
		}
		return nil
	}

	alert := SmartOpsAlert{
		Key:       "channel_timeout_priority_demotion:936",
		Node:      "node-a",
		Status:    smartOpsAlertStatusFiring,
		Component: "channel",
	}
	require.Error(t, deliverSmartOpsAlertNotification(alert))
	require.NoError(t, deliverSmartOpsAlertNotification(alert))
	require.Equal(t, 2, sendCount)
}

func TestClearSmartOpsAlertRepeatIntervalKeepsNewerLock(t *testing.T) {
	withSmartOpsDeliveryRedis(t)
	originalSetting := *operation_setting.GetMonitorSetting()
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = originalSetting })
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AlertRepeatIntervalSeconds: 60,
	}
	alert := SmartOpsAlert{
		Key:       "channel_timeout_priority_demotion:936",
		Node:      "node-a",
		Status:    smartOpsAlertStatusFiring,
		Component: "channel",
	}
	require.True(t, shouldDeliverSmartOpsAlertNotification(&alert))
	require.NotEmpty(t, alert.repeatLockToken)
	key := smartOpsAlertRepeatKey(alert)
	ctx := context.Background()
	require.NoError(t, maxcommon.RDB.Set(ctx, key, "newer-lock", time.Minute).Err())
	clearSmartOpsAlertRepeatKey(alert)
	value, err := maxcommon.RDB.Get(ctx, key).Result()
	require.NoError(t, err)
	require.Equal(t, "newer-lock", value)
}

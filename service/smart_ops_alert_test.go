package service

import (
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestSmartOpsAlertRequiresConsecutiveSamplesAndNotifiesRecovery(t *testing.T) {
	state := smartOpsAlertState{}
	firstAt := time.Unix(100, 0)

	event := state.observe("system_cpu", 95, 90, firstAt, 2)
	require.Nil(t, event, "a single threshold breach must not page an administrator")
	require.False(t, state.active)

	event = state.observe("system_cpu", 96, 90, firstAt.Add(5*time.Second), 2)
	require.NotNil(t, event)
	require.Equal(t, smartOpsAlertStatusFiring, event.Status)
	require.Equal(t, "system_cpu", event.Key)
	require.True(t, state.active)

	event = state.observe("system_cpu", 97, 90, firstAt.Add(10*time.Second), 2)
	require.Nil(t, event, "an ongoing incident must be deduplicated")

	event = state.observe("system_cpu", 70, 90, firstAt.Add(15*time.Second), 2)
	require.NotNil(t, event)
	require.Equal(t, smartOpsAlertStatusResolved, event.Status)
	require.False(t, state.active)
}

func TestEvaluateSmartOpsSystemAlertsNotifiesOnlyOnTransitions(t *testing.T) {
	originalSender := smartOpsAlertNotificationSender
	t.Cleanup(func() { smartOpsAlertNotificationSender = originalSender })

	smartOpsAlertMonitor.Lock()
	smartOpsAlertMonitor.states = nil
	smartOpsAlertMonitor.active = nil
	smartOpsAlertMonitor.Unlock()

	var events []SmartOpsAlert
	smartOpsAlertNotificationSender = func(alert SmartOpsAlert) {
		events = append(events, alert)
	}

	config := common.PerformanceMonitorConfig{
		Enabled:         true,
		CPUThreshold:    90,
		MemoryThreshold: 0,
		DiskThreshold:   0,
	}
	status := common.SystemStatus{CPUUsage: 95}
	base := time.Unix(200, 0)

	evaluateSmartOpsSystemAlerts(status, config, base)
	evaluateSmartOpsSystemAlerts(status, config, base.Add(5*time.Second))
	evaluateSmartOpsSystemAlerts(status, config, base.Add(10*time.Second))
	status.CPUUsage = 70
	evaluateSmartOpsSystemAlerts(status, config, base.Add(15*time.Second))

	require.Len(t, events, 2)
	require.Equal(t, smartOpsAlertStatusFiring, events[0].Status)
	require.Equal(t, smartOpsAlertStatusResolved, events[1].Status)
}

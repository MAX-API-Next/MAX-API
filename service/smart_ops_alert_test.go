package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
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

func TestSmartOpsAlertDoesNotCountTheSameSampleTwice(t *testing.T) {
	state := smartOpsAlertState{}
	observedAt := time.Unix(150, 0)

	require.Nil(t, state.observe("system_cpu", 95, 90, observedAt, 2))
	event := state.observe("system_cpu", 95, 90, observedAt, 2)

	require.Nil(t, event, "the detector must not turn one sampled value into two consecutive samples")
	require.False(t, state.active)
	require.Equal(t, 1, state.consecutive)
}

func TestDisablingSmartOpsThresholdSuppressesWithoutRecoveryNotification(t *testing.T) {
	state := smartOpsAlertState{active: true, consecutive: 2}
	disabledAt := time.Unix(175, 0)

	event := state.observe("system_cpu", 95, 0, disabledAt, 2)

	require.Nil(t, event, "disabling a detector is not a resource recovery")
	require.False(t, state.active)
	require.Zero(t, state.consecutive)
	require.Equal(t, disabledAt, state.lastObserved, "re-enabling must not count a sample captured before the detector was disabled")
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
	base := time.Unix(200, 0)
	status := common.SystemStatus{CPUUsage: 95, CPUValid: true, ObservedAt: base}

	evaluateSmartOpsSystemAlerts(status, config, base)
	status.ObservedAt = base.Add(5 * time.Second)
	evaluateSmartOpsSystemAlerts(status, config, base.Add(5*time.Second))
	status.ObservedAt = base.Add(10 * time.Second)
	evaluateSmartOpsSystemAlerts(status, config, base.Add(10*time.Second))
	status.CPUUsage = 70
	status.ObservedAt = base.Add(15 * time.Second)
	evaluateSmartOpsSystemAlerts(status, config, base.Add(15*time.Second))

	require.Len(t, events, 2)
	require.Equal(t, smartOpsAlertStatusFiring, events[0].Status)
	require.Equal(t, smartOpsAlertStatusResolved, events[1].Status)
}

func TestEvaluateSmartOpsSystemAlertsDoesNotResolveFromInvalidProbe(t *testing.T) {
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

	config := common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90}
	base := time.Unix(300, 0)
	status := common.SystemStatus{CPUUsage: 95, CPUValid: true, ObservedAt: base}
	evaluateSmartOpsSystemAlerts(status, config, base)
	status.ObservedAt = base.Add(5 * time.Second)
	evaluateSmartOpsSystemAlerts(status, config, base.Add(5*time.Second))

	status = common.SystemStatus{ObservedAt: base.Add(10 * time.Second)}
	evaluateSmartOpsSystemAlerts(status, config, base.Add(10*time.Second))

	require.Len(t, events, 1)
	require.Equal(t, smartOpsAlertStatusFiring, events[0].Status)
	require.Len(t, GetSmartOpsAlerts(), 1, "an unknown sample must preserve the active incident")
}

func TestSmartOpsAlertNotifyTypeSeparatesNodes(t *testing.T) {
	alert := SmartOpsAlert{Key: "system_cpu", Node: "node-a", Status: smartOpsAlertStatusFiring}
	nodeAType := smartOpsAlertNotifyType(alert)
	alert.Node = "node-b"
	nodeBType := smartOpsAlertNotifyType(alert)

	require.NotEqual(t, nodeAType, nodeBType)
	require.Contains(t, nodeAType, "node-a")
	require.Contains(t, nodeBType, "node-b")

	alert.Node = "node-a"
	alert.Status = smartOpsAlertStatusResolved
	require.NotEqual(t, nodeAType, smartOpsAlertNotifyType(alert), "recovery events must not consume the firing-event budget")
}

func TestDeliverSmartOpsAlertRetriesOnlyFailedRecipients(t *testing.T) {
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

	smartOpsAlertRetryDelays = []time.Duration{0, 0}
	smartOpsAlertLoadRecipients = func() ([]smartOpsAlertRecipient, error) {
		return []smartOpsAlertRecipient{{ID: 1}, {ID: 2}}, nil
	}
	limitChecks := map[int]int{}
	smartOpsAlertCheckNotificationLimit = func(userID int, _ string) (bool, error) {
		limitChecks[userID]++
		return true, nil
	}
	sendAttempts := map[int]int{}
	var deliveredNotification dto.Notify
	smartOpsAlertSendToRecipient = func(recipient smartOpsAlertRecipient, notification dto.Notify) error {
		sendAttempts[recipient.ID]++
		if recipient.ID == 2 && sendAttempts[recipient.ID] < 3 {
			return fmt.Errorf("temporary delivery failure")
		}
		deliveredNotification = notification
		return nil
	}

	err := deliverSmartOpsAlertNotification(SmartOpsAlert{
		Key:        "system_cpu",
		Node:       "node-a",
		Status:     smartOpsAlertStatusFiring,
		ObservedAt: time.Unix(400, 0),
	})

	require.NoError(t, err)
	require.Equal(t, map[int]int{1: 1, 2: 1}, limitChecks, "a retry must not consume a new logical notification slot")
	require.Equal(t, 1, sendAttempts[1], "successful administrators must not receive duplicates")
	require.Equal(t, 3, sendAttempts[2], "only the failed administrator should be retried")
	require.Contains(t, deliveredNotification.Content, "查看：/smart-ops/alerts")
}

func TestDeliverSmartOpsAlertRetriesRecipientLookup(t *testing.T) {
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
	loadAttempts := 0
	smartOpsAlertLoadRecipients = func() ([]smartOpsAlertRecipient, error) {
		loadAttempts++
		if loadAttempts == 1 {
			return nil, fmt.Errorf("temporary database failure")
		}
		return []smartOpsAlertRecipient{{ID: 1}}, nil
	}
	smartOpsAlertCheckNotificationLimit = func(_ int, _ string) (bool, error) { return true, nil }
	sent := 0
	smartOpsAlertSendToRecipient = func(_ smartOpsAlertRecipient, _ dto.Notify) error {
		sent++
		return nil
	}

	err := deliverSmartOpsAlertNotification(SmartOpsAlert{Key: "system_cpu", Node: "node-a"})

	require.NoError(t, err)
	require.Equal(t, 2, loadAttempts)
	require.Equal(t, 1, sent)
}

func TestSmartOpsAlertNotificationPoolRunsIndependentIncidentsConcurrentlyAndPreservesOrder(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	independentDelivered := make(chan struct{}, 1)
	incidentOrder := make(chan string, 2)

	pool := newSmartOpsAlertNotificationPool(2, 4, func(alert SmartOpsAlert) error {
		if alert.Key == "system_cpu" && alert.Node == "node-a" {
			if alert.Status == smartOpsAlertStatusFiring {
				close(firstStarted)
				<-releaseFirst
			}
			incidentOrder <- alert.Status
			return nil
		}
		independentDelivered <- struct{}{}
		return nil
	})
	t.Cleanup(pool.close)

	firing := SmartOpsAlert{Key: "system_cpu", Node: "node-a", Status: smartOpsAlertStatusFiring}
	resolved := firing
	resolved.Status = smartOpsAlertStatusResolved
	require.Equal(t, pool.workerIndex(firing), pool.workerIndex(resolved), "one incident must stay on one ordered worker")

	independent := SmartOpsAlert{Key: "system_memory", Node: "node-b", Status: smartOpsAlertStatusFiring}
	if pool.workerIndex(independent) == pool.workerIndex(firing) {
		independent = SmartOpsAlert{Key: "system_disk", Node: "node-c", Status: smartOpsAlertStatusFiring}
	}
	require.NotEqual(t, pool.workerIndex(firing), pool.workerIndex(independent), "the regression fixture must use another worker")

	require.True(t, pool.enqueue(firing))
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("the first incident did not start delivery")
	}

	require.True(t, pool.enqueue(resolved))
	require.True(t, pool.enqueue(independent))
	select {
	case <-independentDelivered:
	case <-time.After(time.Second):
		t.Fatal("an unrelated alert was blocked behind a slow incident")
	}

	close(releaseFirst)
	for _, expected := range []string{smartOpsAlertStatusFiring, smartOpsAlertStatusResolved} {
		select {
		case actual := <-incidentOrder:
			require.Equal(t, expected, actual)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s delivery", expected)
		}
	}
}

package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
)

const (
	smartOpsAlertStatusFiring    = "firing"
	smartOpsAlertStatusResolved  = "resolved"
	smartOpsAlertSeverityWarning = "warning"
	smartOpsAlertSampleInterval  = 5 * time.Second
	smartOpsAlertRequiredSamples = 2
)

// SmartOpsAlert is the read-only incident projection exposed to administrators.
// It intentionally contains only resource metadata and never includes secrets,
// request bodies, channel keys, or billing data.
type SmartOpsAlert struct {
	Key          string    `json:"key"`
	Status       string    `json:"status"`
	Severity     string    `json:"severity"`
	Component    string    `json:"component"`
	Node         string    `json:"node,omitempty"`
	CurrentValue float64   `json:"current_value"`
	Threshold    float64   `json:"threshold"`
	ObservedAt   time.Time `json:"observed_at"`
	Message      string    `json:"message"`
}

type smartOpsAlertEvent struct {
	Key          string
	Status       string
	CurrentValue float64
	Threshold    float64
	ObservedAt   time.Time
}

type smartOpsAlertState struct {
	active       bool
	consecutive  int
	lastValue    float64
	lastObserved time.Time
}

func (state *smartOpsAlertState) observe(key string, value, threshold float64, observedAt time.Time, requiredSamples int) *smartOpsAlertEvent {
	if threshold <= 0 {
		state.consecutive = 0
		if !state.active {
			return nil
		}
		state.active = false
		return &smartOpsAlertEvent{Key: key, Status: smartOpsAlertStatusResolved, CurrentValue: value, Threshold: threshold, ObservedAt: observedAt}
	}

	state.lastValue = value
	state.lastObserved = observedAt
	if value <= threshold {
		state.consecutive = 0
		if !state.active {
			return nil
		}
		state.active = false
		return &smartOpsAlertEvent{Key: key, Status: smartOpsAlertStatusResolved, CurrentValue: value, Threshold: threshold, ObservedAt: observedAt}
	}

	state.consecutive++
	if state.active || state.consecutive < requiredSamples {
		return nil
	}
	state.active = true
	return &smartOpsAlertEvent{Key: key, Status: smartOpsAlertStatusFiring, CurrentValue: value, Threshold: threshold, ObservedAt: observedAt}
}

type smartOpsAlertDefinition struct {
	key       string
	component string
	label     string
	value     func(common.SystemStatus) float64
	threshold func(common.PerformanceMonitorConfig) float64
}

var smartOpsAlertDefinitions = []smartOpsAlertDefinition{
	{
		key:       "system_cpu",
		component: "system",
		label:     "CPU 使用率",
		value:     func(status common.SystemStatus) float64 { return status.CPUUsage },
		threshold: func(config common.PerformanceMonitorConfig) float64 { return float64(config.CPUThreshold) },
	},
	{
		key:       "system_memory",
		component: "system",
		label:     "内存使用率",
		value:     func(status common.SystemStatus) float64 { return status.MemoryUsage },
		threshold: func(config common.PerformanceMonitorConfig) float64 { return float64(config.MemoryThreshold) },
	},
	{
		key:       "system_disk",
		component: "system",
		label:     "磁盘使用率",
		value:     func(status common.SystemStatus) float64 { return status.DiskUsage },
		threshold: func(config common.PerformanceMonitorConfig) float64 { return float64(config.DiskThreshold) },
	},
}

var smartOpsAlertMonitor struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	states map[string]*smartOpsAlertState
	active map[string]SmartOpsAlert
}

var smartOpsAlertNotificationSender = func(alert SmartOpsAlert) {
	go sendSmartOpsAlertNotification(alert)
}

// StartSmartOpsAlertMonitor starts the in-process detector. It is deliberately
// separate from the resource sampler so the existing sampler remains cheap and
// the detector can be disabled or stopped independently during shutdown.
func StartSmartOpsAlertMonitor() {
	smartOpsAlertMonitor.Lock()
	defer smartOpsAlertMonitor.Unlock()
	if smartOpsAlertMonitor.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	smartOpsAlertMonitor.cancel = cancel
	smartOpsAlertMonitor.done = done
	smartOpsAlertMonitor.states = make(map[string]*smartOpsAlertState)
	smartOpsAlertMonitor.active = make(map[string]SmartOpsAlert)
	go func() {
		defer close(done)
		runSmartOpsAlertMonitor(ctx)
	}()
}

func runSmartOpsAlertMonitor(ctx context.Context) {
	ticker := time.NewTicker(smartOpsAlertSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			evaluateSmartOpsSystemAlerts(common.GetSystemStatus(), common.GetPerformanceMonitorConfig(), now)
		}
	}
}

// StopSmartOpsAlertMonitor stops the detector without affecting request
// handling or the resource sampler.
func StopSmartOpsAlertMonitor(ctx context.Context) error {
	smartOpsAlertMonitor.Lock()
	cancel := smartOpsAlertMonitor.cancel
	done := smartOpsAlertMonitor.done
	smartOpsAlertMonitor.Unlock()
	if cancel == nil || done == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
		smartOpsAlertMonitor.Lock()
		if smartOpsAlertMonitor.done == done {
			smartOpsAlertMonitor.cancel = nil
			smartOpsAlertMonitor.done = nil
		}
		smartOpsAlertMonitor.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func evaluateSmartOpsSystemAlerts(status common.SystemStatus, config common.PerformanceMonitorConfig, now time.Time) {
	if !config.Enabled {
		smartOpsAlertMonitor.Lock()
		for _, state := range smartOpsAlertMonitor.states {
			state.active = false
			state.consecutive = 0
		}
		clear(smartOpsAlertMonitor.active)
		smartOpsAlertMonitor.Unlock()
		return
	}

	var events []SmartOpsAlert
	smartOpsAlertMonitor.Lock()
	if smartOpsAlertMonitor.states == nil {
		smartOpsAlertMonitor.states = make(map[string]*smartOpsAlertState)
	}
	if smartOpsAlertMonitor.active == nil {
		smartOpsAlertMonitor.active = make(map[string]SmartOpsAlert)
	}
	for _, definition := range smartOpsAlertDefinitions {
		state := smartOpsAlertMonitor.states[definition.key]
		if state == nil {
			state = &smartOpsAlertState{}
			smartOpsAlertMonitor.states[definition.key] = state
		}
		event := state.observe(
			definition.key,
			definition.value(status),
			definition.threshold(config),
			now,
			smartOpsAlertRequiredSamples,
		)
		if event == nil {
			if state.active {
				alert := smartOpsAlertMonitor.active[definition.key]
				alert.CurrentValue = definition.value(status)
				alert.Threshold = definition.threshold(config)
				alert.ObservedAt = now
				alert.Message = formatSmartOpsAlertMessage(definition.label, &smartOpsAlertEvent{
					Status:       smartOpsAlertStatusFiring,
					CurrentValue: alert.CurrentValue,
					Threshold:    alert.Threshold,
				})
				smartOpsAlertMonitor.active[definition.key] = alert
			}
			continue
		}
		alert := SmartOpsAlert{
			Key:          event.Key,
			Status:       event.Status,
			Severity:     smartOpsAlertSeverityWarning,
			Component:    definition.component,
			Node:         smartOpsAlertNodeName(),
			CurrentValue: event.CurrentValue,
			Threshold:    event.Threshold,
			ObservedAt:   event.ObservedAt,
			Message:      formatSmartOpsAlertMessage(definition.label, event),
		}
		if event.Status == smartOpsAlertStatusFiring {
			smartOpsAlertMonitor.active[event.Key] = alert
		} else {
			delete(smartOpsAlertMonitor.active, event.Key)
		}
		events = append(events, alert)
	}
	smartOpsAlertMonitor.Unlock()

	for _, alert := range events {
		smartOpsAlertNotificationSender(alert)
	}
}

func formatSmartOpsAlertMessage(label string, event *smartOpsAlertEvent) string {
	if event.Status == smartOpsAlertStatusResolved {
		return fmt.Sprintf("%s 已恢复：当前 %.1f%%，阈值 %.1f%%", label, event.CurrentValue, event.Threshold)
	}
	return fmt.Sprintf("%s 超过阈值：当前 %.1f%%，阈值 %.1f%%。请检查智能运维中心", label, event.CurrentValue, event.Threshold)
}

func smartOpsAlertNodeName() string {
	identity := common.GetNodeIdentity()
	if identity.Name != "" {
		return identity.Name
	}
	return "unknown"
}

// GetSmartOpsAlerts returns the currently firing alerts for the administrator
// cockpit. It is a process-local projection and may be empty immediately after
// restart until the sampler produces two consecutive samples.
func GetSmartOpsAlerts() []SmartOpsAlert {
	smartOpsAlertMonitor.Lock()
	defer smartOpsAlertMonitor.Unlock()
	alerts := make([]SmartOpsAlert, 0, len(smartOpsAlertMonitor.active))
	for _, alert := range smartOpsAlertMonitor.active {
		alerts = append(alerts, alert)
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Key < alerts[j].Key })
	return alerts
}

func sendSmartOpsAlertNotification(alert SmartOpsAlert) {
	statusText := "异常"
	if alert.Status == smartOpsAlertStatusResolved {
		statusText = "恢复"
	}
	subject := fmt.Sprintf("智能运维告警：%s", statusText)
	content := fmt.Sprintf("节点：%s\n组件：%s\n%s\n时间：%s\n查看：/smart-ops/channel-performance\n当前告警接口：/api/smart-ops/alerts", alert.Node, alert.Component, alert.Message, alert.ObservedAt.Format(time.RFC3339))
	notifyType := fmt.Sprintf("%s:%s", dto.NotifyTypeSmartOpsAlert, alert.Key)
	if err := NotifyAdminUsers(dto.NewNotify(notifyType, subject, content, nil)); err != nil {
		common.SysLog(fmt.Sprintf("failed to send smart ops alert notification: %s", err.Error()))
	}
}

// NotifyAdminUsers broadcasts an operational alert to enabled administrators
// using each administrator's existing notification method configuration.
func NotifyAdminUsers(data dto.Notify) error {
	if model.DB == nil {
		return fmt.Errorf("database is not initialized")
	}

	var users []model.User
	if err := model.DB.
		Select("id", "email", "role", "status", "setting").
		Where("status = ? AND role >= ?", common.UserStatusEnabled, common.RoleAdminUser).
		Find(&users).Error; err != nil {
		return fmt.Errorf("failed to query smart ops notification users: %w", err)
	}

	var firstErr error
	for _, user := range users {
		if err := NotifyUser(user.Id, user.Email, user.GetSetting(), data); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(fmt.Sprintf("failed to notify admin user %d for smart ops alert: %s", user.Id, err.Error()))
		}
	}
	return firstErr
}

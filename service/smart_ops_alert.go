package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/billing_reconciliation_setting"
)

var ErrInvalidBillingSettlementReconciliationQuery = errors.New("invalid billing settlement reconciliation query")
var ErrInvalidBillingSettlementReconciliationReview = errors.New("invalid billing settlement reconciliation review")

const (
	smartOpsAlertStatusFiring    = "firing"
	smartOpsAlertStatusResolved  = "resolved"
	smartOpsAlertSeverityWarning = "warning"
	smartOpsAlertSampleInterval  = 5 * time.Second
	smartOpsAlertRequiredSamples = 2
	smartOpsAlertQueueSize       = 24
	smartOpsAlertWorkerCount     = 3
)

var smartOpsAlertRetryDelays = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	45 * time.Second,
}

// SmartOpsAlert is the read-only incident projection exposed to administrators.
// It intentionally excludes secrets, request bodies, channel keys, and
// mutation controls. Component-specific values retain their own units.
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

func (state *smartOpsAlertState) suppress(observedAt time.Time) {
	state.active = false
	state.consecutive = 0
	state.lastValue = 0
	state.lastObserved = observedAt
}

func (state *smartOpsAlertState) observe(key string, value, threshold float64, observedAt time.Time, requiredSamples int) *smartOpsAlertEvent {
	if threshold <= 0 {
		state.suppress(observedAt)
		return nil
	}
	if observedAt.IsZero() || !observedAt.After(state.lastObserved) {
		return nil
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
	valid     func(common.SystemStatus) bool
	threshold func(common.PerformanceMonitorConfig) float64
}

var smartOpsAlertDefinitions = []smartOpsAlertDefinition{
	{
		key:       "system_cpu",
		component: "system",
		label:     "CPU 使用率",
		value:     func(status common.SystemStatus) float64 { return status.CPUUsage },
		valid:     func(status common.SystemStatus) bool { return status.CPUValid },
		threshold: func(config common.PerformanceMonitorConfig) float64 { return float64(config.CPUThreshold) },
	},
	{
		key:       "system_memory",
		component: "system",
		label:     "内存使用率",
		value:     func(status common.SystemStatus) float64 { return status.MemoryUsage },
		valid:     func(status common.SystemStatus) bool { return status.MemoryValid },
		threshold: func(config common.PerformanceMonitorConfig) float64 { return float64(config.MemoryThreshold) },
	},
	{
		key:       "system_disk",
		component: "system",
		label:     "磁盘使用率",
		value:     func(status common.SystemStatus) float64 { return status.DiskUsage },
		valid:     func(status common.SystemStatus) bool { return status.DiskValid },
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

var smartOpsAlertNotificationSender = enqueueSmartOpsAlertNotification

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
	if smartOpsAlertMonitor.active == nil {
		smartOpsAlertMonitor.active = make(map[string]SmartOpsAlert)
	}
	clearSmartOpsSystemAlertsLocked()
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

func evaluateSmartOpsSystemAlerts(status common.SystemStatus, config common.PerformanceMonitorConfig, _ time.Time) {
	if !config.Enabled {
		smartOpsAlertMonitor.Lock()
		for _, state := range smartOpsAlertMonitor.states {
			state.suppress(status.ObservedAt)
		}
		clearSmartOpsSystemAlertsLocked()
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
		threshold := definition.threshold(config)
		if threshold <= 0 {
			state.suppress(status.ObservedAt)
			delete(smartOpsAlertMonitor.active, definition.key)
			continue
		}
		if !definition.valid(status) || status.ObservedAt.IsZero() || !status.ObservedAt.After(state.lastObserved) {
			continue
		}
		value := definition.value(status)
		event := state.observe(
			definition.key,
			value,
			threshold,
			status.ObservedAt,
			smartOpsAlertRequiredSamples,
		)
		if event == nil {
			if state.active {
				alert := smartOpsAlertMonitor.active[definition.key]
				alert.CurrentValue = value
				alert.Threshold = threshold
				alert.ObservedAt = status.ObservedAt
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

func clearSmartOpsSystemAlertsLocked() {
	for _, definition := range smartOpsAlertDefinitions {
		delete(smartOpsAlertMonitor.active, definition.key)
	}
}

func projectSmartOpsActiveAlert(alert SmartOpsAlert) {
	smartOpsAlertMonitor.Lock()
	defer smartOpsAlertMonitor.Unlock()
	if smartOpsAlertMonitor.active == nil {
		smartOpsAlertMonitor.active = make(map[string]SmartOpsAlert)
	}
	if alert.Status == smartOpsAlertStatusResolved {
		delete(smartOpsAlertMonitor.active, alert.Key)
		return
	}
	smartOpsAlertMonitor.active[alert.Key] = alert
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

// GetBillingSettlementReconciliation returns bounded, read-only evidence for
// open administrator alerts. It never retries or mutates financial state.
func GetBillingSettlementReconciliation(limit int) (model.BillingSettlementReconciliationData, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return model.BillingSettlementReconciliationData{}, fmt.Errorf(
			"%w: limit must be between 1 and 200",
			ErrInvalidBillingSettlementReconciliationQuery,
		)
	}
	return model.GetUnresolvedPositiveFinalizeSettlements(limit)
}

func UpdateBillingSettlementBlockingPolicy(blockUserByDefault bool) error {
	return model.UpdateOption(
		billing_reconciliation_setting.OptionKeyBlockUserByDefault,
		strconv.FormatBool(blockUserByDefault),
	)
}

func validateBillingSettlementReviewNote(note string) error {
	noteLength := len([]rune(note))
	if noteLength < 3 || noteLength > 1000 {
		return fmt.Errorf(
			"%w: note must contain between 3 and 1000 characters",
			ErrInvalidBillingSettlementReconciliationReview,
		)
	}
	return nil
}

func ReviewBillingSettlement(id int64, reviewerID int, blockUser *bool, note string) (model.BillingSettlement, error) {
	if id <= 0 || reviewerID <= 0 || blockUser == nil {
		return model.BillingSettlement{}, ErrInvalidBillingSettlementReconciliationReview
	}
	note = strings.TrimSpace(note)
	if err := validateBillingSettlementReviewNote(note); err != nil {
		return model.BillingSettlement{}, err
	}
	note = strings.TrimSpace(common.SanitizePersistedLogContent(common.MaskSensitiveInfo(note)))
	if err := validateBillingSettlementReviewNote(note); err != nil {
		return model.BillingSettlement{}, err
	}
	record, err := model.ReviewBillingSettlement(id, reviewerID, *blockUser, note)
	if err != nil {
		return model.BillingSettlement{}, err
	}
	refreshBillingSettlementBacklogAfterReview()
	return record, nil
}

// ReviewBillingSettlements validates and atomically closes current billing
// reconciliation alerts without changing their financial settlement state.
func ReviewBillingSettlements(targets []model.BillingSettlementReviewTarget, reviewerID int) ([]model.BillingSettlement, error) {
	if reviewerID <= 0 || len(targets) == 0 || len(targets) > 200 {
		return nil, ErrInvalidBillingSettlementReconciliationReview
	}
	seen := make(map[int64]struct{}, len(targets))
	for _, target := range targets {
		if target.ID <= 0 || target.Revision <= 0 {
			return nil, ErrInvalidBillingSettlementReconciliationReview
		}
		if _, exists := seen[target.ID]; exists {
			return nil, ErrInvalidBillingSettlementReconciliationReview
		}
		seen[target.ID] = struct{}{}
	}
	records, err := model.ReviewBillingSettlements(targets, reviewerID)
	if err != nil {
		return nil, err
	}
	refreshBillingSettlementBacklogAfterReview()
	return records, nil
}

type smartOpsAlertRecipient struct {
	ID      int
	Email   string
	Setting dto.UserSetting
}

type smartOpsAlertNotificationPool struct {
	queues    []chan SmartOpsAlert
	deliver   func(SmartOpsAlert) error
	closeOnce sync.Once
	waitGroup sync.WaitGroup
}

func newSmartOpsAlertNotificationPool(workerCount, queueSize int, deliver func(SmartOpsAlert) error) *smartOpsAlertNotificationPool {
	if workerCount < 1 {
		workerCount = 1
	}
	if queueSize < workerCount {
		queueSize = workerCount
	}

	pool := &smartOpsAlertNotificationPool{
		queues:  make([]chan SmartOpsAlert, workerCount),
		deliver: deliver,
	}
	baseCapacity := queueSize / workerCount
	extraCapacity := queueSize % workerCount
	for worker := range workerCount {
		capacity := baseCapacity
		if worker < extraCapacity {
			capacity++
		}
		pool.queues[worker] = make(chan SmartOpsAlert, capacity)
		pool.waitGroup.Add(1)
		go pool.runWorker(pool.queues[worker])
	}
	return pool
}

func (pool *smartOpsAlertNotificationPool) runWorker(queue <-chan SmartOpsAlert) {
	defer pool.waitGroup.Done()
	for alert := range queue {
		if err := pool.deliver(alert); err != nil {
			common.SysLog(fmt.Sprintf("failed to deliver smart ops alert notification: %s", err.Error()))
		}
	}
}

func (pool *smartOpsAlertNotificationPool) workerIndex(alert SmartOpsAlert) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(alert.Node))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(alert.Key))
	return int(hasher.Sum32() % uint32(len(pool.queues)))
}

func (pool *smartOpsAlertNotificationPool) enqueue(alert SmartOpsAlert) bool {
	queue := pool.queues[pool.workerIndex(alert)]
	select {
	case queue <- alert:
		return true
	default:
		return false
	}
}

func (pool *smartOpsAlertNotificationPool) close() {
	pool.closeOnce.Do(func() {
		for _, queue := range pool.queues {
			close(queue)
		}
		pool.waitGroup.Wait()
	})
}

var smartOpsAlertNotificationQueue struct {
	sync.Once
	pool *smartOpsAlertNotificationPool
}

var smartOpsAlertLoadRecipients = loadSmartOpsAlertRecipients
var smartOpsAlertCheckNotificationLimit = CheckNotificationLimit
var smartOpsAlertSendToRecipient = func(recipient smartOpsAlertRecipient, data dto.Notify) error {
	return sendUserNotification(recipient.ID, recipient.Email, recipient.Setting, data)
}

func enqueueSmartOpsAlertNotification(alert SmartOpsAlert) {
	smartOpsAlertNotificationQueue.Do(func() {
		smartOpsAlertNotificationQueue.pool = newSmartOpsAlertNotificationPool(
			smartOpsAlertWorkerCount,
			smartOpsAlertQueueSize,
			deliverSmartOpsAlertNotification,
		)
	})

	if !smartOpsAlertNotificationQueue.pool.enqueue(alert) {
		common.SysLog(fmt.Sprintf("smart ops alert notification queue is full, dropping %s for node %s", alert.Key, alert.Node))
	}
}

func smartOpsAlertNotification(alert SmartOpsAlert) dto.Notify {
	statusText := "异常"
	if alert.Status == smartOpsAlertStatusResolved {
		statusText = "恢复"
	}
	subject := fmt.Sprintf("智能运维告警：%s", statusText)
	content := fmt.Sprintf("节点：%s\n组件：%s\n%s\n时间：%s\n查看：/smart-ops/alerts\n当前告警接口：/api/smart-ops/alerts", alert.Node, alert.Component, alert.Message, alert.ObservedAt.Format(time.RFC3339))
	return dto.NewNotify(smartOpsAlertNotifyType(alert), subject, content, nil)
}

func smartOpsAlertNotifyType(alert SmartOpsAlert) string {
	return fmt.Sprintf("%s:%s:%s:%s", dto.NotifyTypeSmartOpsAlert, alert.Node, alert.Key, alert.Status)
}

func deliverSmartOpsAlertNotification(alert SmartOpsAlert) error {
	var recipients []smartOpsAlertRecipient
	if err := retrySmartOpsAlertOperation(func() error {
		var err error
		recipients, err = smartOpsAlertLoadRecipients()
		return err
	}); err != nil {
		return fmt.Errorf("failed to load administrator recipients: %w", err)
	}

	notification := smartOpsAlertNotification(alert)
	var firstErr error
	for _, recipient := range recipients {
		allowed, err := checkSmartOpsAlertNotificationLimit(recipient.ID, notification.Type)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(fmt.Sprintf("failed to reserve notification slot for admin user %d: %s", recipient.ID, err.Error()))
			continue
		}
		if !allowed {
			err = fmt.Errorf("notification limit exceeded for admin user %d", recipient.ID)
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(err.Error())
			continue
		}
		if err := retrySmartOpsAlertOperation(func() error {
			return smartOpsAlertSendToRecipient(recipient, notification)
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(fmt.Sprintf("failed to notify admin user %d for smart ops alert after retries: %s", recipient.ID, err.Error()))
		}
	}
	return firstErr
}

func retrySmartOpsAlertOperation(operation func() error) error {
	var err error
	for attempt := 0; attempt <= len(smartOpsAlertRetryDelays); attempt++ {
		if err = operation(); err == nil {
			return nil
		}
		if attempt < len(smartOpsAlertRetryDelays) {
			time.Sleep(smartOpsAlertRetryDelays[attempt])
		}
	}
	return err
}

func checkSmartOpsAlertNotificationLimit(userID int, notifyType string) (bool, error) {
	var allowed bool
	err := retrySmartOpsAlertOperation(func() error {
		var err error
		allowed, err = smartOpsAlertCheckNotificationLimit(userID, notifyType)
		return err
	})
	return allowed, err
}

func loadSmartOpsAlertRecipients() ([]smartOpsAlertRecipient, error) {
	if model.DB == nil {
		return nil, fmt.Errorf("database is not initialized")
	}

	var users []model.User
	if err := model.DB.
		Select("id", "email", "role", "status", "setting").
		Where("status = ? AND role >= ?", common.UserStatusEnabled, common.RoleAdminUser).
		Find(&users).Error; err != nil {
		return nil, fmt.Errorf("failed to query smart ops notification users: %w", err)
	}

	recipients := make([]smartOpsAlertRecipient, 0, len(users))
	for _, user := range users {
		recipients = append(recipients, smartOpsAlertRecipient{
			ID:      user.Id,
			Email:   user.Email,
			Setting: user.GetSetting(),
		})
	}
	return recipients, nil
}

// NotifyAdminUsers broadcasts an operational alert to enabled administrators
// using each administrator's existing notification method configuration.
func NotifyAdminUsers(data dto.Notify) error {
	recipients, err := loadSmartOpsAlertRecipients()
	if err != nil {
		return err
	}

	var firstErr error
	for _, recipient := range recipients {
		if err := NotifyUser(recipient.ID, recipient.Email, recipient.Setting, data); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(fmt.Sprintf("failed to notify admin user %d for smart ops alert: %s", recipient.ID, err.Error()))
		}
	}
	return firstErr
}

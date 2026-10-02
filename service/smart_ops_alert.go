package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/billing_reconciliation_setting"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var ErrInvalidBillingSettlementReconciliationQuery = errors.New("invalid billing settlement reconciliation query")
var ErrInvalidBillingSettlementReconciliationReview = errors.New("invalid billing settlement reconciliation review")
var errManualTaskBillingZeroQuotaRequiresMiniMaxH3 = errors.New("only MiniMax-H3 task settlements can use the zero-quota batch action")

const manualTaskBillingDefaultNote = "Administrator-approved manual task usage settlement"
const manualTaskBillingZeroNote = "Administrator-confirmed zero final quota settlement"

const (
	smartOpsAlertStatusFiring     = "firing"
	smartOpsAlertStatusResolved   = "resolved"
	smartOpsAlertSeverityWarning  = "warning"
	smartOpsAlertSeverityCritical = "critical"
	smartOpsAlertSampleInterval   = 5 * time.Second
	smartOpsAlertRequiredSamples  = 2
	smartOpsAlertQueueSize        = 24
	smartOpsAlertWorkerCount      = 3
	smartOpsAlertDeliveryTTL      = 7 * 24 * time.Hour
	smartOpsAlertDeliveryTimeout  = 100 * time.Millisecond
)

const smartOpsAlertDeliveryKeyPrefix = "maxapi:smart-ops:v1:delivery:"

const smartOpsAlertRepeatKeyPrefix = "maxapi:smart-ops:v1:repeat:"

const smartOpsAlertRepeatReleaseScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`

var smartOpsAlertRepeatTokenCounter atomic.Uint64

var smartOpsAlertRetryDelays = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	45 * time.Second,
}

// SmartOpsAlert is the read-only incident projection exposed to administrators.
// It intentionally excludes secrets, request bodies, channel keys, and
// mutation controls. Component-specific values retain their own units.
type SmartOpsAlert struct {
	Key               string    `json:"key"`
	Status            string    `json:"status"`
	Severity          string    `json:"severity"`
	Component         string    `json:"component"`
	Node              string    `json:"node,omitempty"`
	CurrentValue      float64   `json:"current_value"`
	Threshold         float64   `json:"threshold"`
	ObservedAt        time.Time `json:"observed_at"`
	Message           string    `json:"message"`
	DeliveryStatus    string    `json:"delivery_status,omitempty"`
	DeliveryAttempts  int       `json:"delivery_attempts,omitempty"`
	DeliveryUpdatedAt time.Time `json:"delivery_updated_at,omitempty"`
	DeliveryError     string    `json:"delivery_error,omitempty"`
	repeatLockToken   string
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

func init() {
	// The relay package cannot import service without creating a cycle. A
	// process-local observer keeps the runtime decision in Redis while allowing
	// the existing Smart Operations projector and notification queue to react
	// immediately after a timeout transition.
	common.SetChannelHealthRuntimeTransitionObserver(handleChannelHealthRuntimeTransition)
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

func channelHealthPriorityAlertKey(channelID int) string {
	return fmt.Sprintf("channel_timeout_priority_demotion:%d", channelID)
}

func channelHealthDisabledAlertKey(channelID int) string {
	return fmt.Sprintf("channel_timeout_auto_disabled:%d", channelID)
}

func handleChannelHealthRuntimeTransition(transition common.ChannelHealthRuntimeTransition) {
	if transition.ChannelID <= 0 || transition.Transition == "" {
		return
	}
	observedAt := transition.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}

	var alert SmartOpsAlert
	switch transition.Transition {
	case "priority_demotion_firing", "priority_demotion_updated":
		if transition.RuntimeDisabled {
			return
		}
		alert = SmartOpsAlert{
			Key:          channelHealthPriorityAlertKey(transition.ChannelID),
			Status:       smartOpsAlertStatusFiring,
			Severity:     smartOpsAlertSeverityWarning,
			Component:    "channel",
			Node:         smartOpsAlertNodeName(),
			CurrentValue: float64(transition.Penalty),
			Threshold:    float64(transition.Penalty),
			ObservedAt:   observedAt,
			Message:      formatChannelHealthPriorityAlertMessage(transition),
		}
	case "timeout_auto_disabled":
		// A critical disablement supersedes the warning for the same channel.
		// Remove the old projection before publishing the critical event so the
		// cockpit cannot show two active states for one runtime transition.
		projectSmartOpsActiveAlert(SmartOpsAlert{
			Key:    channelHealthPriorityAlertKey(transition.ChannelID),
			Status: smartOpsAlertStatusResolved,
		})
		alert = SmartOpsAlert{
			Key:          channelHealthDisabledAlertKey(transition.ChannelID),
			Status:       smartOpsAlertStatusFiring,
			Severity:     smartOpsAlertSeverityCritical,
			Component:    "channel",
			Node:         smartOpsAlertNodeName(),
			CurrentValue: 1,
			Threshold:    1,
			ObservedAt:   observedAt,
			Message:      formatChannelHealthDisabledAlertMessage(transition),
		}
	case "runtime_recovered":
		// Recovery must close both possible channel projections. The critical
		// disable alert may have been created before a process restart, while a
		// warning can still exist when an operator restores a penalty directly.
		projectSmartOpsActiveAlert(SmartOpsAlert{
			Key:    channelHealthDisabledAlertKey(transition.ChannelID),
			Status: smartOpsAlertStatusResolved,
		})
		projectSmartOpsActiveAlert(SmartOpsAlert{
			Key:    channelHealthPriorityAlertKey(transition.ChannelID),
			Status: smartOpsAlertStatusResolved,
		})
		if setting := operation_setting.GetMonitorSetting(); setting != nil && setting.ChannelTimeoutNotificationEnabled {
			smartOpsAlertNotificationSender(SmartOpsAlert{
				Key:        channelHealthDisabledAlertKey(transition.ChannelID),
				Status:     smartOpsAlertStatusResolved,
				Severity:   smartOpsAlertSeverityCritical,
				Component:  "channel",
				Node:       smartOpsAlertNodeName(),
				ObservedAt: observedAt,
				Message:    fmt.Sprintf("渠道 %d 已恢复运行态，告警已关闭", transition.ChannelID),
			})
		}
		return
	default:
		return
	}

	// Projection is always updated, even when external notifications are
	// disabled. This keeps the cockpit authoritative and makes notification
	// transport an operator preference rather than a state dependency.
	projectSmartOpsActiveAlert(alert)
	setting := operation_setting.GetMonitorSetting()
	if setting != nil && setting.ChannelTimeoutNotificationEnabled {
		smartOpsAlertNotificationSender(alert)
	}
}

func formatChannelHealthPriorityAlertMessage(transition common.ChannelHealthRuntimeTransition) string {
	message := fmt.Sprintf("渠道 %d 已因上游超时降低运行态优先级，扣减 %d", transition.ChannelID, transition.Penalty)
	if !transition.PenaltyUntil.IsZero() {
		message += "，预计恢复时间 " + transition.PenaltyUntil.Format(time.RFC3339)
	} else {
		message += "，当前需人工恢复"
	}
	return message
}

func formatChannelHealthDisabledAlertMessage(transition common.ChannelHealthRuntimeTransition) string {
	message := fmt.Sprintf("渠道 %d 已因重复上游超时被运行态禁用，源状态 repeated_timeout", transition.ChannelID)
	if !transition.DisabledUntil.IsZero() {
		message += "，计划探测时间 " + transition.DisabledUntil.Format(time.RFC3339)
	} else {
		message += "，当前需人工恢复"
	}
	return message
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
// cockpit. Process-local alerts are merged with the bounded Redis channel
// runtime projection so a process restart does not hide an active channel
// penalty or runtime disablement.
func GetSmartOpsAlerts() []SmartOpsAlert {
	runtimeAlerts, runtimeAvailable := loadChannelHealthSmartOpsAlerts()
	smartOpsAlertMonitor.Lock()
	alertsByKey := make(map[string]SmartOpsAlert, len(smartOpsAlertMonitor.active)+len(runtimeAlerts))
	for key, alert := range smartOpsAlertMonitor.active {
		alertsByKey[key] = alert
	}
	if runtimeAvailable {
		runtimeKeys := make(map[string]struct{}, len(runtimeAlerts))
		for _, alert := range runtimeAlerts {
			runtimeKeys[alert.Key] = struct{}{}
		}
		// A priority penalty can expire without emitting a process-local
		// transition. Once Redis has answered successfully, its projection is
		// authoritative and stale channel alerts must not remain active forever.
		for key := range alertsByKey {
			if strings.HasPrefix(key, "channel_timeout_") {
				if _, ok := runtimeKeys[key]; !ok {
					delete(alertsByKey, key)
				}
			}
		}
	}
	for _, alert := range runtimeAlerts {
		alertsByKey[alert.Key] = alert
	}
	smartOpsAlertMonitor.Unlock()
	alerts := make([]SmartOpsAlert, 0, len(alertsByKey))
	for _, alert := range alertsByKey {
		alerts = append(alerts, alert)
	}
	loadSmartOpsAlertDeliveryStates(alerts)
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Key < alerts[j].Key })
	return alerts
}

func loadChannelHealthSmartOpsAlerts() ([]SmartOpsAlert, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	states, err := channelhealth.ListRuntimeStates(ctx)
	if err != nil {
		// Redis is an optional runtime dependency. Existing process-local alerts
		// remain available when the shared projection cannot be read.
		return nil, false
	}
	setting := operation_setting.GetMonitorSetting()
	now := time.Now()
	alerts := make([]SmartOpsAlert, 0, len(states)*2)
	for _, state := range states {
		observedAt := state.LastObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		if state.RuntimeDisabled {
			alerts = append(alerts, SmartOpsAlert{
				Key:          channelHealthDisabledAlertKey(state.ChannelID),
				Status:       smartOpsAlertStatusFiring,
				Severity:     smartOpsAlertSeverityCritical,
				Component:    "channel",
				Node:         smartOpsAlertNodeName(),
				CurrentValue: float64(channelHealthTimeoutCount(state, setting)),
				Threshold:    float64(channelHealthDisableThreshold(setting)),
				ObservedAt:   observedAt,
				Message: fmt.Sprintf(
					"渠道 %d 已因重复上游超时被运行态禁用，源状态 %s",
					state.ChannelID,
					fallbackChannelHealthSource(state.ActiveSource),
				),
			})
			continue
		}
		if state.Penalty <= 0 || (!state.PenaltyUntil.IsZero() && !now.Before(state.PenaltyUntil)) {
			continue
		}
		transition := common.ChannelHealthRuntimeTransition{
			ChannelID:    state.ChannelID,
			Penalty:      state.Penalty,
			PenaltyUntil: state.PenaltyUntil,
			ObservedAt:   observedAt,
		}
		alerts = append(alerts, SmartOpsAlert{
			Key:          channelHealthPriorityAlertKey(state.ChannelID),
			Status:       smartOpsAlertStatusFiring,
			Severity:     smartOpsAlertSeverityWarning,
			Component:    "channel",
			Node:         smartOpsAlertNodeName(),
			CurrentValue: float64(state.Penalty),
			Threshold:    float64(channelHealthPenaltyThreshold(setting)),
			ObservedAt:   observedAt,
			Message:      formatChannelHealthPriorityAlertMessage(transition),
		})
	}
	return alerts, true
}

func channelHealthTimeoutCount(state channelhealth.RuntimeState, setting *operation_setting.MonitorSetting) int64 {
	if setting != nil && setting.TimeoutAutoDisableCountScope == operation_setting.TimeoutAutoDisableCountScopeCombined {
		return state.TimeoutCount["combined"]
	}
	mode := state.LastTimeoutMode
	if mode == "" {
		// Legacy Redis states do not have last_timeout_mode. Preserve their
		// previous behavior until the next countable timeout writes it.
		switch state.LastEvent {
		case channelhealth.TimeoutKindStreamingFirstResult:
			mode = channelhealth.RequestModeStreaming
		case channelhealth.TimeoutKindNonStreamingResponse:
			mode = channelhealth.RequestModeNonStreaming
		}
	}
	switch mode {
	case channelhealth.RequestModeStreaming:
		return state.TimeoutCount[channelhealth.RequestModeStreaming]
	case channelhealth.RequestModeNonStreaming:
		return state.TimeoutCount[channelhealth.RequestModeNonStreaming]
	default:
		return state.TimeoutCount[channelhealth.RequestModeStreaming] + state.TimeoutCount[channelhealth.RequestModeNonStreaming]
	}
}

func channelHealthDisableThreshold(setting *operation_setting.MonitorSetting) int {
	if setting == nil {
		return 0
	}
	return setting.TimeoutAutoDisableCount
}

func channelHealthPenaltyThreshold(setting *operation_setting.MonitorSetting) int {
	if setting == nil {
		return 0
	}
	return setting.PriorityDeduction
}

func fallbackChannelHealthSource(source string) string {
	if source == "" {
		return "repeated_timeout"
	}
	return source
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

// ManualTaskBillingCompletionResult describes the durable child settlement
// that completed a reconciliation-only task finalization. The operation key
// can be used to trace the idempotent balance mutation and its effect replay.
type ManualTaskBillingCompletionResult struct {
	SettlementID        int64  `json:"settlement_id"`
	TaskID              int64  `json:"task_id"`
	UserID              int    `json:"user_id"`
	OperationKey        string `json:"operation_key"`
	ActualQuota         int64  `json:"actual_quota"`
	AppliedFundingDelta int64  `json:"applied_funding_delta"`
	AlreadyApplied      bool   `json:"already_applied"`
}

type ManualTaskBillingBatchFailure struct {
	SettlementID int64  `json:"settlement_id"`
	Code         string `json:"code"`
	// Message is retained for older clients; new clients must translate Code.
	Message string `json:"message,omitempty"`
}

type ManualTaskBillingBatchCompletionResult struct {
	CompletedCount int                             `json:"completed_count"`
	FailedCount    int                             `json:"failed_count"`
	SettlementIDs  []int64                         `json:"settlement_ids"`
	Failed         []ManualTaskBillingBatchFailure `json:"failed"`
}

// ManualTaskBillingCompletionTarget carries the immutable alert snapshot and
// the exact final quota supplied for that task. ActualQuota is a pointer so an
// omitted amount cannot be confused with an explicit zero-cost settlement.
type ManualTaskBillingCompletionTarget struct {
	ID          int64
	Revision    int64
	ActualQuota *int64
}

func normalizeManualTaskBillingNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		note = manualTaskBillingDefaultNote
	}
	if err := validateBillingSettlementReviewNote(note); err != nil {
		return "", err
	}
	note = strings.TrimSpace(common.SanitizePersistedLogContent(common.MaskSensitiveInfo(note)))
	if err := validateBillingSettlementReviewNote(note); err != nil {
		return "", err
	}
	return note, nil
}

func isGeneratedManualTaskBillingNote(note string) bool {
	return note == manualTaskBillingDefaultNote || note == manualTaskBillingZeroNote
}

// CompleteManualTaskBillingSettlement applies an administrator-approved exact
// final task quota. It never charges above the frozen reservation: the only
// allowed balance mutation is an exact refund (including an explicit zero-cost
// result) or a zero delta. Approval is persisted on the child operation before
// the existing idempotent settlement transaction may mutate any balance.
func CompleteManualTaskBillingSettlement(
	id int64,
	expectedRevision int64,
	reviewerID int,
	actualQuota *int64,
	note string,
) (ManualTaskBillingCompletionResult, error) {
	return completeManualTaskBillingSettlement(id, expectedRevision, reviewerID, actualQuota, note, false)
}

func completeManualTaskBillingSettlement(
	id int64,
	expectedRevision int64,
	reviewerID int,
	actualQuota *int64,
	note string,
	requireMiniMaxH3 bool,
) (ManualTaskBillingCompletionResult, error) {
	if id <= 0 || expectedRevision <= 0 || reviewerID <= 0 || actualQuota == nil {
		return ManualTaskBillingCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
	}
	normalizedNote, noteErr := normalizeManualTaskBillingNote(note)
	if noteErr != nil {
		return ManualTaskBillingCompletionResult{}, noteErr
	}
	note = normalizedNote

	eligibility, err := prepareManualTaskBillingCompletion(id, expectedRevision, *actualQuota, requireMiniMaxH3, note)
	if err != nil {
		return ManualTaskBillingCompletionResult{}, err
	}
	original := eligibility.original
	originalAlreadyResolved := eligibility.originalAlreadyResolved
	validatedNote, replayErr := validateManualTaskBillingCompletionReplay(eligibility, reviewerID, note)
	if replayErr != nil {
		return ManualTaskBillingCompletionResult{}, replayErr
	}
	note = validatedNote
	task := eligibility.task
	input := eligibility.input

	if _, err := model.EnsureManualTaskBillingCompletion(*input, reviewerID, note); err != nil {
		return ManualTaskBillingCompletionResult{}, err
	}
	appliedFundingDelta, childAlreadyApplied, err := model.ApplyBillingSettlementOnce(*input)
	if err != nil {
		return ManualTaskBillingCompletionResult{}, err
	}
	if appliedFundingDelta != input.FundingDelta {
		return ManualTaskBillingCompletionResult{}, model.ErrBillingSettlementOperationConflict
	}
	settledTask, err := model.GetTaskByID(task.ID)
	if err != nil {
		return ManualTaskBillingCompletionResult{}, err
	}
	if int64(settledTask.Quota) != *actualQuota {
		return ManualTaskBillingCompletionResult{}, model.ErrBillingSettlementTaskConflict
	}
	if _, originalResolvedByReplay, err := model.ResolveManualTaskBillingSettlement(
		id,
		expectedRevision,
		reviewerID,
		note,
	); err != nil {
		return ManualTaskBillingCompletionResult{}, err
	} else {
		originalAlreadyResolved = originalAlreadyResolved || originalResolvedByReplay
	}

	// Funding and task quota are already durable. Effect replay is independently
	// idempotent and may remain pending without hiding the completed financial
	// state or blocking the provider-driven terminal task transition.
	if effectErr := model.ProcessBillingSettlementEffect(input.OperationKey); effectErr != nil {
		common.SysLog(fmt.Sprintf("manual task billing settlement effect remains pending: operation=%s error=%v", input.OperationKey, effectErr))
	}
	refreshBillingSettlementBacklogAfterReview()
	return ManualTaskBillingCompletionResult{
		SettlementID:        id,
		TaskID:              task.ID,
		UserID:              original.UserID,
		OperationKey:        input.OperationKey,
		ActualQuota:         *actualQuota,
		AppliedFundingDelta: appliedFundingDelta,
		AlreadyApplied:      originalAlreadyResolved || eligibility.childWasApplied || childAlreadyApplied,
	}, nil
}

// CompleteManualTaskBillingSettlementsZero settles a bounded set of manual
// task-finalization records with an explicit final quota of zero. Each item
// uses the normal idempotent single-record path, so a failure or restart does
// not roll back or duplicate a different item in the same administrator action.
func CompleteManualTaskBillingSettlementsZero(
	targets []model.BillingSettlementReviewTarget,
	reviewerID int,
) (ManualTaskBillingBatchCompletionResult, error) {
	if reviewerID <= 0 || len(targets) == 0 || len(targets) > 200 {
		return ManualTaskBillingBatchCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
	}
	seen := make(map[int64]struct{}, len(targets))
	result := ManualTaskBillingBatchCompletionResult{
		SettlementIDs: make([]int64, 0, len(targets)),
		Failed:        make([]ManualTaskBillingBatchFailure, 0),
	}
	for _, target := range targets {
		if target.ID <= 0 || target.Revision <= 0 {
			return ManualTaskBillingBatchCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
		}
		if _, exists := seen[target.ID]; exists {
			return ManualTaskBillingBatchCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
		}
		seen[target.ID] = struct{}{}
	}
	// Validate the complete selection before the first completion can mutate
	// balances. A stale or non-H3 item therefore cannot leave earlier items
	// settled while the batch request reports a later validation failure.
	validationFailures := make(map[int64]string, len(targets))
	for _, target := range targets {
		if err := validateManualTaskBillingZeroTarget(target, reviewerID); err != nil {
			code := manualTaskBillingBatchErrorCode(err)
			if code == "settlement_failed" {
				common.SysError(fmt.Sprintf(
					"manual task billing batch pre-validation failed: settlement_id=%d error=%s",
					target.ID,
					common.SanitizePersistedLogContent(common.MaskSensitiveInfo(err.Error())),
				))
			}
			validationFailures[target.ID] = code
		}
	}
	if len(validationFailures) > 0 {
		for _, target := range targets {
			code, failed := validationFailures[target.ID]
			if !failed {
				code = "not_attempted"
			}
			appendManualTaskBillingBatchFailure(&result, target.ID, code)
		}
		return result, nil
	}
	for _, target := range targets {
		if _, err := completeManualTaskBillingSettlement(
			target.ID,
			target.Revision,
			reviewerID,
			ptrInt64(0),
			manualTaskBillingZeroNote,
			true,
		); err != nil {
			code := manualTaskBillingBatchErrorCode(err)
			if code == "settlement_failed" {
				common.SysError(fmt.Sprintf(
					"manual task billing batch settlement failed: settlement_id=%d error=%s",
					target.ID,
					common.SanitizePersistedLogContent(common.MaskSensitiveInfo(err.Error())),
				))
			}
			appendManualTaskBillingBatchFailure(&result, target.ID, code)
			continue
		}
		result.CompletedCount++
		result.SettlementIDs = append(result.SettlementIDs, target.ID)
	}
	return result, nil
}

// CompleteManualTaskBillingSettlements applies administrator-supplied exact
// final quotas to a bounded set of task-finalization records. Validation of
// every item runs before the first balance mutation; each mutation then uses
// the existing idempotent single-record path and reports per-item failures.
func CompleteManualTaskBillingSettlements(
	targets []ManualTaskBillingCompletionTarget,
	reviewerID int,
) (ManualTaskBillingBatchCompletionResult, error) {
	if reviewerID <= 0 || len(targets) == 0 || len(targets) > 200 {
		return ManualTaskBillingBatchCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
	}
	seen := make(map[int64]struct{}, len(targets))
	for _, target := range targets {
		if target.ID <= 0 || target.Revision <= 0 || target.ActualQuota == nil {
			return ManualTaskBillingBatchCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
		}
		if _, exists := seen[target.ID]; exists {
			return ManualTaskBillingBatchCompletionResult{}, ErrInvalidBillingSettlementReconciliationReview
		}
		seen[target.ID] = struct{}{}
	}
	result := ManualTaskBillingBatchCompletionResult{
		SettlementIDs: make([]int64, 0, len(targets)),
		Failed:        make([]ManualTaskBillingBatchFailure, 0),
	}
	validationFailures := make(map[int64]string, len(targets))
	for _, target := range targets {
		eligibility, err := prepareManualTaskBillingCompletion(target.ID, target.Revision, *target.ActualQuota, false, manualTaskBillingDefaultNote)
		if err == nil {
			_, err = validateManualTaskBillingCompletionReplay(eligibility, reviewerID, manualTaskBillingDefaultNote)
		}
		if err != nil {
			code := manualTaskBillingBatchErrorCode(err)
			if code == "settlement_failed" {
				common.SysError(fmt.Sprintf(
					"manual task billing exact batch pre-validation failed: settlement_id=%d error=%s",
					target.ID,
					common.SanitizePersistedLogContent(common.MaskSensitiveInfo(err.Error())),
				))
			}
			validationFailures[target.ID] = code
		}
	}
	if len(validationFailures) > 0 {
		for _, target := range targets {
			code, failed := validationFailures[target.ID]
			if !failed {
				code = "not_attempted"
			}
			appendManualTaskBillingBatchFailure(&result, target.ID, code)
		}
		return result, nil
	}
	for _, target := range targets {
		if _, err := completeManualTaskBillingSettlement(
			target.ID,
			target.Revision,
			reviewerID,
			target.ActualQuota,
			manualTaskBillingDefaultNote,
			false,
		); err != nil {
			code := manualTaskBillingBatchErrorCode(err)
			if code == "settlement_failed" {
				common.SysError(fmt.Sprintf(
					"manual task billing exact batch settlement failed: settlement_id=%d error=%s",
					target.ID,
					common.SanitizePersistedLogContent(common.MaskSensitiveInfo(err.Error())),
				))
			}
			appendManualTaskBillingBatchFailure(&result, target.ID, code)
			continue
		}
		result.CompletedCount++
		result.SettlementIDs = append(result.SettlementIDs, target.ID)
	}
	return result, nil
}

func ptrInt64(value int64) *int64 {
	return &value
}

func appendManualTaskBillingBatchFailure(
	result *ManualTaskBillingBatchCompletionResult,
	settlementID int64,
	code string,
) {
	result.Failed = append(result.Failed, ManualTaskBillingBatchFailure{
		SettlementID: settlementID,
		Code:         code,
		Message:      manualTaskBillingBatchErrorMessage(code),
	})
	result.FailedCount = len(result.Failed)
}

type manualTaskBillingEligibility struct {
	original                model.BillingSettlement
	originalAlreadyResolved bool
	task                    *model.Task
	input                   *model.BillingSettlementInput
	childWasApplied         bool
}

// prepareManualTaskBillingCompletion validates the immutable task settlement
// snapshot and builds the exact child input before any balance mutation. Both
// single-record and batch callers use this same financial eligibility contract.
func prepareManualTaskBillingCompletion(
	id int64,
	expectedRevision int64,
	actualQuota int64,
	requireMiniMaxH3 bool,
	note string,
) (manualTaskBillingEligibility, error) {
	original, originalAlreadyResolved, err := model.GetManualTaskBillingSettlement(id, expectedRevision)
	if err != nil {
		return manualTaskBillingEligibility{}, err
	}
	if actualQuota < 0 || actualQuota > original.TaskQuota ||
		original.TaskQuota < 0 || original.TaskQuotaTarget != original.TaskQuota ||
		original.FundingDelta != 0 || original.TokenDelta != 0 ||
		int64(int(original.TaskQuota)) != original.TaskQuota ||
		int64(int(actualQuota)) != actualQuota {
		return manualTaskBillingEligibility{}, ErrInvalidBillingSettlementReconciliationReview
	}

	task, err := model.GetTaskByID(original.TaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return manualTaskBillingEligibility{}, model.ErrBillingSettlementReviewConflict
		}
		return manualTaskBillingEligibility{}, err
	}
	if requireMiniMaxH3 && !model.IsMiniMaxH3Task(task) {
		return manualTaskBillingEligibility{}, errors.Join(
			model.ErrBillingSettlementReviewConflict,
			errManualTaskBillingZeroQuotaRequiresMiniMaxH3,
		)
	}
	expectedSource := model.BillingSettlementSourceWallet
	if taskIsSubscription(task) {
		expectedSource = model.BillingSettlementSourceSubscription
	}
	if original.Source != expectedSource ||
		original.UserID != task.UserId ||
		original.SubscriptionID != task.PrivateData.SubscriptionId ||
		original.TokenID != task.PrivateData.TokenId ||
		original.SubscriptionPreConsumeRequestID != task.PrivateData.BillingRequestId {
		return manualTaskBillingEligibility{}, model.ErrBillingSettlementReviewConflict
	}

	existingChild, childFound, err := model.GetManualTaskBillingCompletion(task.ID)
	if err != nil {
		return manualTaskBillingEligibility{}, err
	}
	childWasApplied := childFound && existingChild.Status == model.BillingSettlementStatusApplied
	if childFound && existingChild.TaskQuotaTarget != actualQuota {
		return manualTaskBillingEligibility{}, model.ErrBillingSettlementReviewConflict
	}
	if int64(task.Quota) != original.TaskQuota &&
		(int64(task.Quota) != actualQuota || !childWasApplied) {
		return manualTaskBillingEligibility{}, model.ErrBillingSettlementReviewConflict
	}

	completionTask := *task
	completionTask.Quota = int(original.TaskQuota)
	var usage *types.TaskUsage
	if completionTask.PrivateData.BillingContext != nil {
		usage = types.CloneTaskUsage(completionTask.PrivateData.BillingContext.TaskUsage)
	}
	input := buildTaskExactFinalSettlementInput(
		&completionTask,
		int(actualQuota),
		usage,
		note,
	)
	if input == nil {
		return manualTaskBillingEligibility{}, model.ErrBillingSettlementReviewConflict
	}
	if completionTask.PrivateData.BillingContext != nil {
		attachTaskUsageEnvelopeMetadata(input, completionTask.PrivateData.BillingContext.TaskUsageEnvelope)
	}
	input.OperationKey = model.BillingTaskManualCompletionOperationKey(task.ID)

	return manualTaskBillingEligibility{
		original:                original,
		originalAlreadyResolved: originalAlreadyResolved,
		task:                    task,
		input:                   input,
		childWasApplied:         childWasApplied,
	}, nil
}

// validateManualTaskBillingCompletionReplay keeps the batch pre-validation
// contract identical to the single-item replay path. A generated note may be
// replaced by the durable note on replay, but a different reviewer or a
// missing durable note is always a conflict.
func validateManualTaskBillingCompletionReplay(
	eligibility manualTaskBillingEligibility,
	reviewerID int,
	note string,
) (string, error) {
	if !eligibility.originalAlreadyResolved {
		return note, nil
	}
	original := eligibility.original
	if original.ReconciliationReviewedBy != reviewerID {
		return "", model.ErrBillingSettlementReviewConflict
	}
	if note != original.ReconciliationReviewNote {
		if !isGeneratedManualTaskBillingNote(note) || strings.TrimSpace(original.ReconciliationReviewNote) == "" {
			return "", model.ErrBillingSettlementReviewConflict
		}
		note = original.ReconciliationReviewNote
	}
	return note, nil
}

// validateManualTaskBillingZeroTarget performs reviewer-specific checks after
// the shared completion eligibility contract has been evaluated.
func validateManualTaskBillingZeroTarget(
	target model.BillingSettlementReviewTarget,
	reviewerID int,
) error {
	eligibility, err := prepareManualTaskBillingCompletion(target.ID, target.Revision, 0, true, manualTaskBillingZeroNote)
	if err != nil {
		return err
	}
	_, err = validateManualTaskBillingCompletionReplay(eligibility, reviewerID, manualTaskBillingZeroNote)
	return err
}

func manualTaskBillingBatchErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidBillingSettlementReconciliationReview):
		return "invalid_settlement_request"
	case errors.Is(err, errManualTaskBillingZeroQuotaRequiresMiniMaxH3):
		return "minimax_h3_required"
	case errors.Is(err, model.ErrBillingSettlementReviewConflict),
		errors.Is(err, model.ErrBillingSettlementTaskConflict),
		errors.Is(err, model.ErrBillingSettlementOperationConflict):
		return "record_conflict"
	case errors.Is(err, model.ErrBillingSettlementManualReview):
		return "manual_review_required"
	case errors.Is(err, model.ErrTokenQuotaInsufficient):
		return "token_quota_inconsistent"
	case errors.Is(err, model.ErrSubscriptionRefundClamped):
		return "subscription_refund_clamped"
	case errors.Is(err, model.ErrSubscriptionSettlementUnbound),
		errors.Is(err, model.ErrSubscriptionSettlementPeriodChanged):
		return "subscription_reservation_invalid"
	default:
		return "settlement_failed"
	}
}

func manualTaskBillingBatchErrorMessage(code string) string {
	switch code {
	case "not_attempted":
		return "not attempted because another selected settlement failed validation"
	case "invalid_settlement_request":
		return "the supplied final quota or the settlement snapshot is invalid for this record"
	case "minimax_h3_required":
		return "only MiniMax-H3 task settlements can use the zero-quota batch action"
	case "record_conflict":
		return "record changed or could not be applied safely; refresh and reconcile it"
	case "manual_review_required":
		return "record still requires a manual financial review"
	case "token_quota_inconsistent":
		return "the token quota mirror is inconsistent; repair the token record"
	case "subscription_refund_clamped":
		return "the subscription usage mirror is lower than the refund"
	case "subscription_reservation_invalid":
		return "the subscription reservation is unbound or its period changed"
	default:
		return "manual task billing settlement failed; inspect the reconciliation record"
	}
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

type smartOpsAlertDeliveryProjection struct {
	alert    SmartOpsAlert
	status   string
	attempts int
	detail   string
	eventAt  time.Time
}

var smartOpsAlertDeliveryProjectionQueue struct {
	sync.Once
	queue chan smartOpsAlertDeliveryProjection
}

func startSmartOpsAlertDeliveryProjectionWorker() {
	smartOpsAlertDeliveryProjectionQueue.Do(func() {
		smartOpsAlertDeliveryProjectionQueue.queue = make(chan smartOpsAlertDeliveryProjection, smartOpsAlertQueueSize)
		go func() {
			for projection := range smartOpsAlertDeliveryProjectionQueue.queue {
				recordSmartOpsAlertDeliveryStateAt(projection.alert, projection.status, projection.attempts, projection.detail, projection.eventAt)
			}
		}()
	})
}

func enqueueSmartOpsAlertDeliveryProjection(projection smartOpsAlertDeliveryProjection) bool {
	startSmartOpsAlertDeliveryProjectionWorker()
	select {
	case smartOpsAlertDeliveryProjectionQueue.queue <- projection:
		return true
	default:
		return false
	}
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
		// The relay/observer path only enqueues. Redis projection writes happen
		// here, on a bounded worker, so a slow Redis cannot delay a response.
		recordSmartOpsAlertDeliveryState(alert, "queued", 0, "")
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
	alert = normalizeSmartOpsAlertObservedAt(alert)
	smartOpsAlertNotificationQueue.Do(func() {
		smartOpsAlertNotificationQueue.pool = newSmartOpsAlertNotificationPool(
			smartOpsAlertWorkerCount,
			smartOpsAlertQueueSize,
			deliverSmartOpsAlertNotification,
		)
	})

	if !smartOpsAlertNotificationQueue.pool.enqueue(alert) {
		eventAt := alert.ObservedAt
		if eventAt.IsZero() {
			eventAt = time.Now()
		}
		if !enqueueSmartOpsAlertDeliveryProjection(smartOpsAlertDeliveryProjection{
			alert:   alert,
			status:  "failed",
			detail:  "notification_queue_full",
			eventAt: eventAt,
		}) {
			// Both queues are intentionally bounded. Losing this optional
			// diagnostic projection is preferable to creating an unbounded
			// goroutine or blocking the relay path.
			common.SysLog(fmt.Sprintf("smart ops alert delivery projection queue is full for %s", alert.Key))
		}
		common.SysLog(fmt.Sprintf("smart ops alert notification queue is full, dropping %s for node %s", alert.Key, alert.Node))
	}
}

func normalizeSmartOpsAlertObservedAt(alert SmartOpsAlert) SmartOpsAlert {
	if alert.ObservedAt.IsZero() {
		alert.ObservedAt = time.Now()
	}
	return alert
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

func smartOpsAlertDeliveryKey(alert SmartOpsAlert) string {
	digest := sha256.Sum256([]byte(smartOpsAlertNotifyType(alert)))
	return fmt.Sprintf("%s%x", smartOpsAlertDeliveryKeyPrefix, digest)
}

func smartOpsAlertRepeatKey(alert SmartOpsAlert) string {
	digest := sha256.Sum256([]byte(alert.Key))
	return fmt.Sprintf("%s%x", smartOpsAlertRepeatKeyPrefix, digest)
}

// shouldDeliverSmartOpsAlertNotification applies the configured interval to
// repeated priority-demotion updates. Critical disablement and all recovery
// events always pass through. Redis failure is fail-open so an operational
// warning is not silently lost.
func shouldDeliverSmartOpsAlertNotification(alert *SmartOpsAlert) bool {
	if alert == nil || alert.Component != "channel" || alert.Status != smartOpsAlertStatusFiring ||
		!strings.HasPrefix(alert.Key, "channel_timeout_priority_demotion:") {
		return true
	}
	setting := operation_setting.GetMonitorSetting()
	if setting == nil || setting.AlertRepeatIntervalSeconds <= 0 || !common.RedisEnabled || common.RDB == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), smartOpsAlertDeliveryTimeout)
	defer cancel()
	token := fmt.Sprintf("%s:%d:%d", alert.Node, time.Now().UnixNano(), smartOpsAlertRepeatTokenCounter.Add(1))
	allowed, err := common.RDB.SetNX(ctx, smartOpsAlertRepeatKey(*alert), token, time.Duration(setting.AlertRepeatIntervalSeconds)*time.Second).Result()
	if err != nil {
		return true
	}
	if allowed {
		alert.repeatLockToken = token
	}
	return allowed
}

func clearSmartOpsAlertRepeatKey(alert SmartOpsAlert) {
	if alert.Component != "channel" || alert.Status != smartOpsAlertStatusFiring ||
		!strings.HasPrefix(alert.Key, "channel_timeout_priority_demotion:") ||
		alert.repeatLockToken == "" ||
		!common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), smartOpsAlertDeliveryTimeout)
	defer cancel()
	_, _ = common.RDB.Eval(ctx, smartOpsAlertRepeatReleaseScript, []string{smartOpsAlertRepeatKey(alert)}, alert.repeatLockToken).Result()
}

func smartOpsAlertDeliveryStatusRank(status string) int {
	switch status {
	case "queued":
		return 10
	case "sending":
		return 20
	case "sent", "skipped_unconfigured":
		return 40
	case "skipped_repeat":
		return 50
	case "failed", "delivery_unknown":
		return 30
	default:
		return 0
	}
}

// recordSmartOpsAlertDeliveryState stores a bounded delivery projection in
// Redis. It is diagnostic and recoverable; failures must never change the
// alert transition or block the notification worker. Status updates use the
// source observation time and a phase rank so an older queued/failed event
// cannot overwrite a newer terminal state when projection queues finish out of
// order.
func recordSmartOpsAlertDeliveryState(alert SmartOpsAlert, status string, attempts int, detail string) {
	eventAt := alert.ObservedAt
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	recordSmartOpsAlertDeliveryStateAt(alert, status, attempts, detail, eventAt)
}

func recordSmartOpsAlertDeliveryStateAt(alert SmartOpsAlert, status string, attempts int, detail string, eventAt time.Time) {
	if !common.RedisEnabled || common.RDB == nil || status == "" {
		return
	}
	if attempts < 0 {
		attempts = 0
	}
	if len(detail) > 512 {
		detail = detail[:512]
	}
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	ctx, cancel := context.WithTimeout(context.Background(), smartOpsAlertDeliveryTimeout)
	defer cancel()
	key := smartOpsAlertDeliveryKey(alert)
	fields := map[string]interface{}{
		"alert_key":            alert.Key,
		"alert_status":         alert.Status,
		"alert_node":           alert.Node,
		"delivery_status":      status,
		"attempts":             strconv.Itoa(attempts),
		"error":                detail,
		"delivery_event_at":    strconv.FormatInt(eventAt.UnixMilli(), 10),
		"delivery_status_rank": strconv.Itoa(smartOpsAlertDeliveryStatusRank(status)),
		"updated_at":           time.Now().UTC().Format(time.RFC3339Nano),
	}
	_ = common.RDB.Watch(ctx, func(tx *redis.Tx) error {
		current, err := tx.HMGet(ctx, key, "delivery_event_at", "delivery_status_rank").Result()
		if err != nil {
			return err
		}
		currentEventAt, _ := strconv.ParseInt(fmt.Sprint(current[0]), 10, 64)
		currentRank, _ := strconv.Atoi(fmt.Sprint(current[1]))
		incomingEventAt := eventAt.UnixMilli()
		incomingRank := smartOpsAlertDeliveryStatusRank(status)
		if currentEventAt > incomingEventAt ||
			(currentEventAt == incomingEventAt && currentRank >= incomingRank) {
			return nil
		}
		pipe := tx.TxPipeline()
		pipe.HSet(ctx, key, fields)
		pipe.Expire(ctx, key, smartOpsAlertDeliveryTTL)
		_, err = pipe.Exec(ctx)
		return err
	}, key)
}

func loadSmartOpsAlertDeliveryState(alert *SmartOpsAlert) {
	if alert == nil {
		return
	}
	alerts := []SmartOpsAlert{*alert}
	loadSmartOpsAlertDeliveryStates(alerts)
	*alert = alerts[0]
}

// loadSmartOpsAlertDeliveryStates reads all delivery projections in one
// bounded Redis pipeline. Delivery metadata is diagnostic, so a failed read
// leaves the alert itself intact and simply omits the optional fields.
func loadSmartOpsAlertDeliveryStates(alerts []SmartOpsAlert) {
	if len(alerts) == 0 || !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), smartOpsAlertDeliveryTimeout)
	defer cancel()
	pipe := common.RDB.Pipeline()
	commands := make([]*redis.StringStringMapCmd, len(alerts))
	for index := range alerts {
		commands[index] = pipe.HGetAll(ctx, smartOpsAlertDeliveryKey(alerts[index]))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}
	for index, command := range commands {
		fields, err := command.Result()
		if err != nil {
			continue
		}
		alerts[index].DeliveryStatus = fields["delivery_status"]
		alerts[index].DeliveryAttempts, _ = strconv.Atoi(fields["attempts"])
		alerts[index].DeliveryError = fields["error"]
		if value := fields["updated_at"]; value != "" {
			alerts[index].DeliveryUpdatedAt, _ = time.Parse(time.RFC3339Nano, value)
		}
	}
}

func deliverSmartOpsAlertNotification(alert SmartOpsAlert) error {
	if !shouldDeliverSmartOpsAlertNotification(&alert) {
		recordSmartOpsAlertDeliveryState(alert, "skipped_repeat", 0, "alert_repeat_interval_seconds")
		return nil
	}
	recordSmartOpsAlertDeliveryState(alert, "sending", 0, "")
	var recipients []smartOpsAlertRecipient
	if err := retrySmartOpsAlertOperation(func() error {
		var err error
		recipients, err = smartOpsAlertLoadRecipients()
		return err
	}); err != nil {
		clearSmartOpsAlertRepeatKey(alert)
		recordSmartOpsAlertDeliveryState(alert, "delivery_unknown", len(smartOpsAlertRetryDelays)+1, err.Error())
		return fmt.Errorf("failed to load administrator recipients: %w", err)
	}
	if len(recipients) == 0 {
		clearSmartOpsAlertRepeatKey(alert)
		recordSmartOpsAlertDeliveryState(alert, "skipped_unconfigured", 0, "no enabled administrator notification recipient")
		return nil
	}

	notification := smartOpsAlertNotification(alert)
	var firstErr error
	deliveryAttempts := 0
	for _, recipient := range recipients {
		allowed, err := checkSmartOpsAlertNotificationLimit(recipient.ID, notification.Type)
		if err != nil {
			deliveryAttempts++
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(fmt.Sprintf("failed to reserve notification slot for admin user %d: %s", recipient.ID, err.Error()))
			continue
		}
		if !allowed {
			deliveryAttempts++
			err = fmt.Errorf("notification limit exceeded for admin user %d", recipient.ID)
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(err.Error())
			continue
		}
		if err := retrySmartOpsAlertOperation(func() error {
			deliveryAttempts++
			return smartOpsAlertSendToRecipient(recipient, notification)
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			common.SysLog(fmt.Sprintf("failed to notify admin user %d for smart ops alert after retries: %s", recipient.ID, err.Error()))
		}
	}
	if firstErr != nil {
		clearSmartOpsAlertRepeatKey(alert)
		recordSmartOpsAlertDeliveryState(alert, "failed", deliveryAttempts, firstErr.Error())
	} else {
		recordSmartOpsAlertDeliveryState(alert, "sent", deliveryAttempts, "")
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
		setting := user.GetSetting()
		if !smartOpsAlertRecipientConfigured(user.Email, setting) {
			continue
		}
		recipients = append(recipients, smartOpsAlertRecipient{
			ID:      user.Id,
			Email:   user.Email,
			Setting: setting,
		})
	}
	return recipients, nil
}

func smartOpsAlertRecipientConfigured(email string, setting dto.UserSetting) bool {
	switch setting.NotifyType {
	case dto.NotifyTypeWebhook:
		return strings.TrimSpace(setting.WebhookUrl) != ""
	case dto.NotifyTypeBark:
		return strings.TrimSpace(setting.BarkUrl) != ""
	case dto.NotifyTypeGotify:
		return strings.TrimSpace(setting.GotifyUrl) != "" && strings.TrimSpace(setting.GotifyToken) != ""
	case "", dto.NotifyTypeEmail:
		return strings.TrimSpace(setting.NotificationEmail) != "" || strings.TrimSpace(email) != ""
	default:
		return false
	}
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

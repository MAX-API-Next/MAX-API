package channelhealth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/go-redis/redis/v8"
)

const (
	RequestModeStreaming    = "streaming"
	RequestModeNonStreaming = "non_streaming"

	TimeoutKindStreamingFirstResult = "streaming_first_result_timeout"
	TimeoutKindNonStreamingResponse = "non_streaming_response_timeout"
	TimeoutKindStreamIdle           = "stream_idle_timeout"
	TimeoutKindFirstResponse        = "first_response"

	runtimeStateKeyPrefix        = "maxapi:channel-timeout:v1:state:"
	runtimeStateIndexKey         = "maxapi:channel-timeout:v1:index"
	runtimeStateIndexMigratedKey = "maxapi:channel-timeout:v1:index:migrated"
	runtimeStateIndexCursorKey   = "maxapi:channel-timeout:v1:index:migration-cursor"
	runtimeDedupeKeyPrefix       = "maxapi:channel-timeout:v1:dedupe:"
	runtimeStateTTL              = 7 * 24 * time.Hour
	runtimeDecisionTimeout       = 100 * time.Millisecond
	runtimeWatchAttempts         = 32
)

const removeStaleRuntimeMemberScript = `if redis.call("EXISTS", KEYS[1]) == 0 then return redis.call("SREM", KEYS[2], ARGV[1]) end return 0`

var ErrRuntimeUnavailable = errors.New("channel timeout runtime is unavailable")

type AttemptEvidence struct {
	ChannelID       int
	AttemptID       string
	RequestMode     string
	TimeoutKind     string
	Success         bool
	TimeoutEligible bool
	AutoBan         bool
	ObservedAt      time.Time
}

type RuntimeState struct {
	ChannelID       int
	Penalty         int64
	PenaltyUntil    time.Time
	RuntimeDisabled bool
	DisabledUntil   time.Time
	ActiveSource    string
	Generation      int64
	TimeoutCount    map[string]int64
	SampleCount     map[string]int64
	WindowStartedAt map[string]time.Time
	LastEvent       string
	// LastTimeoutMode remains stable across successful samples and non-counted
	// events so same-mode alert projections keep using the triggering mode.
	LastTimeoutMode           string
	LastObservedAt            time.Time
	RecoveryProbeSuccessCount int64
	RecoveryWatermark         time.Time
}

type EvaluationResult struct {
	State        RuntimeState
	Transition   string
	Deduplicated bool
	Applied      bool
}

func runtimeStateKey(channelID int) string {
	return fmt.Sprintf("%s%d", runtimeStateKeyPrefix, channelID)
}

func runtimeDedupeKey(evidence AttemptEvidence) string {
	hashedAttemptID := sha256.Sum256([]byte(evidence.AttemptID))
	return fmt.Sprintf("%s%d:%s:%x", runtimeDedupeKeyPrefix, evidence.ChannelID, evidence.RequestMode, hashedAttemptID)
}

func normalizeMode(mode string) string {
	if mode == RequestModeStreaming {
		return RequestModeStreaming
	}
	return RequestModeNonStreaming
}

func timeoutThreshold(setting *operation_setting.MonitorSetting, mode string) int {
	if mode == RequestModeStreaming {
		return setting.StreamingFirstResultTimeoutSeconds
	}
	return setting.NonStreamingResponseTimeoutSeconds
}

func isCountableTimeout(kind string) bool {
	return kind == TimeoutKindStreamingFirstResult || kind == TimeoutKindNonStreamingResponse
}

func isSuccessfulSample(evidence AttemptEvidence) bool {
	return evidence.Success && evidence.TimeoutKind == TimeoutKindFirstResponse
}

func isRuntimeRecoveryEvent(event string) bool {
	return event == "manual_recovery" || strings.HasPrefix(event, "recovery_probe_")
}

func advancedSampleGateEnabled(setting *operation_setting.MonitorSetting) bool {
	return setting != nil && (setting.TimeoutAutoDisableMinimumSamples > 0 || setting.TimeoutAutoDisableRatioPercent > 0)
}

func countModeForSetting(setting *operation_setting.MonitorSetting, mode string) string {
	if setting != nil && setting.TimeoutAutoDisableCountScope == operation_setting.TimeoutAutoDisableCountScopeCombined {
		return "combined"
	}
	return mode
}

func resetTimeoutWindow(state *RuntimeState) {
	if state == nil {
		return
	}
	for _, mode := range []string{RequestModeStreaming, RequestModeNonStreaming, "combined"} {
		state.TimeoutCount[mode] = 0
		state.SampleCount[mode] = 0
		state.WindowStartedAt[mode] = time.Time{}
	}
}

func emptyRuntimeState(channelID int) RuntimeState {
	return RuntimeState{
		ChannelID:       channelID,
		TimeoutCount:    map[string]int64{},
		SampleCount:     map[string]int64{},
		WindowStartedAt: map[string]time.Time{},
	}
}

func decodeRuntimeState(channelID int, fields map[string]string) RuntimeState {
	state := emptyRuntimeState(channelID)
	state.Penalty = parseInt64(fields["penalty"])
	state.PenaltyUntil = parseTime(fields["penalty_until"])
	state.RuntimeDisabled = fields["runtime_disabled"] == "true"
	state.DisabledUntil = parseTime(fields["disabled_until"])
	state.ActiveSource = fields["active_source"]
	state.Generation = parseInt64(fields["generation"])
	state.LastEvent = fields["last_event"]
	state.LastTimeoutMode = normalizeRuntimeTimeoutMode(fields["last_timeout_mode"])
	state.LastObservedAt = parseTime(fields["last_observed_at"])
	state.RecoveryProbeSuccessCount = parseInt64(fields["recovery_probe_success_count"])
	state.RecoveryWatermark = parseTime(fields["recovery_watermark"])
	if state.RecoveryWatermark.IsZero() && isRuntimeRecoveryEvent(state.LastEvent) {
		// Backfill the durable boundary for states written before the watermark
		// field existed; the last recovery observation is the safest available
		// lower bound.
		state.RecoveryWatermark = state.LastObservedAt
	}
	for _, mode := range []string{RequestModeStreaming, RequestModeNonStreaming, "combined"} {
		state.TimeoutCount[mode] = parseInt64(fields["timeout_count_"+mode])
		state.SampleCount[mode] = parseInt64(fields["sample_count_"+mode])
		state.WindowStartedAt[mode] = parseTime(fields["window_start_"+mode])
	}
	return state
}

func encodeRuntimeState(state RuntimeState) map[string]interface{} {
	fields := map[string]interface{}{
		"channel_id":                   strconv.Itoa(state.ChannelID),
		"penalty":                      strconv.FormatInt(state.Penalty, 10),
		"penalty_until":                formatTime(state.PenaltyUntil),
		"runtime_disabled":             strconv.FormatBool(state.RuntimeDisabled),
		"disabled_until":               formatTime(state.DisabledUntil),
		"active_source":                state.ActiveSource,
		"generation":                   strconv.FormatInt(state.Generation, 10),
		"last_event":                   state.LastEvent,
		"last_timeout_mode":            state.LastTimeoutMode,
		"last_observed_at":             formatTime(state.LastObservedAt),
		"recovery_probe_success_count": strconv.FormatInt(state.RecoveryProbeSuccessCount, 10),
		"recovery_watermark":           formatTime(state.RecoveryWatermark),
	}
	for _, mode := range []string{RequestModeStreaming, RequestModeNonStreaming, "combined"} {
		fields["timeout_count_"+mode] = strconv.FormatInt(state.TimeoutCount[mode], 10)
		fields["sample_count_"+mode] = strconv.FormatInt(state.SampleCount[mode], 10)
		fields["window_start_"+mode] = formatTime(state.WindowStartedAt[mode])
	}
	return fields
}

func normalizeRuntimeTimeoutMode(mode string) string {
	if mode == RequestModeStreaming || mode == RequestModeNonStreaming {
		return mode
	}
	return ""
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// LoadRuntimeState is read-only and fails closed for callers that need to
// apply an automatic routing mutation.
func LoadRuntimeState(ctx context.Context, channelID int) (RuntimeState, error) {
	state := emptyRuntimeState(channelID)
	if channelID <= 0 || !maxcommon.RedisEnabled || maxcommon.RDB == nil {
		return state, ErrRuntimeUnavailable
	}
	fields, err := maxcommon.RDB.HGetAll(ctx, runtimeStateKey(channelID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return state, err
	}
	return decodeRuntimeState(channelID, fields), nil
}

// LoadRuntimeStates reads several channel states in one Redis round trip. A
// routing caller should treat any returned error as fail-open and keep using
// base priorities.
func LoadRuntimeStates(ctx context.Context, channelIDs []int) (map[int]RuntimeState, error) {
	states := make(map[int]RuntimeState, len(channelIDs))
	if len(channelIDs) == 0 {
		return states, nil
	}
	if !maxcommon.RedisEnabled || maxcommon.RDB == nil {
		return states, ErrRuntimeUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pipe := maxcommon.RDB.Pipeline()
	commands := make(map[int]*redis.StringStringMapCmd, len(channelIDs))
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		commands[channelID] = pipe.HGetAll(ctx, runtimeStateKey(channelID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return states, err
	}
	for channelID, command := range commands {
		fields, err := command.Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return states, err
		}
		states[channelID] = decodeRuntimeState(channelID, fields)
	}
	return states, nil
}

// ListRuntimeStates returns the bounded shared Redis projection used to
// rebuild Smart Operations after a process restart. It never reads SQL.
func ListRuntimeStates(ctx context.Context) ([]RuntimeState, error) {
	if !maxcommon.RedisEnabled || maxcommon.RDB == nil {
		return nil, ErrRuntimeUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	members, err := maxcommon.RDB.SMembers(ctx, runtimeStateIndexKey).Result()
	if err != nil {
		return nil, err
	}
	migrated, err := maxcommon.RDB.Exists(ctx, runtimeStateIndexMigratedKey).Result()
	if err != nil {
		return nil, err
	}
	if migrated > 0 {
		return listRuntimeStatesByMembers(ctx, members)
	}

	// States written before the index was introduced are discovered by a bounded
	// legacy scan and added to the index below. The cursor is persisted so an
	// interrupted scan resumes where it stopped; a complete scan records a
	// marker. New state transitions add their channel ID in the same Redis
	// transaction, so normal requests never scan the shared keyspace after
	// migration completes.
	keys := make([]string, 0, 32)
	var cursor uint64
	if value, err := maxcommon.RDB.Get(ctx, runtimeStateIndexCursorKey).Result(); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	} else if err == nil {
		cursor, _ = strconv.ParseUint(value, 10, 64)
	}
	scanComplete := false
	for {
		batch, next, err := maxcommon.RDB.Scan(ctx, cursor, runtimeStateKeyPrefix+"*", 200).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			scanComplete = true
			break
		}
		if len(keys) >= 1000 {
			break
		}
	}
	allMembers := make([]string, 0, len(members)+len(keys))
	seenMembers := make(map[string]struct{}, len(members)+len(keys))
	for _, member := range append(members, keys...) {
		member = strings.TrimPrefix(member, runtimeStateKeyPrefix)
		if _, ok := seenMembers[member]; ok {
			continue
		}
		seenMembers[member] = struct{}{}
		allMembers = append(allMembers, member)
	}
	members = allMembers
	if len(members) == 0 && !scanComplete {
		if err := maxcommon.RDB.Set(ctx, runtimeStateIndexCursorKey, strconv.FormatUint(cursor, 10), 0).Err(); err != nil {
			return nil, err
		}
		return []RuntimeState{}, nil
	}
	states, err := listRuntimeStatesByMembers(ctx, members)
	if err != nil {
		return nil, err
	}
	validMembers := make([]string, 0, len(states))
	for _, state := range states {
		validMembers = append(validMembers, strconv.Itoa(state.ChannelID))
	}
	if scanComplete {
		pipe := maxcommon.RDB.TxPipeline()
		if len(validMembers) > 0 {
			pipe.SAdd(ctx, runtimeStateIndexKey, validMembers)
		}
		pipe.Set(ctx, runtimeStateIndexMigratedKey, "1", 0)
		pipe.Del(ctx, runtimeStateIndexCursorKey)
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
	} else {
		pipe := maxcommon.RDB.TxPipeline()
		if len(validMembers) > 0 {
			pipe.SAdd(ctx, runtimeStateIndexKey, validMembers)
		}
		pipe.Set(ctx, runtimeStateIndexCursorKey, strconv.FormatUint(cursor, 10), 0)
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
	}
	return states, nil
}

func listRuntimeStatesByMembers(ctx context.Context, members []string) ([]RuntimeState, error) {
	pipe := maxcommon.RDB.Pipeline()
	commands := make([]*redis.StringStringMapCmd, 0, len(members))
	validMembers := make([]string, 0, len(members))
	for _, member := range members {
		channelID, err := strconv.Atoi(strings.TrimSpace(member))
		if err != nil || channelID <= 0 {
			continue
		}
		validMembers = append(validMembers, strconv.Itoa(channelID))
		commands = append(commands, pipe.HGetAll(ctx, runtimeStateKey(channelID)))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	states := make([]RuntimeState, 0, len(commands))
	for index, command := range commands {
		channelID, _ := strconv.Atoi(validMembers[index])
		fields, err := command.Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, err
		}
		if len(fields) == 0 {
			_ = maxcommon.RDB.Eval(ctx, removeStaleRuntimeMemberScript,
				[]string{runtimeStateKey(channelID), runtimeStateIndexKey}, validMembers[index]).Err()
			continue
		}
		states = append(states, decodeRuntimeState(channelID, fields))
	}
	return states, nil
}

// EvaluateTimeoutGuard performs one idempotent runtime state transition. It
// never updates SQL channel state and returns ErrRuntimeUnavailable when a
// shared Redis decision cannot be made.
func EvaluateTimeoutGuard(ctx context.Context, evidence AttemptEvidence, setting *operation_setting.MonitorSetting) (EvaluationResult, error) {
	result := EvaluationResult{State: emptyRuntimeState(evidence.ChannelID)}
	if evidence.ChannelID <= 0 || evidence.AttemptID == "" || setting == nil {
		return result, errors.New("invalid channel timeout evidence")
	}
	if !maxcommon.RedisEnabled || maxcommon.RDB == nil {
		return result, ErrRuntimeUnavailable
	}
	if !setting.AutoPriorityDemotionEnabled && !setting.TimeoutAutoDisableEnabled {
		return result, nil
	}
	if evidence.Success && (!evidence.AutoBan || !setting.TimeoutAutoDisableEnabled || !advancedSampleGateEnabled(setting)) {
		return result, nil
	}
	decisionCtx, cancel := boundedRuntimeContext(ctx)
	defer cancel()
	ctx = decisionCtx
	now := evidence.ObservedAt
	if now.IsZero() {
		now = time.Now()
	}
	mode := normalizeMode(evidence.RequestMode)
	key := runtimeStateKey(evidence.ChannelID)
	dedupeKey := runtimeDedupeKey(evidence)
	windowTTL := time.Duration(setting.TimeoutAutoDisableWindowSeconds) * time.Second
	if windowTTL <= 0 {
		windowTTL = time.Hour
	}

	err := watchRuntimeState(ctx, []string{key, dedupeKey}, func(tx *redis.Tx) error {
		// WATCH may invoke the callback more than once. Never expose a result
		// assembled by a transaction that is later retried or rejected.
		result = EvaluationResult{State: emptyRuntimeState(evidence.ChannelID)}
		deduped, err := tx.Exists(ctx, dedupeKey).Result()
		if err != nil {
			return err
		}
		fields, err := tx.HGetAll(ctx, key).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		state := decodeRuntimeState(evidence.ChannelID, fields)
		result.State = state
		if !state.RecoveryWatermark.IsZero() && now.Before(state.RecoveryWatermark) {
			// A timeout observed before the last recovery must not be allowed to
			// reapply a penalty after the recovery transaction wins the watch,
			// even when a newer success sample has replaced LastEvent.
			return nil
		}
		if deduped > 0 {
			result.Deduplicated = true
			return nil
		}

		shouldCount := evidence.TimeoutEligible && isCountableTimeout(evidence.TimeoutKind) &&
			setting.TimeoutAutoDisableEnabled && evidence.AutoBan &&
			timeoutThreshold(setting, mode) > 0
		shouldRecordSample := setting.TimeoutAutoDisableEnabled && evidence.AutoBan &&
			advancedSampleGateEnabled(setting) && timeoutThreshold(setting, mode) > 0 &&
			(isSuccessfulSample(evidence) || shouldCount)
		countMode := countModeForSetting(setting, mode)
		if shouldCount || shouldRecordSample {
			windowStart := state.WindowStartedAt[countMode]
			if windowStart.IsZero() || now.Sub(windowStart) >= windowTTL {
				windowStart = now
				state.TimeoutCount[countMode] = 0
				state.SampleCount[countMode] = 0
			}
			state.WindowStartedAt[countMode] = windowStart
		}
		if shouldCount {
			state.TimeoutCount[countMode]++
			state.LastTimeoutMode = mode
		}
		if shouldRecordSample {
			state.SampleCount[countMode]++
		}

		if evidence.TimeoutEligible && isCountableTimeout(evidence.TimeoutKind) &&
			setting.AutoPriorityDemotionEnabled &&
			setting.PriorityDeduction > 0 &&
			timeoutThreshold(setting, mode) > 0 &&
			!state.RuntimeDisabled {
			previouslyActive := state.Penalty > 0 && (state.PenaltyUntil.IsZero() || now.Before(state.PenaltyUntil))
			state.Penalty = int64(setting.PriorityDeduction)
			if setting.RecoveryMode == operation_setting.RecoveryModeAutomatic {
				until := now.Add(time.Duration(setting.PenaltyCooldownSeconds) * time.Second)
				if previouslyActive && !state.PenaltyUntil.IsZero() &&
					(!setting.ExtendOnRepeatTimeout || until.Before(state.PenaltyUntil)) {
					until = state.PenaltyUntil
				}
				state.PenaltyUntil = until
			} else {
				state.PenaltyUntil = time.Time{}
			}
			if !previouslyActive {
				result.Transition = "priority_demotion_firing"
			} else {
				result.Transition = "priority_demotion_updated"
			}
			result.Applied = true
		}

		if (shouldCount || shouldRecordSample) && setting.TimeoutAutoDisableCount > 0 {
			if state.TimeoutCount[countMode] >= int64(setting.TimeoutAutoDisableCount) &&
				(!advancedSampleGateEnabled(setting) ||
					(state.SampleCount[countMode] >= int64(setting.TimeoutAutoDisableMinimumSamples) &&
						(setting.TimeoutAutoDisableRatioPercent <= 0 ||
							float64(state.TimeoutCount[countMode])*100 >= setting.TimeoutAutoDisableRatioPercent*float64(state.SampleCount[countMode])))) &&
				!state.RuntimeDisabled {
				state.RuntimeDisabled = true
				state.ActiveSource = "repeated_timeout"
				if setting.TimeoutAutoDisableDurationSeconds > 0 {
					state.DisabledUntil = now.Add(time.Duration(setting.TimeoutAutoDisableDurationSeconds) * time.Second)
				}
				state.Penalty = 0
				state.PenaltyUntil = time.Time{}
				result.Transition = "timeout_auto_disabled"
				result.Applied = true
			}
		}

		state.Generation++
		state.LastEvent = evidence.TimeoutKind
		state.LastObservedAt = now
		result.State = state
		pipe := tx.TxPipeline()
		pipe.HSet(ctx, key, encodeRuntimeState(state))
		pipe.SAdd(ctx, runtimeStateIndexKey, strconv.Itoa(evidence.ChannelID))
		pipe.Set(ctx, dedupeKey, "1", windowTTL)
		if state.RuntimeDisabled &&
			(setting.TimeoutAutoDisableDurationSeconds == 0 ||
				setting.TimeoutAutoDisableRecoveryMode == operation_setting.TimeoutAutoDisableRecoveryManual) ||
			state.Penalty > 0 && setting.RecoveryMode == operation_setting.RecoveryModeManual {
			pipe.Persist(ctx, key)
		} else {
			pipe.Expire(ctx, key, runtimeStateTTLFor(state, now))
		}
		_, err = pipe.Exec(ctx)
		return err
	})
	if err != nil {
		if errors.Is(err, redis.TxFailedErr) {
			return EvaluationResult{State: emptyRuntimeState(evidence.ChannelID)}, fmt.Errorf("channel timeout runtime contention: %w", err)
		}
		return EvaluationResult{State: emptyRuntimeState(evidence.ChannelID)}, err
	}
	return result, nil
}

func boundedRuntimeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, runtimeDecisionTimeout)
}

func runtimeStateTTLFor(state RuntimeState, now time.Time) time.Duration {
	ttl := runtimeStateTTL
	for _, until := range []time.Time{state.PenaltyUntil, state.DisabledUntil} {
		if until.IsZero() || !until.After(now) {
			continue
		}
		// Keep a small grace period so Redis' whole-second TTL precision cannot
		// remove an active penalty or disablement at its exact deadline.
		if remaining := until.Sub(now) + time.Hour; remaining > ttl {
			ttl = remaining
		}
	}
	return ttl
}

// watchRuntimeState retries optimistic Redis transactions that lost a
// concurrent update. The retry budget is bounded by the caller's decision
// context so contention never turns into an unbounded relay delay.
func watchRuntimeState(ctx context.Context, keys []string, fn func(*redis.Tx) error) error {
	var err error
	for attempt := 0; attempt < runtimeWatchAttempts; attempt++ {
		err = maxcommon.RDB.Watch(ctx, fn, keys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}

// RecoverRuntimeState records one controlled recovery probe. Probe attempts
// are separate from production timeout evidence and can only clear a runtime
// disablement after the configured number of consecutive successes.
func RecoverRuntimeState(ctx context.Context, channelID int, probeID string, success bool, setting *operation_setting.MonitorSetting) (EvaluationResult, error) {
	result := EvaluationResult{State: emptyRuntimeState(channelID)}
	if channelID <= 0 || strings.TrimSpace(probeID) == "" || setting == nil {
		return result, errors.New("invalid channel recovery probe")
	}
	if !maxcommon.RedisEnabled || maxcommon.RDB == nil {
		return result, ErrRuntimeUnavailable
	}
	if setting.TimeoutAutoDisableRecoveryMode != operation_setting.TimeoutAutoDisableRecoveryProbe ||
		setting.TimeoutAutoDisableDurationSeconds <= 0 {
		return result, errors.New("automatic channel recovery is not enabled")
	}
	decisionCtx, cancel := boundedRuntimeContext(ctx)
	defer cancel()
	key := runtimeStateKey(channelID)
	dedupeKey := runtimeRecoveryDedupeKey(channelID, probeID)
	now := time.Now()
	probeThreshold := setting.TimeoutAutoDisableSuccessfulProbeCount
	if probeThreshold <= 0 {
		probeThreshold = 1
	}
	probeTTL := time.Duration(setting.TimeoutAutoDisableWindowSeconds) * time.Second
	if probeTTL <= 0 {
		probeTTL = time.Hour
	}

	err := watchRuntimeState(decisionCtx, []string{key, dedupeKey}, func(tx *redis.Tx) error {
		result = EvaluationResult{State: emptyRuntimeState(channelID)}
		deduped, err := tx.Exists(decisionCtx, dedupeKey).Result()
		if err != nil {
			return err
		}
		fields, err := tx.HGetAll(decisionCtx, key).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		state := decodeRuntimeState(channelID, fields)
		result.State = state
		if !state.RuntimeDisabled {
			return nil
		}
		if deduped > 0 {
			result.Deduplicated = true
			return nil
		}
		if !state.DisabledUntil.IsZero() && now.Before(state.DisabledUntil) {
			return nil
		}
		if success {
			state.RecoveryProbeSuccessCount++
			if state.RecoveryProbeSuccessCount >= int64(probeThreshold) {
				resetTimeoutWindow(&state)
				state.RuntimeDisabled = false
				state.DisabledUntil = time.Time{}
				state.ActiveSource = ""
				state.Penalty = 0
				state.PenaltyUntil = time.Time{}
				state.RecoveryProbeSuccessCount = 0
				state.LastEvent = "recovery_probe_succeeded"
				state.RecoveryWatermark = now
				result.Transition = "runtime_recovered"
				result.Applied = true
			} else {
				state.LastEvent = "recovery_probe_succeeded"
			}
		} else {
			state.RecoveryProbeSuccessCount = 0
			state.LastEvent = "recovery_probe_failed"
		}
		state.Generation++
		state.LastObservedAt = now
		result.State = state
		pipe := tx.TxPipeline()
		pipe.HSet(decisionCtx, key, encodeRuntimeState(state))
		pipe.SAdd(decisionCtx, runtimeStateIndexKey, strconv.Itoa(channelID))
		pipe.Set(decisionCtx, dedupeKey, "1", probeTTL)
		pipe.Expire(decisionCtx, key, runtimeStateTTLFor(state, now))
		_, err = pipe.Exec(decisionCtx)
		return err
	})
	if err != nil {
		if errors.Is(err, redis.TxFailedErr) {
			return EvaluationResult{State: emptyRuntimeState(channelID)}, fmt.Errorf("channel recovery runtime contention: %w", err)
		}
		return EvaluationResult{State: emptyRuntimeState(channelID)}, err
	}
	return result, nil
}

// RestoreRuntimeState clears only the timeout strategy's Redis runtime state.
// It never changes the persisted channel status or ability rows.
func RestoreRuntimeState(ctx context.Context, channelID int) (EvaluationResult, error) {
	result := EvaluationResult{State: emptyRuntimeState(channelID)}
	if channelID <= 0 || !maxcommon.RedisEnabled || maxcommon.RDB == nil {
		return result, ErrRuntimeUnavailable
	}
	decisionCtx, cancel := boundedRuntimeContext(ctx)
	defer cancel()
	key := runtimeStateKey(channelID)
	err := watchRuntimeState(decisionCtx, []string{key}, func(tx *redis.Tx) error {
		result = EvaluationResult{State: emptyRuntimeState(channelID)}
		fields, err := tx.HGetAll(decisionCtx, key).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		state := decodeRuntimeState(channelID, fields)
		if !state.RuntimeDisabled && state.Penalty <= 0 {
			result.State = state
			return nil
		}
		state.RuntimeDisabled = false
		state.DisabledUntil = time.Time{}
		state.ActiveSource = ""
		state.Penalty = 0
		state.PenaltyUntil = time.Time{}
		state.RecoveryProbeSuccessCount = 0
		resetTimeoutWindow(&state)
		state.Generation++
		state.LastEvent = "manual_recovery"
		state.LastObservedAt = time.Now()
		state.RecoveryWatermark = state.LastObservedAt
		result.State = state
		result.Transition = "runtime_recovered"
		result.Applied = true
		pipe := tx.TxPipeline()
		pipe.HSet(decisionCtx, key, encodeRuntimeState(state))
		pipe.SAdd(decisionCtx, runtimeStateIndexKey, strconv.Itoa(channelID))
		pipe.Expire(decisionCtx, key, runtimeStateTTLFor(state, state.LastObservedAt))
		_, err = pipe.Exec(decisionCtx)
		return err
	})
	if err != nil {
		if errors.Is(err, redis.TxFailedErr) {
			return EvaluationResult{State: emptyRuntimeState(channelID)}, fmt.Errorf("manual channel recovery contention: %w", err)
		}
		return EvaluationResult{State: emptyRuntimeState(channelID)}, err
	}
	return result, nil
}

func EffectivePriority(base int64, state RuntimeState, now time.Time) int64 {
	if now.IsZero() {
		now = time.Now()
	}
	if state.RuntimeDisabled {
		return base
	}
	if state.Penalty <= 0 || (!state.PenaltyUntil.IsZero() && !now.Before(state.PenaltyUntil)) {
		return base
	}
	effective := base - state.Penalty
	if effective < 0 && base >= 0 {
		return 0
	}
	return effective
}

func RuntimeStateKey(channelID int) string {
	return runtimeStateKey(channelID)
}

func runtimeRecoveryDedupeKey(channelID int, probeID string) string {
	hashedProbeID := sha256.Sum256([]byte(strings.TrimSpace(probeID)))
	return fmt.Sprintf("%srecovery:%d:%x", runtimeDedupeKeyPrefix, channelID, hashedProbeID)
}

func RuntimeStateSummary(state RuntimeState) string {
	parts := []string{
		"channel=" + strconv.Itoa(state.ChannelID),
		"penalty=" + strconv.FormatInt(state.Penalty, 10),
		"disabled=" + strconv.FormatBool(state.RuntimeDisabled),
	}
	if state.ActiveSource != "" {
		parts = append(parts, "source="+state.ActiveSource)
	}
	return strings.Join(parts, " ")
}

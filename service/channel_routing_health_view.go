package service

import (
	"context"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
)

const (
	channelRoutingHealthReadTimeout = 100 * time.Millisecond
	channelRoutingHealthBatchSize   = 200
	channelRoutingHealthMaxChannels = 1000
)

// ChannelRoutingHealthView preserves the existing sanitized channel response.
// The additional snapshot never enters the persistence model or routing cache.
type ChannelRoutingHealthView struct {
	*model.Channel
	RoutingHealth dto.ChannelRoutingHealth `json:"routing_health"`
}

// BuildChannelRoutingHealthViews enriches only channels already selected by a
// management query. It does not query SQL, mutate channels, or publish transitions.
func BuildChannelRoutingHealthViews(ctx context.Context, channels []*model.Channel) []*ChannelRoutingHealthView {
	common.OptionMapRWMutex.RLock()
	setting := *operation_setting.GetMonitorSetting()
	common.OptionMapRWMutex.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, channelRoutingHealthReadTimeout)
	defer cancel()
	return buildChannelRoutingHealthViews(ctx, channels, setting, channelhealth.LoadRuntimeStatesStrict)
}

type channelRoutingHealthReader func(context.Context, []int) (map[int]channelhealth.RuntimeStateReadResult, error)

func buildChannelRoutingHealthViews(ctx context.Context, channels []*model.Channel, setting operation_setting.MonitorSetting, read channelRoutingHealthReader) []*ChannelRoutingHealthView {
	views := make([]*ChannelRoutingHealthView, len(channels))
	enabled := setting.AutoPriorityDemotionEnabled || setting.TimeoutAutoDisableEnabled
	ids := make([]int, 0, min(len(channels), channelRoutingHealthMaxChannels))
	selected := make(map[int]bool)
	if enabled {
		for _, channel := range channels {
			if channel == nil || channel.Id <= 0 || selected[channel.Id] || len(ids) >= channelRoutingHealthMaxChannels {
				continue
			}
			selected[channel.Id] = true
			ids = append(ids, channel.Id)
		}
	}
	states := make(map[int]channelhealth.RuntimeStateReadResult, len(ids))
	failureReason := "projection_budget_exceeded"
	if enabled && (!common.RedisEnabled || common.RDB == nil) {
		failureReason = "redis_unconfigured"
	} else {
		for start := 0; start < len(ids); start += channelRoutingHealthBatchSize {
			if ctx.Err() != nil {
				break
			}
			end := min(start+channelRoutingHealthBatchSize, len(ids))
			loaded, err := read(ctx, ids[start:end])
			if err != nil {
				failureReason = "redis_read_failed"
				break
			}
			for _, id := range ids[start:end] {
				if state, ok := loaded[id]; ok {
					states[id] = state
				}
			}
		}
	}
	now := time.Now().UTC()
	for index, channel := range channels {
		if channel == nil {
			continue
		}
		view := &ChannelRoutingHealthView{Channel: channel}
		view.RoutingHealth = unknownChannelRoutingHealth(channel.GetPriority(), now, failureReason)
		switch {
		case !enabled:
			view.RoutingHealth = dto.ChannelRoutingHealth{
				SchemaVersion:     1,
				Status:            dto.ChannelRoutingHealthPolicyOff,
				Source:            dto.ChannelRoutingHealthSourcePolicyOff,
				BasePriority:      channel.GetPriority(),
				EffectivePriority: common.GetPointer(channel.GetPriority()),
				ActivePenalty:     common.GetPointer(int64(0)),
				RuntimeDisabled:   common.GetPointer(false),
				EvaluatedAt:       now,
			}
		case channel.Id <= 0:
			view.RoutingHealth.UnavailableReason = common.GetPointer("invalid_channel")
		case !selected[channel.Id]:
			view.RoutingHealth.UnavailableReason = common.GetPointer("projection_budget_exceeded")
		default:
			if result, ok := states[channel.Id]; ok {
				if result.Err != nil {
					view.RoutingHealth.UnavailableReason = common.GetPointer("redis_invalid_state")
				} else {
					view.RoutingHealth = projectChannelRoutingHealth(channel.GetPriority(), result.State, setting, now)
				}
			}
		}
		views[index] = view
	}
	return views
}

func unknownChannelRoutingHealth(base int64, now time.Time, reason string) dto.ChannelRoutingHealth {
	return dto.ChannelRoutingHealth{
		SchemaVersion:     1,
		Status:            dto.ChannelRoutingHealthUnknown,
		Source:            dto.ChannelRoutingHealthSourceUnavailable,
		BasePriority:      base,
		EvaluatedAt:       now,
		UnavailableReason: common.GetPointer(reason),
	}
}

func projectChannelRoutingHealth(base int64, state channelhealth.RuntimeState, setting operation_setting.MonitorSetting, now time.Time) dto.ChannelRoutingHealth {
	disabled := state.RuntimeDisabled && setting.TimeoutAutoDisableEnabled
	view := dto.ChannelRoutingHealth{
		SchemaVersion:         1,
		Status:                dto.ChannelRoutingHealthNormal,
		Source:                dto.ChannelRoutingHealthSourceRedis,
		BasePriority:          base,
		StoredPenalty:         common.GetPointer(state.Penalty),
		ActivePenalty:         common.GetPointer(int64(0)),
		StoredRuntimeDisabled: common.GetPointer(state.RuntimeDisabled),
		RuntimeDisabled:       common.GetPointer(disabled),
		PenaltyUntil:          channelRoutingHealthTime(state.PenaltyUntil),
		DisabledUntil:         channelRoutingHealthTime(state.DisabledUntil),
		LastObservedAt:        channelRoutingHealthTime(state.LastObservedAt),
		EvaluatedAt:           now,
	}
	disableRecoveryMode := operation_setting.TimeoutAutoDisableRecoveryProbe
	if setting.TimeoutAutoDisableRecoveryMode == operation_setting.TimeoutAutoDisableRecoveryManual {
		disableRecoveryMode = operation_setting.TimeoutAutoDisableRecoveryManual
	}
	view.DisableRecoveryMode = &disableRecoveryMode
	if state.Penalty > 0 {
		recoveryMode := operation_setting.RecoveryModeAutomatic
		if state.PenaltyUntil.IsZero() {
			recoveryMode = operation_setting.RecoveryModeManual
		}
		view.PenaltyRecoveryMode = &recoveryMode
	}
	if disabled {
		view.Status = dto.ChannelRoutingHealthRuntimeDisabled
		return view
	}
	view.EffectivePriority = common.GetPointer(channelhealth.EffectivePriority(base, state, now))
	// Mirror both routing paths: an existing penalty can remain effective when
	// only auto-disable is enabled; a stored disabled flag suppresses its deduction.
	if !state.RuntimeDisabled && state.Penalty > 0 && (state.PenaltyUntil.IsZero() || now.Before(state.PenaltyUntil)) {
		view.ActivePenalty = common.GetPointer(state.Penalty)
		view.Status = dto.ChannelRoutingHealthPenalized
	}
	return view
}

func channelRoutingHealthTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return common.GetPointer(value.UTC())
}

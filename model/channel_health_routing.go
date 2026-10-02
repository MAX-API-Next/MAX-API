package model

import (
	"context"
	"time"

	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
)

type channelRoutingCandidate struct {
	channel  *Channel
	priority int64
}

func channelHealthRoutingEnabled() bool {
	setting := operation_setting.GetMonitorSetting()
	return setting != nil && (setting.AutoPriorityDemotionEnabled || setting.TimeoutAutoDisableEnabled)
}

func loadChannelRuntimeStates(channelIDs []int) map[int]channelhealth.RuntimeState {
	if !channelHealthRoutingEnabled() || len(channelIDs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	states, err := channelhealth.LoadRuntimeStates(ctx, channelIDs)
	if err != nil {
		// Redis is an optional runtime dependency for this feature. The caller
		// must keep the existing base-priority selection when it is unavailable.
		return nil
	}
	return states
}

func buildChannelRoutingCandidates(channels []*Channel) []channelRoutingCandidate {
	if len(channels) == 0 {
		return nil
	}
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel != nil {
			channelIDs = append(channelIDs, channel.Id)
		}
	}
	states := loadChannelRuntimeStates(channelIDs)
	now := time.Now()
	candidates := make([]channelRoutingCandidate, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		priority := channel.GetPriority()
		if state, ok := states[channel.Id]; ok {
			if state.RuntimeDisabled {
				continue
			}
			priority = channelhealth.EffectivePriority(priority, state, now)
		}
		candidates = append(candidates, channelRoutingCandidate{channel: channel, priority: priority})
	}
	return candidates
}

func buildAbilityRoutingCandidates(abilities []Ability) []abilityRoutingCandidate {
	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	states := loadChannelRuntimeStates(channelIDs)
	now := time.Now()
	candidates := make([]abilityRoutingCandidate, 0, len(abilities))
	for _, ability := range abilities {
		priority := abilityPriority(ability)
		if state, ok := states[ability.ChannelId]; ok {
			if state.RuntimeDisabled {
				continue
			}
			priority = channelhealth.EffectivePriority(priority, state, now)
		}
		candidates = append(candidates, abilityRoutingCandidate{ability: ability, priority: priority})
	}
	return candidates
}

type abilityRoutingCandidate struct {
	ability  Ability
	priority int64
}

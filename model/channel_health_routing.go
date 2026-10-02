package model

import (
	"context"
	"sync"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/go-redis/redis/v8"
)

const (
	runtimeStateCacheTTL        = time.Second
	runtimeStateFailureCooldown = 250 * time.Millisecond
)

type runtimeStateCacheEntry struct {
	client     *redis.Client
	state      channelhealth.RuntimeState
	expiresAt  time.Time
	retryAfter time.Time
}

var runtimeStateCache = struct {
	sync.Mutex
	entries map[int]runtimeStateCacheEntry
}{entries: make(map[int]runtimeStateCacheEntry)}

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
	now := time.Now()
	client := maxcommon.RDB
	if !maxcommon.RedisEnabled || client == nil {
		return nil
	}
	states := make(map[int]channelhealth.RuntimeState, len(channelIDs))
	pending := make([]int, 0, len(channelIDs))
	runtimeStateCache.Lock()
	for _, channelID := range channelIDs {
		entry, ok := runtimeStateCache.entries[channelID]
		if ok && entry.client == client && now.Before(entry.retryAfter) {
			continue
		}
		if ok && entry.client == client && now.Before(entry.expiresAt) {
			states[channelID] = entry.state
			continue
		}
		pending = append(pending, channelID)
	}
	runtimeStateCache.Unlock()
	if len(pending) == 0 {
		return states
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	loaded, err := channelhealth.LoadRuntimeStates(ctx, pending)
	if err != nil {
		runtimeStateCache.Lock()
		for _, channelID := range pending {
			runtimeStateCache.entries[channelID] = runtimeStateCacheEntry{
				client:     client,
				retryAfter: now.Add(runtimeStateFailureCooldown),
			}
		}
		runtimeStateCache.Unlock()
		// Redis is an optional runtime dependency for this feature. The caller
		// must keep the existing base-priority selection for channels without a
		// cached state. Freshly cached states remain usable during the cooldown.
		if len(states) == 0 {
			return nil
		}
		return states
	}
	runtimeStateCache.Lock()
	for channelID, state := range loaded {
		runtimeStateCache.entries[channelID] = runtimeStateCacheEntry{
			client:    client,
			state:     state,
			expiresAt: now.Add(runtimeStateCacheTTL),
		}
		states[channelID] = state
	}
	runtimeStateCache.Unlock()
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

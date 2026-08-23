package service

import (
	"errors"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx                *gin.Context
	TokenGroup         string
	ModelName          string
	RequestPath        string
	Retry              *int
	resetNextTry       bool
	excludedChannelIDs map[int]struct{}
}

// SetSelectedRoutingGroup keeps the configured route and the actual group in
// separate context keys. ContextKeyUsingGroup is the effective group used by
// logging/billing; ContextKeyTokenGroup and the route plan retain the token's
// configured routing policy.
func SetSelectedRoutingGroup(c *gin.Context, group string) {
	if c == nil || group == "" {
		return
	}
	common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// ExcludeChannel prevents a retried request from selecting the same failed
// channel again while leaving each new request independent.
func (p *RetryParam) ExcludeChannel(channelID int) {
	if channelID <= 0 {
		return
	}
	if p.excludedChannelIDs == nil {
		p.excludedChannelIDs = make(map[int]struct{})
	}
	p.excludedChannelIDs[channelID] = struct{}{}
}

// CacheGetRandomSatisfiedChannel tries to get a random channel that satisfies the requirements.
// 尝试获取一个满足要求的随机渠道。
//
// For auto route token groups ("auto" or "auto:<name>") with cross-group Retry enabled:
// 对于启用了跨分组重试的自动链路 tokenGroup（"auto" 或 "auto:<name>"）：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted → GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 → 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
	if routePlan, ok := GetTokenRoutePlan(param.Ctx); ok && !routePlan.Legacy {
		return selectChannelFromTokenRoutePlan(param, routePlan)
	}

	if setting.IsAutoRouteKey(param.TokenGroup) {
		autoGroups := GetUserAutoGroupByRoute(userGroup, param.TokenGroup)
		if len(autoGroups) == 0 {
			return nil, selectGroup, errors.New("auto route groups is not enabled")
		}

		// startGroupIndex: the group index to start searching from
		// startGroupIndex: 开始搜索的分组索引
		startGroupIndex := 0
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			// Calculate priorityRetry for current group
			// 计算当前分组的 priorityRetry
			priorityRetry := param.GetRetry()
			// If moved to a new group, reset priorityRetry and update startRetryIndex
			// 如果切换到新分组，重置 priorityRetry 并更新 startRetryIndex
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

			channel, err = model.GetRandomSatisfiedChannelExcluding(autoGroup, param.ModelName, priorityRetry, param.RequestPath, param.excludedChannelIDs)
			if err != nil {
				return nil, autoGroup, err
			}
			if channel == nil {
				// Current group has no available channel for this model, try next group
				// 当前分组没有该模型的可用渠道，尝试下一个分组
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", autoGroup, param.ModelName, priorityRetry)
				// 重置状态以尝试下一个分组
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				continue
			}
			SetSelectedRoutingGroup(param.Ctx, autoGroup)
			selectGroup = autoGroup
			logger.LogDebug(param.Ctx, "Auto selected group: %s", autoGroup)

			// Prepare state for next retry
			// 为下一次重试准备状态
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				// Current group has exhausted all retries, prepare to switch to next group
				// This request still uses current group, but next retry will use next group
				// 当前分组已用完所有重试次数，准备切换到下一个分组
				// 本次请求仍使用当前分组，但下次重试将使用下一个分组
				logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", autoGroup, priorityRetry, common.RetryTimes)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				// Stay in current group, save current state
				// 保持在当前分组，保存当前状态
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
	} else {
		channel, err = model.GetRandomSatisfiedChannelExcluding(param.TokenGroup, param.ModelName, param.GetRetry(), param.RequestPath, param.excludedChannelIDs)
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return channel, selectGroup, nil
}

func selectChannelFromTokenRoutePlan(param *RetryParam, plan *TokenRoutePlan) (*model.Channel, string, error) {
	if plan == nil || len(plan.OrderedGroups) == 0 {
		return nil, param.TokenGroup, errors.New("token route plan has no available groups")
	}

	startGroupIndex := 0
	if raw, ok := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); ok {
		if index, valid := raw.(int); valid && index >= 0 && index < len(plan.OrderedGroups) {
			startGroupIndex = index
		}
	}
	groupStartRetry := param.GetRetry()
	if raw, ok := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex); ok {
		if retry, valid := raw.(int); valid && retry >= 0 && retry <= param.GetRetry() {
			groupStartRetry = retry
		}
	}

	selectGroup := plan.OrderedGroups[startGroupIndex]
	for index := startGroupIndex; index < len(plan.OrderedGroups); index++ {
		group := plan.OrderedGroups[index]
		selectGroup = group
		priorityRetry := param.GetRetry() - groupStartRetry
		if index > startGroupIndex || priorityRetry < 0 {
			priorityRetry = 0
			groupStartRetry = param.GetRetry()
		}

		logger.LogDebug(param.Ctx, "Token route plan selecting group: %s, priorityRetry: %d", group, priorityRetry)
		channel, err := model.GetRandomSatisfiedChannelExcluding(
			group,
			param.ModelName,
			priorityRetry,
			param.RequestPath,
			param.excludedChannelIDs,
		)
		if err != nil {
			return nil, group, err
		}
		if channel == nil {
			if param.GetRetry() > 0 && !plan.RetryOnFailure {
				return nil, group, nil
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, param.GetRetry())
			continue
		}

		SetSelectedRoutingGroup(param.Ctx, group)
		if plan.RetryOnFailure && index < len(plan.OrderedGroups)-1 {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index+1)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, param.GetRetry()+1)
		} else {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, index)
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, groupStartRetry)
		}
		return channel, group, nil
	}

	return nil, selectGroup, nil
}

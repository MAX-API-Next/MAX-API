package service

import (
	"strings"

	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	return CanUseTokenGroup(userGroup, groupName)
}

func IsAutoRouteKey(groupName string) bool {
	return setting.IsAutoRouteKey(groupName)
}

func CanUseTokenGroup(userGroup, groupName string) bool {
	return canUseTokenGroup(userGroup, groupName, true)
}

func CanUseTokenGroupRuntime(userGroup, groupName string) bool {
	return canUseTokenGroup(userGroup, groupName, false)
}

func canUseTokenGroup(userGroup, groupName string, requireUserSelectable bool) bool {
	if setting.IsAutoRouteKey(groupName) {
		_, ok := GetUserAutoRoute(userGroup, groupName, requireUserSelectable)
		return ok
	}
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func filterUserAutoRouteGroups(userGroup string, routeGroups []string) []string {
	groups := GetUserUsableGroups(userGroup)
	autoGroups := make([]string, 0)
	for _, group := range routeGroups {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

func GetUserAutoRoute(userGroup, routeKey string, requireUserSelectable bool) (setting.AutoGroupRoute, bool) {
	if routeKey == "" {
		routeKey = setting.GetDefaultAutoRouteKey()
	}
	route, ok := setting.GetAutoRoute(routeKey)
	if !ok || !route.Enabled {
		return setting.AutoGroupRoute{}, false
	}
	if requireUserSelectable && !route.UserSelectable {
		return setting.AutoGroupRoute{}, false
	}
	route.Groups = filterUserAutoRouteGroups(userGroup, route.Groups)
	if len(route.Groups) == 0 {
		return setting.AutoGroupRoute{}, false
	}
	return route, true
}

func GetUserAutoRoutes(userGroup string, requireUserSelectable bool) []setting.AutoGroupRoute {
	routes := setting.GetAutoRoutes()
	userRoutes := make([]setting.AutoGroupRoute, 0, len(routes))
	for _, route := range routes {
		userRoute, ok := GetUserAutoRoute(userGroup, route.Key, requireUserSelectable)
		if ok {
			userRoutes = append(userRoutes, userRoute)
		}
	}
	return userRoutes
}

// GetUserAutoGroup 根据用户分组获取默认自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	return GetUserAutoGroupByRoute(userGroup, setting.GetDefaultAutoRouteKey())
}

func GetUserAutoGroupByRoute(userGroup, routeKey string) []string {
	route, ok := GetUserAutoRoute(userGroup, routeKey, false)
	if !ok {
		return []string{}
	}
	return route.Groups
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}

package ratio_setting

import (
	"errors"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/types"
)

var defaultGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}

const (
	defaultAutoRouteGroupName = "auto"
	autoRouteGroupNamePrefix  = "auto:"
)

var groupRatioMap = types.NewRWMap[string, float64]()

var defaultGroupGroupRatio = map[string]map[string]float64{
	"vip": {
		"edit_this": 0.9,
	},
}

var groupGroupRatioMap = types.NewRWMap[string, map[string]float64]()

var defaultGroupSpecialUsableGroup = map[string]map[string]string{
	"vip": {
		"append_1":   "vip_special_group_1",
		"-:remove_1": "vip_removed_group_1",
	},
}

type GroupRatioSetting struct {
	GroupRatio              *types.RWMap[string, float64]            `json:"group_ratio"`
	GroupGroupRatio         *types.RWMap[string, map[string]float64] `json:"group_group_ratio"`
	GroupSpecialUsableGroup *types.RWMap[string, map[string]string]  `json:"group_special_usable_group"`
}

var groupRatioSetting GroupRatioSetting

func init() {
	groupSpecialUsableGroup := types.NewRWMap[string, map[string]string]()
	groupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)

	groupRatioMap.AddAll(defaultGroupRatio)
	groupGroupRatioMap.AddAll(defaultGroupGroupRatio)

	groupRatioSetting = GroupRatioSetting{
		GroupSpecialUsableGroup: groupSpecialUsableGroup,
		GroupRatio:              groupRatioMap,
		GroupGroupRatio:         groupGroupRatioMap,
	}

	config.GlobalConfig.Register("group_ratio_setting", &groupRatioSetting)
}

func GetGroupRatioSetting() *GroupRatioSetting {
	if groupRatioSetting.GroupSpecialUsableGroup == nil {
		groupRatioSetting.GroupSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
		groupRatioSetting.GroupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)
	}
	return &groupRatioSetting
}

func GetGroupRatioCopy() map[string]float64 {
	return filterReservedAutoRouteGroupRatios(groupRatioMap.ReadAll())
}

func ContainsGroupRatio(name string) bool {
	if isReservedAutoRouteGroupName(strings.TrimSpace(name)) {
		return false
	}
	_, ok := groupRatioMap.Get(name)
	return ok
}

func GroupRatio2JSONString() string {
	jsonBytes, err := common.Marshal(GetGroupRatioCopy())
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func UpdateGroupRatioByJSONString(jsonStr string) error {
	normalized, err := NormalizeGroupRatioJSONString(jsonStr)
	if err != nil {
		return err
	}
	return types.LoadFromJsonString(groupRatioMap, normalized)
}

func GetGroupRatio(name string) float64 {
	if isReservedAutoRouteGroupName(strings.TrimSpace(name)) {
		common.SysLog("group ratio not found: " + name)
		return 1
	}
	ratio, ok := groupRatioMap.Get(name)
	if !ok {
		common.SysLog("group ratio not found: " + name)
		return 1
	}
	return ratio
}

func GetGroupGroupRatio(userGroup, usingGroup string) (float64, bool) {
	gp, ok := groupGroupRatioMap.Get(userGroup)
	if !ok {
		return -1, false
	}
	ratio, ok := gp[usingGroup]
	if !ok {
		return -1, false
	}
	return ratio, true
}

func GroupGroupRatio2JSONString() string {
	return groupGroupRatioMap.MarshalJSONString()
}

func UpdateGroupGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupGroupRatioMap, jsonStr)
}

func CheckGroupRatio(jsonStr string) error {
	_, err := normalizeGroupRatioMap(jsonStr)
	return err
}

func NormalizeGroupRatioJSONString(jsonStr string) (string, error) {
	normalized, err := normalizeGroupRatioMap(jsonStr)
	if err != nil {
		return "", err
	}
	jsonBytes, err := common.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

func normalizeGroupRatioMap(jsonStr string) (map[string]float64, error) {
	checkGroupRatio := make(map[string]float64)
	err := common.Unmarshal([]byte(jsonStr), &checkGroupRatio)
	if err != nil {
		return nil, err
	}
	normalized := make(map[string]float64, len(checkGroupRatio))
	for name, ratio := range checkGroupRatio {
		trimmedName := strings.TrimSpace(name)
		if isReservedAutoRouteGroupName(trimmedName) {
			continue
		}
		if ratio < 0 {
			return nil, errors.New("group ratio must be not less than 0: " + name)
		}
		normalized[name] = ratio
	}
	return normalized, nil
}

func filterReservedAutoRouteGroupRatios(ratios map[string]float64) map[string]float64 {
	filtered := make(map[string]float64, len(ratios))
	for name, ratio := range ratios {
		if isReservedAutoRouteGroupName(strings.TrimSpace(name)) {
			continue
		}
		filtered[name] = ratio
	}
	return filtered
}

func isReservedAutoRouteGroupName(name string) bool {
	return name == defaultAutoRouteGroupName || strings.HasPrefix(name, autoRouteGroupNamePrefix)
}

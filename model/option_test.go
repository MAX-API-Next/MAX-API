package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func setupOptionMapTestState(t *testing.T) {
	t.Helper()

	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func optionMapContainsForTest(key string) bool {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	_, ok := common.OptionMap[key]
	return ok
}

func deleteOptionsForTest(t *testing.T, keys ...string) {
	t.Helper()
	require.NoError(t, DB.Where(commonKeyCol+" IN ?", keys).Delete(&Option{}).Error)
}

func optionExistsForTest(t *testing.T, key string) bool {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", key).Count(&count).Error)
	return count > 0
}

func TestUpdateOptionRejectsAutoRouteGroupRatioNamesBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)

	err := UpdateOption("GroupRatio", `{"auto":1}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "auto route namespace")
	require.False(t, optionMapContainsForTest("GroupRatio"))

	err = UpdateOptionsBulk(map[string]string{
		"group_ratio_setting.group_ratio": `{"auto:fast":1}`,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "auto route namespace")
	require.False(t, optionMapContainsForTest("group_ratio_setting.group_ratio"))
}

func TestUpdateOptionMapRejectsAutoRouteGroupRatioNames(t *testing.T) {
	setupOptionMapTestState(t)

	err := updateOptionMap("GroupRatio", `{"auto":1}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "auto route namespace")
	require.NotContains(t, ratio_setting.GetGroupRatioCopy(), "auto")

	err = updateOptionMap("group_ratio_setting.group_ratio", `{"auto:fast":1}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "auto route namespace")
	require.NotContains(t, ratio_setting.GetGroupRatioCopy(), "auto:fast")

	require.NoError(t, updateOptionMap("GroupRatio", `{"default":1,"vip":0.5}`))
	require.Equal(t, 0.5, ratio_setting.GetGroupRatio("vip"))
}

func TestValidateOptionUpdateRejectsRuntimeConfigParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "auto groups reject nested auto routes",
			key:   "AutoGroups",
			value: `["default","auto:fast"]`,
		},
		{
			name: "auto route config rejects disabled default",
			key:  "AutoGroupRoutes",
			value: `{
				"version":1,
				"default_route":"auto",
				"routes":[{"key":"auto","enabled":false,"user_selectable":true,"groups":["default"]}]
			}`,
		},
		{
			name:  "model ratio rejects malformed json",
			key:   "ModelRatio",
			value: `{`,
		},
		{
			name:  "pay methods reject malformed json",
			key:   "PayMethods",
			value: `{`,
		},
		{
			name:  "status code ranges reject out of bounds",
			key:   "AutomaticRetryStatusCodes",
			value: `999`,
		},
		{
			name:  "request rate limits reject invalid limits",
			key:   "ModelRequestRateLimitGroup",
			value: `{"vip":[-1,1]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, validateOptionUpdate(tt.key, tt.value))
		})
	}
}

func TestUpdateOptionsBulkRejectsRuntimeConfigErrorsBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	deleteOptionsForTest(t, "SystemName", "ModelRatio")
	t.Cleanup(func() {
		deleteOptionsForTest(t, "SystemName", "ModelRatio")
	})

	err := UpdateOptionsBulk(map[string]string{
		"SystemName": "should-not-persist",
		"ModelRatio": `{`,
	})
	require.Error(t, err)
	require.False(t, optionExistsForTest(t, "SystemName"))
	require.False(t, optionExistsForTest(t, "ModelRatio"))
	require.False(t, optionMapContainsForTest("SystemName"))
	require.False(t, optionMapContainsForTest("ModelRatio"))
}

package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
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

func optionValueForTest(t *testing.T, key string) string {
	t.Helper()
	var option Option
	require.NoError(t, DB.First(&option, commonKeyCol+" = ?", key).Error)
	return option.Value
}

func optionMapValueForTest(key string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[key]
}

func TestUpdateOptionFiltersAutoRouteGroupRatioNamesBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	deleteOptionsForTest(t, "GroupRatio", "group_ratio_setting.group_ratio")
	t.Cleanup(func() {
		deleteOptionsForTest(t, "GroupRatio", "group_ratio_setting.group_ratio")
	})

	err := UpdateOption("GroupRatio", `{"auto":1,"default":1.25}`)
	require.NoError(t, err)
	require.NotContains(t, optionValueForTest(t, "GroupRatio"), "auto")
	require.NotContains(t, optionMapValueForTest("GroupRatio"), "auto")
	require.NotContains(t, ratio_setting.GetGroupRatioCopy(), "auto")
	require.Equal(t, 1.25, ratio_setting.GetGroupRatio("default"))

	err = UpdateOptionsBulk(map[string]string{
		"group_ratio_setting.group_ratio": `{"auto:fast":1,"vip":0.5}`,
	})
	require.NoError(t, err)
	require.NotContains(t, optionValueForTest(t, "group_ratio_setting.group_ratio"), "auto:fast")
	require.NotContains(t, optionMapValueForTest("group_ratio_setting.group_ratio"), "auto:fast")
	require.NotContains(t, ratio_setting.GetGroupRatioCopy(), "auto:fast")
	require.Equal(t, 0.5, ratio_setting.GetGroupRatio("vip"))
}

func TestUpdateOptionMapFiltersAutoRouteGroupRatioNames(t *testing.T) {
	setupOptionMapTestState(t)
	groupRatioSetting := ratio_setting.GetGroupRatioSetting()
	registeredGroupRatio := groupRatioSetting.GroupRatio

	err := updateOptionMap("GroupRatio", `{"auto":1,"default":1.25}`)
	require.NoError(t, err)
	require.NotContains(t, ratio_setting.GetGroupRatioCopy(), "auto")
	require.Equal(t, 1.25, ratio_setting.GetGroupRatio("default"))

	err = updateOptionMap("group_ratio_setting.group_ratio", `{"auto:fast":1,"vip":0.5}`)
	require.NoError(t, err)
	require.NotContains(t, ratio_setting.GetGroupRatioCopy(), "auto:fast")
	require.Equal(t, 0.5, ratio_setting.GetGroupRatio("vip"))
	require.Same(t, registeredGroupRatio, ratio_setting.GetGroupRatioSetting().GroupRatio)
	require.JSONEq(t, `{"vip":0.5}`, config.GlobalConfig.ExportAllConfigs()["group_ratio_setting.group_ratio"])

	require.NoError(t, updateOptionMap("GroupRatio", `{"default":1,"vip":0.5}`))
	require.Equal(t, 0.5, ratio_setting.GetGroupRatio("vip"))
	require.Same(t, registeredGroupRatio, ratio_setting.GetGroupRatioSetting().GroupRatio)
	require.JSONEq(t, `{"default":1,"vip":0.5}`, config.GlobalConfig.ExportAllConfigs()["group_ratio_setting.group_ratio"])
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

func TestValidateOptionUpdateRejectsUnsafePricingValues(t *testing.T) {
	pricingKeys := []string{
		"ModelRatio",
		"ModelPrice",
		"CacheRatio",
		"CreateCacheRatio",
		"CompletionRatio",
		"ImageRatio",
		"AudioRatio",
		"AudioCompletionRatio",
	}

	for _, key := range pricingKeys {
		t.Run(key+" rejects null", func(t *testing.T) {
			require.Error(t, validateOptionUpdate(key, `{"unsafe-model":null}`))
		})
		t.Run(key+" rejects negative values", func(t *testing.T) {
			require.Error(t, validateOptionUpdate(key, `{"unsafe-model":-0.01}`))
		})
		t.Run(key+" allows zero", func(t *testing.T) {
			require.NoError(t, validateOptionUpdate(key, `{"free-model":0}`))
		})
	}
}

func TestValidateOptionUpdateRejectsNullRWMapConfigs(t *testing.T) {
	for _, key := range []string{
		"billing_setting.billing_mode",
		"billing_setting.billing_expr",
		"task_billing_setting.rate_cards",
	} {
		t.Run(key, func(t *testing.T) {
			require.Error(t, validateOptionUpdate(key, "null"))
		})
	}
}

func TestUpdateOptionsBulkRejectsNullRWMapConfigBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	deleteOptionsForTest(t, "SystemName", "billing_setting.billing_mode")
	t.Cleanup(func() {
		deleteOptionsForTest(t, "SystemName", "billing_setting.billing_mode")
	})

	err := UpdateOptionsBulk(map[string]string{
		"SystemName":                   "should-not-persist",
		"billing_setting.billing_mode": "null",
	})

	require.Error(t, err)
	require.False(t, optionExistsForTest(t, "SystemName"))
	require.False(t, optionExistsForTest(t, "billing_setting.billing_mode"))
	require.False(t, optionMapContainsForTest("SystemName"))
	require.False(t, optionMapContainsForTest("billing_setting.billing_mode"))
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

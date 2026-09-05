package model

import (
	"fmt"
	"strings"
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

func registerOptionKeysForTest(keys ...string) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	for _, key := range keys {
		common.OptionMap[key] = ""
	}
}

func TestUpdateOptionRejectsUnregisteredKeyBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	deleteOptionsForTest(t, "unregistered.option")
	t.Cleanup(func() { deleteOptionsForTest(t, "unregistered.option") })

	err := UpdateOption("unregistered.option", "value")
	require.Error(t, err)
	require.False(t, optionExistsForTest(t, "unregistered.option"))
}

func TestUpdateOptionsBulkRejectsUnregisteredKeyBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	registerOptionKeysForTest("SystemName")
	deleteOptionsForTest(t, "SystemName", "unregistered.option")
	t.Cleanup(func() { deleteOptionsForTest(t, "SystemName", "unregistered.option") })

	err := UpdateOptionsBulk(map[string]string{
		"SystemName":          "should-not-persist",
		"unregistered.option": "value",
	})
	require.Error(t, err)
	require.False(t, optionExistsForTest(t, "SystemName"))
	require.False(t, optionExistsForTest(t, "unregistered.option"))
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
	registerOptionKeysForTest("GroupRatio", "group_ratio_setting.group_ratio")
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

func TestValidateOptionUpdateRejectsInvalidPreConsumedQuota(t *testing.T) {
	for _, value := range []string{"-1", "1.5", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			require.Error(t, validateOptionUpdate("PreConsumedQuota", value))
		})
	}

	for _, value := range []string{"0", "500", " 1000 "} {
		t.Run("accepts-"+strings.TrimSpace(value), func(t *testing.T) {
			require.NoError(t, validateOptionUpdate("PreConsumedQuota", value))
		})
	}
}

func TestNormalizePreConsumedQuota(t *testing.T) {
	normalized, err := normalizeOptionUpdateValue("PreConsumedQuota", " 1000 ")
	require.NoError(t, err)
	require.Equal(t, "1000", normalized)

	for _, value := range []string{"-1", "1.5", "not-a-number"} {
		_, err := normalizeOptionUpdateValue("PreConsumedQuota", value)
		require.Error(t, err)
	}
}

func TestUpdateOptionMapRejectsInvalidPreConsumedQuotaWithoutMutation(t *testing.T) {
	setupOptionMapTestState(t)
	registerOptionKeysForTest("PreConsumedQuota")
	originalPreConsumedQuota := common.PreConsumedQuota
	common.PreConsumedQuota = 123
	common.OptionMapRWMutex.Lock()
	common.OptionMap["PreConsumedQuota"] = "123"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.PreConsumedQuota = originalPreConsumedQuota })

	err := updateOptionMap("PreConsumedQuota", "not-a-number")

	require.Error(t, err)
	require.Equal(t, 123, common.PreConsumedQuota)
	require.Equal(t, "123", optionMapValueForTest("PreConsumedQuota"))
}

func TestUpdateOptionPersistsPreConsumedQuota(t *testing.T) {
	setupOptionMapTestState(t)
	registerOptionKeysForTest("PreConsumedQuota")
	deleteOptionsForTest(t, "PreConsumedQuota")
	originalPreConsumedQuota := common.PreConsumedQuota
	t.Cleanup(func() {
		common.PreConsumedQuota = originalPreConsumedQuota
		deleteOptionsForTest(t, "PreConsumedQuota")
	})

	require.NoError(t, UpdateOption("PreConsumedQuota", " 1000 "))
	require.Equal(t, "1000", optionValueForTest(t, "PreConsumedQuota"))
	require.Equal(t, "1000", optionMapValueForTest("PreConsumedQuota"))
	require.Equal(t, 1000, common.PreConsumedQuota)
}

func TestValidateOptionUpdateRejectsOversizedPricingMaps(t *testing.T) {
	const entryCount = 20001
	values := make(map[string]float64, entryCount)
	for i := 0; i < entryCount; i++ {
		values[fmt.Sprintf("model-%d", i)] = 1
	}
	data, err := common.Marshal(values)
	require.NoError(t, err)

	require.Error(t, validateOptionUpdate("ModelRatio", string(data)))
}

func TestValidateOptionUpdateRejectsUnsafePricingKeys(t *testing.T) {
	longModelName := strings.Repeat("a", 257)

	require.Error(t, validateOptionUpdate("CompletionRatio", fmt.Sprintf(`{"%s":1}`, longModelName)))
	require.Error(t, validateOptionUpdate("CompletionRatio", `{" ":1}`))
	require.Error(t, validateOptionUpdate("ModelRatio", `{"gemini-2.5-flash-thinking-a":1,"gemini-2.5-flash-thinking-b":2}`))
	require.Error(t, validateOptionUpdate("CompletionRatio", `{"gemini-2.5-pro-thinking-a":1,"gemini-2.5-pro-thinking-b":2}`))
}

func TestValidateOptionUpdateRejectsNullRWMapConfigs(t *testing.T) {
	for _, key := range []string{
		"billing_setting.billing_mode",
		"billing_setting.billing_expr",
		"task_billing_setting.rate_cards",
		"task_billing_setting.h3_profiles",
	} {
		t.Run(key, func(t *testing.T) {
			require.Error(t, validateOptionUpdate(key, "null"))
		})
	}
}

func TestNormalizeDataExportIntervalRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "1441", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			_, err := normalizeOptionUpdateValue("DataExportInterval", value)
			require.Error(t, err)
		})
	}

	normalized, err := normalizeOptionUpdateValue("DataExportInterval", " 60 ")
	require.NoError(t, err)
	require.Equal(t, "60", normalized)
}

func TestUpdateOptionsBulkRejectsNullRWMapConfigBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	registerOptionKeysForTest("SystemName", "billing_setting.billing_mode")
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
	require.Equal(t, "", optionMapValueForTest("SystemName"))
	require.Equal(t, "", optionMapValueForTest("billing_setting.billing_mode"))
}

func TestUpdateOptionsBulkRejectsRuntimeConfigErrorsBeforePersistence(t *testing.T) {
	setupOptionMapTestState(t)
	registerOptionKeysForTest("SystemName", "ModelRatio")
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
	require.Equal(t, "", optionMapValueForTest("SystemName"))
	require.Equal(t, "", optionMapValueForTest("ModelRatio"))
}

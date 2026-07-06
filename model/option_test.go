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

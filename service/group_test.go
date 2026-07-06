package service

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/stretchr/testify/require"
)

func TestHiddenAutoRouteRuntimeUsableButNotUserSelectable(t *testing.T) {
	userGroupsSnapshot := setting.GetUserUsableGroupsCopy()
	userGroupsBytes, err := common.Marshal(userGroupsSnapshot)
	require.NoError(t, err)
	autoRoutesSnapshot := setting.AutoGroupRoutes2JsonString()
	defer func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(string(userGroupsBytes)))
		require.NoError(t, setting.UpdateAutoGroupRoutesByJsonString(autoRoutesSnapshot))
	}()

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, setting.UpdateAutoGroupRoutesByJsonString(`{
		"version": 1,
		"default_route": "auto",
		"routes": [
			{
				"key": "auto",
				"name": "Auto",
				"enabled": true,
				"user_selectable": true,
				"groups": ["default"]
			},
			{
				"key": "auto:internal",
				"name": "Internal",
				"enabled": true,
				"user_selectable": false,
				"groups": ["vip", "svip"]
			}
		]
	}`))

	_, selectable := GetUserAutoRoute("default", "auto:internal", true)
	require.False(t, selectable)
	require.False(t, CanUseTokenGroup("default", "auto:internal"))

	route, runtimeUsable := GetUserAutoRoute("default", "auto:internal", false)
	require.True(t, runtimeUsable)
	require.True(t, CanUseTokenGroupRuntime("default", "auto:internal"))
	require.Equal(t, []string{"vip"}, route.Groups)
}

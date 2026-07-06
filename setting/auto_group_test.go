package setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func snapshotAutoGroupState() ([]string, bool, AutoGroupRoutesConfig) {
	autoGroupMu.RLock()
	defer autoGroupMu.RUnlock()
	return autoGroups, autoGroupRoutesExplicit, cloneAutoGroupRoutesConfig(autoGroupRoutesConfig)
}

func restoreAutoGroupState(groups []string, explicit bool, config AutoGroupRoutesConfig) {
	autoGroupMu.Lock()
	defer autoGroupMu.Unlock()
	autoGroups = groups
	autoGroupRoutesExplicit = explicit
	autoGroupRoutesConfig = config
}

func TestParseAutoGroupRoutesConfigAcceptsLegacyAutoGroups(t *testing.T) {
	config, err := ParseAutoGroupRoutesConfig(`["default","vip","default"]`)
	require.NoError(t, err)
	require.Equal(t, DefaultAutoRouteKey, config.DefaultRoute)
	require.Len(t, config.Routes, 1)
	require.Equal(t, []string{"default", "vip"}, config.Routes[0].Groups)
}

func TestUpdateAutoGroupRoutesIgnoresStaleLegacyAutoGroups(t *testing.T) {
	groups, explicit, config := snapshotAutoGroupState()
	defer restoreAutoGroupState(groups, explicit, config)

	err := UpdateAutoGroupRoutesByJsonString(`{
		"version": 1,
		"default_route": "auto:fast",
		"routes": [
			{
				"key": "auto",
				"name": "Auto",
				"enabled": true,
				"user_selectable": true,
				"groups": ["default"]
			},
			{
				"key": "auto:fast",
				"name": "Fast",
				"enabled": true,
				"user_selectable": true,
				"groups": ["vip", "svip"]
			}
		]
	}`)
	require.NoError(t, err)
	require.Equal(t, []string{"vip", "svip"}, GetAutoGroups())
	require.Equal(t, "auto:fast", GetDefaultAutoRouteKey())

	err = UpdateAutoGroupsByJsonString(`["stale"]`)
	require.NoError(t, err)
	require.Equal(t, []string{"vip", "svip"}, GetAutoGroups())
}

func TestParseAutoGroupRoutesRejectsNestedAutoRouteAsRealGroup(t *testing.T) {
	_, err := ParseAutoGroupRoutesConfig(`{
		"version": 1,
		"default_route": "auto",
		"routes": [
			{
				"key": "auto",
				"enabled": true,
				"user_selectable": true,
				"groups": ["default", "auto:fast"]
			}
		]
	}`)
	require.Error(t, err)
}

func TestParseAutoGroupRoutesDefaultsMissingRouteFlagsToEnabled(t *testing.T) {
	config, err := ParseAutoGroupRoutesConfig(`{
		"version": 1,
		"default_route": "auto",
		"routes": [
			{
				"key": "auto",
				"name": "Auto",
				"groups": ["default"]
			}
		]
	}`)
	require.NoError(t, err)
	require.Len(t, config.Routes, 1)
	require.True(t, config.Routes[0].Enabled)
	require.True(t, config.Routes[0].UserSelectable)
}

func TestParseAutoGroupRoutesRejectsDisabledDefaultRoute(t *testing.T) {
	_, err := ParseAutoGroupRoutesConfig(`{
		"version": 1,
		"default_route": "auto:fast",
		"routes": [
			{
				"key": "auto",
				"name": "Auto",
				"enabled": true,
				"user_selectable": true,
				"groups": ["default"]
			},
			{
				"key": "auto:fast",
				"name": "Fast",
				"enabled": false,
				"user_selectable": true,
				"groups": ["vip"]
			}
		]
	}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be enabled")
}

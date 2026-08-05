package service

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func setupTokenRoutingTestGroups(t *testing.T) {
	t.Helper()

	usableGroups := setting.GetUserUsableGroupsCopy()
	usableGroupsJSON, err := common.Marshal(usableGroups)
	require.NoError(t, err)
	autoRoutesJSON := setting.AutoGroupRoutes2JsonString()
	groupRatiosJSON := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(string(usableGroupsJSON)))
		require.NoError(t, setting.UpdateAutoGroupRoutesByJsonString(autoRoutesJSON))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatiosJSON))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, setting.UpdateAutoGroupRoutesByJsonString(`{
		"version": 1,
		"default_route": "auto",
		"routes": [
			{"key":"auto","name":"Auto","enabled":true,"user_selectable":true,"groups":["default","vip"]},
			{"key":"auto:internal","name":"Internal","enabled":true,"user_selectable":false,"groups":["vip"]}
		]
	}`))
}

func TestSmartTokenRoutePlanUsesConfiguredAutomaticGroupOrder(t *testing.T) {
	setupTokenRoutingTestGroups(t)

	plan, err := BuildTokenRoutePlan(model.TokenRoutingPolicy{
		Version:        model.TokenRoutingPolicyVersion,
		Mode:           model.TokenRoutingModeSmart,
		Route:          "auto",
		RetryOnFailure: true,
	}, "default")
	require.NoError(t, err)
	require.Equal(t, []string{"default", "vip"}, plan.OrderedGroups)
	require.True(t, plan.RetryOnFailure)
}

func TestLegacyEmptyGroupUsesAuthenticatedUserGroup(t *testing.T) {
	setupTokenRoutingTestGroups(t)

	policy := LegacyTokenRoutingPolicy("", false, "vip")
	require.Equal(t, model.TokenRoutingModeManual, policy.Mode)
	require.Equal(t, []string{"vip"}, policy.Groups)

	plan, err := BuildTokenRoutePlan(policy, "vip")
	require.NoError(t, err)
	require.Equal(t, []string{"vip"}, plan.OrderedGroups)
}

func TestResolveTokenRoutingPolicySkipsUnavailableStoredManualGroups(t *testing.T) {
	setupTokenRoutingTestGroups(t)
	token := &model.Token{}
	require.NoError(t, token.SetRoutingPolicy(&model.TokenRoutingPolicy{
		Version:        model.TokenRoutingPolicyVersion,
		Mode:           model.TokenRoutingModeManual,
		Groups:         []string{"missing", "vip", "default"},
		RetryOnFailure: true,
	}))

	policy, legacy, err := ResolveTokenRoutingPolicy(token, "default")
	require.NoError(t, err)
	require.False(t, legacy)
	require.Equal(t, []string{"vip", "default"}, policy.Groups)
}

func TestResolveTokenRoutingPolicyFailsWhenNoStoredManualGroupsRemain(t *testing.T) {
	setupTokenRoutingTestGroups(t)
	token := &model.Token{}
	require.NoError(t, token.SetRoutingPolicy(&model.TokenRoutingPolicy{
		Version: model.TokenRoutingPolicyVersion,
		Mode:    model.TokenRoutingModeManual,
		Groups:  []string{"missing"},
	}))

	_, _, err := ResolveTokenRoutingPolicy(token, "default")
	require.ErrorContains(t, err, "no available groups")
}

func TestExplicitSmartRoutingRequiresUserSelectableRoute(t *testing.T) {
	setupTokenRoutingTestGroups(t)
	policy := model.TokenRoutingPolicy{
		Version: model.TokenRoutingPolicyVersion,
		Mode:    model.TokenRoutingModeSmart,
		Route:   "auto:internal",
	}

	_, err := NormalizeTokenRoutingPolicy(policy, "default")
	require.ErrorContains(t, err, "not available")
	_, err = NormalizeLegacyTokenRoutingPolicy(policy, "default")
	require.NoError(t, err)
}

func TestProjectTokenRoutingPolicyKeepsLegacyGroupProjection(t *testing.T) {
	group, retry := ProjectTokenRoutingPolicy(model.TokenRoutingPolicy{
		Mode:           model.TokenRoutingModeManual,
		Groups:         []string{"vip", "default"},
		RetryOnFailure: true,
	})
	require.Equal(t, "vip", group)
	require.True(t, retry)

	group, retry = ProjectTokenRoutingPolicy(model.TokenRoutingPolicy{
		Mode:           model.TokenRoutingModeSmart,
		Route:          "auto",
		RetryOnFailure: false,
	})
	require.Equal(t, "auto", group)
	require.False(t, retry)
}

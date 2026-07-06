package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupRatioFiltersAutoRouteNamespace(t *testing.T) {
	original := GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupRatioByJSONString(original))
	})

	normalized, err := NormalizeGroupRatioJSONString(`{"auto":1,"auto:fast":2," auto:cheap ":3,"default":1.25,"vip":0.5}`)
	require.NoError(t, err)
	require.NotContains(t, normalized, `"auto"`)
	require.NotContains(t, normalized, `"auto:fast"`)
	require.NotContains(t, normalized, `" auto:cheap "`)
	require.Contains(t, normalized, `"default":1.25`)
	require.Contains(t, normalized, `"vip":0.5`)

	require.NoError(t, UpdateGroupRatioByJSONString(`{"auto":1,"default":1.25}`))
	require.NotContains(t, GetGroupRatioCopy(), "auto")
	require.False(t, ContainsGroupRatio("auto"))
	require.Equal(t, 1.0, GetGroupRatio("auto"))
	require.Equal(t, 1.25, GetGroupRatio("default"))
}

func TestCheckGroupRatioAcceptsNormalGroups(t *testing.T) {
	require.NoError(t, CheckGroupRatio(`{"default":1,"vip":0}`))
}

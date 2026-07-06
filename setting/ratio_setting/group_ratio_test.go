package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckGroupRatioRejectsAutoRouteNamespace(t *testing.T) {
	for _, jsonStr := range []string{
		`{"auto":1}`,
		`{"auto:fast":1}`,
		`{" auto:fast ":1}`,
	} {
		err := CheckGroupRatio(jsonStr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "auto route namespace")
	}
}

func TestCheckGroupRatioAcceptsNormalGroups(t *testing.T) {
	require.NoError(t, CheckGroupRatio(`{"default":1,"vip":0}`))
}

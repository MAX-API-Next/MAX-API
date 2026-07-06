package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/stretchr/testify/require"
)

func TestAddAutoRouteUsableGroupDoesNotOverwriteRatioGroup(t *testing.T) {
	usableGroups := map[string]map[string]interface{}{
		"auto:fast": {
			"ratio": 0.5,
			"desc":  "real ratio group",
		},
	}

	addAutoRouteUsableGroup(usableGroups, setting.AutoGroupRoute{
		Key:    "auto:fast",
		Name:   "Fast route",
		Groups: []string{"vip"},
	})

	require.Equal(t, map[string]interface{}{
		"ratio": 0.5,
		"desc":  "real ratio group",
	}, usableGroups["auto:fast"])
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"math"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestDriverFactsPreservePresenceExtensionsAndUnits(t *testing.T) {
	schema := map[string]UsageField{"units": {Type: "number", Unit: "credit"}, "audio": {Type: "boolean"}, "mode": {Type: "enum", Enum: []string{"off", "on"}}}
	input := types.TaskPricingFacts{"units": 0.6, "audio": false, "mode": "off", "input_images": float64(0), "video_input": "none"}
	got, err := NormalizeDriverFacts(schema, input, false)
	require.NoError(t, err)
	require.Equal(t, input, got)
	got["units"] = float64(0)
	require.Equal(t, 0.6, input["units"], "normalization must detach from the producer")
	zero, err := NormalizeDriverFacts(schema, types.TaskPricingFacts{"units": float64(0)}, true)
	require.NoError(t, err)
	require.Contains(t, zero, "units")
	require.Equal(t, float64(0), zero["units"])
	missing, err := NormalizeDriverFacts(schema, nil, true)
	require.NoError(t, err)
	require.Nil(t, missing)
	saturated, err := NormalizeDriverFacts(schema, types.TaskPricingFacts{"units": float64(common.MaxQuota) + 1}, true)
	require.NoError(t, err)
	require.Equal(t, float64(common.MaxQuota), saturated["units"], "API v1 numeric saturation must not round fractional credit below the ceiling")
	for _, bad := range []types.TaskPricingFacts{{"units": -1.0}, {"units": math.Inf(1)}, {"units": "0.6"}, {"audio": float64(0)}, {"mode": "unknown"}, {"image_count": float64(129)}, {"seconds": float64(3601)}, {"extension": float64(3601)}} {
		_, err := NormalizeDriverFacts(schema, bad, false)
		require.ErrorIs(t, err, ErrPluginHook)
	}
	_, err = NormalizeDriverFacts(schema, types.TaskPricingFacts{"extension": map[string]any{"nested": "value"}}, false)
	require.ErrorIs(t, err, ErrPluginSDK)
}

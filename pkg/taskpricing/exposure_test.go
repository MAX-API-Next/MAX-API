// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func exposureFixture() ExposureInput {
	c, _ := KnownContract("kling", "kling-v1")
	cap := 8.0 // Synthetic host-enforced fixture bound, not a real Kling limit.
	p := c.FieldPolicy["units"]
	p.Max = &cap
	c.FieldPolicy["units"] = p
	return ExposureInput{Profile: NewAPITask, Expression: `u("units") * 0.1`, Contract: c, Submit: types.TaskPricingFacts{"units": 6},
		PricingAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: 1, ReservationFloor: 700}
}

func TestTaskExposureUsesSharedCoreAndIndependentUpperEndpoints(t *testing.T) {
	in := exposureFixture()
	got, err := QuoteExposure(in)
	require.NoError(t, err)
	require.Equal(t, 600, got.Estimate.QuotaAfterGroup) // 6*.1*1000
	require.Equal(t, 800, got.Maximum.QuotaAfterGroup)  // 8*.1*1000
	require.Equal(t, 800, got.RequiredReservationQuota)
	require.False(t, got.AdmissionReady)
	require.False(t, got.ProviderCostProven)
	in.Submit["units"] = 0
	got, err = QuoteExposure(in)
	require.NoError(t, err)
	require.Zero(t, got.Estimate.QuotaAfterGroup)
	require.Equal(t, 800, got.RequiredReservationQuota, "zero estimate is not a zero exposure bound")
	in.Submit["units"] = 6
	// Maximum is below the floor: apply the floor once, never add it.
	in.Expression = `v1:u("units") * 0.01`
	got, err = QuoteExposure(in)
	require.NoError(t, err)
	require.Equal(t, 80, got.Maximum.QuotaAfterGroup)
	require.Equal(t, 700, got.RequiredReservationQuota)
	in.GroupRatio = 0
	got, err = QuoteExposure(in)
	require.NoError(t, err)
	require.Zero(t, got.RequiredReservationQuota)
	in.GroupRatio = 1
	in.Expression = "0"
	got, err = QuoteExposure(in)
	require.NoError(t, err)
	require.Zero(t, got.Maximum.QuotaAfterGroup)
	require.Equal(t, 700, got.RequiredReservationQuota)

	in.Contract, _ = KnownContract("hailuo", "MiniMax-H3")
	in.Submit = types.TaskPricingFacts{"seconds": 6, "resolution": "768P", "input_images": 2, "input_video_seconds": 3}
	in.Expression = `u("seconds") * 0.1`
	got, err = QuoteExposure(in)
	require.NoError(t, err)
	require.Equal(t, 1500, got.Maximum.QuotaAfterGroup) // 15s*.1*1000; no fake cap on unreferenced images
	in.Contract, _ = KnownContract("doubao", "doubao-seedance-1-5-pro-251215")
	cap := 200000.0 // Synthetic test policy only.
	p := in.Contract.FieldPolicy["tokens"]
	p.Max = &cap
	in.Contract.FieldPolicy["tokens"] = p
	in.Submit = types.TaskPricingFacts{"tokens": 100000, "resolution": "720p", "generate_audio": false}
	in.Expression = `u("tokens") / 1000000 * (u("generate_audio") ? 5 : 3)`
	got, err = QuoteExposure(in)
	require.NoError(t, err)
	require.Equal(t, 600, got.Maximum.QuotaAfterGroup) // 200k/1M*3*1000; false must stay false
	require.Equal(t, false, got.EndpointFacts["generate_audio"])
	delete(in.Submit, "generate_audio")
	got, err = QuoteExposure(in)
	require.Error(t, err)
	require.Nil(t, got)
}

func TestTaskExposureRejectsUnprovedMathBoundsAndOverflow(t *testing.T) {
	for _, profile := range []string{"", LegacyToken, LegacyMiniMax} {
		in := exposureFixture()
		in.Profile = profile
		got, err := QuoteExposure(in)
		require.Error(t, err)
		require.Nil(t, got)
	}
	for _, expression := range []string{`u("units") * (8 - u("units"))`, `u("units") > 3 ? 1 : 2`, `1 / u("units")`,
		`max(u("units"), 1)`, `param("cost")`, `true ? u("units") : param("cost")`, `9223372036854775807 * 2 * u("units") + 1000`,
		`(9007199254740991 + 9007199254740991) * u("units")`, `u("units") * 1e300`} {
		in := exposureFixture()
		in.Expression = expression
		got, err := QuoteExposure(in)
		require.Error(t, err, expression)
		require.Nil(t, got)
	}
	in := exposureFixture()
	in.Contract, _ = KnownContract("kling", "kling-v1")
	got, err := QuoteExposure(in)
	require.ErrorIs(t, err, ErrExposureUnproven)
	require.Nil(t, got)
	in.Contract, _ = KnownContract("doubao", "doubao-seedance-1-5-pro-251215")
	in.Expression = `u("tokens") / 1000000`
	in.Submit = types.TaskPricingFacts{"tokens": 100, "resolution": "720p", "generate_audio": false}
	got, err = QuoteExposure(in)
	require.ErrorIs(t, err, ErrExposureUnproven)
	require.Nil(t, got)
}

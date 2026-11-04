// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

// Independent wire contract: 6s*.1*1000=600; floor700; max15s=1500.
// It deliberately does not call Freeze to derive expected totals.
func boundedWirePlan(t *testing.T) *types.TaskBillingPlan {
	t.Helper()
	c, ok := KnownContract("hailuo", "MiniMax-H3")
	require.True(t, ok)
	c, err := NormalizeContract(c)
	require.NoError(t, err)
	digest, err := ContractDigest(c)
	require.NoError(t, err)
	expression := "u(\"seconds\") * 0.1"
	data, err := common.Marshal(map[string]any{
		"profile": NewAPITask, "source": "task_expression", "rule_key": "hailuo::MiniMax-H3", "schema_version": 1, "mode": "bounded_actual",
		"config_hash": "independent-price-v1", "currency": "USD", "group_ratio": 1, "quota_per_unit": 1000,
		"usage_producer_kind": "task_plugin", "usage_source_id": "hailuo", "usage_schema_version": 1, "usage_contract_digest": digest,
		"estimate_quota": 600, "reserve_quota": 1500,
		"expression_plan": map[string]any{
			"version": 2, "original_model": "MiniMax-H3", "mapped_model": "MiniMax-H3", "selected_model": "MiniMax-H3", "selected_plugin_key": "hailuo",
			"driver": "task_plugin", "plugin_key": "hailuo", "plugin_version": "1.0.0", "package_digest": strings.Repeat("a", 64),
			"channel_id": 902, "action": "generate", "request_id": "bound-h3", "binding_version": "independent-binding-v1",
			"expression": expression, "expression_hash": fmt.Sprintf("%x", sha256.Sum256([]byte(expression))),
			"pricing_at": "2026-11-02T00:00:00Z", "rounding": "half_away_from_zero@1", "reservation_floor": 700,
			"contract": c, "submit_facts": map[string]any{"seconds": 6, "resolution": "768P", "input_images": 0, "input_video_seconds": 0},
			"charge_bound": map[string]any{"method": "nonnegative-monotone-endpoint@1", "maximum_quota": 1500},
		},
	})
	require.NoError(t, err)
	var plan types.TaskBillingPlan
	require.NoError(t, common.Unmarshal(data, &plan))
	return &plan
}

func TestBoundedSnapshotIndependentWireRestoresUpperReservation(t *testing.T) {
	plan := boundedWirePlan(t)
	require.NoError(t, ValidateSnapshot(plan))
	require.Equal(t, 600, plan.EstimateQuota)
	require.Equal(t, 1500, plan.ReserveQuota)
	final, err := QuoteFinal(plan, finalFixture(plan, types.TaskPricingFacts{"seconds": 8, "input_images": 0, "input_video_seconds": 0}))
	require.NoError(t, err)
	require.Equal(t, 800, final.QuotaAfterGroup)
	require.Equal(t, 700, plan.ReserveQuota-final.QuotaAfterGroup)
}

func boundedFreezeInput(t *testing.T) FreezeInput {
	t.Helper()
	p := boundedWirePlan(t)
	c := p.ExpressionPlan.Contract
	return FreezeInput{
		PreviewInput: PreviewInput{
			Profile: NewAPITask, Prices: types.TaskPricingMaps{PluginBillingExpr: map[string]string{"hailuo::MiniMax-H3": "u(\"seconds\") * 0.1"}},
			Source:        Source{Driver: types.TaskUsageProducerKindTaskPlugin, PluginKey: "hailuo"},
			OriginalModel: "MiniMax-H3", MappedModel: "MiniMax-H3", Contract: c,
			Expected:  Guards{"independent-price-v1", p.UsageContractDigest, "independent-binding-v1"},
			Current:   Guards{"independent-price-v1", p.UsageContractDigest, "independent-binding-v1"},
			Submit:    types.TaskPricingFacts{"seconds": 6, "resolution": "768P", "input_images": 0, "input_video_seconds": 0},
			PricingAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: 1, ReservationFloor: 700,
		},
		ChannelID: 902, RequestID: "bound-h3", Action: "generate", SourceID: "hailuo", PluginVersion: "1.0.0", PackageDigest: strings.Repeat("a", 64),
	}
}

func TestBoundedFreezeKeepsEstimateAndAppliesOneMaximumFloor(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		ratio                              float64
		floor, estimate, maximum, reserved int
	}{
		{"upper-above-floor", 1, 700, 600, 1500, 1500},
		{"floor-above-upper", 1, 2000, 600, 1500, 2000},
		{"free-group", 0, 700, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := boundedFreezeInput(t)
			in.GroupRatio, in.ReservationFloor = tc.ratio, tc.floor
			plan, err := FreezeBounded(in)
			require.NoError(t, err)
			require.Equal(t, 2, plan.ExpressionPlan.Version)
			require.Equal(t, tc.estimate, plan.EstimateQuota)
			require.Equal(t, tc.maximum, plan.ExpressionPlan.ChargeBound.MaximumQuota)
			require.Equal(t, tc.reserved, plan.ReserveQuota)
			wire, err := common.Marshal(plan)
			require.NoError(t, err)
			var restored types.TaskBillingPlan
			require.NoError(t, common.Unmarshal(wire, &restored))
			require.NoError(t, ValidateSnapshot(&restored))
		})
	}
	in := boundedFreezeInput(t)
	old, err := Freeze(in)
	require.NoError(t, err)
	require.Equal(t, 1, old.ExpressionPlan.Version)
	require.Nil(t, old.ExpressionPlan.ChargeBound)
	require.Equal(t, 700, old.ReserveQuota, "the v1 contract remains unchanged")
}

func TestBoundedSnapshotRejectsTamperingAndClonesProof(t *testing.T) {
	for name, corrupt := range map[string]func(*types.TaskBillingPlan){
		"missing-bound":        func(p *types.TaskBillingPlan) { p.ExpressionPlan.ChargeBound = nil },
		"v1-with-bound":        func(p *types.TaskBillingPlan) { p.ExpressionPlan.Version = 1 },
		"unknown-version":      func(p *types.TaskBillingPlan) { p.ExpressionPlan.Version = 3 },
		"unknown-method":       func(p *types.TaskBillingPlan) { p.ExpressionPlan.ChargeBound.Method = "unproved" },
		"wrong-maximum":        func(p *types.TaskBillingPlan) { p.ExpressionPlan.ChargeBound.MaximumQuota = 700 },
		"estimate-reservation": func(p *types.TaskBillingPlan) { p.ReserveQuota = 700 },
		"floor-added-to-upper": func(p *types.TaskBillingPlan) { p.ReserveQuota = 2200 },
	} {
		t.Run(name, func(t *testing.T) {
			p := boundedWirePlan(t)
			corrupt(p)
			require.Error(t, ValidateSnapshot(p))
		})
	}
	original := boundedWirePlan(t)
	cloned := types.CloneTaskBillingPlan(original)
	cloned.ExpressionPlan.ChargeBound.MaximumQuota = 99
	require.Equal(t, 1500, original.ExpressionPlan.ChargeBound.MaximumQuota)
}

func TestBoundedFreezeRejectsMissingMaxAndKeepsFalsePresence(t *testing.T) {
	uncapped := freezeFixture(t, fixtures(t)[1])
	// The old sample contains a synthetic cap; use the actual uncapped host
	// configuration contract here instead of treating sample data as evidence.
	uncapped.Contract, _ = KnownContract("kling", "kling-v1")
	digest, err := ContractDigest(uncapped.Contract)
	require.NoError(t, err)
	uncapped.Expected.ContractDigest, uncapped.Current.ContractDigest = digest, digest
	uncapped.Prices.PluginBillingExpr[uncapped.Source.PluginKey+"::"+uncapped.OriginalModel] = "u(\"units\") * 0.1"
	_, err = FreezeBounded(uncapped)
	require.ErrorIs(t, err, ErrExposureUnproven)
	in := boundedFreezeInput(t)
	in.Current.PriceVersion = "changed"
	_, err = FreezeBounded(in)
	require.Error(t, err)
	in = boundedFreezeInput(t)
	delete(in.Submit, "seconds")
	_, err = FreezeBounded(in)
	require.Error(t, err)
	// A request-only branch needs no invented token cap; actual provider cost
	// is still not certified. false is a supplied request fact, never missing.
	in = freezeFixture(t, fixtures(t)[2])
	in.Prices.PluginBillingExpr[in.Source.PluginKey+"::"+in.OriginalModel] = "u(\"generate_audio\") ? 0.0 : 0.2"
	in.Submit = types.TaskPricingFacts{"tokens": 200000, "resolution": "720p", "generate_audio": false}
	in.QuotaPerUnit, in.GroupRatio, in.ReservationFloor = 1000, 1, 0
	p, err := FreezeBounded(in)
	require.NoError(t, err)
	require.Equal(t, 200, p.EstimateQuota)
	require.Equal(t, 200, p.ReserveQuota)
	require.Equal(t, false, p.ExpressionPlan.SubmitFacts["generate_audio"])
	delete(in.Submit, "generate_audio")
	_, err = FreezeBounded(in)
	require.Error(t, err)
}

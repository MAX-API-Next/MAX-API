// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestFrozenTaskPlanVersionPrefixSurvivesFreezeAndReload(t *testing.T) {
	for _, prefix := range []string{"", "v1:"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			f := fixtures(t)[1]
			in := freezeFixture(t, f)
			expression := prefix + `u("units") * 0.1`
			in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = expression
			in.Submit = types.TaskPricingFacts{"units": 4}
			in.QuotaPerUnit, in.GroupRatio, in.ReservationFloor = 1000, 1.5, 700
			previewInput := in.PreviewInput
			previewInput.Actual = types.TaskPricingFacts{"units": 2.5}
			preview, err := Preview(previewInput)
			require.NoError(t, err)
			// Independently derived: 4*.1*1000*1.5=600; floor700;
			// final2.5*.1*1000*1.5=375, never another /1M conversion.
			require.Equal(t, 600, preview.Estimate.QuotaAfterGroup)
			require.Equal(t, 700, preview.ReservedQuota)
			require.NotNil(t, preview.Final)
			require.Equal(t, 375, preview.Final.QuotaAfterGroup)
			plan, err := Freeze(in)
			require.NoError(t, err)
			require.Equal(t, expression, plan.ExpressionPlan.Expression)
			expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(expression)))
			require.Equal(t, expectedHash, plan.ExpressionPlan.ExpressionHash)
			wire, err := common.Marshal(plan)
			require.NoError(t, err)
			var restored types.TaskBillingPlan
			require.NoError(t, common.Unmarshal(wire, &restored))
			require.NoError(t, ValidateSnapshot(&restored))
			require.Equal(t, expression, restored.ExpressionPlan.Expression)
			require.Equal(t, expectedHash, restored.ExpressionPlan.ExpressionHash)
			final, err := QuoteFinal(&restored, finalFixture(&restored, types.TaskPricingFacts{"units": 2.5}))
			require.NoError(t, err)
			require.InDelta(t, 0.25, final.Amount, 1e-12)
			require.Equal(t, 375, final.QuotaAfterGroup)
		})
	}
}

func TestFrozenTaskPlanVersionedRequestProbesRemainRejected(t *testing.T) {
	for _, expression := range []string{
		`v1: param("service_tier") == "fast" ? 2.0 : 1.0`,
		`v1: false ? (has(header("openai-beta"), "fast") ? 2.0 : 1.0) : 0.0`,
	} {
		t.Run(expression, func(t *testing.T) {
			f := fixtures(t)[1]
			in := freezeFixture(t, f)
			in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = expression
			_, err := Preview(in.PreviewInput)
			require.NoError(t, err, "read-only pricing supports these dependencies")
			_, err = Freeze(in)
			require.ErrorContains(t, err, "request probes are not supported in frozen task plans")
		})
	}
	for _, expression := range []string{`v2:u("units") * 0.1`, `v1:u("units") +`} {
		in := freezeFixture(t, fixtures(t)[1])
		in.Prices.PluginBillingExpr[in.Source.PluginKey+"::"+in.OriginalModel] = expression
		_, err := Freeze(in)
		require.Error(t, err, "unknown version or malformed expression must still fail")
	}
}

func freezeFixture(t *testing.T, f sample) FreezeInput {
	t.Helper()
	in := previewInput(t, f)
	in.Actual = nil
	in.MappedModel = in.OriginalModel
	return FreezeInput{PreviewInput: in, ChannelID: 902, Action: "generate", RequestID: "frozen-request",
		SourceID: f.PluginKey, PluginVersion: "1.0.0", PackageDigest: strings.Repeat("a", 64)}
}

func finalFixture(plan *types.TaskBillingPlan, facts types.TaskPricingFacts) *types.TaskPricingUsageEnvelope {
	return &types.TaskPricingUsageEnvelope{ProducerKind: plan.UsageProducerKind, SourceID: plan.UsageSourceID,
		PluginVersion: plan.ExpressionPlan.PluginVersion, PackageDigest: plan.ExpressionPlan.PackageDigest,
		ChannelID: plan.ExpressionPlan.ChannelID, RequestID: plan.ExpressionPlan.RequestID, Action: plan.ExpressionPlan.Action,
		SchemaVersion: plan.UsageSchemaVersion, ContractDigest: plan.UsageContractDigest,
		Stage: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete, Facts: facts}
}

func TestFrozenTaskPlanThreeFamilyIndependentAmountsAfterReload(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.ID, func(t *testing.T) {
			in := freezeFixture(t, f)
			plan, err := Freeze(in)
			require.NoError(t, err)
			require.Equal(t, f.Expected.Reserved, plan.ReserveQuota)
			wire, err := common.Marshal(plan)
			require.NoError(t, err)
			var restored types.TaskBillingPlan
			require.NoError(t, common.Unmarshal(wire, &restored))
			// Mutation of live inputs cannot change the persisted contract/price.
			in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = "999"
			in.Submit["untrusted_new_field"] = 777
			for key, field := range in.Contract.UsageSchema {
				field.Unit = "changed_unit"
				in.Contract.UsageSchema[key] = field
				break
			}
			quote, err := QuoteFinal(&restored, finalFixture(&restored, f.Actual))
			require.NoError(t, err)
			// Expected values come from compat-v1's integer/rational ledger.
			require.Equal(t, f.Expected.Target, quote.QuotaAfterGroup)
			require.InDelta(t, f.Expected.FinalUSD, quote.Amount, 1e-10)
			require.Equal(t, plan.ExpressionPlan.PricingAt, quote.PricingAt)
		})
	}
}

func TestFrozenTaskPlanRejectsUnfrozenDependenciesAndCorruption(t *testing.T) {
	f := fixtures(t)[1] // fractional Kling credits
	in := freezeFixture(t, f)
	for _, expression := range []string{`param("service_tier") == "fast" ? 2 : 1`, `false ? header("openai-beta") : 0`} {
		in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = expression
		_, err := Freeze(in)
		require.Error(t, err)
	}
	in = freezeFixture(t, fixtures(t)[1])
	plan, err := Freeze(in)
	require.NoError(t, err)
	for name, corrupt := range map[string]func(*types.TaskBillingPlan){
		"profile":  func(p *types.TaskBillingPlan) { p.Profile = "unknown@1" },
		"hash":     func(p *types.TaskBillingPlan) { p.ExpressionPlan.Expression += " + 1" },
		"rounding": func(p *types.TaskBillingPlan) { p.ExpressionPlan.Rounding = "floor" },
		"unit": func(p *types.TaskBillingPlan) {
			v := p.ExpressionPlan.Contract.UsageSchema["units"]
			v.Unit = "token"
			p.ExpressionPlan.Contract.UsageSchema["units"] = v
		},
		"reserve":  func(p *types.TaskBillingPlan) { p.ReserveQuota++ },
		"estimate": func(p *types.TaskBillingPlan) { p.EstimateQuota++ },
		"request":  func(p *types.TaskBillingPlan) { p.ExpressionPlan.RequestID = "" },
		"package":  func(p *types.TaskBillingPlan) { p.ExpressionPlan.PackageDigest = "not-a-digest" },
		"source":   func(p *types.TaskBillingPlan) { p.UsageSourceID = "another-plugin" },
		"legacy":   func(p *types.TaskBillingPlan) { p.Components = []types.TaskBillingPlanComponent{{Key: "legacy"}} },
	} {
		t.Run(name, func(t *testing.T) {
			copy := types.CloneTaskBillingPlan(plan)
			corrupt(copy)
			require.Error(t, ValidateSnapshot(copy))
			require.NoError(t, ValidateSnapshot(plan), "clone must not alias nested contract state")
		})
	}
}

func TestFrozenTaskPlanFinalPresenceIdentityAndExplicitZero(t *testing.T) {
	plan, err := Freeze(freezeFixture(t, fixtures(t)[1]))
	require.NoError(t, err)
	zero := finalFixture(plan, types.TaskPricingFacts{"units": 0})
	quote, err := QuoteFinal(plan, zero)
	require.NoError(t, err)
	require.Zero(t, quote.QuotaAfterGroup)
	for name, corrupt := range map[string]func(*types.TaskPricingUsageEnvelope){
		"missing":         func(e *types.TaskPricingUsageEnvelope) { e.Facts = nil },
		"partial":         func(e *types.TaskPricingUsageEnvelope) { e.Completeness = "partial" },
		"source":          func(e *types.TaskPricingUsageEnvelope) { e.SourceID = "same-model-other-source" },
		"package-version": func(e *types.TaskPricingUsageEnvelope) { e.PluginVersion = "2.0.0" },
		"package-digest":  func(e *types.TaskPricingUsageEnvelope) { e.PackageDigest = strings.Repeat("b", 64) },
		"channel":         func(e *types.TaskPricingUsageEnvelope) { e.ChannelID++ },
		"request":         func(e *types.TaskPricingUsageEnvelope) { e.RequestID += "-other" },
		"action":          func(e *types.TaskPricingUsageEnvelope) { e.Action = "other" },
		"schema":          func(e *types.TaskPricingUsageEnvelope) { e.SchemaVersion++ },
		"contract":        func(e *types.TaskPricingUsageEnvelope) { e.ContractDigest = "new-unit-contract" },
		"stage":           func(e *types.TaskPricingUsageEnvelope) { e.Stage = "request_estimate" },
		"string":          func(e *types.TaskPricingUsageEnvelope) { e.Facts["units"] = "0" },
		"unknown":         func(e *types.TaskPricingUsageEnvelope) { e.Facts["extra"] = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := types.CloneTaskPricingUsageEnvelope(zero)
			corrupt(copy)
			_, err := QuoteFinal(plan, copy)
			require.Error(t, err)
		})
	}
}

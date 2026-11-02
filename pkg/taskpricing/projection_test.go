// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func projectionFixture() ApprovedPhaseContract {
	c, _ := KnownContract("kling", "kling-v1")
	return ApprovedPhaseContract{Contract: c, Projection: FactProjection{StatusPath: "data.task_status", SuccessStates: []string{"succeed"},
		SubmitExtras: types.TaskPricingFacts{"legacy": float64(0)}, ActualPaths: map[string][]string{"units": {"data.final_unit_deduction", "data.other"}}}}
}

func TestReviewedFactProjectionPreservesPresenceAndChecksRawEvidence(t *testing.T) {
	a := projectionFixture()
	input := types.TaskPricingFacts{"units": 6, "legacy": 0}
	submit, err := a.ProjectSubmit(input)
	require.NoError(t, err)
	require.Equal(t, []string{"legacy"}, submit.DroppedKeys)
	require.Len(t, input, 2, "projection must not mutate hook output")
	for _, raw := range []string{
		`{"data":{"task_status":"succeed","final_unit_deduction":0,"other":9}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":"0","other":9}}`,
		`{"data":{"task_status":"succeed","other":0}}`,
	} {
		final, err := a.ProjectFinal(submit.Facts, types.TaskPricingFacts{"units": 0}, []byte(raw))
		require.NoError(t, err)
		require.Equal(t, float64(0), final.Facts["units"])
	}
	for _, raw := range []string{
		`{"data":{"task_status":"processing","final_unit_deduction":2.5}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":"2.5junk"}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":false}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":null,"other":2.5}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":0,"other":2.5}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":"NaN"}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":"0x1p2"}}`,
		`{"data":{"task_status":"succeed"}}`,
	} {
		out, err := a.ProjectFinal(submit.Facts, types.TaskPricingFacts{"units": 2.5}, []byte(raw))
		require.Error(t, err, raw)
		require.Nil(t, out.Facts)
	}
	out, err := a.ProjectFinal(submit.Facts, nil, []byte(`{"data":{"task_status":"succeed","final_unit_deduction":6}}`))
	require.Error(t, err, "missing actual hook facts cannot inherit the submit estimate")
	require.Nil(t, out.Facts)
	_, err = a.ProjectSubmit(types.TaskPricingFacts{"units": 6, "legacy": 1})
	require.Error(t, err)
	_, err = a.ProjectSubmit(types.TaskPricingFacts{"units": 6, "unknown": 0})
	require.Error(t, err)
	_, err = a.ProjectFinal(submit.Facts, types.TaskPricingFacts{"units": 2.5}, []byte(strings.Repeat(" ", MaxProjectionResponseBytes+1)))
	require.Error(t, err)
	a.Projection.SubmitExtras["units"] = 0
	_, err = a.ProjectSubmit(input)
	require.Error(t, err, "a declared fact cannot be discarded")
}

func TestReviewedFactProjectionPolicyIsDetachedAndVersioned(t *testing.T) {
	a := projectionFixture()
	p := phasePackage(t, "1.0.0", "synthetic projection policy fixture", a.Contract)
	e := PhaseApproval{PluginKey: "kling", Version: "1.0.0", PackageDigest: p.Digest(), Model: "kling-v1", Action: "generate", Contract: a.Contract, Projection: &a.Projection, Evidence: "synthetic policy"}
	r, err := NewPhaseRegistry([]PhaseApproval{e})
	require.NoError(t, err)
	first, err := r.Resolve(p, "kling-v1", "generate")
	require.NoError(t, err)
	e.Projection.ActualPaths["units"][0] = "data.changed"
	first.Projection.ActualPaths["units"][0] = "data.also_changed"
	first.Projection.SubmitExtras["legacy"] = 9
	second, err := r.Resolve(p, "kling-v1", "generate")
	require.NoError(t, err)
	require.Equal(t, "data.final_unit_deduction", second.Projection.ActualPaths["units"][0])
	require.Equal(t, float64(0), second.Projection.SubmitExtras["legacy"])
	replacement, err := NewPhaseRegistry([]PhaseApproval{e})
	require.NoError(t, err)
	changed, err := replacement.Resolve(p, "kling-v1", "generate")
	require.NoError(t, err)
	require.NotEqual(t, second.ApprovalID, changed.ApprovalID)
	require.Equal(t, second.ContractDigest, changed.ContractDigest, "raw witness policy changes without changing the fact schema")
}

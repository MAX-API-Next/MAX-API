// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestApprovedPluginProjectionReusesSettlementAndRejectsPolicyReplacement(t *testing.T) {
	task, adaptor := reserveExpressionTask(t)
	catalog, err := taskplugin.OpenCatalog(t.Context(), pricingCatalogStore(t))
	require.NoError(t, err)
	legacy, _, _ := pricingPackageTask(t, "1.0.0", "credit")
	m := legacy.Manifest()
	m.UsageProfiles = []taskplugin.UsageProfile{{Models: m.Models, Schema: m.UsageSchema}}
	// This is a synthetic accounting seam, not the real Kling v3 JS package.
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 2, Manifest: m, Source: "synthetic projection accounting fixture; never executed"})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	plan.ExpressionPlan.PackageDigest = pkg.Digest() // bind fixture bytes before persisting its binding
	actor := taskplugin.Actor{UserID: task.UserId, TokenID: task.PrivateData.TokenId, ChannelID: task.ChannelId, RequestID: task.PrivateData.BillingRequestId}
	entry := taskpricing.PhaseApproval{PluginKey: "kling", Version: "1.0.0", PackageDigest: pkg.Digest(), Model: "kling-v1", Action: "generate",
		Contract: plan.ExpressionPlan.Contract, Evidence: "synthetic accounting fixture only", Projection: &taskpricing.FactProjection{
			StatusPath: "data.task_status", SuccessStates: []string{"succeed"}, ActualPaths: map[string][]string{"units": {"data.final_unit_deduction"}}}}
	r, err := taskpricing.NewPhaseRegistry([]taskpricing.PhaseApproval{entry})
	require.NoError(t, err)
	binding, _, err := r.Bind(catalog.Host(), actor, "POST", "/isolated/kling/jobs", "kling-v1", "kling-v1", "generate")
	require.NoError(t, err)
	require.NoError(t, AttachTaskPluginBinding(catalog.Host(), task, binding))
	adaptor.result.PricingUsageEnvelope = nil
	before, err := common.Marshal(adaptor.result)
	require.NoError(t, err)
	for _, raw := range []string{
		`{"data":{"task_status":"succeed","final_unit_deduction":"2.5junk"}}`,
		`{"data":{"task_status":"processing","final_unit_deduction":2.5}}`,
		`{"data":{"task_status":"succeed","final_unit_deduction":null}}`,
	} {
		require.Error(t, ApplyApprovedTaskPluginFacts(t.Context(), catalog, r, task, actor, adaptor.result, types.TaskPricingFacts{"units": 2.5}, []byte(raw)))
		after, err := common.Marshal(adaptor.result)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after))
		require.EqualValues(t, 1300, getUserQuota(t, 901))
		require.Zero(t, countLogs(t))
	}
	require.ErrorIs(t, ApplyTaskPricingPluginFacts(t.Context(), catalog, task, actor, adaptor.result, types.TaskPricingFacts{"units": 2.5}), taskplugin.ErrBindingRejected)
	entry.Evidence += " changed review"
	replacement, err := taskpricing.NewPhaseRegistry([]taskpricing.PhaseApproval{entry})
	require.NoError(t, err)
	raw := []byte(`{"data":{"task_status":"succeed","final_unit_deduction":"2.5"}}`)
	require.ErrorIs(t, ApplyApprovedTaskPluginFacts(t.Context(), catalog, replacement, task, actor, adaptor.result, types.TaskPricingFacts{"units": 2.5}, raw), taskplugin.ErrBindingRejected)
	require.NoError(t, ApplyApprovedTaskPluginFacts(t.Context(), catalog, r, task, actor, adaptor.result, types.TaskPricingFacts{"units": 2.5}, raw))
	require.EqualValues(t, 1300, getUserQuota(t, 901), "attaching facts must not settle funds")
	task.IncludePrivateDataInUpdate()
	require.NoError(t, model.DB.Save(task).Error)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, binding.PhaseApprovalID, reloaded.PrivateData.PluginBinding.PhaseApprovalID)
	stale := reloaded
	stored := pollExpressionFixture(t, &reloaded, adaptor)
	assertExpressionLedger(t, stored, 375) // independent: 2.5*.1*1000*1.5; refund325 once
	stored = pollExpressionFixture(t, &stale, adaptor)
	assertExpressionLedger(t, stored, 375)
}

// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestTaskPricingReceiptCompatibilityRequiresExplicitPolicy(t *testing.T) {
	for _, policy := range []ReceiptRequirement{"", "unknown", ReceiptWalletV1} {
		require.ErrorIs(t, ValidateReceiptCompatibility(policy, nil, WalletReceiptInput{}), ErrTaskPricingReceiptConflict)
	}
	// Neither a zero amount nor a missing receipt proves legacy provenance.
	require.NoError(t, ValidateReceiptCompatibility(ReceiptLegacy, nil, WalletReceiptInput{}))
}

func TestTaskPricingReceiptCompatibilityRejectsLegacyDowngrade(t *testing.T) {
	for _, input := range []WalletReceiptInput{
		{Plan: &types.TaskBillingPlan{Profile: types.TaskPluginExpressionProfile}},
		{Plan: &types.TaskBillingPlan{ExpressionPlan: &types.TaskExpressionPlan{}}},
		{Plan: &types.TaskBillingPlan{Source: "task_expression"}},
		{Binding: &types.TaskPluginBinding{Profile: types.TaskPluginExpressionProfile}},
		{Binding: &types.TaskPluginBinding{PhaseApprovalID: "new-profile-marker"}},
		{Binding: &types.TaskPluginBinding{Action: "generate"}},
	} {
		require.ErrorIs(t, ValidateReceiptCompatibility(ReceiptLegacy, nil, input), ErrTaskPricingReceiptConflict)
	}
	for _, receipt := range []*TaskPricingReceipt{{}, {Version: 1, Digest: "unexpected"}, {Version: 999}} {
		require.ErrorIs(t, ValidateReceiptCompatibility(ReceiptLegacy, receipt, WalletReceiptInput{}), ErrTaskPricingReceiptConflict)
	}
}

func TestTaskPricingReceiptCompatibilityPreservesLegacyModels(t *testing.T) {
	// Ordinary rows have no plan; structured H3 and the old typed-fact plugin
	// branch retain their old validators. This gate never reinterprets them.
	for _, input := range []WalletReceiptInput{{},
		{Plan: &types.TaskBillingPlan{Source: "task_parameter", Components: []types.TaskBillingPlanComponent{{Key: "output", UnitPrice: "0"}}}},
		{Binding: &types.TaskPluginBinding{PluginKey: "hailuo", SourceID: "hailuo"}},
	} {
		require.NoError(t, ValidateReceiptCompatibility(ReceiptLegacy, nil, input))
	}
}

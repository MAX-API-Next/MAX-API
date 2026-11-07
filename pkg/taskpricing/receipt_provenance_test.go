// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestReceiptProvenanceRequiresIndependentOperationIdentity(t *testing.T) {
	input := WalletReceiptInput{TaskID: 77, OperationKey: "request:legacy-request:pre-consume"}
	// Deliberately specified independently of the constructor/validator.
	original := ReceiptProvenance{Version: 1, Requirement: ReceiptLegacy, TaskID: 77, OperationKey: input.OperationKey}
	require.NoError(t, ValidateReceiptProvenance(&original, nil, input))
	for name, change := range map[string]func(*ReceiptProvenance){
		"zero version":     func(p *ReceiptProvenance) { p.Version = 0 },
		"unknown version":  func(p *ReceiptProvenance) { p.Version = 999 },
		"missing policy":   func(p *ReceiptProvenance) { p.Requirement = "" },
		"unknown policy":   func(p *ReceiptProvenance) { p.Requirement = "future" },
		"another task":     func(p *ReceiptProvenance) { p.TaskID = 78 },
		"another request":  func(p *ReceiptProvenance) { p.OperationKey = "request:other-request:pre-consume" },
		"finalize child":   func(p *ReceiptProvenance) { p.OperationKey = "task:77:finalize" },
		"missing identity": func(p *ReceiptProvenance) { p.TaskID = 0; p.OperationKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p := original
			change(&p)
			before := p
			require.ErrorIs(t, ValidateReceiptProvenance(&p, nil, input), ErrTaskPricingReceiptConflict)
			require.Equal(t, before, p, "reject without rewriting the original")
		})
	}
	require.ErrorIs(t, ValidateReceiptProvenance(nil, nil, input), ErrTaskPricingReceiptConflict)
	for _, key := range []string{"", "request::pre-consume", "task:77:finalize", "request: padded :pre-consume", "request:" + strings.Repeat("a", 65) + ":pre-consume"} {
		bad := WalletReceiptInput{TaskID: 77, OperationKey: key}
		_, err := PrepareReceiptProvenance(ReceiptLegacy, bad)
		require.ErrorIs(t, err, ErrTaskPricingReceiptConflict)
		p := original
		p.OperationKey = key
		require.ErrorIs(t, ValidateReceiptProvenance(&p, nil, bad), ErrTaskPricingReceiptConflict)
	}
}

func TestReceiptProvenanceLegacyIsExplicitAndDoesNotReinterpretOldPlans(t *testing.T) {
	for _, plan := range []*types.TaskBillingPlan{nil, {Source: "task_parameter", Components: []types.TaskBillingPlanComponent{{Key: "output", UnitPrice: "0"}}}} {
		input := WalletReceiptInput{TaskID: 77, OperationKey: "request:legacy-request:pre-consume", Plan: plan}
		p, err := PrepareReceiptProvenance(ReceiptLegacy, input)
		require.NoError(t, err)
		wire, err := common.Marshal(p)
		require.NoError(t, err)
		require.JSONEq(t, `{"version":1,"requirement":"legacy","task_id":77,"operation_key":"request:legacy-request:pre-consume"}`, string(wire))
		var restored ReceiptProvenance
		require.NoError(t, common.Unmarshal(wire, &restored))
		require.NoError(t, ValidateReceiptProvenance(&restored, nil, input))
		for _, requirement := range []ReceiptRequirement{"", "future", ReceiptWalletV1} {
			_, err := PrepareReceiptProvenance(requirement, input)
			require.ErrorIs(t, err, ErrTaskPricingReceiptConflict)
		}
	}
}

func TestReceiptProvenanceWalletCannotDowngradeOrApproveMissingZeroReceipt(t *testing.T) {
	plan := boundedWirePlan(t)
	input := WalletReceiptInput{TaskID: 77, OperationKey: "request:bound-h3:pre-consume", Group: "default", Plan: plan,
		Binding: &types.TaskPluginBinding{Profile: types.TaskPluginExpressionProfile, Action: "generate", PhaseApprovalID: strings.Repeat("b", 64),
			PluginKey: "hailuo", Version: "1.0.0", PackageDigest: strings.Repeat("a", 64), SourceID: "hailuo", SchemaVersion: 1,
			ContractDigest: plan.UsageContractDigest, Model: "MiniMax-H3", UpstreamModel: "MiniMax-H3", UserID: 1, TokenID: 2, ChannelID: 902, RequestID: "bound-h3"}}
	receipt, err := PrepareWalletReservationReceipt(input)
	require.NoError(t, err)
	p, err := PrepareReceiptProvenance(ReceiptWalletV1, input)
	require.NoError(t, err)
	require.NoError(t, ValidateReceiptProvenance(p, receipt, input))
	require.ErrorIs(t, ValidateReceiptProvenance(nil, receipt, input), ErrTaskPricingReceiptConflict)
	require.ErrorIs(t, ValidateReceiptProvenance(p, nil, input), ErrTaskPricingReceiptConflict)
	// Losing the current plan/binding must not erase the retained new policy.
	lostSnapshot := WalletReceiptInput{TaskID: input.TaskID, OperationKey: input.OperationKey}
	require.ErrorIs(t, ValidateReceiptProvenance(p, nil, lostSnapshot), ErrTaskPricingReceiptConflict)
	require.NoError(t, ValidateReceiptCompatibility(ReceiptLegacy, nil, lostSnapshot), "negative control: a caller-supplied legacy policy alone cannot anchor history")
	before := *p
	p.Requirement = ReceiptLegacy
	require.ErrorIs(t, ValidateReceiptProvenance(p, receipt, input), ErrTaskPricingReceiptConflict)
	require.ErrorIs(t, ValidateReceiptProvenance(p, nil, input), ErrTaskPricingReceiptConflict)
	_, err = PrepareReceiptProvenance(ReceiptLegacy, input)
	require.ErrorIs(t, err, ErrTaskPricingReceiptConflict)
	*p = before
	// Independent zero plan: all three quota projections must be zero.
	plan.GroupRatio, plan.ReserveQuota, plan.EstimateQuota, plan.ExpressionPlan.ChargeBound.MaximumQuota = 0, 0, 0, 0
	receipt, err = PrepareWalletReservationReceipt(input)
	require.NoError(t, err)
	require.NoError(t, ValidateReceiptProvenance(p, receipt, input))
	require.ErrorIs(t, ValidateReceiptProvenance(p, nil, input), ErrTaskPricingReceiptConflict)
}

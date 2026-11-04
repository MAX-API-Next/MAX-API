// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPricingWalletReceiptBindsIndependentWireAndReload(t *testing.T) {
	f := newBoundedPricingFixture(t)
	task, input := seedTaskPricingHandoff(t, f)
	receipt, err := PrepareTaskPricingWalletReceipt(task, input)
	require.NoError(t, err)
	require.Equal(t, 1, receipt.Version)
	// Independently specified v1 wire contract. Amounts are independent constants:
	// estimate 6 * .1 * 1000=600; 15-second bound=1500; floor700 is not added.
	plan := types.CloneTaskBillingPlan(task.PrivateData.BillingContext.TaskBillingPlan)
	require.Equal(t, 600, plan.EstimateQuota)
	require.Equal(t, 1500, plan.ReserveQuota)
	plan.ExpressionPlan.PricingAt = plan.ExpressionPlan.PricingAt.UTC()
	planWire, err := common.Marshal(plan)
	require.NoError(t, err)
	bindingWire, err := common.Marshal(task.PrivateData.PluginBinding)
	require.NoError(t, err)
	preimage := fmt.Sprintf("max-api/task-pricing-wallet-receipt@1\n"+
		`{"version":1,"task_id":%d,"operation_key":"request:bounded-request:pre-consume","source":"wallet","group":"default","plan":%s,"binding":%s}`,
		task.ID, planWire, bindingWire)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(preimage))), receipt.Digest)

	var restored model.Task
	require.NoError(t, f.db.First(&restored, task.ID).Error)
	var decoded taskpricing.TaskPricingReceipt
	wire, err := common.Marshal(receipt)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(wire, &decoded))
	require.NoError(t, ValidateTaskPricingWalletReceipt(&decoded, &restored, input))
	// Equivalent instants and scalar JSON reloads must keep their identity.
	restored.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan.PricingAt = plan.ExpressionPlan.PricingAt.In(time.FixedZone("fixture", 8*3600))
	require.NoError(t, ValidateTaskPricingWalletReceipt(&decoded, &restored, input))
}

func TestTaskPricingWalletReceiptRejectsSameReservationDifferentValidPlan(t *testing.T) {
	f := newBoundedPricingFixture(t)
	task, input := seedTaskPricingHandoff(t, f)
	receipt, err := PrepareTaskPricingWalletReceipt(task, input)
	require.NoError(t, err)
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	plan.ExpressionPlan.SubmitFacts["seconds"] = 7
	plan.EstimateQuota = 700 // independent: 7 * .1 * 1000
	require.NoError(t, taskpricing.ValidateSnapshot(plan))
	require.Equal(t, 1500, plan.ReserveQuota)
	replacement, err := PrepareTaskPricingWalletReceipt(task, input)
	require.NoError(t, err, "replacement is itself valid but is not the original plan")
	require.NotEqual(t, receipt.Digest, replacement.Digest)
	require.ErrorIs(t, ValidateTaskPricingWalletReceipt(receipt, task, input), taskpricing.ErrTaskPricingReceiptConflict)
}

func TestTaskPricingWalletReceiptRejectsIdentityAndFundingMismatch(t *testing.T) {
	for _, change := range []string{"nil-task", "unpersisted", "nil-context", "nil-plan", "nil-binding", "legacy", "v1", "unapproved", "user", "token", "channel", "request", "group", "action", "source", "subscription", "subscription-request", "subscription-finalize", "missing-token", "effect", "operation", "task-id", "baseline", "target", "funding", "token-delta", "pre-request", "pre-model", "requested", "effective", "dispatched", "terminal"} {
		t.Run(change, func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			task, input := seedTaskPricingHandoff(t, f)
			switch change {
			case "nil-task":
				task = nil
			case "unpersisted":
				task.ID = 0
			case "nil-context":
				task.PrivateData.BillingContext = nil
			case "nil-plan":
				task.PrivateData.BillingContext.TaskBillingPlan = nil
			case "nil-binding":
				task.PrivateData.PluginBinding = nil
			case "legacy":
				task.PrivateData.BillingContext.TaskBillingPlan.Profile = ""
			case "v1":
				task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan.Version = 1
			case "unapproved":
				task.PrivateData.PluginBinding.PhaseApprovalID = ""
			case "user":
				input.UserID++
			case "token":
				input.TokenID++
			case "channel":
				task.ChannelId++
			case "request":
				task.PrivateData.BillingRequestId += " changed"
			case "group":
				task.Group = " "
			case "action":
				task.Action = "other"
			case "source":
				input.Source = model.BillingSettlementSourceSubscription
			case "subscription":
				input.SubscriptionID = 1
			case "subscription-request":
				input.SubscriptionPreConsumeRequestID = "other"
			case "subscription-finalize":
				input.FinalizeSubscriptionPreConsume = true
			case "missing-token":
				input.AllowMissingToken = true
			case "effect":
				input.Effect = &model.BillingSettlementEffect{}
			case "operation":
				input.OperationKey += "other"
			case "task-id":
				input.TaskID++
			case "baseline":
				input.TaskQuota = 1
			case "target":
				input.TaskQuotaTarget--
			case "funding":
				input.FundingDelta--
			case "token-delta":
				input.TokenDelta--
			case "pre-request":
				input.PreConsumeRequestID += "other"
			case "pre-model":
				input.PreConsumeModelName = "other"
			case "requested":
				input.PreConsumeRequestedQuota--
			case "effective":
				input.PreConsumeEffectiveQuota--
			case "dispatched":
				task.PrivateData.UpstreamTaskID = "already-sent"
			case "terminal":
				task.Status = model.TaskStatusSuccess
			}
			receipt, err := PrepareTaskPricingWalletReceipt(task, input)
			require.ErrorIs(t, err, taskpricing.ErrTaskPricingReceiptConflict)
			require.Nil(t, receipt)
			require.EqualValues(t, 3000, getUserQuota(t, 901))
		})
	}
}

func TestTaskPricingWalletReceiptExplicitZeroAndMissingReceipt(t *testing.T) {
	f := newBoundedPricingFixture(t)
	require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "GroupRatio").Update("value", `{"default":0}`).Error)
	task, input := seedTaskPricingHandoff(t, f)
	require.Zero(t, input.FundingDelta)
	receipt, err := PrepareTaskPricingWalletReceipt(task, input)
	require.NoError(t, err)
	require.NoError(t, ValidateTaskPricingWalletReceipt(receipt, task, input))
	for _, invalid := range []*taskpricing.TaskPricingReceipt{nil, {}, {Version: 2, Digest: receipt.Digest}, {Version: 1}, {Version: 1, Digest: strings.ToUpper(receipt.Digest)}, {Version: 1, Digest: strings.Repeat("0", 64)}} {
		require.ErrorIs(t, ValidateTaskPricingWalletReceipt(invalid, task, input), taskpricing.ErrTaskPricingReceiptConflict)
	}
	// Receipt presence proves equality only, never that this zero intent applied.
	observation, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
	require.NoError(t, err)
	require.Nil(t, observation.AppliedReservation)
	require.False(t, observation.PlanReceiptBound)
	require.False(t, observation.AdmissionReady)
}

func TestTaskPricingWalletReceiptIsPureAndBindsApprovalAndGroup(t *testing.T) {
	f := newBoundedPricingFixture(t)
	task, input := seedTaskPricingHandoff(t, f)
	original, err := common.Marshal(task.PrivateData.BillingContext.TaskBillingPlan)
	require.NoError(t, err)
	operations := 0
	for _, register := range []func(string, func(*gorm.DB)) error{
		f.db.Callback().Query().Before("gorm:query").Register,
		f.db.Callback().Create().Before("gorm:create").Register,
		f.db.Callback().Update().Before("gorm:update").Register,
		f.db.Callback().Delete().Before("gorm:delete").Register,
	} {
		require.NoError(t, register("receipt-no-io", func(*gorm.DB) { operations++ }))
	}
	receipt, err := PrepareTaskPricingWalletReceipt(task, input)
	require.NoError(t, err)
	after, err := common.Marshal(task.PrivateData.BillingContext.TaskBillingPlan)
	require.NoError(t, err)
	require.Equal(t, original, after, "canonicalization must not rewrite the caller's frozen plan")
	input.TokenKey = "synthetic secret excluded from receipt"
	require.NoError(t, ValidateTaskPricingWalletReceipt(receipt, task, input))
	task.Group = "other-group"
	require.Error(t, ValidateTaskPricingWalletReceipt(receipt, task, input))
	task.Group = "default"
	task.PrivateData.PluginBinding.PhaseApprovalID = strings.Repeat("1", 64)
	require.Error(t, ValidateTaskPricingWalletReceipt(receipt, task, input))
	require.Zero(t, operations)
}

func TestTaskPricingWalletReceiptBindsSchemaAndExplicitZeroPresence(t *testing.T) {
	for _, change := range []string{"unit", "enum", "missing-zero"} {
		t.Run(change, func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			task, input := seedTaskPricingHandoff(t, f)
			receipt, err := PrepareTaskPricingWalletReceipt(task, input)
			require.NoError(t, err)
			plan := task.PrivateData.BillingContext.TaskBillingPlan
			switch change {
			case "unit":
				field := plan.ExpressionPlan.Contract.UsageSchema["input_images"]
				field.Unit = "credit"
				plan.ExpressionPlan.Contract.UsageSchema["input_images"] = field
			case "enum":
				field := plan.ExpressionPlan.Contract.UsageSchema["resolution"]
				field.Enum = append(field.Enum, "synthetic-new-value")
				plan.ExpressionPlan.Contract.UsageSchema["resolution"] = field
			case "missing-zero":
				require.EqualValues(t, 0, plan.ExpressionPlan.SubmitFacts["input_images"])
				delete(plan.ExpressionPlan.SubmitFacts, "input_images")
			}
			digest, err := taskpricing.ContractDigest(plan.ExpressionPlan.Contract)
			require.NoError(t, err)
			plan.UsageContractDigest = digest
			task.PrivateData.PluginBinding.ContractDigest = digest
			require.NoError(t, taskpricing.ValidateSnapshot(plan))
			require.Equal(t, 600, plan.EstimateQuota)
			require.Equal(t, 1500, plan.ReserveQuota)
			require.ErrorIs(t, ValidateTaskPricingWalletReceipt(receipt, task, input), taskpricing.ErrTaskPricingReceiptConflict)
		})
	}
}

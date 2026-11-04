// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
)

// PrepareTaskPricingWalletReceipt checks the existing task/funding input
// boundary without persisting or applying anything. No production admission or
// recovery consumer calls it yet; it cannot anchor history merely by being run
// over today's task snapshot. The original receipt must survive separately.
func PrepareTaskPricingWalletReceipt(task *model.Task, input model.BillingSettlementInput) (*taskpricing.TaskPricingReceipt, error) {
	frozen, err := taskPricingWalletReceiptInput(task, input)
	if err != nil {
		return nil, err
	}
	return taskpricing.PrepareWalletReservationReceipt(frozen)
}

// ValidateTaskPricingWalletReceipt compares a separately retained original
// receipt to this sample. Funding status, applied amounts and dispatch safety
// require their own authoritative checks; success here never authorizes I/O.
func ValidateTaskPricingWalletReceipt(receipt *taskpricing.TaskPricingReceipt, task *model.Task, input model.BillingSettlementInput) error {
	frozen, err := taskPricingWalletReceiptInput(task, input)
	if err != nil {
		return err
	}
	return taskpricing.ValidateWalletReservationReceipt(receipt, frozen)
}

func taskPricingWalletReceiptInput(task *model.Task, input model.BillingSettlementInput) (taskpricing.WalletReceiptInput, error) {
	invalid := taskpricing.WalletReceiptInput{}
	if task == nil || task.ID <= 0 || task.Status != model.TaskStatusNotStart ||
		!task.PrivateData.AwaitingUpstreamID || task.PrivateData.UpstreamTaskID != "" ||
		task.PrivateData.BillingSource != model.BillingSettlementSourceWallet || task.PrivateData.SubscriptionId != 0 ||
		task.PrivateData.PluginBinding == nil || !taskPluginBindingMatches(task, *task.PrivateData.PluginBinding) {
		return invalid, taskpricing.ErrTaskPricingReceiptConflict
	}
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	if plan.ExpressionPlan == nil || plan.ExpressionPlan.Version != 2 ||
		task.PrivateData.BillingContext.OriginModelName != plan.ExpressionPlan.OriginalModel ||
		task.PrivateData.BillingContext.GroupRatio != plan.GroupRatio {
		return invalid, taskpricing.ErrTaskPricingReceiptConflict
	}
	quota := int64(plan.ReserveQuota)
	if input.OperationKey != "request:"+task.PrivateData.BillingRequestId+":pre-consume" ||
		input.Source != model.BillingSettlementSourceWallet || input.UserID != task.UserId || input.TokenID != task.PrivateData.TokenId ||
		input.SubscriptionID != 0 || input.SubscriptionPreConsumeRequestID != "" || input.FinalizeSubscriptionPreConsume ||
		input.AllowMissingToken || input.Effect != nil || input.TaskID != task.ID || input.TaskQuota != 0 || input.TaskQuotaTarget != quota ||
		input.FundingDelta != quota || input.TokenDelta != quota ||
		input.PreConsumeRequestID != task.PrivateData.BillingRequestId || input.PreConsumeModelName != plan.ExpressionPlan.OriginalModel ||
		input.PreConsumeRequestedQuota != quota || input.PreConsumeEffectiveQuota != quota {
		return invalid, taskpricing.ErrTaskPricingReceiptConflict
	}
	return taskpricing.WalletReceiptInput{TaskID: task.ID, OperationKey: input.OperationKey, Group: task.Group,
		Plan: plan, Binding: task.PrivateData.PluginBinding}, nil
}

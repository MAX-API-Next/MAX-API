// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Lifecycle reference: QuantumNous/new-api 45746eee relay/relay_task.go.
// MAX retains its own durable funding and settlement authority.
package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
	"gorm.io/gorm"
)

type TaskPricingHandoffState string

const (
	TaskPricingHandoffPrepared TaskPricingHandoffState = "prepared"
	TaskPricingHandoffPending  TaskPricingHandoffState = "pending"
	TaskPricingHandoffManual   TaskPricingHandoffState = "manual"
	TaskPricingHandoffApplied  TaskPricingHandoffState = "applied"
)

var ErrTaskPricingHandoffConflict = errors.New("task pricing handoff evidence is inconsistent or unsupported")

// TaskPricingHandoffObservation is an internal, read-only sample of a wallet
// reservation before dispatch. AppliedReservation is nil until funding is
// durably applied; a non-nil zero is an explicitly applied free reservation.
// PlanDigest fingerprints the observed task snapshot only. Existing funding
// records do not independently anchor that digest, so PlanReceiptBound and
// AdmissionReady remain false even when the wallet reservation is applied.
type TaskPricingHandoffObservation struct {
	TaskID                 int64
	State                  TaskPricingHandoffState
	Plan                   *types.TaskBillingPlan
	PlanDigest             string
	Reservation            int
	AppliedReservation     *int64
	AutomaticRetryEligible bool
	PlanReceiptBound       bool
	AdmissionReady         bool
}

// ReadTaskPricingWalletHandoff never creates a task/intent, applies or retries
// funds, refreshes live prices, restores an executor, or authorizes dispatch.
// The actor is host-owned. This narrow observation supports only bound v2
// plugin plans on a wallet-backed, not-yet-dispatched task. Subscription/legacy
// funding, request-only receipts and later task lifecycles fail closed.
func ReadTaskPricingWalletHandoff(ctx context.Context, taskID int64, actor taskplugin.Actor) (*TaskPricingHandoffObservation, error) {
	if ctx == nil || model.DB == nil || taskID <= 0 || actor.UserID <= 0 || actor.TokenID <= 0 || actor.ChannelID <= 0 ||
		actor.RequestID == "" || len(actor.RequestID) > 64 || strings.TrimSpace(actor.RequestID) != actor.RequestID {
		return nil, ErrTaskPricingHandoffConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var observation *TaskPricingHandoffObservation
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.First(&task, taskID).Error; err != nil {
			return err
		}
		if !pricingActorMatches(&task, actor) || task.Status != model.TaskStatusNotStart ||
			!task.PrivateData.AwaitingUpstreamID || task.PrivateData.UpstreamTaskID != "" ||
			task.PrivateData.BillingSource != model.BillingSettlementSourceWallet || task.PrivateData.SubscriptionId != 0 ||
			task.PrivateData.PluginBinding == nil || !taskPluginBindingMatches(&task, *task.PrivateData.PluginBinding) {
			return ErrTaskPricingHandoffConflict
		}
		plan := task.PrivateData.BillingContext.TaskBillingPlan
		if plan.Profile != types.TaskPluginExpressionProfile || plan.ExpressionPlan == nil ||
			plan.ExpressionPlan.Version != 2 || task.PrivateData.PluginBinding.PhaseApprovalID == "" ||
			task.PrivateData.BillingContext.OriginModelName != plan.ExpressionPlan.OriginalModel ||
			task.PrivateData.BillingContext.GroupRatio != plan.GroupRatio {
			return ErrTaskPricingHandoffConflict
		}
		data, err := common.Marshal(plan)
		if err != nil {
			return err
		}
		sample := &TaskPricingHandoffObservation{TaskID: task.ID, Plan: types.CloneTaskBillingPlan(plan),
			PlanDigest: fmt.Sprintf("%x", sha256.Sum256(data)), Reservation: plan.ReserveQuota}
		var selection model.BillingPreConsumeSelection
		selected := tx.Where("request_id = ?", actor.RequestID).Limit(1).Find(&selection)
		if selected.Error != nil {
			return selected.Error
		}
		quota := int64(plan.ReserveQuota)
		if selected.RowsAffected > 0 && (selection.RequestID != actor.RequestID || selection.Source != model.BillingSettlementSourceWallet ||
			selection.UserID != actor.UserID || selection.TokenID != actor.TokenID ||
			selection.ModelName != plan.ExpressionPlan.OriginalModel || selection.RequestedQuota != quota || selection.EffectiveQuota != quota) {
			return ErrTaskPricingHandoffConflict
		}
		// Old subscription receipts can predate the common source-selection row.
		// A wallet observation must not silently ignore that other funding leg.
		var subscriptions int64
		if err := tx.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", actor.RequestID).Count(&subscriptions).Error; err != nil {
			return err
		}
		if subscriptions != 0 {
			return ErrTaskPricingHandoffConflict
		}
		var record model.BillingSettlement
		found := tx.Where("operation_key = ?", "request:"+actor.RequestID+":pre-consume").Limit(1).Find(&record)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			if task.Quota != 0 || selected.RowsAffected != 0 {
				return ErrTaskPricingHandoffConflict
			}
			sample.State = TaskPricingHandoffPrepared
			observation = sample
			return nil
		}
		if record.OperationKey != "request:"+actor.RequestID+":pre-consume" ||
			record.Source != model.BillingSettlementSourceWallet || record.UserID != actor.UserID || record.TokenID != actor.TokenID ||
			record.SubscriptionID != 0 || record.SubscriptionPreConsumeRequestID != "" || record.FinalizeSubscriptionPreConsume ||
			record.TaskID != task.ID || record.TaskQuota != 0 || record.TaskQuotaTarget != quota ||
			record.FundingDelta != quota || record.TokenDelta != quota || record.AllowMissingToken || record.EffectPayload != "" ||
			record.PreConsumeRequestID != actor.RequestID || record.PreConsumeModelName != plan.ExpressionPlan.OriginalModel ||
			record.PreConsumeRequestedQuota != quota || record.PreConsumeEffectiveQuota != quota {
			return ErrTaskPricingHandoffConflict
		}
		switch record.Status {
		case model.BillingSettlementStatusPending, model.BillingSettlementStatusManual:
			if task.Quota != 0 || record.AppliedFundingDelta != 0 || record.AppliedTokenDelta != 0 {
				return ErrTaskPricingHandoffConflict
			}
			sample.State = TaskPricingHandoffState(record.Status)
			// Pending is executable by the existing funding runner, unlike a
			// merely prepared task. This does not initiate such an execution.
			sample.AutomaticRetryEligible = record.Status == model.BillingSettlementStatusPending
		case model.BillingSettlementStatusApplied:
			if selected.RowsAffected != 1 || record.AppliedFundingDelta != quota || record.AppliedTokenDelta != quota || int64(task.Quota) != quota {
				return ErrTaskPricingHandoffConflict
			}
			sample.State = TaskPricingHandoffApplied
			applied := record.AppliedFundingDelta
			sample.AppliedReservation = &applied
		default:
			return ErrTaskPricingHandoffConflict
		}
		observation = sample
		return nil
	})
	if err != nil {
		return nil, err
	}
	return observation, nil
}

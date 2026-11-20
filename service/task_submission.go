// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
)

const boundWalletDebitRecoveryKey = "max_bound_task_wallet_debit_recovery"

type boundWalletDebitRecovery struct {
	taskID int64
	refund model.BillingSettlementInput
}

// Claim the definite terminal transition before the existing request refund.
// An open task-finalize owner, confirmed provider work or read error must keep
// its task and funding available for the original reconciliation path.
func CleanupDefiniteTaskSubmitFailure(c *gin.Context, info *relaycommon.RelayInfo, reason string) {
	if info == nil {
		return
	}
	if info.TaskRelayInfo != nil && info.PersistedTaskID > 0 {
		var intent *model.BillingSettlementInput
		var failedReserve int64
		session, isSession := info.Billing.(*BillingSession)
		statusOnly := info.Billing == nil
		if statusOnly && c != nil {
			candidate, _ := c.Get(boundWalletDebitRecoveryKey)
			if recovery, ok := candidate.(boundWalletDebitRecovery); ok && recovery.taskID == info.PersistedTaskID &&
				recovery.refund.OperationKey == model.BillingRequestFinalizeOperationKey(info.RequestId) &&
				recovery.refund.UserID == info.UserId && recovery.refund.TokenID == info.TokenId {
				intent, statusOnly = &recovery.refund, false
			}
		}
		if info.Billing != nil {
			preparer, ok := info.Billing.(RefundSettlementPreparer)
			if !ok {
				common.SysError("task submit refund requires a durable settlement preparer")
				return
			}
			var err error
			if isSession {
				intent, failedReserve, err = session.prepareTaskSubmitFailureRefund()
			} else {
				intent, err = preparer.PrepareRefundSettlement()
			}
			if err != nil {
				common.SysError(fmt.Sprintf("task submit refund preparation requires reconciliation: task_id=%d result=%s", info.PersistedTaskID, taskSubmitRecoveryErrorCategory(err)))
				return
			}
			if intent == nil && isSession {
				statusOnly = session.isUnsettledZeroChargeTask(info)
			}
		}
		if intent == nil && !statusOnly {
			return // The session can still have an in-flight or completed owner.
		}
		var won bool
		var err error
		claimCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		for attempt := 0; attempt < 3; attempt++ {
			if statusOnly {
				won, err = model.ClaimTaskSubmitFailureStatusContext(claimCtx, info.PersistedTaskID, info.RequestId, reason, info.UserId, info.TokenId)
			} else if failedReserve != 0 {
				won, err = model.ClaimTaskSubmitFailureAfterReserveError(claimCtx, info.PersistedTaskID, reason, intent, failedReserve)
			} else {
				won, err = model.ClaimTaskSubmitFailureWithSettlementContext(claimCtx, info.PersistedTaskID, reason, intent)
			}
			if err == nil || claimCtx.Err() != nil || errors.Is(err, model.ErrTaskSubmitRefundIdentityMismatch) || errors.Is(err, model.ErrBillingSettlementOperationConflict) || errors.Is(err, model.ErrTaskSubmitFailureRecoveryRequiresReview) {
				break
			}
		}
		cancel()
		if err != nil {
			if intent != nil {
				status, journalErr := model.RecordTaskSubmitFailureWithReserveRecovery(info.PersistedTaskID, reason, *intent, failedReserve)
				switch {
				case model.QueueTaskSubmitFailureRecovery(info.PersistedTaskID, info.RequestId, reason, info.UserId, info.TokenId, intent, failedReserve, journalErr):
					common.SysLog(fmt.Sprintf("task submit rejection retained for journal retry: task_id=%d", info.PersistedTaskID))
				case journalErr != nil:
					common.SysError(fmt.Sprintf("task submit rejection recovery was not recorded: task_id=%d claim=%s journal=%s", info.PersistedTaskID, taskSubmitRecoveryErrorCategory(err), taskSubmitRecoveryErrorCategory(journalErr)))
				case status == model.TaskSubmitFailureRecoveryPending:
					common.SysLog(fmt.Sprintf("task submit rejection queued for recovery: task_id=%d", info.PersistedTaskID))
				case status == model.TaskSubmitFailureRecoveryRecorded:
					common.SysLog(fmt.Sprintf("task submit rejection already has a recorded refund intent: task_id=%d", info.PersistedTaskID))
				default:
					common.SysError(fmt.Sprintf("task submit rejection still requires manual review: task_id=%d", info.PersistedTaskID))
				}
			} else {
				status, journalErr := model.RecordTaskSubmitFailureStatusRecovery(info.PersistedTaskID, info.RequestId, reason, info.UserId, info.TokenId)
				if model.QueueTaskSubmitFailureRecovery(info.PersistedTaskID, info.RequestId, reason, info.UserId, info.TokenId, nil, 0, journalErr) {
					common.SysLog(fmt.Sprintf("task submit rejection retained for journal retry: task_id=%d", info.PersistedTaskID))
				} else if journalErr != nil {
					common.SysError(fmt.Sprintf("task submit rejection status recovery could not be verified: task_id=%d claim=%s journal=%s", info.PersistedTaskID, taskSubmitRecoveryErrorCategory(err), taskSubmitRecoveryErrorCategory(journalErr)))
				} else if status == model.TaskSubmitFailureRecoveryPending {
					common.SysLog(fmt.Sprintf("task submit rejection queued for status recovery: task_id=%d", info.PersistedTaskID))
				} else if status == model.TaskSubmitFailureRecoveryManual {
					common.SysError(fmt.Sprintf("task submit rejection status still requires manual review: task_id=%d", info.PersistedTaskID))
				}
			}
			return
		}
		if !won || statusOnly {
			return
		}
		if failedReserve != 0 {
			if intent.FundingDelta != 0 {
				if _, _, err := model.ApplyBillingSettlementOnce(*intent); err != nil {
					common.SysLog(fmt.Sprintf("task submit refund remains in ledger: task_id=%d result=%s", info.PersistedTaskID, taskSubmitRecoveryErrorCategory(err)))
					return
				}
			}
			session.finishRefundIntent(true)
			return
		}
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
}

func taskSubmitRecoveryErrorCategory(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "deadline"
	case errors.Is(err, model.ErrTaskSubmitRefundIdentityMismatch):
		return "identity_mismatch"
	case errors.Is(err, model.ErrBillingSettlementOperationConflict), errors.Is(err, model.ErrTaskFinalizeOwned):
		return "conflict"
	case errors.Is(err, model.ErrTaskSubmitFailureRecoveryRequiresReview), errors.Is(err, model.ErrBillingSettlementManualReview):
		return "manual_review"
	default:
		return "storage_error"
	}
}

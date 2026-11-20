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

// Claim the definite terminal transition before the existing request refund.
// An open task-finalize owner, confirmed provider work or read error must keep
// its task and funding available for the original reconciliation path.
func CleanupDefiniteTaskSubmitFailure(c *gin.Context, info *relaycommon.RelayInfo, reason string) {
	if info == nil {
		return
	}
	if info.TaskRelayInfo != nil && info.PersistedTaskID > 0 {
		var intent *model.BillingSettlementInput
		statusOnly := info.Billing == nil
		if info.Billing != nil {
			preparer, ok := info.Billing.(RefundSettlementPreparer)
			if !ok {
				common.SysError("task submit refund requires a durable settlement preparer")
				return
			}
			var err error
			intent, err = preparer.PrepareRefundSettlement()
			if err != nil {
				common.SysError("task submit refund preparation requires reconciliation: " + err.Error())
				return
			}
			if session, ok := info.Billing.(*BillingSession); intent == nil && ok {
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
				status, journalErr := model.RecordTaskSubmitFailureRecoveryWithStatus(info.PersistedTaskID, reason, *intent)
				switch {
				case journalErr != nil:
					common.SysError(fmt.Sprintf("task submit rejection recovery was not recorded: task_id=%d; inspect the original placeholder and preconsume records", info.PersistedTaskID))
				case status == model.TaskSubmitFailureRecoveryPending:
					common.SysLog(fmt.Sprintf("task submit rejection queued for recovery: task_id=%d", info.PersistedTaskID))
				case status == model.TaskSubmitFailureRecoveryRecorded:
					common.SysLog(fmt.Sprintf("task submit rejection already has a recorded refund intent: task_id=%d", info.PersistedTaskID))
				default:
					common.SysError(fmt.Sprintf("task submit rejection still requires manual review: task_id=%d", info.PersistedTaskID))
				}
			} else {
				status, journalErr := model.RecordTaskSubmitFailureStatusRecovery(info.PersistedTaskID, info.RequestId, reason, info.UserId, info.TokenId)
				if journalErr != nil {
					common.SysError(fmt.Sprintf("task submit rejection status recovery could not be verified: task_id=%d", info.PersistedTaskID))
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
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"errors"
	"fmt"

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
		}
		var won bool
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			won, err = model.ClaimTaskSubmitFailureWithSettlement(info.PersistedTaskID, reason, intent)
			if err == nil || errors.Is(err, model.ErrTaskSubmitRefundIdentityMismatch) || errors.Is(err, model.ErrBillingSettlementOperationConflict) {
				break
			}
		}
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
				common.SysError(fmt.Sprintf("task submit failure ownership could not be determined: task_id=%d", info.PersistedTaskID))
			}
			return
		}
		if !won {
			return
		}
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
}

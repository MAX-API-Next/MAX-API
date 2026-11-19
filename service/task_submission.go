// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
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
	if info.PersistedTaskID > 0 {
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
		won, err := model.ClaimTaskSubmitFailureWithSettlement(info.PersistedTaskID, reason, intent)
		if err != nil {
			common.SysError("task submit failure ownership requires reconciliation: " + err.Error())
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

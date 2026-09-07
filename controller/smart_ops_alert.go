package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

// GetSmartOpsAlerts exposes the current in-process operational alerts to
// administrators. The endpoint is read-only and does not trigger remediation.
func GetSmartOpsAlerts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    service.GetSmartOpsAlerts(),
	})
}

type billingSettlementBlockingPolicyRequest struct {
	BlockUserByDefault *bool `json:"block_user_by_default"`
}

func UpdateBillingSettlementBlockingPolicy(c *gin.Context) {
	var request billingSettlementBlockingPolicyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.BlockUserByDefault == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "block_user_by_default is required",
		})
		return
	}
	if err := service.UpdateBillingSettlementBlockingPolicy(*request.BlockUserByDefault); err != nil {
		common.SysError(fmt.Sprintf("failed to update billing reconciliation blocking policy: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to update billing reconciliation blocking policy",
		})
		return
	}
	recordManageAudit(c, "billing.reconciliation_blocking_policy_update", map[string]interface{}{
		"block_user_by_default": *request.BlockUserByDefault,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"block_user_by_default": *request.BlockUserByDefault,
		},
	})
}

type billingSettlementReviewRequest struct {
	BlockUser *bool  `json:"block_user"`
	Note      string `json:"note"`
}

type billingSettlementBatchReviewRequest struct {
	Items []model.BillingSettlementReviewTarget `json:"items"`
}

type manualTaskBillingCompletionRequest struct {
	Revision    int64  `json:"revision"`
	ActualQuota *int64 `json:"actual_quota"`
	Note        string `json:"note"`
}

// ReviewBillingSettlements closes a bounded set of current reconciliation
// alerts after the administrator acknowledges them.
func ReviewBillingSettlements(c *gin.Context) {
	var request billingSettlementBatchReviewRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid billing settlement batch review request",
		})
		return
	}
	records, err := service.ReviewBillingSettlements(request.Items, c.GetInt("id"))
	if err != nil {
		status := http.StatusInternalServerError
		message := "failed to review billing settlements"
		switch {
		case errors.Is(err, service.ErrInvalidBillingSettlementReconciliationReview):
			status = http.StatusBadRequest
			message = err.Error()
		case errors.Is(err, model.ErrBillingSettlementReviewConflict):
			status = http.StatusConflict
			message = "one or more billing settlement alerts changed; refresh and review again"
		case errors.Is(err, model.ErrBillingSettlementCompletionRequired):
			status = http.StatusConflict
			message = "one or more task settlements require an exact final quota and cannot be batch closed"
		}
		if status == http.StatusInternalServerError {
			common.SysError(fmt.Sprintf("failed to review billing settlements: %v", err))
		}
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	settlementIDs := make([]int64, 0, len(records))
	targetUserIDs := make([]int, 0, len(records))
	for _, record := range records {
		settlementIDs = append(settlementIDs, record.ID)
		targetUserIDs = append(targetUserIDs, record.UserID)
	}
	recordManageAudit(c, "billing.reconciliation_review_batch", map[string]interface{}{
		"count":           len(records),
		"settlement_ids":  settlementIDs,
		"target_user_ids": targetUserIDs,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"reviewed_count": len(records),
			"settlement_ids": settlementIDs,
		},
	})
}

func ReviewBillingSettlement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid billing settlement id",
		})
		return
	}
	var request billingSettlementReviewRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid billing settlement review request",
		})
		return
	}
	record, err := service.ReviewBillingSettlement(id, c.GetInt("id"), request.BlockUser, request.Note)
	if err != nil {
		status := http.StatusInternalServerError
		message := "failed to review billing settlement"
		switch {
		case errors.Is(err, service.ErrInvalidBillingSettlementReconciliationReview):
			status = http.StatusBadRequest
			message = err.Error()
		case errors.Is(err, model.ErrBillingSettlementReviewConflict):
			status = http.StatusConflict
			message = "billing settlement is no longer pending manual reconciliation"
		case errors.Is(err, model.ErrBillingSettlementCompletionRequired):
			status = http.StatusConflict
			message = "task settlement requires an exact final quota before it can be closed"
		}
		if status == http.StatusInternalServerError {
			common.SysError(fmt.Sprintf("failed to review billing settlement %d: %v", id, err))
		}
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	recordManageAudit(c, "billing.reconciliation_review", map[string]interface{}{
		"settlement_id":  record.ID,
		"target_user_id": record.UserID,
		"block_user":     *request.BlockUser,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":                         record.ID,
			"reconciliation_reviewed_at": record.ReconciliationReviewedAt,
			"block_user":                 *request.BlockUser,
		},
	})
}

// CompleteManualTaskBillingSettlement applies a root administrator's exact
// final quota to a reconciliation-only task settlement. Zero is a valid and
// explicit financial result; omission is rejected.
func CompleteManualTaskBillingSettlement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid billing settlement id"})
		return
	}
	var request manualTaskBillingCompletionRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid manual task billing completion request"})
		return
	}
	result, err := service.CompleteManualTaskBillingSettlement(
		id,
		request.Revision,
		c.GetInt("id"),
		request.ActualQuota,
		request.Note,
	)
	if err != nil {
		status := http.StatusInternalServerError
		message := "failed to complete manual task billing settlement"
		switch {
		case errors.Is(err, service.ErrInvalidBillingSettlementReconciliationReview):
			status = http.StatusBadRequest
			message = err.Error()
		case errors.Is(err, model.ErrBillingSettlementReviewConflict),
			errors.Is(err, model.ErrBillingSettlementOperationConflict),
			errors.Is(err, model.ErrBillingSettlementTaskConflict),
			errors.Is(err, model.ErrBillingSettlementManualReview),
			errors.Is(err, model.ErrTokenQuotaInsufficient),
			errors.Is(err, model.ErrSubscriptionSettlementUnbound),
			errors.Is(err, model.ErrSubscriptionSettlementPeriodChanged):
			status = http.StatusConflict
			message = "manual task billing settlement could not be applied safely; refresh and reconcile the current record"
		}
		if status == http.StatusInternalServerError {
			common.SysError(fmt.Sprintf("failed to complete manual task billing settlement %d: %v", id, err))
		}
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	recordManageAudit(c, "billing.manual_task_settlement_complete", map[string]interface{}{
		"settlement_id":         result.SettlementID,
		"task_id":               result.TaskID,
		"target_user_id":        result.UserID,
		"operation_key":         result.OperationKey,
		"actual_quota":          result.ActualQuota,
		"applied_funding_delta": result.AppliedFundingDelta,
		"already_applied":       result.AlreadyApplied,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// GetBillingSettlementReconciliation exposes bounded, read-only evidence for
// open billing reconciliation records. It does not retry or mutate records.
func GetBillingSettlementReconciliation(c *gin.Context) {
	limit, err := parseIntQuery(c, "limit")
	if err != nil {
		respondInvalidPerformanceQuery(
			c,
			err,
			service.ErrInvalidBillingSettlementReconciliationQuery,
			"failed to query billing settlement reconciliation",
		)
		return
	}
	result, err := service.GetBillingSettlementReconciliation(limit)
	if err != nil {
		respondPerformanceError(
			c,
			err,
			service.ErrInvalidBillingSettlementReconciliationQuery,
			"failed to query billing settlement reconciliation",
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

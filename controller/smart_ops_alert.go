package controller

import (
	"errors"
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
		}
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	recordManageAuditFor(c, record.UserID, "billing.reconciliation_review", map[string]interface{}{
		"settlement_id": record.ID,
		"block_user":    *request.BlockUser,
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

// GetBillingSettlementReconciliation exposes bounded, read-only evidence for
// unresolved positive final settlements. It does not retry or mutate records.
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

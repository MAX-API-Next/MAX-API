package controller

import (
	"errors"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxUserStatusBatchSize = 100

type batchUserStatusRequest struct {
	Action string `json:"action"`
	Users  []struct {
		ID     int `json:"id"`
		Status int `json:"status"`
	} `json:"users"`
}

type batchUserStatusResult struct {
	ID      int    `json:"id"`
	Outcome string `json:"outcome"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"status,omitempty"`
}

// BatchManageUserStatus deliberately accepts only enable/disable. Each item has
// its own transaction and result: one protected account must not hide failures
// or undo successful changes to unrelated accounts.
func BatchManageUserStatus(c *gin.Context) {
	if c.GetInt("id") <= 0 || c.GetInt("role") < common.RoleAdminUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Forbidden"})
		return
	}
	var req batchUserStatusRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if (req.Action != "enable" && req.Action != "disable") || len(req.Users) == 0 || len(req.Users) > maxUserStatusBatchSize {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	seen := make(map[int]bool, len(req.Users))
	for _, user := range req.Users {
		if user.ID <= 0 || seen[user.ID] || (user.Status != common.UserStatusEnabled && user.Status != common.UserStatusDisabled) {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		seen[user.ID] = true
	}
	status := common.UserStatusDisabled
	if req.Action == "enable" {
		status = common.UserStatusEnabled
	}
	results := make([]batchUserStatusResult, 0, len(req.Users))
	for _, user := range req.Users {
		change, err := model.SetUserStatusByAdmin(c.Request.Context(), c.GetInt("id"), user.ID, user.Status, status)
		result := batchUserStatusResult{ID: user.ID, Outcome: "unchanged", Status: change.Status}
		switch {
		case errors.Is(err, model.ErrUserStatusForbidden):
			result.Outcome, result.Code = "rejected", "forbidden"
		case errors.Is(err, gorm.ErrRecordNotFound):
			result.Outcome, result.Code = "rejected", "not_found"
		case errors.Is(err, model.ErrUserStatusConflict):
			result.Outcome, result.Code = "rejected", "conflict"
		case err != nil:
			result.Outcome, result.Code = "unknown", "unknown"
			common.SysError("batch user status change could not be confirmed: " + err.Error())
		case change.Changed:
			result.Outcome = "updated"
		}
		if err == nil {
			recordManageAuditFor(c, user.ID, "user.manage", map[string]interface{}{
				"action": req.Action, "id": user.ID, "username": change.Username,
				"batch": true, "outcome": result.Outcome,
				"previous_status": change.PreviousStatus, "status": change.Status,
			})
		} else {
			// Keep rejected/uncertain attempts visible without suggesting that a
			// status change succeeded or disclosing another user's details.
			recordManageAudit(c, "user.manage", map[string]interface{}{
				"action": req.Action + ":" + result.Outcome, "id": user.ID,
				"username": "", "batch": true, "outcome": result.Outcome, "code": result.Code,
			})
		}
		results = append(results, result)
	}
	// success means the batch was processed, NOT that every account changed.
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"results": results}})
}

package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingSettlementMutationRequestsPreserveExplicitFalse(t *testing.T) {
	var policy billingSettlementBlockingPolicyRequest
	require.NoError(t, common.Unmarshal([]byte(`{"block_user_by_default":false}`), &policy))
	require.NotNil(t, policy.BlockUserByDefault)
	assert.False(t, *policy.BlockUserByDefault)

	var review billingSettlementReviewRequest
	require.NoError(t, common.Unmarshal([]byte(`{"block_user":false,"note":"verified"}`), &review))
	require.NotNil(t, review.BlockUser)
	assert.False(t, *review.BlockUser)
}

func TestReviewBillingSettlementAuditRecordsActingAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.BillingSettlement{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	administrator := model.User{
		Id:       7101,
		Username: "billing-review-admin",
		AffCode:  "billing-review-admin-aff",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	owner := model.User{
		Id:       7102,
		Username: "billing-review-owner",
		AffCode:  "billing-review-owner-aff",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&administrator).Error)
	require.NoError(t, db.Create(&owner).Error)
	settlement := model.BillingSettlement{
		OperationKey: "request:controller-review-audit:finalize",
		Source:       model.BillingSettlementSourceWallet,
		UserID:       owner.Id,
		FundingDelta: 10,
		TokenDelta:   10,
		Status:       model.BillingSettlementStatusPending,
		CreatedAt:    1,
		UpdatedAt:    1,
		Revision:     1,
	}
	require.NoError(t, db.Create(&settlement).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/smart-ops/billing-settlements/%d/review", settlement.ID),
		bytes.NewBufferString(`{"block_user":false,"note":"Verified provider evidence"}`),
	)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", settlement.ID)}}
	ctx.Set("id", administrator.Id)
	ctx.Set("username", administrator.Username)
	ctx.Set("role", administrator.Role)

	ReviewBillingSettlement(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var audit model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Order("id DESC").First(&audit).Error)
	assert.Equal(t, administrator.Id, audit.UserId)
	assert.Equal(t, administrator.Username, audit.Username)
	var other struct {
		Op struct {
			Action string                 `json:"action"`
			Params map[string]interface{} `json:"params"`
		} `json:"op"`
	}
	require.NoError(t, common.UnmarshalJsonStr(audit.Other, &other))
	assert.Equal(t, "billing.reconciliation_review", other.Op.Action)
	assert.EqualValues(t, owner.Id, other.Op.Params["target_user_id"])
}

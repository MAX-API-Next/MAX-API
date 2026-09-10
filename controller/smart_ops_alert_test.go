package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	var completion manualTaskBillingCompletionRequest
	require.NoError(t, common.Unmarshal([]byte(`{"revision":1,"actual_quota":0,"note":"verified"}`), &completion))
	require.NotNil(t, completion.ActualQuota)
	assert.Zero(t, *completion.ActualQuota)

	var omittedCompletion manualTaskBillingCompletionRequest
	require.NoError(t, common.Unmarshal([]byte(`{"revision":1,"note":"verified"}`), &omittedCompletion))
	assert.Nil(t, omittedCompletion.ActualQuota)
}

func TestCompleteManualTaskBillingSettlementAuditsExactCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.LogConsumeEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Task{},
		&model.BillingSettlement{},
		&model.CacheInvalidationTask{},
		&model.Log{},
	))
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
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})

	administrator := model.User{
		Id:       7051,
		Username: "manual-task-billing-root",
		AffCode:  "manual-task-billing-root-aff",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	owner := model.User{
		Id:       7052,
		Username: "manual-task-billing-owner",
		AffCode:  "manual-task-billing-owner-aff",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    900,
	}
	require.NoError(t, db.Create(&administrator).Error)
	require.NoError(t, db.Create(&owner).Error)
	task := model.Task{
		TaskID:    "controller-manual-task-completion",
		UserId:    owner.Id,
		Group:     "default",
		Quota:     100,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		CreatedAt: 1,
		UpdatedAt: 1,
		PrivateData: model.TaskPrivateData{
			BillingSource: "wallet",
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "manual-task-model",
			},
		},
	}
	task.SetData(map[string]any{"provider_status": "complete"})
	require.NoError(t, db.Create(&task).Error)
	settlement := model.BillingSettlement{
		OperationKey:    model.BillingTaskFinalizeOperationKey(task.ID),
		Source:          model.BillingSettlementSourceWallet,
		UserID:          owner.Id,
		TaskID:          task.ID,
		TaskQuota:       100,
		TaskQuotaTarget: 100,
		Status:          model.BillingSettlementStatusManual,
		LastError:       "provider usage requires an exact quota",
		CreatedAt:       1,
		UpdatedAt:       1,
		Revision:        1,
	}
	require.NoError(t, db.Create(&settlement).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/smart-ops/billing-settlements/%d/complete-task", settlement.ID),
		bytes.NewBufferString(`{"revision":1,"actual_quota":40,"note":"Verified provider evidence and exact usage."}`),
	)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", settlement.ID)}}
	ctx.Set("id", administrator.Id)
	ctx.Set("username", administrator.Username)
	ctx.Set("role", administrator.Role)

	CompleteManualTaskBillingSettlement(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"actual_quota":40`)
	var storedOwner model.User
	require.NoError(t, db.First(&storedOwner, owner.Id).Error)
	assert.EqualValues(t, 960, storedOwner.Quota)
	var storedTask model.Task
	require.NoError(t, db.First(&storedTask, task.ID).Error)
	assert.Equal(t, 40, storedTask.Quota)
	var storedSettlement model.BillingSettlement
	require.NoError(t, db.First(&storedSettlement, settlement.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, storedSettlement.Status)
	assert.Equal(t, administrator.Id, storedSettlement.ReconciliationReviewedBy)

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
	assert.Equal(t, "billing.manual_task_settlement_complete", other.Op.Action)
	assert.EqualValues(t, settlement.ID, other.Op.Params["settlement_id"])
	assert.EqualValues(t, task.ID, other.Op.Params["task_id"])
	assert.EqualValues(t, owner.Id, other.Op.Params["target_user_id"])
	assert.EqualValues(t, 40, other.Op.Params["actual_quota"])
	assert.Equal(t, model.BillingTaskManualCompletionOperationKey(task.ID), other.Op.Params["operation_key"])
}

func TestCompleteManualTaskBillingSettlementMapsTokenRefundConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.LogConsumeEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Task{},
		&model.BillingSettlement{},
		&model.CacheInvalidationTask{},
		&model.Log{},
	))
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
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})

	administrator := model.User{
		Id: 7061, Username: "manual-task-token-root", AffCode: "manual-task-token-root-aff",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default",
	}
	owner := model.User{
		Id: 7062, Username: "manual-task-token-owner", AffCode: "manual-task-token-owner-aff",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 900,
	}
	token := model.Token{
		Id: 7063, UserId: owner.Id, Key: "manual-task-token-conflict", Status: common.TokenStatusEnabled,
		Name: "manual-task-token-conflict", RemainQuota: 100, UsedQuota: 10, Group: "default",
	}
	require.NoError(t, db.Create(&administrator).Error)
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&token).Error)
	task := model.Task{
		TaskID: "controller-manual-task-token-conflict", UserId: owner.Id, Group: "default", Quota: 100,
		Status: model.TaskStatus(model.TaskStatusInProgress), CreatedAt: 1, UpdatedAt: 1,
		PrivateData: model.TaskPrivateData{
			TokenId: token.Id, BillingSource: "wallet",
			BillingContext: &model.TaskBillingContext{OriginModelName: "manual-task-model"},
		},
	}
	task.SetData(map[string]any{"provider_status": "complete"})
	require.NoError(t, db.Create(&task).Error)
	settlement := model.BillingSettlement{
		OperationKey: model.BillingTaskFinalizeOperationKey(task.ID), Source: model.BillingSettlementSourceWallet,
		UserID: owner.Id, TokenID: token.Id, TaskID: task.ID, TaskQuota: 100, TaskQuotaTarget: 100,
		Status: model.BillingSettlementStatusManual, LastError: "provider usage requires an exact quota",
		CreatedAt: 1, UpdatedAt: 1, Revision: 1,
	}
	require.NoError(t, db.Create(&settlement).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/smart-ops/billing-settlements/%d/complete-task", settlement.ID),
		bytes.NewBufferString(`{"revision":1,"actual_quota":40,"note":"Verified provider evidence and exact usage."}`),
	)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", settlement.ID)}}
	ctx.Set("id", administrator.Id)
	ctx.Set("username", administrator.Username)
	ctx.Set("role", administrator.Role)

	CompleteManualTaskBillingSettlement(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "token quota mirror")
	var storedOwner model.User
	require.NoError(t, db.First(&storedOwner, owner.Id).Error)
	assert.EqualValues(t, 900, storedOwner.Quota)
	var storedToken model.Token
	require.NoError(t, db.First(&storedToken, token.Id).Error)
	assert.EqualValues(t, 100, storedToken.RemainQuota)
	assert.EqualValues(t, 10, storedToken.UsedQuota)
}

func TestCompleteManualTaskBillingSettlementMapsClampedSubscriptionRefund(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.LogConsumeEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Task{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
		&model.BillingSettlement{},
		&model.CacheInvalidationTask{},
		&model.Log{},
	))
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
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})

	administrator := model.User{
		Id: 7071, Username: "manual-task-subscription-root", AffCode: "manual-task-subscription-root-aff",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default",
	}
	owner := model.User{
		Id: 7072, Username: "manual-task-subscription-owner", AffCode: "manual-task-subscription-owner-aff",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}
	token := model.Token{
		Id: 7073, UserId: owner.Id, Key: "manual-task-subscription-token", Status: common.TokenStatusEnabled,
		Name: "manual-task-subscription-token", RemainQuota: 100, UsedQuota: 100, Group: "default",
	}
	subscription := model.UserSubscription{
		Id: 7074, UserId: owner.Id, AmountTotal: 1000, AmountUsed: 50,
		Status: "active", StartTime: 1, EndTime: time.Now().Add(24 * time.Hour).Unix(),
	}
	require.NoError(t, db.Create(&administrator).Error)
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&subscription).Error)
	task := model.Task{
		TaskID: "controller-manual-task-subscription-clamp", UserId: owner.Id, Group: "default", Quota: 100,
		Status: model.TaskStatus(model.TaskStatusInProgress), CreatedAt: 1, UpdatedAt: 1,
		PrivateData: model.TaskPrivateData{
			TokenId: token.Id, SubscriptionId: subscription.Id, BillingSource: "subscription",
			BillingRequestId: "controller-manual-task-subscription-clamp-preconsume",
			BillingContext:   &model.TaskBillingContext{OriginModelName: "manual-task-model"},
		},
	}
	task.SetData(map[string]any{"provider_status": "complete"})
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, db.Create(&model.SubscriptionPreConsumeRecord{
		RequestId: task.PrivateData.BillingRequestId, UserId: owner.Id, TokenId: token.Id,
		UserSubscriptionId: subscription.Id, PreConsumed: 100, Status: "consumed",
	}).Error)
	settlement := model.BillingSettlement{
		OperationKey: model.BillingTaskFinalizeOperationKey(task.ID), Source: model.BillingSettlementSourceSubscription,
		UserID: owner.Id, TokenID: token.Id, SubscriptionID: subscription.Id,
		SubscriptionPreConsumeRequestID: task.PrivateData.BillingRequestId,
		TaskID:                          task.ID, TaskQuota: 100, TaskQuotaTarget: 100,
		Status: model.BillingSettlementStatusManual, LastError: "provider usage requires an exact quota",
		CreatedAt: 1, UpdatedAt: 1, Revision: 1,
	}
	require.NoError(t, db.Create(&settlement).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/smart-ops/billing-settlements/%d/complete-task", settlement.ID),
		bytes.NewBufferString(`{"revision":1,"actual_quota":0,"note":"Verified provider evidence for a full refund."}`),
	)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", settlement.ID)}}
	ctx.Set("id", administrator.Id)
	ctx.Set("username", administrator.Username)
	ctx.Set("role", administrator.Role)

	CompleteManualTaskBillingSettlement(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "subscription usage mirror")
	var storedSubscription model.UserSubscription
	require.NoError(t, db.First(&storedSubscription, subscription.Id).Error)
	assert.EqualValues(t, 50, storedSubscription.AmountUsed)
	var storedToken model.Token
	require.NoError(t, db.First(&storedToken, token.Id).Error)
	assert.EqualValues(t, 100, storedToken.RemainQuota)
	assert.EqualValues(t, 100, storedToken.UsedQuota)
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

func TestReviewBillingSettlementsBatchClosesAlertsWithoutNotes(t *testing.T) {
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
		Id:       7201,
		Username: "billing-batch-review-admin",
		AffCode:  "billing-batch-review-admin-aff",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	owners := []model.User{
		{Id: 7202, Username: "billing-batch-owner-a", AffCode: "billing-batch-owner-a-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"},
		{Id: 7203, Username: "billing-batch-owner-b", AffCode: "billing-batch-owner-b-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"},
	}
	require.NoError(t, db.Create(&administrator).Error)
	require.NoError(t, db.Create(&owners).Error)
	settlements := []model.BillingSettlement{
		{
			OperationKey: "request:controller-batch-review-a:finalize", Source: model.BillingSettlementSourceWallet,
			UserID: owners[0].Id, FundingDelta: 10, TokenDelta: 10,
			Status: model.BillingSettlementStatusPending, CreatedAt: 1, UpdatedAt: 2, Revision: 3,
		},
		{
			OperationKey: "request:controller-batch-review-b:finalize", Source: model.BillingSettlementSourceWallet,
			UserID: owners[1].Id, FundingDelta: 11, TokenDelta: 11,
			Status: model.BillingSettlementStatusManual, CreatedAt: 2, UpdatedAt: 3, Revision: 4,
		},
	}
	require.NoError(t, db.Create(&settlements).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/smart-ops/billing-settlements/reviews",
		bytes.NewBufferString(fmt.Sprintf(
			`{"items":[{"id":%d,"revision":%d},{"id":%d,"revision":%d}]}`,
			settlements[0].ID,
			settlements[0].Revision,
			settlements[1].ID,
			settlements[1].Revision,
		)),
	)
	ctx.Set("id", administrator.Id)
	ctx.Set("username", administrator.Username)
	ctx.Set("role", administrator.Role)

	ReviewBillingSettlements(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var stored []model.BillingSettlement
	require.NoError(t, db.Where("id IN ?", []int64{settlements[0].ID, settlements[1].ID}).Order("id ASC").Find(&stored).Error)
	require.Len(t, stored, 2)
	for index := range stored {
		assert.Positive(t, stored[index].ReconciliationReviewedAt)
		assert.Equal(t, administrator.Id, stored[index].ReconciliationReviewedBy)
		assert.Empty(t, stored[index].ReconciliationReviewNote)
		require.NotNil(t, stored[index].UserBlockingOverride)
		assert.False(t, *stored[index].UserBlockingOverride)
		assert.Equal(t, settlements[index].Status, stored[index].Status)
		assert.Equal(t, settlements[index].UpdatedAt, stored[index].UpdatedAt)
		assert.Equal(t, settlements[index].Revision, stored[index].Revision)
	}

	var audit model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Order("id DESC").First(&audit).Error)
	var other struct {
		Op struct {
			Action string                 `json:"action"`
			Params map[string]interface{} `json:"params"`
		} `json:"op"`
	}
	require.NoError(t, common.UnmarshalJsonStr(audit.Other, &other))
	assert.Equal(t, "billing.reconciliation_review_batch", other.Op.Action)
	assert.EqualValues(t, 2, other.Op.Params["count"])
}

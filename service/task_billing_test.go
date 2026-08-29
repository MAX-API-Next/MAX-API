package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.BillingLogReceipt{},
		&model.Channel{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
		&model.BillingSettlement{},
		&model.BillingPreConsumeSelection{},
		&model.CacheInvalidationTask{},
		&model.Midjourney{},
		&model.MidjourneyBillingClaim{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func TestShouldApplyTaskResultProgressSkipsNonTerminalCompleteProgress(t *testing.T) {
	assert.False(t, shouldApplyTaskResultProgress(&relaycommon.TaskInfo{
		Status:   string(model.TaskStatusInProgress),
		Progress: "100%",
	}))
	assert.True(t, shouldApplyTaskResultProgress(&relaycommon.TaskInfo{
		Status:   string(model.TaskStatusInProgress),
		Progress: "75%",
	}))
	assert.True(t, shouldApplyTaskResultProgress(&relaycommon.TaskInfo{
		Status:   string(model.TaskStatusSuccess),
		Progress: "100%",
	}))
}

func TestBuildTaskSubmissionSettlementEffectCarriesRequestMetadata(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set(common.RequestIdKey, "task-client-request")
	ctx.Set(common.UpstreamRequestIdKey, "task-upstream-request")
	info := &relaycommon.RelayInfo{
		RequestId:       "task-relay-fallback-request",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 91},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate},
		OriginModelName: "task-model",
		TokenId:         92,
		UsingGroup:      "default",
		StartTime:       time.Now().Add(-3 * time.Second),
		IsStream:        true,
	}

	effect := BuildTaskSubmissionSettlementEffect(ctx, info, 123)

	require.NotNil(t, effect)
	assert.True(t, effect.UpdateUsage)
	assert.True(t, effect.QuotaIsActual)
	assert.EqualValues(t, 123, effect.Quota)
	assert.Equal(t, "task-client-request", effect.RequestID)
	assert.Equal(t, "task-upstream-request", effect.UpstreamRequestID)
	assert.GreaterOrEqual(t, effect.UseTimeSeconds, 3)
	assert.True(t, effect.IsStream)
}

func TestSweepTimedOutUnconfirmedSubmitRequiresReviewWithoutRefund(t *testing.T) {
	truncate(t)
	originalTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = originalTimeout })

	task := &model.Task{
		TaskID:     "task_unconfirmed_submit",
		Status:     model.TaskStatusNotStart,
		Quota:      30,
		SubmitTime: time.Now().Add(-2 * time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			AwaitingUpstreamID: true,
			BillingRequestId:   "unconfirmed-submit-request",
			BillingSource:      model.BillingSettlementSourceWallet,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	sweepTimedOutTasks(context.Background())

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	require.Equal(t, 30, reloaded.Quota)
	require.Contains(t, reloaded.FailReason, "未自动退款")
	var settlementCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Count(&settlementCount).Error)
	require.Zero(t, settlementCount)
}

func TestSweepTimedOutTaskWaitsForSubmissionSettlement(t *testing.T) {
	truncate(t)
	const userID = 711
	seedUser(t, userID, 80)
	originalTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = originalTimeout })
	now := time.Now().Unix()
	task := &model.Task{
		TaskID: "task_timeout_waits_for_settlement", Status: model.TaskStatusSubmitted,
		UserId: userID, Quota: 20, SubmitTime: time.Now().Add(-2 * time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID:   "provider-timeout-waits-for-settlement",
			BillingRequestId: "timeout-waits-for-settlement",
			BillingSource:    model.BillingSettlementSourceWallet,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	submissionInput := model.BillingSettlementInput{
		OperationKey:    model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId),
		Source:          model.BillingSettlementSourceWallet,
		UserID:          userID,
		FundingDelta:    10,
		TaskID:          task.ID,
		TaskQuota:       20,
		TaskQuotaTarget: 30,
	}
	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: submissionInput.OperationKey,
		Source:       submissionInput.Source, UserID: submissionInput.UserID,
		FundingDelta: submissionInput.FundingDelta, TaskID: submissionInput.TaskID,
		TaskQuota: submissionInput.TaskQuota, TaskQuotaTarget: submissionInput.TaskQuotaTarget,
		Status:    model.BillingSettlementStatusPending,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}).Error)

	sweepTimedOutTasks(context.Background())

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSubmitted, reloaded.Status)
	assert.Equal(t, 20, reloaded.Quota)
	assert.EqualValues(t, 80, getUserQuota(t, userID))

	applied, alreadyApplied, err := model.ApplyBillingSettlementOnce(submissionInput)
	require.NoError(t, err)
	assert.False(t, alreadyApplied)
	assert.EqualValues(t, 10, applied)
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSubmitted, reloaded.Status)
	assert.Equal(t, 30, reloaded.Quota)
	assert.EqualValues(t, 70, getUserQuota(t, userID))

	sweepTimedOutTasks(context.Background())

	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)
	assert.EqualValues(t, 100, getUserQuota(t, userID))
	var refund model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", fmt.Sprintf("task:%d:refund", task.ID)).First(&refund).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, refund.Status)
	assert.EqualValues(t, -30, refund.AppliedFundingDelta)
}

func TestSweepTimedOutTasksScansPastPendingSettlementPage(t *testing.T) {
	truncate(t)
	originalTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = originalTimeout })

	now := time.Now().Unix()
	pendingSubmitTime := legacyTaskRefundCutoff - 2
	tasks := make([]model.Task, 0, 101)
	settlements := make([]model.BillingSettlement, 0, 100)
	for i := 0; i < 100; i++ {
		requestID := fmt.Sprintf("timeout-pending-page-%d", i)
		tasks = append(tasks, model.Task{
			TaskID:     fmt.Sprintf("task_timeout_pending_page_%d", i),
			Status:     model.TaskStatusSubmitted,
			SubmitTime: pendingSubmitTime,
			PrivateData: model.TaskPrivateData{
				BillingRequestId: requestID,
			},
		})
		settlements = append(settlements, model.BillingSettlement{
			OperationKey: model.BillingRequestFinalizeOperationKey(requestID),
			Source:       model.BillingSettlementSourceWallet,
			UserID:       9000 + i,
			FundingDelta: 1,
			Status:       model.BillingSettlementStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
			Revision:     1,
		})
	}
	tasks = append(tasks, model.Task{
		TaskID:     "task_timeout_after_pending_page",
		Status:     model.TaskStatusSubmitted,
		SubmitTime: pendingSubmitTime + 1,
	})
	require.NoError(t, model.DB.CreateInBatches(&tasks, 25).Error)
	require.NoError(t, model.DB.CreateInBatches(&settlements, 25).Error)

	sweepTimedOutTasks(context.Background())

	var actionable model.Task
	require.NoError(t, model.DB.Where("task_id = ?", "task_timeout_after_pending_page").First(&actionable).Error)
	assert.EqualValues(t, model.TaskStatusFailure, actionable.Status)
	var pending model.Task
	require.NoError(t, model.DB.Where("task_id = ?", "task_timeout_pending_page_0").First(&pending).Error)
	assert.EqualValues(t, model.TaskStatusSubmitted, pending.Status)
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM billing_log_receipts")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM subscription_plans")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM subscription_pre_consume_records")
		model.DB.Exec("DELETE FROM billing_settlements")
		model.DB.Exec("DELETE FROM billing_pre_consume_selections")
		model.DB.Exec("DELETE FROM cache_invalidation_tasks")
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM midjourney_billing_claims")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: int64(quota), Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: int64(remainQuota),
		// Task fixtures model a token that already paid the pre-consumed quota.
		UsedQuota: int64(remainQuota),
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

func persistTask(t *testing.T, task *model.Task) {
	t.Helper()
	if task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.BillingRequestId == "" {
		task.PrivateData.BillingRequestId = "task-preconsume:" + task.TaskID
		var sub model.UserSubscription
		require.NoError(t, model.DB.Where("id = ?", task.PrivateData.SubscriptionId).First(&sub).Error)
		require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
			RequestId: task.PrivateData.BillingRequestId, UserId: task.UserId,
			TokenId: task.PrivateData.TokenId, UserSubscriptionId: sub.Id, PreConsumed: int64(task.Quota), Status: "consumed",
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		}).Error)
	}
	require.NoError(t, model.DB.Create(task).Error)
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int64 {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return int(token.RemainQuota)
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return int(token.UsedQuota)
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

func resetQuotaDataCache(t *testing.T) {
	t.Helper()
	model.CacheQuotaDataLock.Lock()
	defer model.CacheQuotaDataLock.Unlock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)

	RefundTaskQuota(ctx, task, "task failed: upstream error")

	// User quota should increase by preConsumed
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, tokenRemain-preConsumed, getTokenUsedQuota(t, tokenID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Zero(t, reloaded.Quota)
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	persistTask(t, task)

	RefundTaskQuota(ctx, task, "subscription task failed")

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)
	persistTask(t, task)

	RefundTaskQuota(ctx, task, "zero quota task")

	// No change to user quota
	assert.EqualValues(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0
	persistTask(t, task)

	RefundTaskQuota(ctx, task, "no token task failed")

	// User quota refunded
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestRefundTaskQuota_IsDurablyIdempotent(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 5, 5, 5
	const initQuota, preConsumed, tokenRemain = 10000, 2500, 4000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-durable-refund", tokenRemain)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)

	assert.True(t, RefundTaskQuota(ctx, task, "first failure"))
	task.Quota = preConsumed // Simulate a stale copy held by another poller.
	assert.True(t, RefundTaskQuota(ctx, task, "duplicate failure"))

	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))
	var logOther map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(getLastLog(t).Other, &logOther))
	assert.Equal(t, "first failure", logOther["reason"])
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Zero(t, reloaded.Quota)
}

func TestRefundTaskQuotaRefundsFundingWhenTokenWasDeleted(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 8, 8, 8
	const initialUserQuota, preConsumed = 1000, 250
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "deleted-before-refund", 2000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)
	require.NoError(t, model.DB.Delete(&model.Token{}, tokenID).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "token deleted while task was running"))
	assert.EqualValues(t, initialUserQuota+preConsumed, getUserQuota(t, userID))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Zero(t, reloaded.Quota)
}

func TestBackgroundTaskSettlementRecoveryRestoresLogAndUsageExactlyOnce(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 41, 42, 43
	seedUser(t, userID, 1000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 10, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)

	RecalculateTaskQuota(ctx, task, 15, "background recovery")
	var pending model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "task:"+fmt.Sprint(task.ID)+":finalize").First(&pending).Error)
	require.Equal(t, model.BillingSettlementStatusPending, pending.Status)

	seedToken(t, tokenID, userID, "background-recovery-token", 100)
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("id = ?", pending.ID).Update("next_attempt", 0).Error)
	model.ProcessPendingBillingSettlementsOnce()

	var reloadedTask model.Task
	require.NoError(t, model.DB.First(&reloadedTask, task.ID).Error)
	assert.EqualValues(t, 15, reloadedTask.Quota)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.EqualValues(t, 995, user.Quota)
	assert.EqualValues(t, 5, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	assert.EqualValues(t, 95, token.RemainQuota)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	assert.EqualValues(t, 5, channel.UsedQuota)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestConcurrentTaskSettlementEffectProcessingIsIdempotent(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 51, 52, 53
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "concurrent-effect-token", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 10, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)

	operationKey := "task:" + fmt.Sprint(task.ID) + ":concurrent-effect"
	_, _, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
		OperationKey: operationKey, Source: model.BillingSettlementSourceWallet,
		UserID: userID, TokenID: tokenID, FundingDelta: 5, TokenDelta: 5,
		TaskID: task.ID, TaskQuota: 10, TaskQuotaTarget: 15,
		Effect: &model.BillingSettlementEffect{
			LogType: model.LogTypeConsume, Content: "concurrent effect",
			ChannelID: channelID, ModelName: "test-model", TokenID: tokenID,
			Group: "default", Other: map[string]interface{}{"task_id": task.TaskID},
			UpdateUsage: true,
		},
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- model.ProcessBillingSettlementEffect(operationKey)
		}()
	}
	wg.Wait()
	close(errs)
	for effectErr := range errs {
		require.NoError(t, effectErr)
	}

	assert.Equal(t, int64(1), countLogs(t))
	var user model.User
	var channel model.Channel
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	assert.EqualValues(t, 5, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	assert.EqualValues(t, 5, channel.UsedQuota)
}

func TestTaskFinalizationRejectsChangedTargetWithoutSecondMutation(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 61, 62, 63
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "stable-task-finalize-token", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 10, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)

	RecalculateTaskQuota(ctx, task, 15, "first final result")
	RecalculateTaskQuota(ctx, task, 20, "changed replay result")

	assert.EqualValues(t, 995, getUserQuota(t, userID))
	assert.Equal(t, 95, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, 15, reloaded.Quota)
}

func TestProcessSunoTaskResponsePersistsRefundIntentBeforeApplying(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 6, 6, 6
	const pendingRefund, userQuota, tokenRemain = 250, 1000, 2000
	seedUser(t, userID, userQuota)
	seedToken(t, tokenID, userID, "sk-suno-pending-refund", tokenRemain)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, pendingRefund, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)

	processSunoTaskResponse(ctx, task, dto.SunoDataResponse{
		TaskID: task.TaskID, Status: string(model.TaskStatusFailure),
		FailReason: "upstream failed", FinishTime: time.Now().Unix(),
	})

	assert.EqualValues(t, userQuota+pendingRefund, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+pendingRefund, getTokenRemainQuota(t, tokenID))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Zero(t, reloaded.Quota)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", fmt.Sprintf("task:%d:refund", task.ID)).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
}

func TestProcessSunoTaskResponseRefundsWhenFailureReasonSanitizesEmpty(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 7, 7, 7
	const pendingRefund, userQuota, tokenRemain = 250, 1000, 2000
	seedUser(t, userID, userQuota)
	seedToken(t, tokenID, userID, "sk-suno-empty-sanitized-reason", tokenRemain)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, pendingRefund, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)

	processSunoTaskResponse(ctx, task, dto.SunoDataResponse{
		TaskID:     task.TaskID,
		Status:     string(model.TaskStatusInProgress),
		FailReason: "\x00\t",
		Data:       task.Data,
	})

	assert.EqualValues(t, userQuota+pendingRefund, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+pendingRefund, getTokenRemainQuota(t, tokenID))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Empty(t, reloaded.FailReason)
	assert.Zero(t, reloaded.Quota)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", fmt.Sprintf("task:%d:refund", task.ID)).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
}

func TestTaskFinalSettlementPendingBlocksTerminalPollingUntilFundingApplied(t *testing.T) {
	truncate(t)
	const userID = 701
	now := time.Now().Unix()
	task := makeTask(userID, 0, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingRequestId = "task-final-settlement-pending"
	persistTask(t, task)

	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: "request:task-final-settlement-pending:finalize",
		Source:       model.BillingSettlementSourceWallet,
		UserID:       userID,
		FundingDelta: 50,
		Status:       model.BillingSettlementStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
		Revision:     1,
	}).Error)

	pending, err := taskFinalSettlementPending(task)
	require.NoError(t, err)
	assert.True(t, pending)

	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", "request:task-final-settlement-pending:finalize").
		Update("status", model.BillingSettlementStatusApplied).Error)
	pending, err = taskFinalSettlementPending(task)
	require.NoError(t, err)
	assert.False(t, pending)
}

func TestTaskFinalSettlementEffectPendingDoesNotBlockPolling(t *testing.T) {
	truncate(t)
	const userID = 702
	now := time.Now().Unix()
	task := makeTask(userID, 0, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingRequestId = "task-final-effect-pending"
	persistTask(t, task)

	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: "request:task-final-effect-pending:finalize",
		Source:       model.BillingSettlementSourceWallet,
		UserID:       userID,
		FundingDelta: 0,
		Status:       model.BillingSettlementStatusApplied,
		EffectStatus: model.BillingSettlementEffectPending,
		CreatedAt:    now,
		UpdatedAt:    now,
		Revision:     1,
	}).Error)

	pending, err := taskFinalSettlementPending(task)
	require.NoError(t, err)
	assert.False(t, pending)
}

func TestProcessSunoTaskResponseWaitsForSubmissionSettlement(t *testing.T) {
	truncate(t)
	const userID = 703
	now := time.Now().Unix()
	task := makeTask(userID, 0, 100, 0, BillingSourceWallet, 0)
	task.Platform = constant.TaskPlatformSuno
	task.PrivateData.BillingRequestId = "suno-waits-for-submission-settlement"
	persistTask(t, task)
	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: "request:suno-waits-for-submission-settlement:finalize",
		Source:       model.BillingSettlementSourceWallet, UserID: userID,
		FundingDelta: 50, Status: model.BillingSettlementStatusPending,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}).Error)
	response := dto.SunoDataResponse{
		TaskID: task.TaskID, Status: string(model.TaskStatusSuccess),
		FinishTime: now, Data: json.RawMessage(`{"status":"SUCCESS"}`),
	}

	processSunoTaskResponse(context.Background(), task, response)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, reloaded.Status)

	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", "request:suno-waits-for-submission-settlement:finalize").
		Update("status", model.BillingSettlementStatusApplied).Error)
	processSunoTaskResponse(context.Background(), &reloaded, response)
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)
}

func TestMidjourneyTerminalCallbackWaitsForSubmissionSettlement(t *testing.T) {
	truncate(t)
	const userID, channelID = 704, 705
	now := time.Now().Unix()
	legacy := &model.Midjourney{
		UserId: userID, ChannelId: channelID, MjId: "provider-mj-waits-for-settlement",
		Status: "IN_PROGRESS", Progress: "50%", Code: 1,
	}
	require.NoError(t, model.DB.Create(legacy).Error)
	billingTask := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	billingTask.Platform = constant.TaskPlatformMidjourney
	billingTask.PrivateData.BillingRequestId = "mj-waits-for-submission-settlement"
	persistTask(t, billingTask)
	require.NoError(t, model.DB.Create(&model.MidjourneyBillingClaim{
		ChannelID: channelID, MjID: legacy.MjId, UserID: userID,
		BillingTaskID: billingTask.ID, BillingRequestID: billingTask.PrivateData.BillingRequestId,
		CreatedAt: now,
	}).Error)
	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: "request:mj-waits-for-submission-settlement:finalize",
		Source:       model.BillingSettlementSourceWallet, UserID: userID,
		FundingDelta: 50, Status: model.BillingSettlementStatusPending,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}).Error)
	response := dto.MidjourneyDto{
		MjId: legacy.MjId, Status: "SUCCESS", Progress: "100%", FinishTime: now * 1000,
	}

	require.NoError(t, UpdateMidjourneyTaskFromResponse(context.Background(), legacy, response))
	var reloadedLegacy model.Midjourney
	require.NoError(t, model.DB.First(&reloadedLegacy, legacy.Id).Error)
	assert.Equal(t, "IN_PROGRESS", reloadedLegacy.Status)
	var reloadedBilling model.Task
	require.NoError(t, model.DB.First(&reloadedBilling, billingTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, reloadedBilling.Status)

	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", "request:mj-waits-for-submission-settlement:finalize").
		Update("status", model.BillingSettlementStatusApplied).Error)
	require.NoError(t, UpdateMidjourneyTaskFromResponse(context.Background(), &reloadedLegacy, response))
	require.NoError(t, model.DB.First(&reloadedLegacy, legacy.Id).Error)
	assert.Equal(t, "SUCCESS", reloadedLegacy.Status)
	require.NoError(t, model.DB.First(&reloadedBilling, billingTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloadedBilling.Status)
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should decrease by the delta (1000 additional charge)
	assert.EqualValues(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_PositiveDeltaAttributesQuotaDataToSubmitNode(t *testing.T) {
	truncate(t)
	resetQuotaDataCache(t)
	ctx := context.Background()

	originalDataExportEnabled := common.DataExportEnabled
	originalNodeName := common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "polling-node"
	t.Cleanup(func() {
		common.DataExportEnabled = originalDataExportEnabled
		common.NodeName = originalNodeName
		resetQuotaDataCache(t)
	})

	const userID, tokenID, channelID = 110, 110, 110
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3500
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-node", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.NodeName = "submit-node"
	persistTask(t, task)

	RecalculateTaskQuota(ctx, task, actualQuota, "node attribution")

	require.Eventually(t, func() bool {
		model.CacheQuotaDataLock.Lock()
		defer model.CacheQuotaDataLock.Unlock()
		for _, quotaData := range model.CacheQuotaData {
			if quotaData.UserID == userID &&
				quotaData.ModelName == "test-model" &&
				quotaData.Quota == actualQuota-preConsumed &&
				quotaData.TokenID == tokenID &&
				quotaData.ChannelID == channelID &&
				quotaData.UseGroup == "default" {
				return quotaData.NodeName == "submit-node"
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.EqualValues(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 13
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, 5000, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	// No change (early return)
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	persistTask(t, task)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling reproduces the CAS + billing logic from updateVideoSingleTask.
// It takes a persisted task (already in DB), applies the new status, and performs
// the conditional update + billing exactly as the polling loop does.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	snap := task.Snapshot()

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
		shouldSettle = true
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		if quota != 0 {
			shouldRefund = true
		}
	default:
		task.Progress = "50%"
	}

	isDone := task.Status == model.TaskStatus(model.TaskStatusSuccess) || task.Status == model.TaskStatus(model.TaskStatusFailure)
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	if shouldSettle && actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "test settle")
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)

	// Refund should have happened
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	seedChannel(t, channelID)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.EqualValues(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, 0)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, 0)

	// Per-call: no recalculation by tokens
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_DeltaSettlementDisabledSnapshotSkipsAdjustments(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 34, 34, 34
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-disabled-snapshot", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.DeltaSettlementDisabled = common.GetPointer(true)
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: 3000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, constant.ChannelTypeDoubaoVideo, dto.ChannelOtherSettings{})

	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_DeltaSettlementDisabledChannelFallbackForLegacyTask(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 35, 35, 35
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-disabled-channel", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.DeltaSettlementDisabled = nil
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: 3000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, constant.ChannelTypeDoubaoVideo, dto.ChannelOtherSettings{
		DisableTaskDeltaSettlement: true,
	})

	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_DeltaSettlementSnapshotOverridesCurrentChannelSetting(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 36, 36, 36
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-enabled-snapshot", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.DeltaSettlementDisabled = common.GetPointer(false)
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, constant.ChannelTypeDoubaoVideo, dto.ChannelOtherSettings{
		DisableTaskDeltaSettlement: true,
	})

	assert.EqualValues(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestSettle_DeltaSettlementDisabledIgnoredForNonDoubaoVideo(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 37, 37, 37
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-disabled-non-doubao", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.DeltaSettlementDisabled = common.GetPointer(true)
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, constant.ChannelTypeKling, dto.ChannelOtherSettings{
		DisableTaskDeltaSettlement: true,
	})

	assert.EqualValues(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestSettle_NonPerCall_AdaptorAdjustWorks(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false
	persistTask(t, task)

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult, 0)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.EqualValues(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

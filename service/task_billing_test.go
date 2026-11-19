package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
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
		&model.TaskSubmitFailureRecovery{},
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
	ctx.Set("token_name", "request-time-task-token")
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
	assert.Equal(t, "request-time-task-token", effect.TokenName)
	assert.GreaterOrEqual(t, effect.UseTimeSeconds, 3)
	assert.True(t, effect.IsStream)
}

func TestBuildTaskExactFinalSettlementUsesPreConsumedQuotaMetadata(t *testing.T) {
	task := makeTask(901, 902, 100, 0, BillingSourceWallet, 0)
	task.ID = 903
	task.TaskID = "task-exact-finalize"

	input := buildTaskExactFinalSettlementInput(task, 80, nil, "exact settlement")

	require.NotNil(t, input)
	require.NotNil(t, input.Effect)
	assert.Equal(t, 100, input.Effect.Other["pre_consumed_quota"])
	assert.NotContains(t, input.Effect.Other, "reserved_quota")
}

func TestTaskBillingOtherRestrictsStructuredReconciliationMetadata(t *testing.T) {
	zero := int64(0)
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				TaskBillingPlan: &types.TaskBillingPlan{
					Source:       "minimax_h3_v2",
					RuleKey:      "minimax_h3_v2",
					ConfigHash:   "must-not-reach-user-logs",
					GroupRatio:   1.25,
					QuotaPerUnit: 500000,
					ReserveQuota: 800,
					Components: []types.TaskBillingPlanComponent{{
						Key: "output_video", UnitPrice: "9.99", ReservedQuantity: 5,
					}},
				},
				TaskUsage: &types.TaskUsage{
					OutputDurationMs: &zero, InputVideoDurationMs: &zero, InputAudioDurationMs: &zero,
					InputImageCount: &zero, InputVideoCount: &zero, InputAudioCount: &zero,
					Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
				},
			},
		},
	}

	other := taskBillingOther(task)
	planMetadata, ok := other["task_billing_plan"].(map[string]interface{})
	require.True(t, ok)
	require.Len(t, planMetadata, 3)
	require.Equal(t, "minimax_h3_v2", planMetadata["source"])
	require.Equal(t, "minimax_h3_v2", planMetadata["rule_key"])
	require.EqualValues(t, 800, planMetadata["reserve_quota"])
	require.NotContains(t, planMetadata, "config_hash")
	require.NotContains(t, planMetadata, "group_ratio")
	require.NotContains(t, planMetadata, "quota_per_unit")
	require.NotContains(t, planMetadata, "components")

	usageMetadata, ok := other["task_usage"].(map[string]interface{})
	require.True(t, ok)
	require.Len(t, usageMetadata, 8)
	for _, key := range []string{
		"output_duration_ms", "input_video_duration_ms", "input_audio_duration_ms",
		"input_image_count", "input_video_count", "input_audio_count",
	} {
		require.Contains(t, usageMetadata, key)
		require.EqualValues(t, 0, usageMetadata[key])
	}
	require.Equal(t, types.TaskUsageSourceProviderResponse, usageMetadata["source"])
	require.Equal(t, types.TaskUsageCompletenessComplete, usageMetadata["completeness"])
}

func TestBuildTaskExactFinalSettlementUsesAllowlistedUsageMetadata(t *testing.T) {
	task := makeTask(901, 902, 100, 0, BillingSourceWallet, 0)
	task.ID = 903
	task.PrivateData.BillingContext.TaskBillingPlan = &types.TaskBillingPlan{
		RuleKey: "minimax_h3_v2", ReserveQuota: 100, ConfigHash: "must-not-reach-user-logs",
	}
	task.PrivateData.BillingContext.TaskUsage = &types.TaskUsage{
		Completeness: types.TaskUsageCompletenessPartial,
	}
	zero := int64(0)
	usage := &types.TaskUsage{
		OutputDurationMs: &zero,
		InputImageCount:  &zero,
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}

	input := buildTaskExactFinalSettlementInput(task, 80, usage, "exact settlement")

	require.NotNil(t, input)
	require.NotNil(t, input.Effect)
	usageMetadata, ok := input.Effect.Other["task_usage"].(map[string]interface{})
	require.True(t, ok)
	require.EqualValues(t, 0, usageMetadata["output_duration_ms"])
	require.EqualValues(t, 0, usageMetadata["input_image_count"])
	require.Equal(t, types.TaskUsageSourceProviderResponse, usageMetadata["source"])
	require.Equal(t, types.TaskUsageCompletenessComplete, usageMetadata["completeness"])
	require.NotContains(t, usageMetadata, "unit_price")
}

func TestSweepTimedOutUnconfirmedSubmitRequiresReviewWithoutRefund(t *testing.T) {
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
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

func TestTimeoutUpdatesPreserveNewerProviderPayload(t *testing.T) {
	for _, unconfirmed := range []bool{false, true} {
		t.Run(fmt.Sprintf("unconfirmed-%t", unconfirmed), func(t *testing.T) {
			truncate(t)
			resetTimedOutTaskSweepCursorForTest(t)
			previous := constant.TaskTimeoutMinutes
			constant.TaskTimeoutMinutes = 1
			t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })
			seedUser(t, 741, 80)
			seedToken(t, 742, 741, "synthetic-payload-token", 80)
			seedChannel(t, 743)
			task := makeTask(741, 743, 20, 742, BillingSourceWallet, 0)
			task.Status = model.TaskStatusSubmitted
			task.SubmitTime = time.Now().Add(-2 * time.Minute).Unix()
			task.PrivateData.AwaitingUpstreamID = unconfirmed
			task.Data = json.RawMessage(`{"provider_progress":"old"}`)
			persistTask(t, task)
			const newer = `{"provider_progress":"new"}`
			injected := false
			require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("publish-after-timeout-snapshot", func(tx *gorm.DB) {
				if !injected && tx.Statement.Table == "tasks" && tx.RowsAffected > 0 {
					injected = true
					require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).UpdateColumn("data", []byte(newer)).Error)
				}
			}))
			t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("publish-after-timeout-snapshot") })
			sweepTimedOutTasks(t.Context())
			require.True(t, injected)
			var stored model.Task
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			require.Equal(t, newer, string(stored.Data), "timeout state changes must not overwrite a newer same-version provider payload")
			require.EqualValues(t, model.TaskStatusFailure, stored.Status)
			if unconfirmed {
				require.Equal(t, 20, stored.Quota)
				require.EqualValues(t, 80, getUserQuota(t, 741))
				require.Zero(t, countLogs(t))
			} else {
				require.Zero(t, stored.Quota)
				require.EqualValues(t, 100, getUserQuota(t, 741))
				require.EqualValues(t, 1, countLogs(t))
			}
		})
	}
}

func TestSweepTimeoutSnapshotLosesToConfirmedSubmission(t *testing.T) {
	for _, source := range []string{BillingSourceWallet, BillingSourceSubscription} {
		for _, awaiting := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/awaiting_%t", source, awaiting), func(t *testing.T) {
				truncate(t)
				resetTimedOutTaskSweepCursorForTest(t)
				previous := constant.TaskTimeoutMinutes
				constant.TaskTimeoutMinutes = 1
				t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })
				const userID, tokenID, channelID, subID = 731, 732, 733, 734
				seedUser(t, userID, 80)
				seedToken(t, tokenID, userID, "synthetic-snapshot-token", 80)
				seedChannel(t, channelID)
				if source == BillingSourceSubscription {
					seedSubscription(t, subID, userID, 1000, 100)
				}
				task := makeTask(userID, channelID, 20, tokenID, source, subID)
				task.Status = model.TaskStatusSubmitted
				task.SubmitTime = time.Now().Add(-2 * time.Minute).Unix()
				task.PrivateData.AwaitingUpstreamID = awaiting
				persistTask(t, task)
				confirmed := task.PrivateData
				confirmed.AwaitingUpstreamID = false
				confirmed.UpstreamTaskID = "confirmed-upstream-id"
				injected := false
				// The worker has loaded its old page; publish a newer same-status
				// submission before its decision/transaction, using the real DB.
				require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("confirm-after-timeout-page", func(tx *gorm.DB) {
					if !injected && tx.Statement.Table == "tasks" && tx.RowsAffected > 0 {
						injected = true
						confirmation := *task
						confirmation.PrivateData = confirmed
						require.NoError(t, confirmation.UpdateWithSettlementIntent(nil), "use the actual Relay confirmation writer")
					}
				}))
				t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("confirm-after-timeout-page") })
				sweepTimedOutTasks(t.Context())
				require.True(t, injected)
				var fresh model.Task
				require.NoError(t, model.DB.First(&fresh, task.ID).Error)
				require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), fresh.Status)
				require.Equal(t, confirmed, fresh.PrivateData)
				require.Equal(t, 20, fresh.Quota)
				var intents int64
				require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Count(&intents).Error)
				require.Zero(t, intents, "a stale timeout must not leave an intent")
				require.EqualValues(t, 80, getUserQuota(t, userID))
				require.Equal(t, 80, getTokenRemainQuota(t, tokenID))
				require.Equal(t, 80, getTokenUsedQuota(t, tokenID))
				require.Zero(t, countLogs(t))
				if source == BillingSourceSubscription {
					require.EqualValues(t, 100, getSubscriptionUsed(t, subID))
				}
				// A later fresh sweep reevaluates confirmed-but-overdue work and
				// applies the original refund exactly once, also after reload.
				resetTimedOutTaskSweepCursorForTest(t)
				sweepTimedOutTasks(t.Context())
				sweepTimedOutTasks(t.Context())
				require.NoError(t, model.DB.First(&fresh, task.ID).Error)
				require.Equal(t, model.TaskStatus(model.TaskStatusFailure), fresh.Status)
				require.NotContains(t, fresh.FailReason, "未自动退款")
				require.Zero(t, fresh.Quota)
				require.Equal(t, confirmed, fresh.PrivateData)
				require.Equal(t, 100, getTokenRemainQuota(t, tokenID))
				require.Equal(t, 60, getTokenUsedQuota(t, tokenID))
				if source == BillingSourceWallet {
					require.EqualValues(t, 100, getUserQuota(t, userID))
				} else {
					require.EqualValues(t, 80, getUserQuota(t, userID))
					require.EqualValues(t, 80, getSubscriptionUsed(t, subID))
				}
				require.EqualValues(t, 1, countLogs(t))
				require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Count(&intents).Error)
				require.EqualValues(t, 1, intents)
			})
		}
	}
}

func TestSweepTimeoutCannotReopenAfterConfirmedFunding(t *testing.T) {
	for _, source := range []string{BillingSourceWallet, BillingSourceSubscription} {
		for _, delta := range []int{-5, 5} {
			t.Run(fmt.Sprintf("%s/delta_%d", source, delta), func(t *testing.T) {
				truncate(t)
				resetTimedOutTaskSweepCursorForTest(t)
				priorDB, priorTimeout := model.DB, constant.TaskTimeoutMinutes
				fixed := time.Now()
				model.DB = model.DB.Session(&gorm.Session{NowFunc: func() time.Time { return fixed }})
				constant.TaskTimeoutMinutes = 1
				t.Cleanup(func() { model.DB, constant.TaskTimeoutMinutes = priorDB, priorTimeout })
				const userID, tokenID, channelID, subID = 741, 742, 743, 744
				seedUser(t, userID, 80)
				seedToken(t, tokenID, userID, "synthetic-funded-confirm-token", 80)
				seedChannel(t, channelID)
				if source == BillingSourceSubscription {
					seedSubscription(t, subID, userID, 1000, 100)
				}
				task := makeTask(userID, channelID, 20, tokenID, source, subID)
				task.UpdatedAt = fixed.Unix()
				task.Status = model.TaskStatusSubmitted
				task.SubmitTime = fixed.Add(-2 * time.Minute).Unix()
				task.PrivateData.AwaitingUpstreamID = true
				if source == BillingSourceWallet {
					task.PrivateData.BillingRequestId = fmt.Sprintf("funded-confirm-%s-%d", source, delta)
				}
				persistTask(t, task)
				var funding FundingSource = &WalletFunding{userId: userID, consumed: 20}
				if source == BillingSourceSubscription {
					funding = &SubscriptionFunding{requestId: task.PrivateData.BillingRequestId, userId: userID, subscriptionId: subID, preConsumed: 20}
				}
				session := &BillingSession{relayInfo: &relaycommon.RelayInfo{RequestId: task.PrivateData.BillingRequestId, UserId: userID, TokenId: tokenID, TokenKey: "synthetic-funded-confirm-token", TaskRelayInfo: &relaycommon.TaskRelayInfo{PersistedTaskID: task.ID}}, funding: funding, preConsumedQuota: 20, tokenConsumed: 20}
				injected := false
				require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("fund-after-timeout-page", func(tx *gorm.DB) {
					if !injected && tx.Statement.Table == "tasks" && tx.RowsAffected > 0 {
						injected = true
						confirmed := *task
						confirmed.PrivateData.AwaitingUpstreamID = false
						confirmed.PrivateData.UpstreamTaskID = "confirmed-funded-id"
						input, err := session.PrepareSettlement(20 + delta)
						require.NoError(t, err)
						require.NoError(t, confirmed.UpdateWithSettlementIntent(input))
						require.NoError(t, session.Settle(20+delta))
						var projected model.Task
						require.NoError(t, model.DB.First(&projected, task.ID).Error)
						require.Equal(t, fixed.Unix(), projected.UpdatedAt, "quota guard must be tested while the timestamp still matches the old snapshot")
						require.Equal(t, 20+delta, projected.Quota)
					}
				}))
				t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("fund-after-timeout-page") })
				sweepTimedOutTasks(t.Context())
				require.True(t, injected)
				var fresh model.Task
				require.NoError(t, model.DB.First(&fresh, task.ID).Error)
				require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), fresh.Status)
				require.Equal(t, "confirmed-funded-id", fresh.PrivateData.UpstreamTaskID)
				require.False(t, fresh.PrivateData.AwaitingUpstreamID)
				require.Equal(t, 20+delta, fresh.Quota)
				require.Equal(t, 80-delta, getTokenRemainQuota(t, tokenID))
				require.Equal(t, 80+delta, getTokenUsedQuota(t, tokenID))
				if source == BillingSourceWallet {
					require.EqualValues(t, 80-delta, getUserQuota(t, userID))
				} else {
					require.EqualValues(t, 100+delta, getSubscriptionUsed(t, subID))
				}
				var records []model.BillingSettlement
				require.NoError(t, model.DB.Find(&records).Error)
				require.Len(t, records, 1, "stale timeout cannot append another funding operation")
				require.Equal(t, model.BillingSettlementStatusApplied, records[0].Status)
				require.NoError(t, session.Settle(20+delta))
				resetTimedOutTaskSweepCursorForTest(t)
				sweepTimedOutTasks(t.Context())
				sweepTimedOutTasks(t.Context())
				require.NoError(t, model.DB.First(&fresh, task.ID).Error)
				require.Equal(t, model.TaskStatus(model.TaskStatusFailure), fresh.Status)
				require.NotContains(t, fresh.FailReason, "未自动退款")
				if source == BillingSourceSubscription && delta < 0 {
					// Existing full-refund coverage uses the original pre-consume20,
					// so refund15 after a -5 request adjustment requires reconciliation.
					// This metadata/CAS fix must not invent a second funding policy.
					require.Equal(t, 15, fresh.Quota)
					require.EqualValues(t, 95, getSubscriptionUsed(t, subID))
					require.Equal(t, 85, getTokenRemainQuota(t, tokenID))
					require.Equal(t, 75, getTokenUsedQuota(t, tokenID))
					require.NoError(t, model.DB.Find(&records).Error)
					require.Len(t, records, 2)
					var manual *model.BillingSettlement
					for i := range records {
						if records[i].Status == model.BillingSettlementStatusManual {
							manual = &records[i]
						}
					}
					require.NotNil(t, manual)
					require.EqualValues(t, -15, manual.FundingDelta)
					require.Zero(t, manual.AppliedFundingDelta)
					require.Zero(t, manual.AppliedTokenDelta)
					require.Contains(t, manual.LastError, "does not cover pre-consumed amount")
					require.Zero(t, countLogs(t))
					return
				}
				require.Zero(t, fresh.Quota)
				require.Equal(t, 100, getTokenRemainQuota(t, tokenID))
				require.Equal(t, 60, getTokenUsedQuota(t, tokenID))
				if source == BillingSourceWallet {
					require.EqualValues(t, 100, getUserQuota(t, userID))
				} else {
					require.EqualValues(t, 80, getSubscriptionUsed(t, subID))
				}
				require.NoError(t, model.DB.Find(&records).Error)
				require.Len(t, records, 2)
				var fundingSum, tokenSum int64
				for _, record := range records {
					require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
					fundingSum += record.AppliedFundingDelta
					tokenSum += record.AppliedTokenDelta
				}
				require.EqualValues(t, -20, fundingSum)
				require.EqualValues(t, -20, tokenSum)
			})
		}
	}
}

func TestSweepTimedOutTaskWaitsForSubmissionSettlement(t *testing.T) {
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
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

func isolateTaskPluginBillingDatabase(t *testing.T) {
	t.Helper()
	isolated, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := isolated.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, isolated.AutoMigrate(&model.Task{}, &model.TaskSubmitFailureRecovery{}, &model.User{}, &model.Token{}, &model.Log{}, &model.BillingLogReceipt{}, &model.Channel{}, &model.TopUp{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{}, &model.Midjourney{}, &model.MidjourneyBillingClaim{}, &model.Option{}, &model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{}, &model.TaskPluginDeletion{}))
	previousDB, previousLog := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = isolated, isolated
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLog; _ = sqlDB.Close() })
}

func TestSweepTimedOutHistoricalPluginBindingPreservesRefund(t *testing.T) {
	for _, asText := range []bool{false, true} {
		t.Run(fmt.Sprintf("text_%t", asText), func(t *testing.T) { testSweepTimedOutHistoricalPluginBindingPreservesRefund(t, asText) })
	}
}

func testSweepTimedOutHistoricalPluginBindingPreservesRefund(t *testing.T, asText bool) {
	t.Helper()
	isolateTaskPluginBillingDatabase(t)
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
	const userID, tokenID = 717, 718
	seedUser(t, userID, 80)
	seedToken(t, tokenID, userID, "synthetic-timeout-token", 80)
	originalTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = originalTimeout })
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "historical-source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "stored", Name: "Stored", Version: "1.0.0", Models: []string{"historical-model"}}})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
	require.NoError(t, err)
	catalog, err := taskplugin.OpenCatalog(t.Context(), model.NewTaskPluginCatalogStore(model.DB))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	bad := &model.Task{TaskID: "unreadable-timeout-neighbor", Status: model.TaskStatusSubmitted, UserId: userID, Quota: 20, SubmitTime: time.Now().Add(-3 * time.Minute).Unix()}
	require.NoError(t, model.DB.Create(bad).Error)
	const malformed = `{"upstream_task_id":42}`
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", bad.ID).UpdateColumn("private_data", malformed).Error)
	task := &model.Task{TaskID: "historical-timeout-refund", Status: model.TaskStatusSubmitted, UserId: userID, Quota: 20, SubmitTime: time.Now().Add(-2 * time.Minute).Unix(), PrivateData: model.TaskPrivateData{UpstreamTaskID: "old-provider-id", BillingSource: model.BillingSettlementSourceWallet, BillingRequestId: "historical-timeout", TokenId: tokenID}}
	require.NoError(t, model.DB.Create(task).Error)
	task.PrivateData.PluginBinding = &types.TaskPluginBinding{PluginKey: "stored"}
	raw, err := common.Marshal(task.PrivateData)
	require.NoError(t, err)
	var storedPrivate any = raw
	if asText {
		storedPrivate = string(raw)
	}
	require.NoError(t, model.DB.Model(task).UpdateColumn("private_data", storedPrivate).Error, "seed an existing legacy row, not a new binding admission")
	var historical model.Task
	require.NoError(t, model.DB.First(&historical, task.ID).Error)
	require.NotNil(t, historical.PrivateData.PluginBinding)
	require.Equal(t, "stored", historical.PrivateData.PluginBinding.PluginKey)
	sweepTimedOutTasks(context.Background())
	var result model.Task
	require.NoError(t, model.DB.First(&result, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), result.Status)
	require.Zero(t, result.Quota)
	require.EqualValues(t, 100, getUserQuota(t, userID))
	require.EqualValues(t, 100, getTokenRemainQuota(t, tokenID))
	var badRow struct {
		Status      model.TaskStatus
		Quota       int
		PrivateData string
	}
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", bad.ID).Take(&badRow).Error)
	require.EqualValues(t, model.TaskStatusSubmitted, badRow.Status)
	require.Equal(t, 20, badRow.Quota)
	require.Equal(t, malformed, badRow.PrivateData, "bad metadata is retained for repair, never replaced with zero values")
	var badIntents int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("task_id = ?", bad.ID).Count(&badIntents).Error)
	require.Zero(t, badIntents, "an unreadable neighbor must not create a refund or finalization intent")
	var actual string
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Select("private_data").Scan(&actual).Error)
	require.Equal(t, string(raw), actual, "timeout must leave historical metadata bytes intact")
	sweepTimedOutTasks(context.Background())
	require.EqualValues(t, 100, getUserQuota(t, userID))
	require.EqualValues(t, 100, getTokenRemainQuota(t, tokenID))
	var refund model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", fmt.Sprintf("task:%d:refund", task.ID)).First(&refund).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, refund.Status)
	require.EqualValues(t, -20, refund.AppliedFundingDelta)
	replay := BuildTaskRefundSettlementInput(task, fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes))
	require.NotNil(t, replay)
	_, already, err := model.ApplyBillingSettlementOnce(*replay)
	require.NoError(t, err)
	require.True(t, already)
	require.EqualValues(t, 100, getUserQuota(t, userID))
}

type historicalCompletionAdaptor struct{ mockAdaptor }

type historicalProgressAdaptor struct{ historicalCompletionAdaptor }

func (*historicalProgressAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: "50%"}, nil
}

func TestPollingProgressPreservesUnknownTextReference(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	seedUser(t, 727, 80)
	seedToken(t, 728, 727, "synthetic-history-progress", 80)
	seedChannel(t, 729)
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "history", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "stored", Name: "Stored", Version: "1.0.0", Models: []string{"historical-model"}}})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
	require.NoError(t, err)
	catalog, err := taskplugin.OpenCatalog(t.Context(), model.NewTaskPluginCatalogStore(model.DB))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	row := makeTask(727, 729, 20, 728, model.BillingSettlementSourceWallet, 0)
	row.TaskID, row.Status = "history-progress", model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(row).Error)
	raw, err := common.Marshal(row.PrivateData)
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, common.UnmarshalUseNumber(raw, &object))
	object["extension_history"] = map[string]any{"plugin_key": "stored", "version": "1.0.0"}
	raw, err = common.Marshal(object)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(row).UpdateColumn("private_data", string(raw)).Error)
	var loaded model.Task
	require.NoError(t, model.DB.First(&loaded, row.ID).Error)
	taskWrites, privateWrites := 0, 0
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("observe-progress-private-column", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			if values, ok := tx.Statement.Dest.(map[string]any); ok {
				taskWrites++
				if _, present := values["private_data"]; present {
					privateWrites++
				}
			}
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("observe-progress-private-column") })
	for attempt := 0; attempt < 2; attempt++ {
		require.NoError(t, updateVideoSingleTask(t.Context(), &historicalProgressAdaptor{}, &model.Channel{Id: 729, Type: constant.ChannelTypeKling}, loaded.TaskID, map[string]*model.Task{loaded.TaskID: &loaded}))
		require.NoError(t, model.DB.First(&loaded, row.ID).Error)
	}
	var stored []byte
	require.NoError(t, model.DB.Model(row).Select("private_data").Row().Scan(&stored))
	require.Contains(t, string(stored), "extension_history")
	require.Equal(t, "50%", loaded.Progress)
	require.Positive(t, taskWrites)
	require.Zero(t, privateWrites, "progress-only polling omits the unchanged private column")
	require.Equal(t, 20, loaded.Quota)
	require.ErrorIs(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}), taskplugin.ErrCatalogInUse)
	require.EqualValues(t, 80, getUserQuota(t, 727))
	require.Equal(t, 80, getTokenRemainQuota(t, 728))
	require.Zero(t, countLogs(t))
}

func (*historicalCompletionAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

func (*historicalCompletionAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Url: "https://example.invalid/completed"}, nil
}

func TestPollingHistoricalPluginBindingCompletesAndSettlesOnce(t *testing.T) {
	for _, asText := range []bool{false, true} {
		t.Run(fmt.Sprintf("text_%t", asText), func(t *testing.T) { testPollingHistoricalPluginBindingCompletesAndSettlesOnce(t, asText) })
	}
}

func testPollingHistoricalPluginBindingCompletesAndSettlesOnce(t *testing.T, asText bool) {
	t.Helper()
	isolateTaskPluginBillingDatabase(t)
	const userID, tokenID, channelID = 727, 728, 729
	seedUser(t, userID, 80)
	seedToken(t, tokenID, userID, "synthetic-completion-token", 80)
	seedChannel(t, channelID)
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "historical-source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "stored", Name: "Stored", Version: "1.0.0", Models: []string{"historical-model"}}})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
	require.NoError(t, err)
	catalog, err := taskplugin.OpenCatalog(t.Context(), model.NewTaskPluginCatalogStore(model.DB))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	task := makeTask(userID, channelID, 20, tokenID, model.BillingSettlementSourceWallet, 0)
	task.TaskID = "historical-completion"
	require.NoError(t, model.DB.Create(task).Error)
	task.PrivateData.PluginBinding = &types.TaskPluginBinding{PluginKey: "stored", UserID: userID}
	raw, err := common.Marshal(task.PrivateData)
	require.NoError(t, err)
	var storedPrivate any = raw
	if asText {
		storedPrivate = string(raw)
	}
	require.NoError(t, model.DB.Model(task).UpdateColumn("private_data", storedPrivate).Error)
	var loaded model.Task
	require.NoError(t, model.DB.First(&loaded, task.ID).Error)
	require.NotNil(t, loaded.PrivateData.PluginBinding)
	adaptor := &historicalCompletionAdaptor{mockAdaptor{adjustReturn: 30}}
	channel := &model.Channel{Id: channelID, Type: constant.ChannelTypeKling}
	for attempt := 0; attempt < 2; attempt++ {
		require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, loaded.TaskID, map[string]*model.Task{loaded.TaskID: &loaded}))
		var restarted model.Task
		require.NoError(t, model.DB.First(&restarted, task.ID).Error, "reload into a fresh value as a restarted poller would")
		loaded = restarted
		require.EqualValues(t, model.TaskStatusSuccess, loaded.Status)
		require.Equal(t, "https://example.invalid/completed", loaded.PrivateData.ResultURL)
		require.Equal(t, task.PrivateData.PluginBinding, loaded.PrivateData.PluginBinding)
		require.Equal(t, 30, loaded.Quota)
		require.EqualValues(t, 70, getUserQuota(t, userID), "final30 minus reserved20 consumes10 exactly once")
		require.Equal(t, 70, getTokenRemainQuota(t, tokenID))
	}
	var settlements []model.BillingSettlement
	require.NoError(t, model.DB.Where("task_id = ?", task.ID).Find(&settlements).Error)
	require.Len(t, settlements, 1)
	require.Equal(t, model.BillingSettlementStatusApplied, settlements[0].Status)
	require.EqualValues(t, 10, settlements[0].AppliedFundingDelta)
}

func TestSweepTimedOutTasksScansPastPendingSettlementPage(t *testing.T) {
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
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

func TestSweepTimedOutTasksBoundsScansAndContinuesFromCursor(t *testing.T) {
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
	originalTimeout := constant.TaskTimeoutMinutes
	originalScanBudget := timedOutTaskScanBudget
	constant.TaskTimeoutMinutes = 1
	timedOutTaskScanBudget = 2
	t.Cleanup(func() {
		constant.TaskTimeoutMinutes = originalTimeout
		timedOutTaskScanBudget = originalScanBudget
	})

	now := time.Now().Unix()
	submitTime := legacyTaskRefundCutoff - 2
	for i := 0; i < 2; i++ {
		requestID := fmt.Sprintf("bounded-timeout-pending-%d", i)
		task := model.Task{
			TaskID:      fmt.Sprintf("bounded_timeout_pending_%d", i),
			Status:      model.TaskStatusSubmitted,
			SubmitTime:  submitTime,
			PrivateData: model.TaskPrivateData{BillingRequestId: requestID},
		}
		require.NoError(t, model.DB.Create(&task).Error)
		require.NoError(t, model.DB.Create(&model.BillingSettlement{
			OperationKey: model.BillingRequestFinalizeOperationKey(requestID),
			Source:       model.BillingSettlementSourceWallet,
			UserID:       9100 + i,
			FundingDelta: 1,
			Status:       model.BillingSettlementStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
			Revision:     1,
		}).Error)
	}
	actionable := model.Task{
		TaskID:     "bounded_timeout_actionable",
		Status:     model.TaskStatusSubmitted,
		SubmitTime: submitTime + 1,
	}
	require.NoError(t, model.DB.Create(&actionable).Error)

	sweepTimedOutTasks(context.Background())
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, actionable.ID).Error)
	assert.EqualValues(t, model.TaskStatusSubmitted, reloaded.Status)

	sweepTimedOutTasks(context.Background())
	require.NoError(t, model.DB.First(&reloaded, actionable.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
}

func TestSweepTimedOutTasksDoesNotSpendBudgetOnTaskFinalizeOwnedTasks(t *testing.T) {
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
	originalTimeout := constant.TaskTimeoutMinutes
	originalScanBudget := timedOutTaskScanBudget
	constant.TaskTimeoutMinutes = 1
	timedOutTaskScanBudget = 1
	t.Cleanup(func() {
		constant.TaskTimeoutMinutes = originalTimeout
		timedOutTaskScanBudget = originalScanBudget
	})

	now := time.Now().Unix()
	submitTime := legacyTaskRefundCutoff - 2
	protected := []model.Task{
		{TaskID: "timeout-finalize-pending", Status: model.TaskStatusSubmitted, SubmitTime: submitTime},
		{TaskID: "timeout-finalize-manual", Status: model.TaskStatusSubmitted, SubmitTime: submitTime},
		{TaskID: "timeout-finalize-applied", Status: model.TaskStatusSubmitted, SubmitTime: submitTime},
	}
	require.NoError(t, model.DB.Create(&protected).Error)
	statuses := []string{
		model.BillingSettlementStatusPending,
		model.BillingSettlementStatusManual,
		model.BillingSettlementStatusApplied,
	}
	for i := range protected {
		require.NoError(t, model.DB.Create(&model.BillingSettlement{
			OperationKey: model.BillingTaskFinalizeOperationKey(protected[i].ID),
			Source:       model.BillingSettlementSourceWallet,
			TaskID:       protected[i].ID,
			Status:       statuses[i],
			CreatedAt:    now,
			UpdatedAt:    now,
			Revision:     1,
		}).Error)
	}
	actionable := model.Task{
		TaskID:     "timeout-after-finalize-backlog",
		Status:     model.TaskStatusSubmitted,
		SubmitTime: submitTime + 1,
	}
	require.NoError(t, model.DB.Create(&actionable).Error)

	sweepTimedOutTasks(context.Background())

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, actionable.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	for i := range protected {
		reloaded = model.Task{}
		require.NoError(t, model.DB.First(&reloaded, protected[i].ID).Error)
		if statuses[i] == model.BillingSettlementStatusApplied {
			assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
			assert.Contains(t, reloaded.FailReason, "manual reconciliation")
		} else {
			assert.EqualValues(t, model.TaskStatusSubmitted, reloaded.Status)
		}
	}
}

func TestSweepTimedOutTasksRecoversAppliedFinalizeTerminalEvidence(t *testing.T) {
	truncate(t)
	resetTimedOutTaskSweepCursorForTest(t)
	originalTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = originalTimeout })

	task := &model.Task{
		TaskID:     "task_applied_finalize_recovery",
		Status:     model.TaskStatusInProgress,
		Quota:      40,
		SubmitTime: time.Now().Add(-2 * time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			PendingTerminalStatus:     model.TaskStatusSuccess,
			PendingTerminalProgress:   "100%",
			PendingTerminalFinishTime: time.Now().Unix(),
			PendingTerminalResultURL:  "https://cdn.example.com/recovered.mp4",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey:        model.BillingTaskFinalizeOperationKey(task.ID),
		TaskID:              task.ID,
		TaskQuota:           40,
		TaskQuotaTarget:     40,
		Status:              model.BillingSettlementStatusApplied,
		AppliedFundingDelta: 0,
		CreatedAt:           now,
		UpdatedAt:           now,
		Revision:            1,
	}).Error)
	candidates, err := model.GetAppliedTaskFinalizeRecoveryCandidates(time.Now().Unix(), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	sweepTimedOutTasks(context.Background())

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Equal(t, "https://cdn.example.com/recovered.mp4", reloaded.PrivateData.ResultURL)
	assert.Equal(t, 40, reloaded.Quota)
	var settlementCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("task_id = ?", task.ID).Count(&settlementCount).Error)
	assert.EqualValues(t, 1, settlementCount)
}

func TestAppliedFinalizeRecoveryCursorSkipsBadPageAndRevisitsRepairs(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	resetTimedOutTaskSweepCursorForTest(t)
	oldTimeout, oldBudget := constant.TaskTimeoutMinutes, timedOutTaskScanBudget
	constant.TaskTimeoutMinutes, timedOutTaskScanBudget = 1, 100
	t.Cleanup(func() { constant.TaskTimeoutMinutes, timedOutTaskScanBudget = oldTimeout, oldBudget })
	seedUser(t, 717, 70)
	seedToken(t, 718, 717, "synthetic-recovery-token", 70)
	const malformed = `{"upstream_task_id":42}`
	now := time.Now().Unix()
	private := model.TaskPrivateData{TokenId: 718, BillingSource: model.BillingSettlementSourceWallet, PendingTerminalStatus: model.TaskStatusSuccess, PendingTerminalProgress: "100%", PendingTerminalFinishTime: now, PendingTerminalResultURL: "https://example.invalid/recovered"}
	var firstBadID int64
	var healthy *model.Task
	for i := 0; i < 101; i++ {
		row := &model.Task{TaskID: fmt.Sprintf("recovery-cursor-%d", i), UserId: 717, Quota: 40, SubmitTime: now - 120, Status: model.TaskStatusInProgress, PrivateData: private}
		require.NoError(t, model.DB.Create(row).Error)
		if i < 100 {
			require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", row.ID).UpdateColumn("private_data", malformed).Error)
			if i == 0 {
				firstBadID = row.ID
			}
		} else {
			healthy = row
		}
		require.NoError(t, model.DB.Create(&model.BillingSettlement{
			OperationKey: model.BillingTaskFinalizeOperationKey(row.ID), TaskID: row.ID, UserID: 717, TokenID: 718,
			Source: model.BillingSettlementSourceWallet, TaskQuota: 30, TaskQuotaTarget: 40,
			FundingDelta: 10, TokenDelta: 10, AppliedFundingDelta: 10, AppliedTokenDelta: 10,
			Status: model.BillingSettlementStatusApplied, EffectStatus: model.BillingSettlementEffectApplied, CreatedAt: now, UpdatedAt: now, Revision: 1,
		}).Error)
	}
	var before []model.BillingSettlement
	require.NoError(t, model.DB.Order("id").Find(&before).Error)
	sweepTimedOutTasks(t.Context())
	var tail model.Task
	require.NoError(t, model.DB.First(&tail, healthy.ID).Error)
	require.EqualValues(t, model.TaskStatusInProgress, tail.Status, "only the first100 candidates may be scanned in this cycle")
	sweepTimedOutTasks(t.Context())
	require.NoError(t, model.DB.First(&tail, healthy.ID).Error)
	require.EqualValues(t, model.TaskStatusSuccess, tail.Status, "the next cycle must advance beyond the unreadable first page")
	require.Equal(t, 40, tail.Quota)
	repaired, err := common.Marshal(private)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", firstBadID).UpdateColumn("private_data", string(repaired)).Error)
	sweepTimedOutTasks(t.Context())
	var repairedRow model.Task
	require.NoError(t, model.DB.First(&repairedRow, firstBadID).Error)
	require.EqualValues(t, model.TaskStatusSuccess, repairedRow.Status, "wrapping the cursor must revisit a repaired old row")
	var remaining int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("status = ?", model.TaskStatusInProgress).Count(&remaining).Error)
	require.EqualValues(t, 99, remaining)
	var after []model.BillingSettlement
	require.NoError(t, model.DB.Order("id").Find(&after).Error)
	require.Equal(t, before, after, "terminal recovery never reapplies or rewrites funding")
	require.EqualValues(t, 70, getUserQuota(t, 717))
	require.Equal(t, 70, getTokenRemainQuota(t, 718))
	require.Zero(t, countLogs(t))
}

func TestSweepRecoveryBudgetReservesOrdinaryTimeouts(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	resetTimedOutTaskSweepCursorForTest(t)
	previous := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })
	seedUser(t, 717, 1000)
	seedToken(t, 718, 717, "synthetic-share-token", 1000)
	now := time.Now().Unix()
	private := model.TaskPrivateData{TokenId: 718, BillingSource: model.BillingSettlementSourceWallet, PendingTerminalStatus: model.TaskStatusSuccess, PendingTerminalProgress: "100%", PendingTerminalFinishTime: now}
	for i := 0; i < 110; i++ {
		row := &model.Task{TaskID: fmt.Sprintf("applied-share-%d", i), UserId: 717, Quota: 1, SubmitTime: now - 120, Status: model.TaskStatusInProgress, PrivateData: private}
		require.NoError(t, model.DB.Create(row).Error)
		if i < 10 {
			require.NoError(t, model.DB.Model(row).UpdateColumn("private_data", `{"upstream_task_id":42}`).Error)
		}
		require.NoError(t, model.DB.Create(&model.BillingSettlement{OperationKey: model.BillingTaskFinalizeOperationKey(row.ID), TaskID: row.ID, UserID: 717, TokenID: 718, Source: model.BillingSettlementSourceWallet, TaskQuota: 1, TaskQuotaTarget: 1, Status: model.BillingSettlementStatusApplied, EffectStatus: model.BillingSettlementEffectApplied, CreatedAt: now, UpdatedAt: now, Revision: 1}).Error)
	}
	for i := 0; i < 50; i++ {
		row := &model.Task{TaskID: fmt.Sprintf("ordinary-share-%d", i), UserId: 717, Quota: 1, SubmitTime: now - 120, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{TokenId: 718, BillingSource: model.BillingSettlementSourceWallet, UpstreamTaskID: fmt.Sprintf("provider-%d", i)}}
		require.NoError(t, model.DB.Create(row).Error)
	}
	var before []model.BillingSettlement
	require.NoError(t, model.DB.Order("id").Find(&before).Error)
	sweepTimedOutTasks(t.Context())
	var recovered, timedOut int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id LIKE ? AND status = ?", "applied-share-%", model.TaskStatusSuccess).Count(&recovered).Error)
	require.EqualValues(t, 50, recovered)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id LIKE ? AND status = ?", "ordinary-share-%", model.TaskStatusFailure).Count(&timedOut).Error)
	require.EqualValues(t, 50, timedOut)
	require.EqualValues(t, 1050, getUserQuota(t, 717))
	require.Equal(t, 1050, getTokenRemainQuota(t, 718))
	require.Equal(t, 950, getTokenUsedQuota(t, 718))
	require.EqualValues(t, 50, countLogs(t))
	sweepTimedOutTasks(t.Context())
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id LIKE ? AND status = ?", "applied-share-%", model.TaskStatusSuccess).Count(&recovered).Error)
	require.EqualValues(t, 100, recovered)
	var after []model.BillingSettlement
	require.NoError(t, model.DB.Where("id <= ?", before[len(before)-1].ID).Order("id").Find(&after).Error)
	require.Equal(t, before, after, "applied recovery must not replay or rewrite funding")
	require.EqualValues(t, 1050, getUserQuota(t, 717))
	require.Equal(t, 1050, getTokenRemainQuota(t, 718))
	require.EqualValues(t, 50, countLogs(t))
}

func TestAppliedRecoveryPassWrapsDespiteContinuousArrivals(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	resetTimedOutTaskSweepCursorForTest(t)
	previous := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })
	seedUser(t, 717, 70)
	seedToken(t, 718, 717, "synthetic-bounded-pass", 70)
	now := time.Now().Unix()
	private := model.TaskPrivateData{TokenId: 718, BillingSource: model.BillingSettlementSourceWallet, PendingTerminalStatus: model.TaskStatusSuccess, PendingTerminalProgress: "100%", PendingTerminalFinishTime: now}
	add := func(name string) *model.Task {
		row := &model.Task{TaskID: name, UserId: 717, Quota: 1, SubmitTime: now - 120, Status: model.TaskStatusInProgress, PrivateData: private}
		require.NoError(t, model.DB.Create(row).Error)
		require.NoError(t, model.DB.Create(&model.BillingSettlement{OperationKey: model.BillingTaskFinalizeOperationKey(row.ID), TaskID: row.ID, UserID: 717, TokenID: 718, Source: model.BillingSettlementSourceWallet, TaskQuota: 1, TaskQuotaTarget: 1, Status: model.BillingSettlementStatusApplied, EffectStatus: model.BillingSettlementEffectApplied, Revision: 1}).Error)
		return row
	}
	bad := add("old-repaired")
	require.NoError(t, model.DB.Model(bad).UpdateColumn("private_data", `{"upstream_task_id":42}`).Error)
	for i := 0; i < 50; i++ {
		add(fmt.Sprintf("initial-%d", i))
	}
	sweepTimedOutTasks(t.Context())
	raw, err := common.Marshal(private)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(bad).UpdateColumn("private_data", string(raw)).Error)
	for i := 0; i < 50; i++ {
		add(fmt.Sprintf("newer-%d", i))
	}
	var before []model.BillingSettlement
	require.NoError(t, model.DB.Order("id").Find(&before).Error)
	sweepTimedOutTasks(t.Context())
	var fresh model.Task
	require.NoError(t, model.DB.First(&fresh, bad.ID).Error)
	require.EqualValues(t, model.TaskStatusSuccess, fresh.Status, "fixed pass bound must revisit the repaired old row despite newer arrivals")
	var newerRecovered int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id LIKE ? AND status = ?", "newer-%", model.TaskStatusSuccess).Count(&newerRecovered).Error)
	require.EqualValues(t, 49, newerRecovered, "the repaired row shares the same 50-transition recovery allowance")
	var after []model.BillingSettlement
	require.NoError(t, model.DB.Order("id").Find(&after).Error)
	require.Equal(t, before, after)
	require.EqualValues(t, 70, getUserQuota(t, 717))
	require.Equal(t, 70, getTokenRemainQuota(t, 718))
	require.Zero(t, countLogs(t))
}

func TestLateAppliedRecoveryCannotSpendOrdinaryAllowance(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	resetTimedOutTaskSweepCursorForTest(t)
	previous := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })
	seedUser(t, 717, 1000)
	seedToken(t, 718, 717, "synthetic-late-share", 1000)
	now := time.Now().Unix()
	private := model.TaskPrivateData{TokenId: 718, BillingSource: model.BillingSettlementSourceWallet, PendingTerminalStatus: model.TaskStatusSuccess, PendingTerminalProgress: "100%", PendingTerminalFinishTime: now}
	addApplied := func(row *model.Task) {
		require.NoError(t, model.DB.Create(&model.BillingSettlement{OperationKey: model.BillingTaskFinalizeOperationKey(row.ID), TaskID: row.ID, UserID: 717, TokenID: 718, Source: model.BillingSettlementSourceWallet, TaskQuota: 1, TaskQuotaTarget: 1, Status: model.BillingSettlementStatusApplied, EffectStatus: model.BillingSettlementEffectApplied, CreatedAt: now, UpdatedAt: now, Revision: 1}).Error)
	}
	for i := 0; i < 50; i++ {
		row := &model.Task{TaskID: fmt.Sprintf("early-recovery-%d", i), UserId: 717, Quota: 1, SubmitTime: now - 120, Status: model.TaskStatusInProgress, PrivateData: private}
		require.NoError(t, model.DB.Create(row).Error)
		addApplied(row)
	}
	late := &model.Task{TaskID: "late-applied", UserId: 717, Quota: 1, SubmitTime: now - 120, Status: model.TaskStatusInProgress, PrivateData: private}
	require.NoError(t, model.DB.Create(late).Error)
	for i := 0; i < 50; i++ {
		require.NoError(t, model.DB.Create(&model.Task{TaskID: fmt.Sprintf("late-ordinary-%d", i), UserId: 717, Quota: 1, SubmitTime: now - 120, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{TokenId: 718, BillingSource: model.BillingSettlementSourceWallet, UpstreamTaskID: "provider"}}).Error)
	}
	injected := false
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("apply-after-ordinary-page", func(tx *gorm.DB) {
		if !injected && tx.Statement.Table == "tasks" && strings.Contains(tx.Statement.SQL.String(), "NOT EXISTS") {
			injected = true
			addApplied(late)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("apply-after-ordinary-page") })
	sweepTimedOutTasks(t.Context())
	require.True(t, injected)
	var fresh model.Task
	require.NoError(t, model.DB.First(&fresh, late.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), fresh.Status)
	var ordinary int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id LIKE ? AND status = ?", "late-ordinary-%", model.TaskStatusFailure).Count(&ordinary).Error)
	require.EqualValues(t, 50, ordinary)
	require.EqualValues(t, 1050, getUserQuota(t, 717))
	require.Equal(t, 1050, getTokenRemainQuota(t, 718))
	require.EqualValues(t, 50, countLogs(t))
	sweepTimedOutTasks(t.Context())
	require.NoError(t, model.DB.First(&fresh, late.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), fresh.Status)
	require.EqualValues(t, 1050, getUserQuota(t, 717))
	require.EqualValues(t, 50, countLogs(t))
}

func resetTimedOutTaskSweepCursorForTest(t *testing.T) {
	t.Helper()
	reset := func() {
		timedOutTaskSweepCursor.Lock()
		timedOutTaskSweepCursor.afterSubmitTime = 0
		timedOutTaskSweepCursor.afterID = 0
		timedOutTaskSweepCursor.appliedAfterSubmitTime = 0
		timedOutTaskSweepCursor.appliedAfterID = 0
		timedOutTaskSweepCursor.appliedThroughTime = 0
		timedOutTaskSweepCursor.appliedThroughID = 0
		timedOutTaskSweepCursor.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM task_submit_failure_recoveries")
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
		OperationKey: model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId),
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
		Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId)).
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
		OperationKey: model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId),
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
		OperationKey: model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId),
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
		Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId)).
		Update("status", model.BillingSettlementStatusApplied).Error)
	processSunoTaskResponse(context.Background(), &reloaded, response)
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)
}

func TestProcessSunoStoredFailureWaitsForSubmissionSettlement(t *testing.T) {
	for index, settlementStatus := range []string{
		model.BillingSettlementStatusPending,
		model.BillingSettlementStatusManual,
	} {
		t.Run(settlementStatus, func(t *testing.T) {
			truncate(t)
			userID := 720 + index
			now := time.Now().Unix()
			seedUser(t, userID, 900)

			task := makeTask(userID, 0, 100, 0, BillingSourceWallet, 0)
			task.Platform = constant.TaskPlatformSuno
			task.Status = model.TaskStatusFailure
			task.Progress = "50%"
			task.FailReason = "stored provider failure"
			task.PrivateData.BillingRequestId = "suno-stored-failure-" + settlementStatus
			persistTask(t, task)
			require.NoError(t, model.DB.Create(&model.BillingSettlement{
				OperationKey: model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId),
				Source:       model.BillingSettlementSourceWallet, UserID: userID,
				Status: settlementStatus, CreatedAt: now, UpdatedAt: now, Revision: 1,
			}).Error)

			response := dto.SunoDataResponse{TaskID: task.TaskID, Data: task.Data}
			processSunoTaskResponse(context.Background(), task, response)

			var blocked model.Task
			require.NoError(t, model.DB.First(&blocked, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusFailure, blocked.Status)
			assert.Equal(t, "50%", blocked.Progress)
			assert.Equal(t, 100, blocked.Quota)
			assert.EqualValues(t, 900, getUserQuota(t, userID))
			var refundCount int64
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", fmt.Sprintf("task:%d:refund", task.ID)).
				Count(&refundCount).Error)
			assert.Zero(t, refundCount)

			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId)).
				Update("status", model.BillingSettlementStatusApplied).Error)
			processSunoTaskResponse(context.Background(), &blocked, response)

			require.NoError(t, model.DB.First(&blocked, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusFailure, blocked.Status)
			assert.Equal(t, "100%", blocked.Progress)
			assert.Zero(t, blocked.Quota)
			assert.EqualValues(t, 1000, getUserQuota(t, userID))
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", fmt.Sprintf("task:%d:refund", task.ID)).
				Count(&refundCount).Error)
			assert.EqualValues(t, 1, refundCount)
		})
	}
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
		OperationKey: model.BillingRequestFinalizeOperationKey(billingTask.PrivateData.BillingRequestId),
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
		Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(billingTask.PrivateData.BillingRequestId)).
		Update("status", model.BillingSettlementStatusApplied).Error)
	require.NoError(t, UpdateMidjourneyTaskFromResponse(context.Background(), &reloadedLegacy, response))
	require.NoError(t, model.DB.First(&reloadedLegacy, legacy.Id).Error)
	assert.Equal(t, "SUCCESS", reloadedLegacy.Status)
	require.NoError(t, model.DB.First(&reloadedBilling, billingTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloadedBilling.Status)
}

func TestMidjourneyMergedFailureWaitsForSubmissionSettlement(t *testing.T) {
	for index, settlementStatus := range []string{
		model.BillingSettlementStatusPending,
		model.BillingSettlementStatusManual,
	} {
		t.Run(settlementStatus, func(t *testing.T) {
			truncate(t)
			userID := 706 + index
			channelID := 708 + index
			now := time.Now().Unix()
			seedUser(t, userID, 900)

			legacy := &model.Midjourney{
				UserId: userID, ChannelId: channelID,
				MjId:   "provider-mj-merged-failure-" + settlementStatus,
				Status: "IN_PROGRESS", Progress: "50%", FailReason: "stored provider failure",
				Code: 1, Quota: 100,
			}
			require.NoError(t, model.DB.Create(legacy).Error)

			billingTask := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
			billingTask.Platform = constant.TaskPlatformMidjourney
			billingTask.Status = model.TaskStatusInProgress
			billingTask.Progress = "50%"
			billingTask.PrivateData.BillingRequestId = "mj-merged-failure-" + settlementStatus
			persistTask(t, billingTask)
			require.NoError(t, model.DB.Create(&model.MidjourneyBillingClaim{
				ChannelID: channelID, MjID: legacy.MjId, UserID: userID,
				BillingTaskID: billingTask.ID, BillingRequestID: billingTask.PrivateData.BillingRequestId,
				CreatedAt: now,
			}).Error)
			require.NoError(t, model.DB.Create(&model.BillingSettlement{
				OperationKey: model.BillingRequestFinalizeOperationKey(billingTask.PrivateData.BillingRequestId),
				Source:       model.BillingSettlementSourceWallet, UserID: userID,
				Status: settlementStatus, CreatedAt: now, UpdatedAt: now, Revision: 1,
			}).Error)

			response := dto.MidjourneyDto{
				MjId: legacy.MjId, Status: "IN_PROGRESS", Progress: "50%",
			}
			require.NoError(t, UpdateMidjourneyTaskFromResponse(context.Background(), legacy, response))

			var blockedLegacy model.Midjourney
			require.NoError(t, model.DB.First(&blockedLegacy, legacy.Id).Error)
			assert.Equal(t, "IN_PROGRESS", blockedLegacy.Status)
			assert.Equal(t, "50%", blockedLegacy.Progress)
			assert.Equal(t, "stored provider failure", blockedLegacy.FailReason)
			var blockedBilling model.Task
			require.NoError(t, model.DB.First(&blockedBilling, billingTask.ID).Error)
			assert.EqualValues(t, model.TaskStatusInProgress, blockedBilling.Status)
			assert.Equal(t, 100, blockedBilling.Quota)
			assert.EqualValues(t, 900, getUserQuota(t, userID))
			var refundCount int64
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", fmt.Sprintf("task:%d:refund", billingTask.ID)).
				Count(&refundCount).Error)
			assert.Zero(t, refundCount)

			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(billingTask.PrivateData.BillingRequestId)).
				Update("status", model.BillingSettlementStatusApplied).Error)
			require.NoError(t, UpdateMidjourneyTaskFromResponse(context.Background(), &blockedLegacy, response))

			require.NoError(t, model.DB.First(&blockedLegacy, legacy.Id).Error)
			assert.Equal(t, "FAILURE", blockedLegacy.Status)
			assert.Equal(t, "100%", blockedLegacy.Progress)
			require.NoError(t, model.DB.First(&blockedBilling, billingTask.ID).Error)
			assert.EqualValues(t, model.TaskStatusFailure, blockedBilling.Status)
			assert.Zero(t, blockedBilling.Quota)
			assert.EqualValues(t, 1000, getUserQuota(t, userID))
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", fmt.Sprintf("task:%d:refund", billingTask.ID)).
				Count(&refundCount).Error)
			assert.EqualValues(t, 1, refundCount)

			require.NoError(t, UpdateMidjourneyTaskFromResponse(context.Background(), &blockedLegacy, response))
			assert.EqualValues(t, 1000, getUserQuota(t, userID))
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
				Where("operation_key = ?", fmt.Sprintf("task:%d:refund", billingTask.ID)).
				Count(&refundCount).Error)
			assert.EqualValues(t, 1, refundCount)
		})
	}
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

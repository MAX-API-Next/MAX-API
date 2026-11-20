// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskSubmitFailureCleanupCoordinatesRealFunding(t *testing.T) {
	for _, funding := range []string{"wallet_only", "subscription_only"} {
		for _, outcome := range []string{"manual-first", "failure-first", "read-error", "pending-restart", "intent-write-error", "concurrent-cleanup", "identity-mismatch", "read-error-recovery", "intent-write-error-recovery", "task-write-error-recovery", "timeout-before-rejection", "timeout-task-write-error-recovery", "timeout-h3-retry-reserve", "timeout-free-to-paid", "timeout-free-to-paid-null", "free-to-paid-null", "free-to-paid-task-write-error-recovery", "different-terminal-failure"} {
			t.Run(funding+"/"+outcome, func(t *testing.T) {
				const child = "MAX_API_TEST_CONCURRENT_SUBMIT_CLEANUP_CHILD"
				if outcome == "concurrent-cleanup" && os.Getenv(child) != "1" {
					exe, err := os.Executable()
					require.NoError(t, err)
					ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, exe, "-test.run=^TestTaskSubmitFailureCleanupCoordinatesRealFunding$/^"+funding+"$/^concurrent-cleanup$", "-test.timeout=20s")
					cmd.Env = append(os.Environ(), child+"=1")
					output, err := cmd.CombinedOutput()
					require.NoError(t, err, "%s", output)
					return
				}
				db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cleanup.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
				require.NoError(t, err)
				oldDB, oldLog := model.DB, model.LOG_DB
				oldSQLite, oldRedis, oldBatch, oldConsume := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
				model.DB, model.LOG_DB = db, db
				common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
				t.Cleanup(func() {
					model.DB, model.LOG_DB = oldDB, oldLog
					common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsume
					pool, _ := db.DB()
					if pool != nil {
						_ = pool.Close()
					}
				})
				require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSubmitFailureRecovery{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
				require.NoError(t, db.Create(&model.User{Id: 51001, Username: "cleanup-fixture", Quota: 1000}).Error)
				require.NoError(t, db.Create(&model.Token{Id: 51002, UserId: 51001, Key: "synthetic-cleanup-key", RemainQuota: 1000}).Error)
				if funding == "subscription_only" {
					require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 51003, Title: "cleanup", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					require.NoError(t, db.Create(&model.UserSubscription{Id: 51004, UserId: 51001, PlanId: 51003, AmountTotal: 1000, Status: "active", StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{RequestId: "cleanup-" + funding + "-" + outcome, UserId: 51001, TokenId: 51002, TokenKey: "synthetic-cleanup-key", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: funding}, PriceData: types.PriceData{ModelRatio: 1}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
				freeRetry := strings.Contains(outcome, "free-to-paid")
				initialQuota := 100
				if freeRetry {
					initialQuota = 0
				} else {
					require.Nil(t, PreConsumeBilling(c, initialQuota, info))
				}
				originalBilling := info.Billing
				row := &model.Task{TaskID: "cleanup-placeholder", UserId: 51001, Quota: initialQuota, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: info.RequestId, BillingSource: info.BillingSource, TokenId: info.TokenId, SubscriptionId: info.SubscriptionId}}
				if outcome == "timeout-h3-retry-reserve" {
					previousUnit := common.QuotaPerUnit
					common.QuotaPerUnit = 1000
					t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
					plan, err := task_billing_setting.BuildH3BillingPlan(task_billing_setting.H3BillingInput{
						Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
					}, 0.05)
					require.NoError(t, err)
					require.Positive(t, plan.ReserveQuota)
					require.LessOrEqual(t, plan.ReserveQuota, row.Quota)
					row.PrivateData.BillingContext = &model.TaskBillingContext{TaskBillingPlan: plan}
				}
				require.NoError(t, db.Create(row).Error)
				if strings.HasSuffix(outcome, "-null") {
					require.NoError(t, db.Model(row).UpdateColumn("quota", nil).Error)
				}
				info.PersistedTaskID = row.ID
				original, err := model.GetTaskPlaceholderByID(row.ID)
				require.NoError(t, err)
				originalPlaceholder := *original
				if strings.HasPrefix(outcome, "timeout-") {
					previousTimeout := constant.TaskTimeoutMinutes
					constant.TaskTimeoutMinutes = 1
					t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })
					resetTimedOutTaskSweepCursorForTest(t)
					require.NoError(t, db.Model(row).UpdateColumn("submit_time", time.Now().Add(-2*time.Minute).Unix()).Error)
					if outcome == "timeout-h3-retry-reserve" {
						// The next attempt reserves before it claims the placeholder.
						require.NoError(t, info.Billing.Reserve(150))
						require.EqualValues(t, 850, getTokenRemainQuota(t, info.TokenId))
					}
					sweepTimedOutTasks(t.Context())
					require.NoError(t, db.First(&row, row.ID).Error)
					if outcome == "timeout-h3-retry-reserve" {
						taskFinalize, found, err := model.GetBillingSettlementStatus(model.BillingTaskFinalizeOperationKey(row.ID))
						require.NoError(t, err)
						assert.False(t, found, "an unconfirmed submission has no terminal usage to reconcile: %s", taskFinalize)
						require.EqualValues(t, 850, getTokenRemainQuota(t, info.TokenId), "unknown outcome keeps all reserved funding")
					}
					require.EqualValues(t, model.TaskStatusFailure, row.Status)
					require.Equal(t, initialQuota, row.Quota, "the real timeout deliberately retains the original reservation")
				}
				if freeRetry {
					// The paid reservation commits before the retry projects
					// its new source and amount on the free placeholder.
					require.Nil(t, PreConsumeBilling(c, 100, info))
					originalBilling = info.Billing
					require.EqualValues(t, 900, getTokenRemainQuota(t, info.TokenId))
					intent, err := info.Billing.(RefundSettlementPreparer).PrepareRefundSettlement()
					require.NoError(t, err)
					require.NotNil(t, intent)
					mismatched := *intent
					mismatched.FundingDelta--
					won, err := model.ClaimTaskSubmitFailureWithSettlement(row.ID, "wrong amount", &mismatched)
					require.ErrorIs(t, err, model.ErrTaskSubmitRefundIdentityMismatch)
					require.False(t, won)
				}
				if outcome == "different-terminal-failure" {
					require.NoError(t, db.Model(row).UpdateColumn("status", model.TaskStatusFailure).Error)
				}
				if strings.Contains(outcome, "task-write-error") {
					require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail-task-writes", func(tx *gorm.DB) {
						if tx.Statement.Table == "tasks" {
							tx.AddError(errors.New("synthetic task UPDATE unavailable"))
						}
					}))
				}
				if freeRetry && (strings.HasPrefix(outcome, "timeout-") || strings.Contains(outcome, "task-write-error")) {
					retry := originalPlaceholder
					retry.Quota = 100
					if strings.HasPrefix(outcome, "timeout-") {
						require.ErrorIs(t, retry.RewritePlaceholder(originalPlaceholder), model.ErrTaskPlaceholderConflict)
					} else {
						require.ErrorContains(t, retry.RewritePlaceholder(originalPlaceholder), "synthetic task UPDATE unavailable")
					}
				}
				input := model.BillingSettlementInput{OperationKey: model.BillingTaskFinalizeOperationKey(row.ID), Source: info.BillingSource, UserID: info.UserId, TokenID: info.TokenId, TokenKey: info.TokenKey, SubscriptionID: info.SubscriptionId, TaskID: row.ID, TaskQuota: 100, TaskQuotaTarget: 100}
				var checkedColumns atomic.Int32
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("verify-task-cleanup-columns", func(tx *gorm.DB) {
					if tx.Statement.Table != "tasks" {
						return
					}
					if values, ok := tx.Statement.Dest.(map[string]any); ok {
						if tx.Statement.Schema == nil {
							tx.AddError(errors.New("missing task schema"))
							return
						}
						for column := range values {
							field := tx.Statement.Schema.LookUpField(column)
							if field == nil || field.DBName != column {
								tx.AddError(errors.New("task cleanup emitted a non-schema column"))
								return
							}
						}
						checkedColumns.Add(1)
					}
				}))
				if funding == "subscription_only" {
					input.SubscriptionPreConsumeRequestID = info.RequestId
				}
				if outcome == "manual-first" {
					won, err := row.UpdateWithStatusAndManualSettlement(model.TaskStatusSubmitted, row.UpdatedAt, input, "requires reconciliation")
					require.NoError(t, err)
					require.True(t, won)
				}
				if strings.HasPrefix(outcome, "read-error") {
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail-cleanup-read", func(tx *gorm.DB) {
						if tx.Statement.Table == "tasks" {
							tx.AddError(errors.New("synthetic ownership read failure"))
						}
					}))
				}
				if strings.HasPrefix(outcome, "intent-write-error") {
					require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail-refund-intent", func(tx *gorm.DB) {
						if record, ok := tx.Statement.Dest.(*model.BillingSettlement); ok && record.OperationKey == model.BillingRequestFinalizeOperationKey(info.RequestId) {
							tx.AddError(errors.New("synthetic refund intent write failure"))
						}
					}))
				}
				if outcome == "pending-restart" {
					preparer := info.Billing.(interface {
						PrepareRefundSettlement() (*model.BillingSettlementInput, error)
					})
					intent, err := preparer.PrepareRefundSettlement()
					require.NoError(t, err)
					require.NotNil(t, intent)
					won, err := model.ClaimTaskSubmitFailureWithSettlement(row.ID, "before process interruption", intent)
					require.NoError(t, err)
					require.True(t, won)
					var pending model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", intent.OperationKey).First(&pending).Error)
					require.Equal(t, model.BillingSettlementStatusPending, pending.Status)
					require.EqualValues(t, -100, pending.FundingDelta)
					require.Zero(t, pending.AppliedFundingDelta)
					model.ProcessPendingBillingSettlementsOnce()
					model.ProcessPendingBillingSettlementsOnce()
					var recovered model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", intent.OperationKey).First(&recovered).Error)
					require.Equal(t, model.BillingSettlementStatusApplied, recovered.Status)
					require.EqualValues(t, -100, recovered.AppliedFundingDelta)
					require.EqualValues(t, -100, recovered.AppliedTokenDelta)
					var recoveredUser model.User
					var recoveredToken model.Token
					require.NoError(t, db.First(&recoveredUser, info.UserId).Error)
					require.NoError(t, db.First(&recoveredToken, info.TokenId).Error)
					require.EqualValues(t, 1000, recoveredUser.Quota)
					require.EqualValues(t, 1000, recoveredToken.RemainQuota)
					require.Zero(t, recoveredToken.UsedQuota)
					if funding == "subscription_only" {
						var recoveredSubscription model.UserSubscription
						require.NoError(t, db.First(&recoveredSubscription, 51004).Error)
						require.Zero(t, recoveredSubscription.AmountUsed)
					}
					info.Billing.Refund(c) // A delayed original session replays the same durable operation.
					if funding == "subscription_only" {
						require.Zero(t, getSubscriptionUsed(t, 51004))
					}
				}
				if outcome == "identity-mismatch" {
					require.NoError(t, db.Model(row).UpdateColumn("private_data", model.TaskPrivateData{AwaitingUpstreamID: true, BillingSource: info.BillingSource, BillingRequestId: "different-generation", TokenId: info.TokenId, SubscriptionId: info.SubscriptionId}).Error)
				}
				if outcome == "concurrent-cleanup" {
					start, done := make(chan struct{}), make(chan struct{}, 2)
					for i := 0; i < 2; i++ {
						go func() { <-start; CleanupDefiniteTaskSubmitFailure(c, info, "concurrent rejection"); done <- struct{}{} }()
					}
					close(start)
					deadline := time.NewTimer(5 * time.Second)
					defer deadline.Stop()
					for i := 0; i < 2; i++ {
						select {
						case <-done:
						case <-deadline.C:
							t.Fatal("concurrent cleanup did not finish")
						}
					}
					var concurrent model.Task
					require.NoError(t, db.First(&concurrent, row.ID).Error)
					require.EqualValues(t, model.TaskStatusFailure, concurrent.Status)
					require.Zero(t, concurrent.Quota)
					var refunded model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).First(&refunded).Error)
					require.Equal(t, model.BillingSettlementStatusApplied, refunded.Status)
					require.EqualValues(t, -100, refunded.AppliedFundingDelta)
					require.EqualValues(t, -100, refunded.AppliedTokenDelta)
					require.EqualValues(t, 1000, getUserQuota(t, info.UserId))
					require.EqualValues(t, 1000, getTokenRemainQuota(t, info.TokenId))
					if funding == "subscription_only" {
						require.Zero(t, getSubscriptionUsed(t, info.SubscriptionId))
					}
				}
				CleanupDefiniteTaskSubmitFailure(c, info, "rewrite rejected")
				CleanupDefiniteTaskSubmitFailure(c, info, "duplicate cleanup")
				if strings.HasPrefix(outcome, "read-error") {
					require.NoError(t, db.Callback().Query().Remove("fail-cleanup-read"))
				}
				if strings.HasPrefix(outcome, "intent-write-error") {
					require.NoError(t, db.Callback().Create().Remove("fail-refund-intent"))
				}
				if strings.Contains(outcome, "task-write-error") {
					require.NoError(t, db.Callback().Update().Remove("fail-task-writes"))
					var work model.TaskSubmitFailureRecovery
					require.NoError(t, db.First(&work, "task_id = ?", row.ID).Error)
					require.Equal(t, model.TaskSubmitFailureRecoveryPending, work.Status)
				}
				recovered := strings.HasSuffix(outcome, "-recovery")
				if recovered {
					info.Billing = nil // Recovery must work without the original session.
					model.ProcessPendingBillingSettlementsOnce()
					model.ProcessPendingBillingSettlementsOnce()
				}
				var task model.Task
				require.NoError(t, db.First(&task, row.ID).Error)
				var user model.User
				require.NoError(t, db.First(&user, 51001).Error)
				var token model.Token
				require.NoError(t, db.First(&token, 51002).Error)
				var refunds int64
				require.NoError(t, db.Model(&model.BillingSettlement{}).Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).Count(&refunds).Error)
				var used int64
				if funding == "subscription_only" {
					var sub model.UserSubscription
					require.NoError(t, db.First(&sub, 51004).Error)
					used = sub.AmountUsed
				}
				if outcome == "failure-first" || outcome == "pending-restart" || outcome == "concurrent-cleanup" || outcome == "timeout-before-rejection" || outcome == "timeout-h3-retry-reserve" || freeRetry || recovered {
					require.Positive(t, checkedColumns.Load(), "the actual cleanup must emit only real database columns")
					require.EqualValues(t, model.TaskStatusFailure, task.Status)
					require.Zero(t, task.Quota)
					require.EqualValues(t, 1, refunds)
					require.EqualValues(t, 1000, token.RemainQuota)
					require.Zero(t, token.UsedQuota)
					require.EqualValues(t, 1000, user.Quota)
					require.Zero(t, used)
					if outcome == "timeout-h3-retry-reserve" || freeRetry {
						var refund model.BillingSettlement
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).First(&refund).Error)
						require.Equal(t, model.BillingSettlementStatusApplied, refund.Status)
						expected := -100
						if outcome == "timeout-h3-retry-reserve" {
							expected = -150
						}
						require.EqualValues(t, expected, refund.AppliedFundingDelta)
						require.EqualValues(t, expected, refund.AppliedTokenDelta)
					}
					won, err := originalPlaceholder.UpdateWithStatusAndManualSettlement(model.TaskStatusSubmitted, originalPlaceholder.UpdatedAt, input, "late decision")
					require.NoError(t, err)
					require.False(t, won)
				} else {
					expectedStatus := model.TaskStatusSubmitted
					if outcome == "different-terminal-failure" {
						expectedStatus = model.TaskStatusFailure
					}
					require.EqualValues(t, expectedStatus, task.Status)
					require.Equal(t, 100, task.Quota)
					require.Zero(t, refunds)
					require.EqualValues(t, 900, token.RemainQuota)
					require.EqualValues(t, 100, token.UsedQuota)
					if funding == "wallet_only" {
						require.EqualValues(t, 900, user.Quota)
					} else {
						require.EqualValues(t, 1000, user.Quota)
						require.EqualValues(t, 100, used)
					}
				}
				var manual model.BillingSettlement
				err = db.Where("operation_key = ?", input.OperationKey).First(&manual).Error
				if outcome == "manual-first" {
					require.NoError(t, err)
					require.Equal(t, model.BillingSettlementStatusManual, manual.Status)
					require.Zero(t, manual.AppliedFundingDelta)
				} else {
					require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				}
				if recovered {
					originalBilling.Refund(c)
					if funding == "subscription_only" {
						require.Zero(t, getSubscriptionUsed(t, 51004))
					}
					require.NoError(t, db.First(&user, info.UserId).Error)
					require.NoError(t, db.First(&token, info.TokenId).Error)
					require.EqualValues(t, 1000, user.Quota)
					require.EqualValues(t, 1000, token.RemainQuota)
					require.Zero(t, token.UsedQuota)
					require.NoError(t, db.Model(&model.BillingSettlement{}).Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).Count(&refunds).Error)
					require.EqualValues(t, 1, refunds)
				}
			})
		}
	}
}

func TestFreeTaskDefiniteRejectionRecoversStatusWithoutRefund(t *testing.T) {
	for _, owner := range []string{"unclaimed", "reservation", "upstream"} {
		t.Run(owner, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "free-rejection.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			previous, oldSQLite := model.DB, common.UsingSQLite
			model.DB, common.UsingSQLite = db, true
			t.Cleanup(func() {
				model.DB, common.UsingSQLite = previous, oldSQLite
				pool, _ := db.DB()
				_ = pool.Close()
			})
			require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSubmitFailureRecovery{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.SubscriptionPreConsumeRecord{}))
			request := "free-rejection-" + owner
			row := &model.Task{TaskID: request, UserId: 71, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{
				AwaitingUpstreamID: true, BillingRequestId: request, TokenId: 72,
			}}
			require.NoError(t, db.Create(row).Error)
			attempts := 0
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("reject-immediate-free-task-write", func(tx *gorm.DB) {
				if tx.Statement.Table == "tasks" {
					attempts++
					deadline, bounded := tx.Statement.Context.Deadline()
					require.True(t, bounded)
					require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
					tx.AddError(errors.New("synthetic task-write outage"))
				}
			}))
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: row.UserId, TokenId: 72, RequestId: request, TaskRelayInfo: &relaycommon.TaskRelayInfo{PersistedTaskID: row.ID}}
			CleanupDefiniteTaskSubmitFailure(ctx, info, "definite free rejection")
			require.Equal(t, 3, attempts)
			require.NoError(t, db.Callback().Update().Remove("reject-immediate-free-task-write"))
			var journal model.TaskSubmitFailureRecovery
			require.NoError(t, db.First(&journal, "task_id = ?", row.ID).Error)
			require.Equal(t, model.TaskSubmitFailureRecoveryPending, journal.Status)
			require.Zero(t, journal.FundingDelta)
			if owner == "reservation" {
				require.NoError(t, db.Create(&model.BillingPreConsumeSelection{RequestID: request, UserID: 71, TokenID: 72,
					Source: model.BillingSettlementSourceWallet, EffectiveQuota: 10}).Error)
			}
			if owner == "upstream" {
				confirmed := row.PrivateData
				confirmed.UpstreamTaskID, confirmed.AwaitingUpstreamID = "confirmed", false
				require.NoError(t, db.Model(row).UpdateColumn("private_data", confirmed).Error)
			}
			model.ProcessPendingBillingSettlementsOnce()
			model.ProcessPendingBillingSettlementsOnce()
			stored, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.NoError(t, db.First(&journal, "task_id = ?", row.ID).Error)
			if owner == "unclaimed" {
				require.EqualValues(t, model.TaskStatusFailure, stored.Status)
				require.Equal(t, "definite free rejection", stored.FailReason)
				require.Equal(t, model.TaskSubmitFailureStatusRecorded, journal.Status)
			} else {
				require.EqualValues(t, model.TaskStatusSubmitted, stored.Status)
				require.Equal(t, model.TaskSubmitFailureRecoveryManual, journal.Status)
			}
			var settlements int64
			require.NoError(t, db.Model(&model.BillingSettlement{}).Count(&settlements).Error)
			require.Zero(t, settlements)
		})
	}
}

func TestFreeTaskDefiniteRejectionAfterTimeout(t *testing.T) {
	for _, mode := range []string{"direct", "replayed", "funded"} {
		t.Run(mode, func(t *testing.T) {
			isolateTaskPluginBillingDatabase(t)
			request := "late-free-" + mode
			row := &model.Task{TaskID: request, Status: model.TaskStatusFailure, UserId: 71,
				SubmitTimeoutRetainedFunding: true, FinishTime: 99, FailReason: "unknown outcome",
				PrivateData: model.TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: request, TokenId: 72}}
			require.NoError(t, model.DB.Create(row).Error)
			require.NoError(t, model.DB.Model(row).UpdateColumn("quota", nil).Error)
			if mode == "funded" {
				require.NoError(t, model.DB.Create(&model.BillingPreConsumeSelection{
					RequestID: request, UserID: 71, TokenID: 72, Source: model.BillingSettlementSourceWallet, EffectiveQuota: 10,
				}).Error)
			}
			const reason = "definite late rejection"
			if mode == "direct" {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{RequestId: request, UserId: 71, TokenId: 72,
					TaskRelayInfo: &relaycommon.TaskRelayInfo{PersistedTaskID: row.ID}}
				CleanupDefiniteTaskSubmitFailure(ctx, info, reason)
			} else {
				status, err := model.RecordTaskSubmitFailureStatusRecovery(row.ID, request, reason, 71, 72)
				require.NoError(t, err)
				require.Equal(t, model.TaskSubmitFailureRecoveryPending, status)
				model.ProcessPendingBillingSettlementsOnce()
				model.ProcessPendingBillingSettlementsOnce()
				var journal model.TaskSubmitFailureRecovery
				require.NoError(t, model.DB.First(&journal, "task_id = ?", row.ID).Error)
				if mode == "funded" {
					require.Equal(t, model.TaskSubmitFailureRecoveryManual, journal.Status)
				} else {
					require.Equal(t, model.TaskSubmitFailureStatusRecorded, journal.Status)
				}
			}
			stored, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.EqualValues(t, model.TaskStatusFailure, stored.Status)
			require.EqualValues(t, 99, stored.FinishTime)
			require.Equal(t, mode == "funded", stored.SubmitTimeoutRetainedFunding)
			if mode == "funded" {
				require.Equal(t, "unknown outcome", stored.FailReason)
			} else {
				require.Equal(t, reason, stored.FailReason)
			}
			var settlements int64
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Count(&settlements).Error)
			require.Zero(t, settlements)
		})
	}
}

func TestZeroChargeWalletSessionRecoversDefiniteRejection(t *testing.T) {
	for _, mode := range []string{"replay", "competing reserve", "settlement in flight"} {
		t.Run(mode, func(t *testing.T) {
			isolateTaskPluginBillingDatabase(t)
			const userID, tokenID = 85, 86
			const tokenKey = "synthetic-zero-charge-wallet"
			seedUser(t, userID, 1000)
			seedToken(t, tokenID, userID, tokenKey, 1000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			request := "verified-wallet-zero"
			info := &relaycommon.RelayInfo{RequestId: request, UserId: userID, TokenId: tokenID, TokenKey: tokenKey,
				UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, PreConsumeBilling(ctx, 0, info))
			require.NotNil(t, info.Billing)
			require.Equal(t, BillingSourceWallet, info.BillingSource)
			row := &model.Task{TaskID: request, UserId: userID, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{
				BillingSource: info.BillingSource, AwaitingUpstreamID: true, BillingRequestId: request, TokenId: tokenID,
			}}
			require.NoError(t, model.DB.Create(row).Error)
			info.PersistedTaskID = row.ID
			const hook = "synthetic:zero-wallet-task-write"
			require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
				if tx.Statement.Table == "tasks" {
					_ = tx.AddError(errors.New("synthetic task write outage"))
				}
			}))
			if mode == "settlement in flight" {
				info.Billing.(*BillingSession).settleInFlight = true
			}
			CleanupDefiniteTaskSubmitFailure(ctx, info, "definite wallet rejection")
			require.NoError(t, model.DB.Callback().Update().Remove(hook))
			var journal model.TaskSubmitFailureRecovery
			err := model.DB.Where("task_id = ?", row.ID).Take(&journal).Error
			if mode == "settlement in flight" {
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				stored, err := model.GetTaskPlaceholderByID(row.ID)
				require.NoError(t, err)
				require.EqualValues(t, model.TaskStatusSubmitted, stored.Status)
				return
			}
			require.NoError(t, err)
			require.Equal(t, model.TaskSubmitFailureRecoveryPending, journal.Status)
			require.Zero(t, journal.FundingDelta)
			if mode == "competing reserve" {
				require.NoError(t, model.DB.Create(&model.BillingSettlement{
					OperationKey: "request:" + request + ":reserve:1",
					Status:       model.BillingSettlementStatusManual, UserID: userID, TokenID: tokenID,
					Source: model.BillingSettlementSourceWallet, FundingDelta: 20, TokenDelta: 20,
				}).Error)
			}
			model.ProcessPendingBillingSettlementsOnce()
			require.NoError(t, model.DB.Where("task_id = ?", row.ID).Take(&journal).Error)
			stored, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			if mode == "replay" {
				require.Equal(t, model.TaskSubmitFailureStatusRecorded, journal.Status)
				require.EqualValues(t, model.TaskStatusFailure, stored.Status)
			} else {
				require.Equal(t, model.TaskSubmitFailureRecoveryManual, journal.Status)
				require.EqualValues(t, model.TaskStatusSubmitted, stored.Status)
			}
			var refundOwners int64
			require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("operation_key = ?", journal.OperationKey).Count(&refundOwners).Error)
			require.Zero(t, refundOwners)
			require.EqualValues(t, 1000, getUserQuota(t, userID))
			require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
		})
	}
}

func TestLateDefiniteRejectionRefundsUnprojectedWalletReservesOnce(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "journal"}[replay], func(t *testing.T) {
			isolateTaskPluginBillingDatabase(t)
			resetTimedOutTaskSweepCursorForTest(t)
			oldTimeout := constant.TaskTimeoutMinutes
			constant.TaskTimeoutMinutes = 1
			t.Cleanup(func() { constant.TaskTimeoutMinutes = oldTimeout })
			const userID, tokenID = 87, 88
			const tokenKey = "synthetic-wallet-reserve"
			const request = "late-wallet-%_reserve"
			seedUser(t, userID, 1000)
			seedToken(t, tokenID, userID, tokenKey, 1000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{RequestId: request, UserId: userID, TokenId: tokenID, TokenKey: tokenKey, ForcePreConsume: true,
				UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, PreConsumeBilling(ctx, 0, info))
			row := &model.Task{TaskID: request, UserId: userID, Status: model.TaskStatusSubmitted, SubmitTime: time.Now().Add(-2 * time.Minute).Unix(),
				PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet, AwaitingUpstreamID: true, BillingRequestId: request, TokenId: tokenID}}
			require.NoError(t, model.DB.Create(row).Error)
			info.PersistedTaskID = row.ID
			require.NoError(t, info.Billing.Reserve(20))
			require.NoError(t, info.Billing.Reserve(30))
			require.EqualValues(t, 970, getUserQuota(t, userID))
			require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
			sweepTimedOutTasks(t.Context())
			expired, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.EqualValues(t, model.TaskStatusFailure, expired.Status)
			require.True(t, expired.SubmitTimeoutRetainedFunding)
			require.EqualValues(t, 970, getUserQuota(t, userID), "the timeout alone cannot authorize a refund")
			const reason = "definite rejection after the timeout"
			if replay {
				intent, err := info.Billing.(*BillingSession).PrepareRefundSettlement()
				require.NoError(t, err)
				require.NotNil(t, intent)
				require.NoError(t, model.RecordTaskSubmitFailureRecovery(row.ID, reason, *intent))
				model.ProcessPendingBillingSettlementsOnce()
				model.ProcessPendingBillingSettlementsOnce()
			} else {
				CleanupDefiniteTaskSubmitFailure(ctx, info, reason)
				CleanupDefiniteTaskSubmitFailure(ctx, info, reason)
			}
			stored, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.Equal(t, expired.FinishTime, stored.FinishTime)
			require.False(t, stored.SubmitTimeoutRetainedFunding)
			require.Equal(t, reason, stored.FailReason)
			var refund model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(request)).Take(&refund).Error)
			require.Equal(t, model.BillingSettlementStatusApplied, refund.Status)
			require.EqualValues(t, -30, refund.AppliedFundingDelta)
			require.EqualValues(t, -30, refund.AppliedTokenDelta)
			require.EqualValues(t, 1000, getUserQuota(t, userID))
			require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
		})
	}
}

func TestDefiniteRejectionReconcilesFailedWalletReserve(t *testing.T) {
	for _, test := range []struct {
		name                        string
		initial                     int
		appliedFirst, journalReplay bool
	}{
		{name: "pending after committed intent", initial: 100},
		{name: "zero original debit"},
		{name: "applied before claim with journal replay", initial: 100, appliedFirst: true, journalReplay: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateTaskPluginBillingDatabase(t)
			const userID, tokenID = 91, 92
			const request = "synthetic-reserve-failure"
			seedUser(t, userID, 1000)
			seedToken(t, tokenID, userID, request, 1000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{RequestId: request, UserId: userID, TokenId: tokenID, TokenKey: request,
				ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, PreConsumeBilling(ctx, test.initial, info))
			row := &model.Task{TaskID: request, UserId: userID, Quota: test.initial, Status: model.TaskStatusSubmitted,
				PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet, AwaitingUpstreamID: true, BillingRequestId: request, TokenId: tokenID}}
			require.NoError(t, model.DB.Create(row).Error)
			info.PersistedTaskID = row.ID
			reserve := model.BillingSettlementInput{OperationKey: "request:" + request + ":reserve:1",
				Source: BillingSourceWallet, UserID: userID, TokenID: tokenID, FundingDelta: 50, TokenDelta: 50, ManualOnFailure: true}
			const reserveRead = "fixture:fail-first-reserve-read"
			outage := errors.New("synthetic post-insert wallet reserve read failure")
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(reserveRead, func(tx *gorm.DB) {
				if tx.Statement.Table == "billing_settlements" {
					_ = tx.AddError(outage)
				}
			}))
			require.ErrorIs(t, info.Billing.Reserve(test.initial+50), outage)
			require.NoError(t, model.DB.Callback().Query().Remove(reserveRead))
			status, found, err := model.GetBillingSettlementStatus(reserve.OperationKey)
			require.NoError(t, err)
			require.True(t, found, "the intent committed before the failed read")
			require.Equal(t, model.BillingSettlementStatusPending, status)
			require.EqualValues(t, 1000-test.initial, getUserQuota(t, userID))
			if test.appliedFirst {
				_, _, err = model.ApplyBillingSettlementOnce(reserve)
				require.NoError(t, err)
				require.EqualValues(t, 1000-test.initial-50, getTokenRemainQuota(t, tokenID))
			}
			const claimWrite = "fixture:fail-rejection-claim"
			if test.journalReplay {
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(claimWrite, func(tx *gorm.DB) {
					if tx.Statement.Table == "tasks" {
						_ = tx.AddError(errors.New("synthetic task write unavailable"))
					}
				}))
			}
			CleanupDefiniteTaskSubmitFailure(ctx, info, "no retry was sent")
			if test.journalReplay {
				require.NoError(t, model.DB.Callback().Update().Remove(claimWrite))
				var journal model.TaskSubmitFailureRecovery
				require.NoError(t, model.DB.First(&journal, "task_id = ?", row.ID).Error)
				require.Equal(t, model.TaskSubmitFailureRecoveryPending, journal.Status)
				require.EqualValues(t, 1, journal.FailedReserveRevision)
			}
			model.ProcessPendingBillingSettlementsOnce()
			model.ProcessPendingBillingSettlementsOnce()
			if !test.appliedFirst {
				_, _, err = model.ApplyBillingSettlementOnce(reserve)
				require.ErrorContains(t, err, "cancelled", "a worker that previously selected the reserve must be unable to debit it")
			}
			stored, err := model.GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.EqualValues(t, model.TaskStatusFailure, stored.Status)
			require.Zero(t, stored.Quota)
			require.Equal(t, "no retry was sent", stored.FailReason)
			require.EqualValues(t, 1000, getUserQuota(t, userID))
			require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
			refundAmount := test.initial
			if test.appliedFirst {
				refundAmount += 50
			}
			var receipt model.BillingSettlement
			err = model.DB.First(&receipt, "operation_key = ?", model.BillingRequestFinalizeOperationKey(request)).Error
			if refundAmount == 0 {
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, -refundAmount, receipt.AppliedFundingDelta)
				require.EqualValues(t, -refundAmount, receipt.AppliedTokenDelta)
			}
		})
	}
}

func TestBoundTaskWalletPreConsumeReadFailureLeavesNoReplayDebit(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	const userID, tokenID = 91, 92
	const request = "synthetic-bound-wallet-selection"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, request, 1000)
	row := &model.Task{TaskID: request, UserId: userID, Status: model.TaskStatusSubmitted,
		PrivateData: model.TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: request, TokenId: tokenID}}
	require.NoError(t, model.DB.Create(row).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RequestId: request, UserId: userID, TokenId: tokenID, TokenKey: request,
		ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PersistedTaskID: row.ID}}
	const hook = "fixture:abort-bound-wallet-preconsume"
	observed := false
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
		if receipt, ok := tx.Statement.Dest.(*model.BillingSettlement); tx.Error == nil && ok && receipt.OperationKey == "request:"+request+":pre-consume" {
			observed = true
			_ = tx.AddError(errors.New("synthetic initial debit read failure"))
		}
	}))
	require.NotNil(t, PreConsumeBilling(ctx, 100, info))
	require.True(t, observed, "the injected failure must occur after the new receipt was written")
	require.NoError(t, model.DB.Callback().Query().Remove(hook))
	require.Nil(t, info.Billing)
	CleanupDefiniteTaskSubmitFailure(ctx, info, "retry not sent")
	model.ProcessPendingBillingSettlementsOnce()
	stored, err := model.GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, model.TaskStatusFailure, stored.Status)
	require.Zero(t, stored.Quota)
	_, found, err := model.GetBillingSettlementStatus("request:" + request + ":pre-consume")
	require.NoError(t, err)
	require.False(t, found)
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestDefiniteRejectionRecoversSubscriptionAfterFailedReserve(t *testing.T) {
	isolateTaskPluginBillingDatabase(t)
	const user, token, subscription = 91, 92, 94
	const request = "synthetic-subscription-reserve-failure"
	seedUser(t, user, 1000)
	seedToken(t, token, user, request, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 93, Title: "fixture", Enabled: true,
		TotalAmount: 120, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.UserSubscription{Id: subscription, UserId: user, PlanId: 93, AmountTotal: 120,
		Status: "active", StartTime: now - 60, EndTime: now + 3600}).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RequestId: request, UserId: user, TokenId: token, TokenKey: request, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, PreConsumeBilling(ctx, 100, info))
	row := &model.Task{TaskID: request, UserId: user, Quota: 100, Status: model.TaskStatusSubmitted,
		PrivateData: model.TaskPrivateData{BillingRequestId: request, AwaitingUpstreamID: true, BillingSource: BillingSourceSubscription,
			SubscriptionId: subscription, TokenId: token}}
	require.NoError(t, model.DB.Create(row).Error)
	info.PersistedTaskID = row.ID
	require.ErrorIs(t, info.Billing.Reserve(150), model.ErrSubscriptionQuotaInsufficient)
	require.EqualValues(t, 100, getSubscriptionUsed(t, subscription))
	const reason = "retry not sent"
	original, revision, err := info.Billing.(*BillingSession).prepareTaskSubmitFailureRefund()
	require.NoError(t, err)
	require.NotNil(t, original)
	require.EqualValues(t, 1, revision)
	CleanupDefiniteTaskSubmitFailure(ctx, info, reason)
	model.ProcessPendingBillingSettlementsOnce()
	model.ProcessPendingBillingSettlementsOnce()
	require.Zero(t, getSubscriptionUsed(t, subscription))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, token))
	stored, err := model.GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, model.TaskStatusFailure, stored.Status)
	require.Zero(t, stored.Quota)
	status, found, err := model.GetBillingSettlementStatus("request:" + request + ":reserve:1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "cancelled", status)
	status, err = model.RecordTaskSubmitFailureWithReserveRecovery(row.ID, reason, *original, revision)
	require.NoError(t, err)
	require.Equal(t, model.TaskSubmitFailureRecoveryPending, status)
	model.ProcessPendingBillingSettlementsOnce()
	var journal model.TaskSubmitFailureRecovery
	require.NoError(t, model.DB.Where("task_id = ?", row.ID).Take(&journal).Error)
	require.Equal(t, model.TaskSubmitFailureRecoveryRecorded, journal.Status)
	require.Zero(t, getSubscriptionUsed(t, subscription))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, token))
}

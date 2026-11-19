// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskSubmitFailureCleanupCoordinatesRealFunding(t *testing.T) {
	for _, funding := range []string{"wallet_only", "subscription_only"} {
		for _, outcome := range []string{"manual-first", "failure-first", "read-error", "pending-restart", "intent-write-error", "concurrent-cleanup", "identity-mismatch"} {
			t.Run(funding+"/"+outcome, func(t *testing.T) {
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
				require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
				require.NoError(t, db.Create(&model.User{Id: 51001, Username: "cleanup-fixture", Quota: 1000}).Error)
				require.NoError(t, db.Create(&model.Token{Id: 51002, UserId: 51001, Key: "synthetic-cleanup-key", RemainQuota: 1000}).Error)
				if funding == "subscription_only" {
					require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 51003, Title: "cleanup", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					require.NoError(t, db.Create(&model.UserSubscription{Id: 51004, UserId: 51001, PlanId: 51003, AmountTotal: 1000, Status: "active", StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{RequestId: "cleanup-" + funding + "-" + outcome, UserId: 51001, TokenId: 51002, TokenKey: "synthetic-cleanup-key", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: funding}, PriceData: types.PriceData{ModelRatio: 1}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
				require.Nil(t, PreConsumeBilling(c, 100, info))
				row := &model.Task{TaskID: "cleanup-placeholder", UserId: 51001, Quota: 100, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: info.RequestId, BillingSource: info.BillingSource, TokenId: info.TokenId, SubscriptionId: info.SubscriptionId}}
				require.NoError(t, db.Create(row).Error)
				info.PersistedTaskID = row.ID
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
				if outcome == "read-error" {
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail-cleanup-read", func(tx *gorm.DB) {
						if tx.Statement.Table == "tasks" {
							tx.AddError(errors.New("synthetic ownership read failure"))
						}
					}))
				}
				if outcome == "intent-write-error" {
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
					info.Billing.Refund(c) // A delayed original session replays the same durable operation.
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
				}
				CleanupDefiniteTaskSubmitFailure(c, info, "rewrite rejected")
				CleanupDefiniteTaskSubmitFailure(c, info, "duplicate cleanup")
				if outcome == "read-error" {
					require.NoError(t, db.Callback().Query().Remove("fail-cleanup-read"))
				}
				if outcome == "intent-write-error" {
					require.NoError(t, db.Callback().Create().Remove("fail-refund-intent"))
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
				if outcome == "failure-first" || outcome == "pending-restart" || outcome == "concurrent-cleanup" {
					require.Positive(t, checkedColumns.Load(), "the actual cleanup must emit only real database columns")
					require.EqualValues(t, model.TaskStatusFailure, task.Status)
					require.Zero(t, task.Quota)
					require.EqualValues(t, 1, refunds)
					require.EqualValues(t, 1000, token.RemainQuota)
					require.Zero(t, token.UsedQuota)
					require.EqualValues(t, 1000, user.Quota)
					require.Zero(t, used)
					won, err := row.UpdateWithStatusAndManualSettlement(model.TaskStatusSubmitted, row.UpdatedAt, input, "late decision")
					require.NoError(t, err)
					require.False(t, won)
				} else {
					require.EqualValues(t, model.TaskStatusSubmitted, task.Status)
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
			})
		}
	}
}

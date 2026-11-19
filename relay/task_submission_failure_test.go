// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package relay

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMidjourneySubmitFailurePreservesFundingOwner(t *testing.T) {
	for _, funding := range []string{"wallet_only", "subscription_only"} {
		for _, outcome := range []string{"manual-reprepare", "manual-rejection", "persist-error", "definite-rejection"} {
			t.Run(funding+"/"+outcome, func(t *testing.T) {
				db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mj-cleanup.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
				require.NoError(t, err)
				oldDB, oldLog := model.DB, model.LOG_DB
				oldSQLite, oldRedis, oldBatch := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled
				model.DB, model.LOG_DB = db, db
				common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled = true, false, false
				t.Cleanup(func() {
					model.DB, model.LOG_DB = oldDB, oldLog
					common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled = oldSQLite, oldRedis, oldBatch
					if pool, err := db.DB(); err == nil {
						_ = pool.Close()
					}
				})
				require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSubmitFailureRecovery{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
				require.NoError(t, db.Create(&model.User{Id: 52001, Username: "mj-cleanup-fixture", Quota: 1000}).Error)
				require.NoError(t, db.Create(&model.Token{Id: 52002, UserId: 52001, Key: "synthetic-mj-cleanup-key", RemainQuota: 1000}).Error)
				if funding == "subscription_only" {
					require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 52003, Title: "mj-cleanup", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					require.NoError(t, db.Create(&model.UserSubscription{Id: 52004, UserId: 52001, PlanId: 52003, AmountTotal: 1000, Status: "active", StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{RequestId: "mj-cleanup-" + funding + "-" + outcome, UserId: 52001, TokenId: 52002, TokenKey: "synthetic-mj-cleanup-key", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: funding}, PriceData: types.PriceData{ModelRatio: 1}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 52005}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				row := &model.Task{TaskID: "mj-cleanup-placeholder", UserId: info.UserId, Quota: 100, Status: model.TaskStatusSubmitted, Platform: constant.TaskPlatformMidjourney, PrivateData: model.TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: info.RequestId, BillingSource: info.BillingSource, TokenId: info.TokenId, SubscriptionId: info.SubscriptionId}}
				require.NoError(t, db.Create(row).Error)
				info.PersistedTaskID = row.ID
				if outcome == "manual-reprepare" || outcome == "manual-rejection" {
					input := model.BillingSettlementInput{OperationKey: model.BillingTaskFinalizeOperationKey(row.ID), Source: info.BillingSource, UserID: info.UserId, TokenID: info.TokenId, TokenKey: info.TokenKey, SubscriptionID: info.SubscriptionId, TaskID: row.ID, TaskQuota: 100, TaskQuotaTarget: 100}
					if funding == "subscription_only" {
						input.SubscriptionPreConsumeRequestID = info.RequestId
					}
					won, err := row.UpdateWithStatusAndManualSettlement(row.Status, row.UpdatedAt, input, "manual decision owns this task")
					require.NoError(t, err)
					require.True(t, won)
				}
				if outcome == "persist-error" {
					require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail-mj-task-update", func(tx *gorm.DB) {
						if tx.Statement.Table == "tasks" {
							tx.AddError(errors.New("synthetic task update failure"))
						}
					}))
				}
				if outcome == "manual-reprepare" {
					// An uncharged action must also retain an existing request's
					// reservation when its persisted placeholder cannot be reused.
					task, response := prepareMidjourneyBillingTask(c, info, constant.MjActionInPaint, types.PriceData{}, false)
					require.Nil(t, task)
					require.NotNil(t, response)
				} else {
					failMidjourneySubmission(c, info, row, "definite rejection")
					failMidjourneySubmission(c, info, row, "duplicate rejection")
				}
				var stored model.Task
				require.NoError(t, db.First(&stored, row.ID).Error)
				var user model.User
				var token model.Token
				require.NoError(t, db.First(&user, info.UserId).Error)
				require.NoError(t, db.First(&token, info.TokenId).Error)
				var refunds int64
				require.NoError(t, db.Model(&model.BillingSettlement{}).Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).Count(&refunds).Error)
				if outcome == "definite-rejection" {
					require.EqualValues(t, model.TaskStatusFailure, stored.Status)
					require.Zero(t, stored.Quota)
					require.EqualValues(t, 1, refunds)
					require.EqualValues(t, 1000, token.RemainQuota)
				} else {
					require.EqualValues(t, model.TaskStatusSubmitted, stored.Status)
					require.Equal(t, 100, stored.Quota)
					require.Zero(t, refunds)
					require.EqualValues(t, 900, token.RemainQuota)
				}
				if funding == "subscription_only" {
					var sub model.UserSubscription
					require.NoError(t, db.First(&sub, 52004).Error)
					if outcome == "definite-rejection" {
						require.Zero(t, sub.AmountUsed)
					} else {
						require.EqualValues(t, 100, sub.AmountUsed)
					}
					require.EqualValues(t, 1000, user.Quota)
				} else {
					require.EqualValues(t, token.RemainQuota, user.Quota)
				}
				if outcome == "persist-error" {
					var recovery model.TaskSubmitFailureRecovery
					require.NoError(t, db.First(&recovery, "task_id = ?", row.ID).Error)
					require.Equal(t, model.TaskSubmitFailureRecoveryPending, recovery.Status)
					require.Equal(t, model.BillingRequestFinalizeOperationKey(info.RequestId), recovery.OperationKey)
					require.NoError(t, db.Callback().Update().Remove("fail-mj-task-update"))
					model.ProcessPendingBillingSettlementsOnce()
					require.NoError(t, db.First(&recovery, "task_id = ?", row.ID).Error)
					require.Equal(t, model.TaskSubmitFailureRecoveryRecorded, recovery.Status)
					attempts := recovery.Attempts
					model.ProcessPendingBillingSettlementsOnce()
					require.NoError(t, db.First(&recovery, "task_id = ?", row.ID).Error)
					require.Equal(t, attempts, recovery.Attempts)
					require.NoError(t, db.First(&stored, row.ID).Error)
					require.EqualValues(t, model.TaskStatusFailure, stored.Status)
					require.Zero(t, stored.Quota)
					var applied model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ?", recovery.OperationKey).First(&applied).Error)
					require.Equal(t, model.BillingSettlementStatusApplied, applied.Status)
					require.EqualValues(t, -100, applied.AppliedFundingDelta)
					require.EqualValues(t, -100, applied.AppliedTokenDelta)
					require.NoError(t, db.First(&token, info.TokenId).Error)
					require.EqualValues(t, 1000, token.RemainQuota)
					require.Zero(t, token.UsedQuota)
					require.NoError(t, db.First(&user, info.UserId).Error)
					require.EqualValues(t, 1000, user.Quota)
					if funding == "subscription_only" {
						var sub model.UserSubscription
						require.NoError(t, db.First(&sub, 52004).Error)
						require.Zero(t, sub.AmountUsed)
					}
				}
			})
		}
	}
}

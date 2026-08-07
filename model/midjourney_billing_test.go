package model

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newMidjourneyBillingTask(t *testing.T, suffix string, quota int) *Task {
	t.Helper()
	task := &Task{
		TaskID:    "task_midjourney_" + suffix,
		Platform:  constant.TaskPlatformMidjourney,
		UserId:    501,
		ChannelId: 601,
		Quota:     quota,
		Action:    constant.MjActionImagine,
		Status:    TaskStatusNotStart,
		Progress:  "0%",
		PrivateData: TaskPrivateData{
			AwaitingUpstreamID: true,
			BillingSource:      BillingSettlementSourceWallet,
			BillingRequestId:   "mj-request-" + suffix,
			TokenId:            701,
		},
	}
	insertTask(t, task)
	return task
}

func TestFinalizeMidjourneySubmissionClaimsUpstreamTaskOnce(t *testing.T) {
	truncateTables(t)
	firstTask := newMidjourneyBillingTask(t, "first", 20)
	secondTask := newMidjourneyBillingTask(t, "second", 20)

	first := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-1", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}
	created, refundDuplicate, err := FinalizeMidjourneySubmission(first, firstTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-first:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		&BillingSettlementInput{
			OperationKey: "request:mj-request-first:refund", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701, FundingDelta: -20, TokenDelta: -20,
		},
	)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	duplicate := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-1", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}
	created, refundDuplicate, err = FinalizeMidjourneySubmission(duplicate, secondTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-second:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		&BillingSettlementInput{
			OperationKey: "request:mj-request-second:refund", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701, FundingDelta: -20, TokenDelta: -20,
		},
	)
	require.NoError(t, err)
	require.False(t, created)
	require.True(t, refundDuplicate)

	var count int64
	require.NoError(t, DB.Model(&Midjourney{}).Where("user_id = ? AND mj_id = ?", 501, "provider-mj-1").Count(&count).Error)
	assert.EqualValues(t, 1, count)

	var claim MidjourneyBillingClaim
	require.NoError(t, DB.Where("channel_id = ? AND mj_id = ?", 601, "provider-mj-1").First(&claim).Error)
	assert.Equal(t, firstTask.ID, claim.BillingTaskID)

	var reloadedSecond Task
	require.NoError(t, DB.First(&reloadedSecond, secondTask.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloadedSecond.Status)
	assert.Zero(t, reloadedSecond.Quota)

	var refund BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "request:mj-request-second:refund").First(&refund).Error)
	assert.Equal(t, BillingSettlementStatusPending, refund.Status)
	assert.EqualValues(t, -20, refund.FundingDelta)
}

func TestMidjourneyUpdateTreatsUnchangedMatchedRowAsCASWin(t *testing.T) {
	truncateTables(t)
	midjourney := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-unchanged-cas",
		Status: "IN_PROGRESS", Progress: "50%", Code: 1,
	}
	require.NoError(t, DB.Create(midjourney).Error)

	callbackName := "test:force-midjourney-zero-rows-affected"
	require.NoError(t, DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			tx.RowsAffected = 0
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

	won, err := midjourney.UpdateWithStatus("IN_PROGRESS")
	require.NoError(t, err)
	require.True(t, won)
}

func TestMidjourneyUpdateTreatsAlreadyTerminalBillingTaskAsCASWin(t *testing.T) {
	truncateTables(t)
	midjourney := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-terminal-billing-cas",
		Status: "IN_PROGRESS", Progress: "50%", Code: 1,
	}
	require.NoError(t, DB.Create(midjourney).Error)
	billingTask := &Task{
		TaskID: "task_midjourney_terminal_billing_cas", Platform: constant.TaskPlatformMidjourney,
		UserId: 501, ChannelId: 601, Status: TaskStatusSuccess, Progress: "100%",
	}
	insertTask(t, billingTask)
	midjourney.Status = "SUCCESS"
	midjourney.Progress = "100%"

	callbackName := "test:force-midjourney-billing-task-zero-rows-affected"
	require.NoError(t, DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.RowsAffected = 0
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

	won, err := midjourney.UpdateWithBillingTaskAndSettlement("IN_PROGRESS", billingTask, nil)
	require.NoError(t, err)
	require.True(t, won)
}

func TestMidjourneyBillingClaimLookupUsesCurrentReadAcrossDialects(t *testing.T) {
	tests := []struct {
		name          string
		open          func(*testing.T) *gorm.DB
		wantForUpdate bool
	}{
		{
			name: "sqlite",
			open: func(t *testing.T) *gorm.DB {
				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
				require.NoError(t, err)
				return db
			},
		},
		{
			name: "mysql",
			open: func(t *testing.T) *gorm.DB {
				conn, err := sql.Open("mysql", "")
				require.NoError(t, err)
				t.Cleanup(func() { _ = conn.Close() })
				db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{
					DryRun: true, DisableAutomaticPing: true,
				})
				require.NoError(t, err)
				return db
			},
			wantForUpdate: true,
		},
		{
			name: "postgres",
			open: func(t *testing.T) *gorm.DB {
				conn, err := sql.Open("pgx", "")
				require.NoError(t, err)
				t.Cleanup(func() { _ = conn.Close() })
				db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: conn}), &gorm.Config{
					DryRun: true, DisableAutomaticPing: true,
				})
				require.NoError(t, err)
				return db
			},
			wantForUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.open(t)
			statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
				var claim MidjourneyBillingClaim
				return midjourneyBillingClaimForUpdate(tx, 601, "provider-mj-current-read", &claim)
			})
			hasForUpdate := strings.Contains(strings.ToUpper(statement), "FOR UPDATE")
			assert.Equal(t, tt.wantForUpdate, hasForUpdate, statement)
		})
	}
}

func TestFinalizeMidjourneySubmissionDoesNotRefundSameRequestReplay(t *testing.T) {
	truncateTables(t)
	firstTask := newMidjourneyBillingTask(t, "replay-first", 20)
	firstTask.PrivateData.BillingRequestId = "mj-request-replay"
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", firstTask.ID).Update("private_data", firstTask.PrivateData).Error)
	mj := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-replay", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}
	created, refundDuplicate, err := FinalizeMidjourneySubmission(mj, firstTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-replay:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		}, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	replayTask := newMidjourneyBillingTask(t, "replay-second", 20)
	replayTask.PrivateData.BillingRequestId = "mj-request-replay"
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", replayTask.ID).Update("private_data", replayTask.PrivateData).Error)
	created, refundDuplicate, err = FinalizeMidjourneySubmission(&Midjourney{
		UserId: 501, ChannelId: 601, MjId: mj.MjId, Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}, replayTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-replay:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		&BillingSettlementInput{
			OperationKey: "request:mj-request-replay:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701, FundingDelta: -20, TokenDelta: -20,
		},
	)
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, refundDuplicate)

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "request:mj-request-replay:finalize").First(&settlement).Error)
	assert.Zero(t, settlement.FundingDelta)
}

func TestMidjourneyFailureCommitsRefundIntentWithTerminalState(t *testing.T) {
	truncateTables(t)
	billingTask := newMidjourneyBillingTask(t, "failure", 30)
	mj := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-failure", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 30,
	}
	created, refundDuplicate, err := FinalizeMidjourneySubmission(mj, billingTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-failure:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		nil,
	)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	loadedTask, err := GetMidjourneyBillingTask(mj.UserId, mj.ChannelId, mj.MjId)
	require.NoError(t, err)
	require.NotNil(t, loadedTask)

	fromStatus := mj.Status
	mj.Status = "FAILURE"
	mj.Progress = "100%"
	mj.FailReason = "provider failed"
	loadedTask.Status = TaskStatusFailure
	loadedTask.Progress = "100%"
	loadedTask.FailReason = mj.FailReason
	operationKey := fmt.Sprintf("task:%d:refund", loadedTask.ID)
	won, err := mj.UpdateWithBillingTaskAndSettlement(fromStatus, loadedTask, &BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 501, TokenID: 701, FundingDelta: -30, TokenDelta: -30,
		TaskID: loadedTask.ID, TaskQuota: 30, TaskQuotaTarget: 0,
	})
	require.NoError(t, err)
	require.True(t, won)

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", operationKey).First(&settlement).Error)
	assert.Equal(t, BillingSettlementStatusPending, settlement.Status)

	var storedMJ Midjourney
	require.NoError(t, DB.First(&storedMJ, mj.Id).Error)
	assert.Equal(t, "FAILURE", storedMJ.Status)
	var storedTask Task
	require.NoError(t, DB.First(&storedTask, loadedTask.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, storedTask.Status)
}

func TestUniqueMidjourneyLookupRejectsCrossChannelAmbiguity(t *testing.T) {
	truncateTables(t)
	sharedID := "provider-mj-shared-across-channels"
	require.NoError(t, DB.Create(&Midjourney{
		UserId: 501, ChannelId: 601, MjId: sharedID, Status: "SUCCESS", Progress: "100%",
	}).Error)
	require.NoError(t, DB.Create(&Midjourney{
		UserId: 501, ChannelId: 602, MjId: sharedID, Status: "SUCCESS", Progress: "100%",
	}).Error)

	_, err := GetUniqueMidjourneyByUserAndMJID(501, sharedID)
	require.ErrorIs(t, err, ErrMidjourneyTaskAmbiguous)
	_, err = GetUniqueMidjourneyByMJID(sharedID)
	require.True(t, errors.Is(err, ErrMidjourneyTaskAmbiguous))
}

func TestMidjourneyTerminalMetadataRefreshKeepsMatchingTaskTerminalState(t *testing.T) {
	truncateTables(t)
	billingTask := newMidjourneyBillingTask(t, "terminal-refresh", 20)
	billingTask.Status = TaskStatusSuccess
	billingTask.Progress = "100%"
	billingTask.PrivateData.ResultURL = "https://example.com/old.png"
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", billingTask.ID).Updates(map[string]any{
		"status": billingTask.Status, "progress": billingTask.Progress, "private_data": billingTask.PrivateData,
	}).Error)
	mj := Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-terminal-refresh",
		Status: "SUCCESS", Progress: "100%", ImageUrl: "https://example.com/old.png", Quota: 20,
	}
	require.NoError(t, DB.Create(&mj).Error)

	mj.ImageUrl = "https://example.com/final.png"
	billingTask.PrivateData.ResultURL = mj.ImageUrl
	billingTask.SetData(map[string]any{"imageUrl": mj.ImageUrl})
	won, err := mj.UpdateWithBillingTaskAndSettlement("SUCCESS", billingTask, nil)
	require.NoError(t, err)
	require.True(t, won)

	var storedTask Task
	require.NoError(t, DB.First(&storedTask, billingTask.ID).Error)
	require.EqualValues(t, TaskStatusSuccess, storedTask.Status)
	require.Equal(t, mj.ImageUrl, storedTask.PrivateData.ResultURL)
	var storedMJ Midjourney
	require.NoError(t, DB.First(&storedMJ, mj.Id).Error)
	require.Equal(t, mj.ImageUrl, storedMJ.ImageUrl)
}

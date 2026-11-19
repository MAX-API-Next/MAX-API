// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPendingSubmitFailureRecoveryOwnsTimeoutAndRetry(t *testing.T) {
	for _, operation := range []string{"candidate", "retry", "status", "manual"} {
		t.Run(operation, func(t *testing.T) {
			db := deletionDB(t)
			require.NoError(t, db.AutoMigrate(&BillingSettlement{}))
			previous, oldSQLite := DB, common.UsingSQLite
			DB, common.UsingSQLite = db, true
			t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
			row := &Task{TaskID: "recovery-" + operation, UserId: 71, ChannelId: 1, Quota: 100, SubmitTime: 1, Status: TaskStatusSubmitted, PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingSource: BillingSettlementSourceWallet, BillingRequestId: "original", TokenId: 72}}
			require.NoError(t, db.Create(row).Error)
			input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey("original"), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, FundingDelta: -100, TokenDelta: -100, AllowMissingToken: true}
			require.NoError(t, RecordTaskSubmitFailureRecovery(row.ID, "definite rejection", input))
			fresh, err := GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			switch operation {
			case "candidate":
				tasks, err := GetTimedOutUnfinishedTasksAfter(time.Now().Unix(), 0, 0, 10)
				require.NoError(t, err)
				require.Empty(t, tasks)
			case "retry":
				fresh.ChannelId = 99
				require.Error(t, fresh.RewritePlaceholder())
			case "status":
				fresh.Status = TaskStatusFailure
				won, err := fresh.UpdateWithStatusSnapshot(TaskStatusSubmitted, fresh.UpdatedAt)
				require.NoError(t, err)
				require.False(t, won)
			case "manual":
				decision := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, TaskID: row.ID, TaskQuota: 100, TaskQuotaTarget: 100}
				won, err := fresh.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, fresh.UpdatedAt, decision, "late timeout decision")
				require.NoError(t, err)
				require.False(t, won)
			}
			var stored Task
			require.NoError(t, db.First(&stored, row.ID).Error)
			require.EqualValues(t, TaskStatusSubmitted, stored.Status, fmt.Sprintf("%s must retain recovery ownership", operation))
			require.Equal(t, 100, stored.Quota)
			require.Equal(t, 1, stored.ChannelId)
			var intents int64
			require.NoError(t, db.Model(&BillingSettlement{}).Count(&intents).Error)
			require.Zero(t, intents)
		})
	}
}

func TestSubmitFailureClaimReservesSQLiteWriterBeforeReading(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}))
	var journal string
	require.NoError(t, db.Raw("PRAGMA journal_mode=WAL").Scan(&journal).Error)
	require.Equal(t, "wal", strings.ToLower(journal))
	var location struct{ File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
	other, err := gorm.Open(sqlite.Open(location.File+"?_pragma=busy_timeout(20)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	row := &Task{TaskID: "sqlite-failure-owner", UserId: 71, Quota: 100, Status: TaskStatusSubmitted, PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingSource: BillingSettlementSourceWallet, BillingRequestId: "sqlite-failure", TokenId: 72}}
	require.NoError(t, db.Create(row).Error)
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey("sqlite-failure"), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, FundingDelta: -100, TokenDelta: -100}
	probed := false
	var probeErr error
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("interleave-task-claim-writer", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" && !probed {
			probed = true
			probeErr = other.Model(&Task{}).Where("id = ?", row.ID).UpdateColumn("updated_at", gorm.Expr("updated_at + 1")).Error
		}
	}))
	won, err := ClaimTaskSubmitFailureWithSettlement(row.ID, "definite rejection", &input)
	require.NoError(t, err)
	require.True(t, won)
	require.True(t, probed)
	var busy interface{ Code() int }
	require.ErrorAs(t, probeErr, &busy)
	require.Equal(t, 5, busy.Code()&255)
	var stored Task
	require.NoError(t, db.First(&stored, row.ID).Error)
	require.EqualValues(t, TaskStatusFailure, stored.Status)
	require.Zero(t, stored.Quota)
	var records []BillingSettlement
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 1)
	require.Equal(t, input.OperationKey, records[0].OperationKey)
	require.Equal(t, BillingSettlementStatusPending, records[0].Status)
	require.EqualValues(t, -100, records[0].FundingDelta)
	require.Zero(t, records[0].AppliedFundingDelta)
}

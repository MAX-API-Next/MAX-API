// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSlowTaskSubmitFailureClaimDoesNotBlockLaterRecovery(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}, &BillingPreConsumeSelection{}))
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	var tasks [2]Task
	for i := range tasks {
		request := fmt.Sprintf("request-%d", i)
		tasks[i] = Task{TaskID: request, Status: TaskStatusSubmitted, Quota: 100, UserId: 71,
			PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingSource: BillingSettlementSourceWallet, BillingRequestId: request, TokenId: 72}}
		require.NoError(t, db.Create(&tasks[i]).Error)
		input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey(request), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, FundingDelta: -100, TokenDelta: -100}
		require.NoError(t, RecordTaskSubmitFailureRecovery(tasks[i].ID, "known rejection", input))
	}
	seen := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("simulate-one-blocked-task-row", func(tx *gorm.DB) {
		if tx.Statement.Table != "tasks" {
			return
		}
		seen++
		if seen == 1 {
			select {
			case <-tx.Statement.Context.Done():
				tx.AddError(tx.Statement.Context.Err())
			case <-time.After(7 * time.Second):
				tx.AddError(errors.New("claim did not set a bounded context"))
			}
		}
	}))
	processPendingTaskSubmitFailures()
	require.Equal(t, 2, seen)
	var journal []TaskSubmitFailureRecovery
	require.NoError(t, db.Order("task_id").Find(&journal).Error)
	require.Len(t, journal, 2)
	require.Equal(t, TaskSubmitFailureRecoveryPending, journal[0].Status)
	require.Equal(t, context.DeadlineExceeded.Error(), journal[0].LastError)
	require.Equal(t, 1, journal[0].Attempts)
	require.Positive(t, journal[0].NextAttempt)
	require.Equal(t, TaskSubmitFailureRecoveryRecorded, journal[1].Status)
	var intents []BillingSettlement
	require.NoError(t, db.Find(&intents).Error)
	require.Len(t, intents, 1)
	require.Equal(t, journal[1].OperationKey, intents[0].OperationKey)
}

func TestTaskSubmitFailureAcknowledgementCommitsWithClaim(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}, &BillingPreConsumeSelection{}))
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	const request = "journal-atomic-claim"
	row := &Task{TaskID: request, Status: TaskStatusSubmitted, Quota: 80, UserId: 71,
		PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingSource: BillingSettlementSourceWallet, BillingRequestId: request, TokenId: 72}}
	require.NoError(t, db.Create(row).Error)
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey(request), Source: BillingSettlementSourceWallet,
		UserID: 71, TokenID: 72, FundingDelta: -80, TokenDelta: -80}
	const reason = "definite rejection"
	require.NoError(t, RecordTaskSubmitFailureRecovery(row.ID, reason, input))
	var stale TaskSubmitFailureRecovery
	require.NoError(t, db.First(&stale, "task_id = ?", row.ID).Error)
	forced := errors.New("synthetic journal ack failure")
	const hook = "synthetic:journal-ack-failure"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if values, mapUpdate := tx.Statement.Dest.(map[string]any); tx.Statement.Table == "task_submit_failure_recoveries" &&
			mapUpdate && values["status"] == TaskSubmitFailureRecoveryRecorded {
			_ = tx.AddError(forced)
		}
	}))
	won, err := ClaimTaskSubmitFailureWithSettlement(row.ID, reason, &input)
	require.ErrorIs(t, err, forced)
	require.False(t, won)
	stored, err := GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, TaskStatusSubmitted, stored.Status)
	require.Equal(t, 80, stored.Quota)
	_, found, err := GetBillingSettlementStatus(input.OperationKey)
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, db.Callback().Update().Remove(hook))
	processPendingTaskSubmitFailures()
	updateTaskSubmitFailureRecovery(stale, TaskSubmitFailureRecoveryManual, ErrTaskSubmitRefundIdentityMismatch)
	var journal TaskSubmitFailureRecovery
	require.NoError(t, db.First(&journal, "task_id = ?", row.ID).Error)
	require.Equal(t, TaskSubmitFailureRecoveryRecorded, journal.Status)
	require.Equal(t, 1, journal.Attempts)
	stored, err = GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, TaskStatusFailure, stored.Status)
	var records int64
	require.NoError(t, db.Model(&BillingSettlement{}).Where("operation_key = ?", journal.OperationKey).Count(&records).Error)
	require.EqualValues(t, 1, records)
}

func TestTaskSubmitFailureStaleBackoffPreservesNewerSchedule(t *testing.T) {
	db := deletionDB(t)
	old := TaskSubmitFailureRecovery{TaskID: 1, OperationKey: "fixture", Status: TaskSubmitFailureRecoveryPending, Attempts: 1}
	require.NoError(t, db.Create(&old).Error)
	newer := writePendingTaskSubmitFailureDisposition(db, old, TaskSubmitFailureRecoveryPending, errors.New("newer retry"))
	require.NoError(t, newer.Error)
	require.EqualValues(t, 1, newer.RowsAffected)
	var before TaskSubmitFailureRecovery
	require.NoError(t, db.First(&before, "task_id = ?", old.TaskID).Error)
	stale := writePendingTaskSubmitFailureDisposition(db, old, TaskSubmitFailureRecoveryManual, errors.New("stale decision"))
	require.NoError(t, stale.Error)
	require.Zero(t, stale.RowsAffected)
	var after TaskSubmitFailureRecovery
	require.NoError(t, db.First(&after, "task_id = ?", old.TaskID).Error)
	require.Equal(t, before, after)
}

func TestTaskSubmitFailureJournalOnlyManualItemsAreVisible(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}))
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	require.NoError(t, db.Create(&[]TaskSubmitFailureRecovery{
		{TaskID: 9007199254740993, Status: TaskSubmitFailureRecoveryManual, OperationKey: "fixture:manual:1", LastError: "query failed https://fixture.invalid?api_key=synthetic-only", UpdatedAt: 20},
		{TaskID: 1, Status: TaskSubmitFailureRecoveryManual, OperationKey: "fixture:manual:2", UpdatedAt: 10},
		{TaskID: 2, Status: TaskSubmitFailureRecoveryPending, OperationKey: "fixture:pending", UpdatedAt: 30},
	}).Error)
	data, err := GetUnresolvedPositiveFinalizeSettlements(1)
	require.NoError(t, err)
	require.Empty(t, data.Items)
	require.EqualValues(t, 2, data.TaskSubmitFailureCount)
	require.Len(t, data.TaskSubmitFailures, 1)
	item := data.TaskSubmitFailures[0]
	require.Equal(t, "9007199254740993", item.TaskID)
	require.NotContains(t, item.LastError, "synthetic-only")
	require.Contains(t, item.LastError, "query failed")
	found, err := LookupTaskSubmitFailureReview(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "1", found[0].TaskID)
	hidden, err := LookupTaskSubmitFailureReview(t.Context(), 2)
	require.NoError(t, err)
	require.Empty(t, hidden)
}

func TestBoundWalletPreConsumeBusyCommitReleasesSQLiteWriter(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &BillingPreConsumeSelection{}, &BillingSettlement{}, &SubscriptionPreConsumeRecord{}, &CacheInvalidationTask{}))
	previousDB, previousSQLite, previousRedis := DB, common.UsingSQLite, common.RedisEnabled
	DB, common.UsingSQLite, common.RedisEnabled = db, true, false
	t.Cleanup(func() { DB, common.UsingSQLite, common.RedisEnabled = previousDB, previousSQLite, previousRedis })
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("PRAGMA journal_mode=DELETE").Error)
	require.NoError(t, db.Exec("PRAGMA busy_timeout=30").Error)
	var location struct{ File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
	other, err := gorm.Open(sqlite.Open(location.File), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	otherPool, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, otherPool.Close()) })
	const request = "bound-first-paid-commit"
	require.NoError(t, db.Create(&User{Id: 71, Username: "fixture", Quota: 1000}).Error)
	require.NoError(t, db.Create(&Token{Id: 72, UserId: 71, Key: "fixture", RemainQuota: 1000}).Error)
	task := Task{TaskID: request, UserId: 71, Status: TaskStatusSubmitted, PrivateData: TaskPrivateData{
		AwaitingUpstreamID: true, BillingRequestId: request, TokenId: 72,
	}}
	require.NoError(t, db.Create(&task).Error)
	reader := other.Begin()
	require.NoError(t, reader.Error)
	t.Cleanup(func() { _ = reader.Rollback().Error })
	var count int64
	require.NoError(t, reader.Model(&Task{}).Count(&count).Error)
	appliedUpdate := false
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("fixture:observe-wallet-apply-before-commit", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_settlements" && tx.Error == nil && tx.RowsAffected == 1 {
			appliedUpdate = true
		}
	}))
	input := BillingSettlementInput{OperationKey: "request:" + request + ":pre-consume", Source: BillingSettlementSourceWallet,
		UserID: 71, TokenID: 72, FundingDelta: 100, TokenDelta: 100, PreConsumeRequestID: request,
		PreConsumeRequestedQuota: 100, PreConsumeEffectiveQuota: 100, ManualOnFailure: true}
	_, _, err = ApplyBoundTaskWalletPreConsumeOnce(input)
	require.ErrorContains(t, err, "database is locked")
	require.True(t, appliedUpdate, "the debit statements must run before COMMIT fails")
	require.NoError(t, reader.Rollback().Error)
	won, err := ClaimTaskSubmitFailureStatusContext(t.Context(), task.ID, request, "retry not sent", 71, 72)
	require.NoError(t, err)
	require.True(t, won)
	_, found, err := GetBillingSettlementStatus(input.OperationKey)
	require.NoError(t, err)
	require.False(t, found)
	var user User
	var token Token
	require.NoError(t, db.First(&user, 71).Error)
	require.NoError(t, db.First(&token, 72).Error)
	require.EqualValues(t, 1000, user.Quota)
	require.EqualValues(t, 1000, token.RemainQuota)
}

func TestTaskSubmitFailureRecoveryMetadataUsesPassDeadline(t *testing.T) {
	db := deletionDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	seen := false
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("check-rejection-journal-metadata-deadline", func(tx *gorm.DB) {
		if !strings.Contains(tx.Statement.SQL.String(), "sqlite_master") {
			return
		}
		seen = true
		deadline, bounded := tx.Statement.Context.Deadline()
		require.True(t, bounded)
		require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
	}))
	processPendingTaskSubmitFailures()
	require.True(t, seen, "exercise the actual migrator query")
}

func TestOverdueSubmitFailureRecoveryPrecedesNewJournalPage(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}, &BillingPreConsumeSelection{}))
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	now := time.Now().Unix()
	for i := 0; i < 26; i++ {
		request := fmt.Sprintf("eligible-rejection-%d", i)
		task := Task{TaskID: request, Status: TaskStatusSubmitted, Quota: 10, UserId: 71,
			PrivateData: TaskPrivateData{BillingSource: BillingSettlementSourceWallet, BillingRequestId: request}}
		require.NoError(t, db.Create(&task).Error)
		input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey(request), Source: BillingSettlementSourceWallet, UserID: 71, FundingDelta: -10}
		require.NoError(t, RecordTaskSubmitFailureRecovery(task.ID, "definite rejection", input))
		// Rows already persisted by older nodes may still use zero as their due time.
		if i == 0 {
			require.NoError(t, db.Model(&TaskSubmitFailureRecovery{}).Where("task_id = ?", task.ID).
				UpdateColumns(map[string]any{"attempts": 1, "next_attempt": now - 30, "created_at": now - 60}).Error)
		} else {
			require.NoError(t, db.Model(&TaskSubmitFailureRecovery{}).Where("task_id = ?", task.ID).UpdateColumn("next_attempt", 0).Error)
		}
	}
	processPendingTaskSubmitFailures()
	var overdue TaskSubmitFailureRecovery
	require.NoError(t, db.First(&overdue, "operation_key = ?", BillingRequestFinalizeOperationKey("eligible-rejection-0")).Error)
	require.Equal(t, TaskSubmitFailureRecoveryRecorded, overdue.Status)
	require.Equal(t, 2, overdue.Attempts)
	var recorded int64
	require.NoError(t, db.Model(&TaskSubmitFailureRecovery{}).Where("status = ?", TaskSubmitFailureRecoveryRecorded).Count(&recorded).Error)
	require.EqualValues(t, 25, recorded)
}

func TestRecordTaskSubmitFailureRecoveryUsesIndependentWriteDeadline(t *testing.T) {
	db := deletionDB(t)
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	seen := false
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("observe-rejection-fallback-budget", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_submit_failure_recoveries" {
			seen = true
			deadline, bounded := tx.Statement.Context.Deadline()
			require.True(t, bounded)
			require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
		}
	}))
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey("bounded-journal"), Source: BillingSettlementSourceWallet, UserID: 71, FundingDelta: -10}
	require.NoError(t, RecordTaskSubmitFailureRecovery(1, "definite rejection", input))
	require.True(t, seen)
	var record TaskSubmitFailureRecovery
	require.NoError(t, db.First(&record, "task_id = ?", 1).Error)
	require.Positive(t, record.NextAttempt)
	require.LessOrEqual(t, record.NextAttempt, time.Now().Unix())
}

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
				require.ErrorIs(t, fresh.RewritePlaceholder(*row), ErrTaskPlaceholderConflict)
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
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}, &BillingPreConsumeSelection{}))
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

func TestSubmitFailureClaimHonorsDeadlineWhileWaitingForSQLiteWriters(t *testing.T) {
	for _, lock := range []string{"process", "database"} {
		t.Run(lock, func(t *testing.T) {
			db := deletionDB(t)
			require.NoError(t, db.AutoMigrate(&BillingSettlement{}))
			var journal string
			require.NoError(t, db.Raw("PRAGMA journal_mode=WAL").Scan(&journal).Error)
			require.Equal(t, "wal", strings.ToLower(journal))
			var location struct{ File string }
			require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
			previous, oldSQLite := DB, common.UsingSQLite
			DB, common.UsingSQLite = db, true
			t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
			row := &Task{TaskID: "bounded-claim", Status: TaskStatusSubmitted, Quota: 100}
			require.NoError(t, db.Create(row).Error)
			release := billingSettlementSQLiteWriteMu.Unlock
			if lock == "process" {
				billingSettlementSQLiteWriteMu.Lock()
			} else {
				other, err := gorm.Open(sqlite.Open(location.File+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
				require.NoError(t, err)
				pool, err := other.DB()
				require.NoError(t, err)
				t.Cleanup(func() { _ = pool.Close() })
				writer := other.Begin()
				require.NoError(t, writer.Error)
				require.NoError(t, writer.Model(row).UpdateColumn("updated_at", row.UpdatedAt+1).Error)
				release = func() { _ = writer.Rollback().Error }
			}
			finished, released := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(released)
				select {
				case <-finished:
				case <-time.After(time.Second):
				}
				release()
			}()
			finish := sync.OnceFunc(func() { close(finished) })
			defer func() { finish(); <-released }()

			const deadline = 150 * time.Millisecond
			ctx, cancel := context.WithTimeout(t.Context(), deadline)
			defer cancel()
			start := time.Now()
			won, err := claimTaskSubmitFailureWithSettlementDB(db.WithContext(ctx), row.ID, "known rejection", nil, nil, false)
			if lock == "process" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				var busy interface{ Code() int }
				require.ErrorAs(t, err, &busy)
				require.Equal(t, 5, busy.Code()&0xff, "writer must report SQLITE_BUSY")
			}
			require.False(t, won)
			require.Less(t, time.Since(start), 500*time.Millisecond)
			var stored Task
			require.NoError(t, db.First(&stored, row.ID).Error)
			require.EqualValues(t, TaskStatusSubmitted, stored.Status)
			require.Equal(t, 100, stored.Quota)
			var busy int
			require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&busy).Error)
			require.Equal(t, 5000, busy, "ordinary pool configuration must be preserved")
			finish()
			<-released
			won, err = claimTaskSubmitFailureWithSettlementDB(db.WithContext(t.Context()), row.ID, "known rejection", nil, nil, false)
			require.NoError(t, err)
			require.True(t, won)
			require.NoError(t, db.First(&stored, row.ID).Error)
			require.EqualValues(t, TaskStatusFailure, stored.Status)
		})
	}
}

func TestSubmitFailureJournalRetriesAfterSQLiteWriterReleases(t *testing.T) {
	db := deletionDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	var location struct{ File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
	other, err := gorm.Open(sqlite.Open(location.File), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	row := &Task{TaskID: "busy-journal", Status: TaskStatusSubmitted, Quota: 100}
	require.NoError(t, db.Create(row).Error)
	writer := other.Begin()
	require.NoError(t, writer.Error)
	t.Cleanup(func() { _ = writer.Rollback().Error })
	require.NoError(t, writer.Model(row).UpdateColumn("updated_at", row.UpdatedAt+1).Error)
	busyAttempts := 0
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("release-busy-journal-writer", func(tx *gorm.DB) {
		if tx.Statement.Table != "task_submit_failure_recoveries" || tx.Error == nil {
			return
		}
		var busy interface{ Code() int }
		if errors.As(tx.Error, &busy) && busy.Code()&0xff == 5 {
			busyAttempts++
			if busyAttempts == 1 {
				_ = writer.Rollback().Error
			}
		}
	}))
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey("busy-journal"), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, FundingDelta: -100, TokenDelta: -100}
	require.NoError(t, RecordTaskSubmitFailureRecovery(row.ID, "definite rejection", input))
	require.Equal(t, 1, busyAttempts)
	var record TaskSubmitFailureRecovery
	require.NoError(t, db.First(&record, row.ID).Error)
	require.Equal(t, input, record.refundInput())
	require.Equal(t, TaskSubmitFailureRecoveryPending, record.Status)
	var count int64
	require.NoError(t, db.Model(&TaskSubmitFailureRecovery{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSubmitFailureJournalRollsBackBusySQLiteCommit(t *testing.T) {
	db := deletionDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1) // A retry must safely reuse the same connection.
	var location struct{ File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
	other, err := gorm.Open(sqlite.Open(location.File), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	readerPool, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, readerPool.Close()) })
	var journal string
	require.NoError(t, db.Raw("PRAGMA journal_mode=DELETE").Scan(&journal).Error)
	require.Equal(t, "delete", strings.ToLower(journal))
	reader := other.Begin()
	require.NoError(t, reader.Error)
	t.Cleanup(func() { _ = reader.Rollback().Error })
	var count int64
	require.NoError(t, reader.Model(&TaskSubmitFailureRecovery{}).Count(&count).Error)
	attempts := 0
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("release-busy-journal-reader", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_submit_failure_recoveries" && tx.Error == nil {
			attempts++
			if attempts == 2 {
				_ = reader.Rollback().Error
			}
		}
	}))
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey("busy-commit"), Source: BillingSettlementSourceWallet, UserID: 71, FundingDelta: -10}
	require.NoError(t, RecordTaskSubmitFailureRecovery(1, "definite rejection", input))
	require.Equal(t, 2, attempts)
	var stored TaskSubmitFailureRecovery
	require.NoError(t, db.First(&stored, 1).Error)
	require.Equal(t, input, stored.refundInput())
	require.NoError(t, db.Model(&TaskSubmitFailureRecovery{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSubmitFailureWorkerCanReplayAfterBusySQLiteCommit(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}, &BillingPreConsumeSelection{}))
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	var journal string
	require.NoError(t, db.Raw("PRAGMA journal_mode=DELETE").Scan(&journal).Error)
	require.Equal(t, "delete", strings.ToLower(journal))
	var location struct{ File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
	other, err := gorm.Open(sqlite.Open(location.File), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	readerPool, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, readerPool.Close()) })
	requestID := "claim-busy-commit"
	row := &Task{TaskID: requestID, UserId: 71, Quota: 100, Status: TaskStatusSubmitted,
		PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingSource: BillingSettlementSourceWallet, BillingRequestId: requestID, TokenId: 72}}
	require.NoError(t, db.Create(row).Error)
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey(requestID), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, FundingDelta: -100, TokenDelta: -100}
	require.NoError(t, RecordTaskSubmitFailureRecovery(row.ID, "definite rejection", input))
	reader := other.Begin()
	require.NoError(t, reader.Error)
	t.Cleanup(func() { _ = reader.Rollback().Error })
	var count int64
	require.NoError(t, reader.Model(&Task{}).Count(&count).Error)
	attemptedIntent := false
	statusWrites := 0
	var readerRelease error
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("release-reader-during-journal-status-retry", func(tx *gorm.DB) {
		if values, mapUpdate := tx.Statement.Dest.(map[string]any); tx.Statement.Table == "task_submit_failure_recoveries" &&
			mapUpdate && values["status"] == TaskSubmitFailureRecoveryPending {
			statusWrites++
			if statusWrites == 2 {
				readerRelease = reader.Rollback().Error
			}
		}
	}))
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("observe-claim-before-busy-commit", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_settlements" && tx.Error == nil {
			attemptedIntent = true
		}
	}))
	beforeBackoff := time.Now().Unix()
	processPendingTaskSubmitFailures()
	require.True(t, attemptedIntent, "the task and intent SQL must have run before the busy commit")
	require.Equal(t, 2, statusWrites)
	require.NoError(t, readerRelease)
	require.NoError(t, db.Callback().Update().Remove("release-reader-during-journal-status-retry"))
	stored, err := GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, TaskStatusSubmitted, stored.Status)
	require.Equal(t, 100, stored.Quota)
	require.NoError(t, db.Model(&BillingSettlement{}).Count(&count).Error)
	require.Zero(t, count)
	var busyTimeout int
	require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error)
	require.Equal(t, 5000, busyTimeout)
	var record TaskSubmitFailureRecovery
	require.NoError(t, db.First(&record, row.ID).Error)
	require.Equal(t, TaskSubmitFailureRecoveryPending, record.Status)
	require.Equal(t, 1, record.Attempts)
	require.GreaterOrEqual(t, record.NextAttempt, beforeBackoff+2)
	require.NoError(t, db.Model(&record).UpdateColumn("next_attempt", 0).Error)
	processPendingTaskSubmitFailures()
	processPendingTaskSubmitFailures()
	require.NoError(t, db.First(&record, row.ID).Error)
	require.Equal(t, TaskSubmitFailureRecoveryRecorded, record.Status)
	var intents []BillingSettlement
	require.NoError(t, db.Find(&intents).Error)
	require.Len(t, intents, 1)
	require.Equal(t, input.OperationKey, intents[0].OperationKey)
	require.EqualValues(t, -100, intents[0].FundingDelta)
	stored, err = GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, TaskStatusFailure, stored.Status)
	require.Zero(t, stored.Quota)
}

func TestSubmitFailureWorkerRechecksManualDispositionBeforeClaim(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSettlement{}))
	previous, oldSQLite := DB, common.UsingSQLite
	DB, common.UsingSQLite = db, true
	t.Cleanup(func() { DB, common.UsingSQLite = previous, oldSQLite })
	requestID := "manual-journal-interleave"
	row := &Task{TaskID: requestID, UserId: 71, Quota: 100, Status: TaskStatusSubmitted,
		PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingSource: BillingSettlementSourceWallet, BillingRequestId: requestID, TokenId: 72}}
	require.NoError(t, db.Create(row).Error)
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey(requestID), Source: BillingSettlementSourceWallet, UserID: 71, TokenID: 72, FundingDelta: -100, TokenDelta: -100}
	require.NoError(t, RecordTaskSubmitFailureRecovery(row.ID, "definite rejection", input))
	changed := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("manual-disposition-after-journal-read", func(tx *gorm.DB) {
		if _, batch := tx.Statement.Dest.(*[]TaskSubmitFailureRecovery); batch && !changed && tx.RowsAffected == 1 {
			changed = true
			require.NoError(t, db.Model(&TaskSubmitFailureRecovery{}).Where("task_id = ?", row.ID).UpdateColumn("status", TaskSubmitFailureRecoveryManual).Error)
		}
	}))
	processPendingTaskSubmitFailures()
	require.True(t, changed)
	won, err := ClaimTaskSubmitFailureWithSettlement(row.ID, "duplicate rejection", &input)
	require.ErrorIs(t, err, ErrTaskSubmitFailureRecoveryRequiresReview)
	require.False(t, won)
	var record TaskSubmitFailureRecovery
	require.NoError(t, db.First(&record, row.ID).Error)
	require.Equal(t, TaskSubmitFailureRecoveryManual, record.Status)
	var count int64
	require.NoError(t, db.Model(&BillingSettlement{}).Count(&count).Error)
	require.Zero(t, count)
	stored, err := GetTaskPlaceholderByID(row.ID)
	require.NoError(t, err)
	require.EqualValues(t, TaskStatusSubmitted, stored.Status)
	require.Equal(t, 100, stored.Quota)
}

func TestSubmitFailureJournalReportsExistingManualStatus(t *testing.T) {
	db := deletionDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	input := BillingSettlementInput{OperationKey: BillingRequestFinalizeOperationKey("manual-journal"), Source: BillingSettlementSourceWallet, UserID: 71, FundingDelta: -100}
	status, err := RecordTaskSubmitFailureRecoveryWithStatus(1, "definite rejection", input)
	require.NoError(t, err)
	require.Equal(t, TaskSubmitFailureRecoveryPending, status)
	var record TaskSubmitFailureRecovery
	require.NoError(t, db.First(&record, 1).Error)
	updateTaskSubmitFailureRecovery(record, TaskSubmitFailureRecoveryManual, ErrTaskFinalizeOwned)
	status, err = RecordTaskSubmitFailureRecoveryWithStatus(1, "duplicate rejection", input)
	require.NoError(t, err)
	require.Equal(t, TaskSubmitFailureRecoveryManual, status)
	require.NoError(t, db.First(&record, 1).Error)
	require.Equal(t, TaskSubmitFailureRecoveryManual, record.Status)
	require.Equal(t, "definite rejection", record.Reason)
	require.Zero(t, record.NextAttempt)
}

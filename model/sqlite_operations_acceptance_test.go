/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sqliteOperationsBusinessSnapshot struct {
	User struct {
		ID           int
		Quota        int64
		UsedQuota    int64
		RequestCount int
	}
	Token struct {
		ID          int
		UserID      int
		RemainQuota int64
		UsedQuota   int64
	}
	Channel struct {
		ID        int
		UsedQuota int64
	}
	Plan struct {
		ID          int
		TotalAmount int64
		ResetPeriod string
	}
	Subscription struct {
		ID          int
		UserID      int
		PlanID      int
		AmountTotal int64
		AmountUsed  int64
		LastResetAt int64
		Status      string
	}
	PreConsume struct {
		RequestID      string
		UserID         int
		SubscriptionID int
		PreConsumed    int64
		Status         string
	}
	Selection struct {
		RequestID      string
		Source         string
		UserID         int
		EffectiveQuota int64
	}
	Settlement struct {
		OperationKey        string
		Source              string
		UserID              int
		FundingDelta        int64
		AppliedFundingDelta int64
		TokenDelta          int64
		AppliedTokenDelta   int64
		Status              string
		EffectStatus        string
		Revision            int64
	}
	Log struct {
		ID        int
		UserID    int
		RequestID string
		Quota     int
		ModelName string
	}
	Receipt struct {
		OperationKey string
		ClaimToken   string
	}
}

func TestSQLiteOperationsAcceptancePreservesBusinessSnapshot(t *testing.T) {
	t.Parallel()

	root := sqliteOperationsRepoRoot(t)
	workDir := t.TempDir()
	primaryDir := filepath.Join(workDir, "primary")
	logDir := filepath.Join(workDir, "log")
	configDir := filepath.Join(workDir, "config")
	require.NoError(t, os.MkdirAll(primaryDir, 0o700))
	require.NoError(t, os.MkdirAll(logDir, 0o700))
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	primaryPath := filepath.Join(primaryDir, "ledger.sqlite")
	logPath := filepath.Join(logDir, "logs.sqlite")
	configPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("database_password: synthetic-only\n"), 0o600))

	seedSQLiteOperationsDataset(t, primaryPath, logPath)
	before := readSQLiteOperationsSnapshot(t, primaryPath, logPath)
	walDB := seedSQLiteOperationsWALFixture(t, primaryPath)
	t.Cleanup(func() { require.NoError(t, closeSQLiteOperationsDatabase(walDB)) })

	archivePath := filepath.Join(workDir, "archive")
	runSQLiteOperationsCLI(t, root, "backup", "--quiesced", "--primary", primaryPath, "--log", logPath, "--config", configPath, "--destination", archivePath)
	runSQLiteOperationsCLI(t, root, "verify", "--backup", archivePath)

	firstRestore := filepath.Join(workDir, "restore-first")
	secondRestore := filepath.Join(workDir, "restore-rollback")
	runSQLiteOperationsCLI(t, root, "restore", "--backup", archivePath, "--target", firstRestore)
	runSQLiteOperationsCLI(t, root, "restore", "--backup", archivePath, "--target", secondRestore)

	firstSnapshot := readSQLiteOperationsSnapshot(t,
		filepath.Join(firstRestore, "primary", filepath.Base(primaryPath)),
		filepath.Join(firstRestore, "log", filepath.Base(logPath)),
	)
	restoredDB := openSQLiteOperationsDatabase(t, filepath.Join(firstRestore, "primary", filepath.Base(primaryPath)))
	var walMarker Option
	require.NoError(t, restoredDB.Where("key = ?", "sqliteops-wal-marker").First(&walMarker).Error)
	require.Equal(t, "committed-in-wal", walMarker.Value)
	require.NoError(t, closeSQLiteOperationsDatabase(restoredDB))
	// Reopening the restored files models a second cold start and catches
	// archives that are readable only while the first connection is open.
	secondStartSnapshot := readSQLiteOperationsSnapshot(t,
		filepath.Join(firstRestore, "primary", filepath.Base(primaryPath)),
		filepath.Join(firstRestore, "log", filepath.Base(logPath)),
	)
	rollbackSnapshot := readSQLiteOperationsSnapshot(t,
		filepath.Join(secondRestore, "primary", filepath.Base(primaryPath)),
		filepath.Join(secondRestore, "log", filepath.Base(logPath)),
	)

	require.Equal(t, before, firstSnapshot, "isolated restore changed the business snapshot")
	require.Equal(t, firstSnapshot, secondStartSnapshot, "a repeated cold start changed the business snapshot")
	require.Equal(t, firstSnapshot, rollbackSnapshot, "the rollback archive copy diverged from the first restore")
	_, err := os.Stat(filepath.Join(firstRestore, filepath.Base(configPath)))
	require.ErrorIs(t, err, os.ErrNotExist, "configuration contents must not be restored by sqliteops")
}

func seedSQLiteOperationsDataset(t *testing.T, primaryPath, logPath string) {
	const (
		userID         = 91001
		tokenID        = 91002
		channelID      = 91003
		planID         = 91004
		subscriptionID = 91005
	)
	now := int64(1_760_000_000)
	requestID := "sqliteops-acceptance-request"
	operationKey := "request:sqliteops-acceptance:finalize"

	db := openSQLiteOperationsDatabase(t, primaryPath)
	require.NoError(t, db.AutoMigrate(
		&User{}, &Token{}, &Channel{}, &SubscriptionPlan{}, &UserSubscription{},
		&SubscriptionPreConsumeRecord{}, &BillingPreConsumeSelection{}, &BillingSettlement{},
		&Option{},
	))
	require.NoError(t, db.Create(&User{
		Id: userID, Username: "sqliteops-acceptance", Password: "synthetic-password",
		Email: "sqliteops@example.invalid", Quota: 100000, UsedQuota: 2345,
		RequestCount: 17, Group: "default",
	}).Error)
	require.NoError(t, db.Create(&Token{
		Id: tokenID, UserId: userID, Key: "synthetic-token-not-a-secret", Name: "acceptance",
		RemainQuota: 87654, UsedQuota: 2346, Status: 1, ExpiredTime: -1,
	}).Error)
	require.NoError(t, db.Create(&Channel{
		Id: channelID, Type: 1, Key: "synthetic-channel-not-a-secret", Name: "acceptance",
		Status: 1, UsedQuota: 4567, Group: "default",
	}).Error)
	require.NoError(t, db.Create(&SubscriptionPlan{
		Id: planID, Title: "SQLite acceptance plan", Currency: "USD", DurationUnit: "month",
		DurationValue: 1, Enabled: true, TotalAmount: 50000, QuotaResetPeriod: SubscriptionResetMonthly,
		CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&UserSubscription{
		Id: subscriptionID, UserId: userID, PlanId: planID, AmountTotal: 50000,
		AmountUsed: 1200, StartTime: now - 3600, EndTime: now + 86400,
		Status: "active", LastResetTime: now - 3600, NextResetTime: now + 30*86400,
		AllowWalletOverflow: true, CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&SubscriptionPreConsumeRecord{
		RequestId: requestID, UserId: userID, TokenId: tokenID, UserSubscriptionId: subscriptionID,
		SubscriptionLastResetTime: now - 3600, ModelName: "synthetic-model", PreConsumed: 300,
		Status: "consumed", CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&BillingPreConsumeSelection{
		RequestID: requestID, Source: BillingSettlementSourceSubscription, UserID: userID,
		TokenID: tokenID, ModelName: "synthetic-model", RequestedQuota: 300,
		EffectiveQuota: 300, CreatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&BillingSettlement{
		OperationKey: operationKey, Source: BillingSettlementSourceSubscription, UserID: userID,
		SubscriptionID: subscriptionID, TokenID: tokenID, FundingDelta: -300,
		AppliedFundingDelta: -300, TokenDelta: 0, AppliedTokenDelta: 0,
		Status: BillingSettlementStatusApplied, EffectStatus: BillingSettlementEffectApplied,
		Revision: 2, PreConsumeRequestID: requestID, PreConsumeModelName: "synthetic-model",
		PreConsumeRequestedQuota: 300, PreConsumeEffectiveQuota: 300, CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, closeSQLiteOperationsDatabase(db))

	logDB := openSQLiteOperationsDatabase(t, logPath)
	require.NoError(t, logDB.AutoMigrate(&Log{}, &BillingLogReceipt{}))
	require.NoError(t, logDB.Create(&Log{
		Id: 91006, UserId: userID, CreatedAt: now, Type: LogTypeConsume,
		Username: "sqliteops-acceptance", TokenName: "acceptance", ModelName: "synthetic-model",
		Quota: 300, PromptTokens: 10, CompletionTokens: 20, TokenId: tokenID,
		ChannelId: channelID, RequestId: requestID, Group: "default", Content: "synthetic log",
	}).Error)
	require.NoError(t, logDB.Create(&BillingLogReceipt{
		OperationKey: operationKey, ClaimToken: "synthetic-claim-token", CreatedAt: now,
	}).Error)
	require.NoError(t, closeSQLiteOperationsDatabase(logDB))
}

func seedSQLiteOperationsWALFixture(t *testing.T, primaryPath string) *gorm.DB {
	db := openSQLiteOperationsDatabase(t, primaryPath)
	require.NoError(t, db.Exec("PRAGMA journal_mode=WAL").Error)
	require.NoError(t, db.Exec("PRAGMA wal_autocheckpoint=0").Error)
	require.NoError(t, db.Create(&Option{Key: "sqliteops-wal-marker", Value: "committed-in-wal"}).Error)
	walInfo, err := os.Stat(primaryPath + "-wal")
	require.NoError(t, err, "the committed fixture row must remain in a real WAL sidecar")
	require.Greater(t, walInfo.Size(), int64(0), "the committed fixture row must remain in a non-empty WAL sidecar")
	return db
}

func readSQLiteOperationsSnapshot(t *testing.T, primaryPath, logPath string) sqliteOperationsBusinessSnapshot {
	const (
		userID         = 91001
		tokenID        = 91002
		channelID      = 91003
		planID         = 91004
		subscriptionID = 91005
		requestID      = "sqliteops-acceptance-request"
		operationKey   = "request:sqliteops-acceptance:finalize"
	)
	var snapshot sqliteOperationsBusinessSnapshot
	db := openSQLiteOperationsDatabase(t, primaryPath)
	var user User
	require.NoError(t, db.First(&user, userID).Error)
	snapshot.User.ID, snapshot.User.Quota, snapshot.User.UsedQuota, snapshot.User.RequestCount = user.Id, user.Quota, user.UsedQuota, user.RequestCount
	var token Token
	require.NoError(t, db.First(&token, tokenID).Error)
	snapshot.Token.ID, snapshot.Token.UserID, snapshot.Token.RemainQuota, snapshot.Token.UsedQuota = token.Id, token.UserId, token.RemainQuota, token.UsedQuota
	var channel Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	snapshot.Channel.ID, snapshot.Channel.UsedQuota = channel.Id, channel.UsedQuota
	var plan SubscriptionPlan
	require.NoError(t, db.First(&plan, planID).Error)
	snapshot.Plan.ID, snapshot.Plan.TotalAmount, snapshot.Plan.ResetPeriod = plan.Id, plan.TotalAmount, plan.QuotaResetPeriod
	var subscription UserSubscription
	require.NoError(t, db.First(&subscription, subscriptionID).Error)
	snapshot.Subscription.ID, snapshot.Subscription.UserID, snapshot.Subscription.PlanID = subscription.Id, subscription.UserId, subscription.PlanId
	snapshot.Subscription.AmountTotal, snapshot.Subscription.AmountUsed = subscription.AmountTotal, subscription.AmountUsed
	snapshot.Subscription.LastResetAt, snapshot.Subscription.Status = subscription.LastResetTime, subscription.Status
	var preConsume SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", requestID).First(&preConsume).Error)
	snapshot.PreConsume.RequestID, snapshot.PreConsume.UserID, snapshot.PreConsume.SubscriptionID = preConsume.RequestId, preConsume.UserId, preConsume.UserSubscriptionId
	snapshot.PreConsume.PreConsumed, snapshot.PreConsume.Status = preConsume.PreConsumed, preConsume.Status
	var selection BillingPreConsumeSelection
	require.NoError(t, db.Where("request_id = ?", requestID).First(&selection).Error)
	snapshot.Selection.RequestID, snapshot.Selection.Source, snapshot.Selection.UserID = selection.RequestID, selection.Source, selection.UserID
	snapshot.Selection.EffectiveQuota = selection.EffectiveQuota
	var settlement BillingSettlement
	require.NoError(t, db.Where("operation_key = ?", operationKey).First(&settlement).Error)
	snapshot.Settlement.OperationKey, snapshot.Settlement.Source, snapshot.Settlement.UserID = settlement.OperationKey, settlement.Source, settlement.UserID
	snapshot.Settlement.FundingDelta, snapshot.Settlement.AppliedFundingDelta = settlement.FundingDelta, settlement.AppliedFundingDelta
	snapshot.Settlement.TokenDelta, snapshot.Settlement.AppliedTokenDelta = settlement.TokenDelta, settlement.AppliedTokenDelta
	snapshot.Settlement.Status, snapshot.Settlement.EffectStatus, snapshot.Settlement.Revision = settlement.Status, settlement.EffectStatus, settlement.Revision
	require.NoError(t, closeSQLiteOperationsDatabase(db))

	logDB := openSQLiteOperationsDatabase(t, logPath)
	var logEntry Log
	require.NoError(t, logDB.First(&logEntry, 91006).Error)
	snapshot.Log.ID, snapshot.Log.UserID, snapshot.Log.RequestID, snapshot.Log.Quota, snapshot.Log.ModelName = logEntry.Id, logEntry.UserId, logEntry.RequestId, logEntry.Quota, logEntry.ModelName
	var receipt BillingLogReceipt
	require.NoError(t, logDB.Where("operation_key = ?", operationKey).First(&receipt).Error)
	snapshot.Receipt.OperationKey, snapshot.Receipt.ClaimToken = receipt.OperationKey, receipt.ClaimToken
	require.NoError(t, closeSQLiteOperationsDatabase(logDB))
	return snapshot
}

func openSQLiteOperationsDatabase(t *testing.T, path string) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

func closeSQLiteOperationsDatabase(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func sqliteOperationsRepoRoot(t *testing.T) string {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	directory := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("could not find repository root from %s", filename)
		}
		directory = parent
	}
}

func runSQLiteOperationsCLI(t *testing.T, root string, args ...string) {
	commandArgs := append([]string{"run", "./tools/sqliteops"}, args...)
	command := exec.Command("go", commandArgs...)
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "sqliteops %v failed: %s", args, output)
}

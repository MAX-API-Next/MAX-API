package model

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type sqlStatementRecorder struct {
	gormlogger.Interface
	statements []string
}

func (r *sqlStatementRecorder) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, _ := fc()
	r.statements = append(r.statements, sql)
}

func (r *sqlStatementRecorder) hasStatementContaining(needle string) bool {
	needle = strings.ToUpper(needle)
	for _, statement := range r.statements {
		if strings.Contains(strings.ToUpper(statement), needle) {
			return true
		}
	}
	return false
}

func quotaDataAggregateKeyForTest(quotaData *QuotaData) string {
	digest := sha256.Sum256([]byte(quotaDataCacheKey(quotaData)))
	return hex.EncodeToString(digest[:])
}

func ensureQuotaDataAggregateKeySchemaForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasColumn(&QuotaData{}, "aggregate_key") {
		require.NoError(t, db.Exec("ALTER TABLE quota_data ADD COLUMN aggregate_key TEXT").Error)
	}
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_quota_data_aggregate_key ON quota_data (aggregate_key)").Error)
}

func observeQuotaDataOperationLockAttemptForTest(t *testing.T, ignoredOwner string) <-chan struct{} {
	t.Helper()
	attempted := make(chan struct{}, 1)
	quotaDataOperationLockAttemptHookMu.Lock()
	oldHook := quotaDataOperationLockAttemptHook
	quotaDataOperationLockAttemptHook = func(owner string) {
		if owner == ignoredOwner {
			return
		}
		select {
		case attempted <- struct{}{}:
		default:
		}
	}
	quotaDataOperationLockAttemptHookMu.Unlock()
	t.Cleanup(func() {
		quotaDataOperationLockAttemptHookMu.Lock()
		defer quotaDataOperationLockAttemptHookMu.Unlock()
		quotaDataOperationLockAttemptHook = oldHook
	})
	return attempted
}

func waitForQuotaDataOperationLockAttempt(t *testing.T, attempted <-chan struct{}, done <-chan error, releaseHolder func()) {
	t.Helper()
	select {
	case <-attempted:
	case err := <-done:
		releaseHolder()
		t.Fatalf("operation completed before attempting the quota_data operation lock: %v", err)
	case <-time.After(time.Second):
		releaseHolder()
		t.Fatal("operation did not attempt to acquire the quota_data operation lock")
	}
}

func TestSaveQuotaDataCacheRequeuesFailedSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	CacheQuotaDataLock.Lock()
	oldCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB = oldDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = oldCache
		CacheQuotaDataLock.Unlock()
	})

	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "quota-user", ModelName: "test-model", Quota: 7, CreatedAt: 3601, TokenUsed: 3,
	})
	require.Error(t, SaveQuotaDataCache(context.Background()))

	CacheQuotaDataLock.Lock()
	assert.Len(t, CacheQuotaData, 1)
	CacheQuotaDataLock.Unlock()

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	require.NoError(t, SaveQuotaDataCache(context.Background()))

	CacheQuotaDataLock.Lock()
	assert.Empty(t, CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	var got QuotaData
	require.NoError(t, db.First(&got).Error)
	assert.Equal(t, 1, got.Count)
	assert.Equal(t, 7, got.Quota)
	assert.Equal(t, 3, got.TokenUsed)
}

func TestSaveQuotaDataCacheReturnsIndividualPersistenceFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	CacheQuotaDataLock.Lock()
	oldCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB = oldDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = oldCache
		CacheQuotaDataLock.Unlock()
	})

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	require.NoError(t, db.Migrator().DropTable(&QuotaDataSnapshot{}))
	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "quota-user", ModelName: "test-model", Quota: 7, CreatedAt: 3601, TokenUsed: 3,
	})

	err = SaveQuotaDataCache(context.Background())
	require.Error(t, err)
	CacheQuotaDataLock.Lock()
	assert.Len(t, CacheQuotaData, 1)
	CacheQuotaDataLock.Unlock()
}

func TestSaveQuotaDataIsIdempotentForRepeatedSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())

	snapshotID := "quota-snapshot-idempotency"
	snapshot := &QuotaData{
		SnapshotID: &snapshotID,
		UserID:     1,
		Username:   "quota-user",
		ModelName:  "test-model",
		CreatedAt:  3600,
		Count:      1,
		Quota:      7,
		TokenUsed:  3,
	}
	require.NoError(t, saveQuotaData(snapshot))
	// A retry after an ambiguous commit must not apply the same counters again.
	require.NoError(t, saveQuotaData(snapshot))

	var rows []QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, 7, rows[0].Quota)
	assert.Equal(t, 3, rows[0].TokenUsed)
}

func TestQuotaDataMigrationCreatesUniqueAggregateKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&QuotaData{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())

	assert.True(t, db.Migrator().HasColumn(&QuotaData{}, "aggregate_key"))
	assert.True(t, db.Migrator().HasIndex(&QuotaData{}, "ux_quota_data_aggregate_key"))
}

func TestQuotaDataMigrationBackfillsAndMergesLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	require.NoError(t, db.Exec(`CREATE TABLE quota_data (
		id integer primary key autoincrement,
		aggregate_key text,
		user_id integer,
		username text,
		model_name text,
		created_at integer,
		use_group text,
		token_id integer,
		channel_id integer,
		node_name text,
		token_used integer,
		count integer,
		quota integer
	)`).Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_quota_data_aggregate_key ON quota_data (aggregate_key)").Error)
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, "quota-user", "test-model", int64(3600), "default", 2, 3, "node-a", 1, 7, 3,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, "quota-user", "test-model", int64(3600), "default", 2, 3, "node-a", 2, 11, 4,
	).Error)
	require.NoError(t, db.AutoMigrate(&QuotaDataSnapshot{}))

	require.NoError(t, migrateQuotaDataAggregateKeys())
	assert.True(t, db.Migrator().HasColumn(&QuotaData{}, "aggregate_key"))
	assert.True(t, db.Migrator().HasIndex(&QuotaData{}, "ux_quota_data_aggregate_key"))

	aggregateKey := quotaDataAggregateKeyForTest(&QuotaData{
		UserID: 1, Username: "quota-user", ModelName: "test-model", CreatedAt: 3600,
		UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-a",
	})
	var rows []QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].AggregateKey)
	assert.Equal(t, aggregateKey, *rows[0].AggregateKey)
	assert.Equal(t, 3, rows[0].Count)
	assert.Equal(t, 18, rows[0].Quota)
	assert.Equal(t, 7, rows[0].TokenUsed)

	require.NoError(t, saveQuotaData(&QuotaData{
		UserID: 1, Username: "quota-user", ModelName: "test-model", CreatedAt: 3600,
		UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-a",
		Count: 1, Quota: 5, TokenUsed: 2,
	}))
	rows = nil
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 4, rows[0].Count)
	assert.Equal(t, 23, rows[0].Quota)
	assert.Equal(t, 9, rows[0].TokenUsed)
}

func TestQuotaDataMigrationKeepsExistingIndexWhileMergingLegacyRows(t *testing.T) {
	recorder := &sqlStatementRecorder{Interface: gormlogger.Default.LogMode(gormlogger.Silent)}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: recorder})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	require.NoError(t, db.Exec(`CREATE TABLE quota_data (
		id integer primary key autoincrement,
		aggregate_key text,
		user_id integer,
		username text,
		model_name text,
		created_at integer,
		use_group text,
		token_id integer,
		channel_id integer,
		node_name text,
		token_used integer,
		count integer,
		quota integer
	)`).Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_quota_data_aggregate_key ON quota_data (aggregate_key)").Error)

	aggregateKey := quotaDataAggregateKeyForTest(&QuotaData{
		UserID: 1, Username: "quota-user", ModelName: "test-model", CreatedAt: 3600,
		UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-a",
	})
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (id, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, 1, "quota-user", "test-model", int64(3600), "default", 2, 3, "node-a", 1, 7, 3,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (id, aggregate_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		2, aggregateKey, 1, "quota-user", "test-model", int64(3600), "default", 2, 3, "node-a", 2, 11, 4,
	).Error)

	require.NoError(t, migrateQuotaDataAggregateKeys())
	require.False(t, recorder.hasStatementContaining("DROP INDEX"), "migration must not drop the live aggregate-key index")

	var rows []QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Id)
	require.NotNil(t, rows[0].AggregateKey)
	assert.Equal(t, aggregateKey, *rows[0].AggregateKey)
	assert.Equal(t, 3, rows[0].Count)
	assert.Equal(t, 18, rows[0].Quota)
	assert.Equal(t, 7, rows[0].TokenUsed)
	assert.True(t, db.Migrator().HasIndex(&QuotaData{}, "ux_quota_data_aggregate_key"))
}

func TestQuotaDataMigrationBatchesAndRepairsStaleKeysWithLiveIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	oldBatchSize := quotaDataAggregateMigrationBatchSize
	DB = db
	quotaDataAggregateMigrationBatchSize = 2
	t.Cleanup(func() {
		DB = oldDB
		quotaDataAggregateMigrationBatchSize = oldBatchSize
	})

	require.NoError(t, db.Exec(`CREATE TABLE quota_data (
		id integer primary key autoincrement,
		aggregate_key text,
		user_id integer,
		username text,
		model_name text,
		created_at integer,
		use_group text,
		token_id integer,
		channel_id integer,
		node_name text,
		token_used integer,
		count integer,
		quota integer
	)`).Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_quota_data_aggregate_key ON quota_data (aggregate_key)").Error)

	rowA := QuotaData{Id: 1, UserID: 1, Username: "quota-user-a", ModelName: "model-a", CreatedAt: 3600, UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-a"}
	rowB := QuotaData{Id: 2, UserID: 2, Username: "quota-user-b", ModelName: "model-b", CreatedAt: 3600, UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-b"}
	keyA := quotaDataAggregateKeyForTest(&rowA)
	keyB := quotaDataAggregateKeyForTest(&rowB)
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (id, aggregate_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, keyB, rowA.UserID, rowA.Username, rowA.ModelName, rowA.CreatedAt, rowA.UseGroup, rowA.TokenID, rowA.ChannelID, rowA.NodeName, 1, 7, 3,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (id, aggregate_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		2, keyA, rowB.UserID, rowB.Username, rowB.ModelName, rowB.CreatedAt, rowB.UseGroup, rowB.TokenID, rowB.ChannelID, rowB.NodeName, 2, 11, 4,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO quota_data (id, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		3, rowA.UserID, rowA.Username, rowA.ModelName, rowA.CreatedAt, rowA.UseGroup, rowA.TokenID, rowA.ChannelID, rowA.NodeName, 3, 13, 5,
	).Error)

	require.NoError(t, migrateQuotaDataAggregateKeys())

	var rows []QuotaData
	require.NoError(t, db.Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.NotNil(t, rows[0].AggregateKey)
	require.NotNil(t, rows[1].AggregateKey)
	assert.Equal(t, 1, rows[0].Id)
	assert.Equal(t, keyA, *rows[0].AggregateKey)
	assert.Equal(t, 4, rows[0].Count)
	assert.Equal(t, 20, rows[0].Quota)
	assert.Equal(t, 8, rows[0].TokenUsed)
	assert.Equal(t, 2, rows[1].Id)
	assert.Equal(t, keyB, *rows[1].AggregateKey)
	assert.Equal(t, 2, rows[1].Count)
	assert.Equal(t, 11, rows[1].Quota)
	assert.Equal(t, 4, rows[1].TokenUsed)
}

func TestQuotaDataAggregateMigrationScratchTablesUseBigInt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() {
		_ = dropQuotaDataAggregateMigrationTables()
		DB = oldDB
	})

	require.NoError(t, resetQuotaDataAggregateMigrationTables())

	type sqliteColumn struct {
		Name string `gorm:"column:name"`
		Type string `gorm:"column:type"`
	}
	var summaryColumns []sqliteColumn
	require.NoError(t, db.Raw("PRAGMA table_info(quota_data_aggregate_key_migration)").Scan(&summaryColumns).Error)
	summaryTypes := make(map[string]string, len(summaryColumns))
	for _, column := range summaryColumns {
		summaryTypes[column.Name] = strings.ToLower(column.Type)
	}
	assert.Contains(t, summaryTypes["survivor_id"], "bigint")
	assert.Contains(t, summaryTypes["count_sum"], "bigint")
	assert.Contains(t, summaryTypes["quota_sum"], "bigint")
	assert.Contains(t, summaryTypes["token_used_sum"], "bigint")

	var memberColumns []sqliteColumn
	require.NoError(t, db.Raw("PRAGMA table_info(quota_data_aggregate_key_migration_members)").Scan(&memberColumns).Error)
	memberTypes := make(map[string]string, len(memberColumns))
	for _, column := range memberColumns {
		memberTypes[column.Name] = strings.ToLower(column.Type)
	}
	assert.Contains(t, memberTypes["id"], "bigint")
}

func TestQuotaDataMigrationWaitsForOperationLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_migration_lock?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldRetry := quotaDataOperationLockRetryInterval
	oldMaxWait := quotaDataOperationLockMaxWait
	DB = db
	quotaDataOperationLockRetryInterval = 10 * time.Millisecond
	quotaDataOperationLockMaxWait = time.Second
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockMaxWait = oldMaxWait
	})

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	holder, err := acquireQuotaDataOperationLock(context.Background())
	require.NoError(t, err)
	holderReleased := false
	releaseHolder := func() {
		if holderReleased {
			return
		}
		require.NoError(t, releaseQuotaDataOperationLock(holder))
		holderReleased = true
	}
	t.Cleanup(releaseHolder)
	attempted := observeQuotaDataOperationLockAttemptForTest(t, holder)

	done := make(chan error, 1)
	go func() {
		done <- mergeQuotaDataAggregateRows()
	}()

	waitForQuotaDataOperationLockAttempt(t, attempted, done, releaseHolder)
	select {
	case err := <-done:
		releaseHolder()
		t.Fatalf("migration completed before the operation lock was released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseHolder()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("migration did not continue after the operation lock was released")
	}
}

func TestSaveQuotaDataWaitsForAggregateMigrationLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_save_lock?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldRetry := quotaDataOperationLockRetryInterval
	oldMaxWait := quotaDataOperationLockMaxWait
	DB = db
	quotaDataOperationLockRetryInterval = 10 * time.Millisecond
	quotaDataOperationLockMaxWait = time.Second
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockMaxWait = oldMaxWait
	})

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	holder, err := acquireQuotaDataOperationLock(context.Background())
	require.NoError(t, err)
	holderReleased := false
	releaseHolder := func() {
		if holderReleased {
			return
		}
		require.NoError(t, releaseQuotaDataOperationLock(holder))
		holderReleased = true
	}
	t.Cleanup(releaseHolder)
	attempted := observeQuotaDataOperationLockAttemptForTest(t, holder)

	done := make(chan error, 1)
	go func() {
		done <- saveQuotaData(&QuotaData{
			UserID: 1, Username: "quota-user", ModelName: "test-model", CreatedAt: 3600,
			UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-a",
			Count: 1, Quota: 7, TokenUsed: 3,
		})
	}()

	waitForQuotaDataOperationLockAttempt(t, attempted, done, releaseHolder)
	select {
	case err := <-done:
		releaseHolder()
		t.Fatalf("quota write completed before the migration lock was released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseHolder()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("quota write did not continue after the migration lock was released")
	}

	var row QuotaData
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, 1, row.Count)
	assert.Equal(t, 7, row.Quota)
	assert.Equal(t, 3, row.TokenUsed)
}

func TestQuotaDataOperationLockDoesNotTrustDuplicateInsertRowsAffected(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_lock_duplicate_rows_affected?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldRetry := quotaDataOperationLockRetryInterval
	oldMaxWait := quotaDataOperationLockMaxWait
	DB = db
	quotaDataOperationLockRetryInterval = 5 * time.Millisecond
	quotaDataOperationLockMaxWait = time.Second
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockMaxWait = oldMaxWait
	})

	require.NoError(t, ensureQuotaDataOperationLockTable())
	holder, err := acquireQuotaDataOperationLock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, releaseQuotaDataOperationLock(holder)) })

	callbackName := "test:quota-lock-duplicate-found-rows"
	require.NoError(t, db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		table := tx.Statement.Table
		if table == "" && tx.Statement.Schema != nil {
			table = tx.Statement.Schema.Table
		}
		if table == quotaDataOperationLockTable {
			tx.RowsAffected = 1
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove(callbackName) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	contender, err := acquireQuotaDataOperationLock(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, contender)

	var current quotaDataOperationLock
	require.NoError(t, db.Where("name = ?", quotaDataOperationLockName).First(&current).Error)
	assert.Equal(t, holder, current.Owner)
}

func TestSaveQuotaDataCacheRequeuesSnapshotWhenCallerStopsWaiting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_save_context?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldRetry := quotaDataOperationLockRetryInterval
	oldMaxWait := quotaDataOperationLockMaxWait
	DB = db
	quotaDataOperationLockRetryInterval = 10 * time.Millisecond
	quotaDataOperationLockMaxWait = time.Second
	CacheQuotaDataLock.Lock()
	oldCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockMaxWait = oldMaxWait
		CacheQuotaDataLock.Lock()
		CacheQuotaData = oldCache
		CacheQuotaDataLock.Unlock()
	})

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	holder, err := acquireQuotaDataOperationLock(context.Background())
	require.NoError(t, err)
	holderReleased := false
	releaseHolder := func() {
		if holderReleased {
			return
		}
		require.NoError(t, releaseQuotaDataOperationLock(holder))
		holderReleased = true
	}
	t.Cleanup(releaseHolder)
	attempted := observeQuotaDataOperationLockAttemptForTest(t, holder)

	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "quota-user", ModelName: "test-model", Quota: 7, CreatedAt: 3601, TokenUsed: 3,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = SaveQuotaDataCache(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 250*time.Millisecond)
	select {
	case <-attempted:
	default:
		t.Fatal("quota cache flush did not attempt to acquire the operation lock")
	}
	CacheQuotaDataLock.Lock()
	cachedSnapshots := len(CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	assert.Equal(t, 1, cachedSnapshots, "the detached snapshot must be requeued when the flush caller stops waiting")

	releaseHolder()
	require.NoError(t, SaveQuotaDataCache(context.Background()))
	var row QuotaData
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, 1, row.Count)
	assert.Equal(t, 7, row.Quota)
	assert.Equal(t, 3, row.TokenUsed)
}

func TestQuotaDataOperationLockHeartbeatRenewsLease(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_lock_heartbeat?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldLease := quotaDataOperationLockLeaseSeconds
	oldRetry := quotaDataOperationLockRetryInterval
	oldMaxWait := quotaDataOperationLockMaxWait
	oldHeartbeat := quotaDataOperationLockHeartbeatEvery
	DB = db
	quotaDataOperationLockLeaseSeconds = 1
	quotaDataOperationLockRetryInterval = 5 * time.Millisecond
	quotaDataOperationLockMaxWait = 500 * time.Millisecond
	quotaDataOperationLockHeartbeatEvery = 20 * time.Millisecond
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockLeaseSeconds = oldLease
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockMaxWait = oldMaxWait
		quotaDataOperationLockHeartbeatEvery = oldHeartbeat
	})

	require.NoError(t, ensureQuotaDataOperationLockTable())
	err = withQuotaDataOperationLock(context.Background(), "heartbeat renewal test", func(lock *quotaDataOperationLockGuard) error {
		var current quotaDataOperationLock
		if err := db.Where("name = ? AND owner = ?", quotaDataOperationLockName, lock.Owner()).First(&current).Error; err != nil {
			return err
		}
		initialExpiresAt := current.ExpiresAt
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if err := lock.Check(); err != nil {
				return err
			}
			if err := db.Where("name = ? AND owner = ?", quotaDataOperationLockName, lock.Owner()).First(&current).Error; err != nil {
				return err
			}
			if current.ExpiresAt > initialExpiresAt {
				return nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		return errors.New("quota_data operation lock lease was not renewed")
	})
	require.NoError(t, err)
}

func TestQuotaDataOperationLockHeartbeatDetectsLostOwnership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_lock_lost?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldLease := quotaDataOperationLockLeaseSeconds
	oldRetry := quotaDataOperationLockRetryInterval
	oldMaxWait := quotaDataOperationLockMaxWait
	oldHeartbeat := quotaDataOperationLockHeartbeatEvery
	DB = db
	quotaDataOperationLockLeaseSeconds = 1
	quotaDataOperationLockRetryInterval = 5 * time.Millisecond
	quotaDataOperationLockMaxWait = 500 * time.Millisecond
	quotaDataOperationLockHeartbeatEvery = 10 * time.Millisecond
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockLeaseSeconds = oldLease
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockMaxWait = oldMaxWait
		quotaDataOperationLockHeartbeatEvery = oldHeartbeat
	})

	require.NoError(t, ensureQuotaDataOperationLockTable())
	acquired := make(chan *quotaDataOperationLockGuard, 1)
	unblock := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withQuotaDataOperationLock(context.Background(), "heartbeat lost-owner test", func(lock *quotaDataOperationLockGuard) error {
			acquired <- lock
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-unblock:
					return nil
				case <-ticker.C:
					if err := lock.Check(); err != nil {
						return err
					}
				}
			}
		})
	}()

	var lock *quotaDataOperationLockGuard
	select {
	case lock = <-acquired:
	case <-time.After(time.Second):
		close(unblock)
		t.Fatal("operation did not acquire the quota_data operation lock")
	}
	require.NoError(t, db.Where("name = ? AND owner = ?", quotaDataOperationLockName, lock.Owner()).Delete(&quotaDataOperationLock{}).Error)

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, errQuotaDataOperationLockLost)
	case <-time.After(time.Second):
		close(unblock)
		t.Fatal("operation did not stop after losing the quota_data operation lock")
	}
}

func TestQuotaDataOperationLockReleaseRetriesTransientFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota_lock_release_retry?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldDB := DB
	oldRetry := quotaDataOperationLockRetryInterval
	oldReleaseRetries := quotaDataOperationLockReleaseRetries
	DB = db
	quotaDataOperationLockRetryInterval = 5 * time.Millisecond
	quotaDataOperationLockReleaseRetries = 2
	t.Cleanup(func() {
		DB = oldDB
		quotaDataOperationLockRetryInterval = oldRetry
		quotaDataOperationLockReleaseRetries = oldReleaseRetries
	})

	require.NoError(t, ensureQuotaDataOperationLockTable())
	holder, err := acquireQuotaDataOperationLock(context.Background())
	require.NoError(t, err)

	attempts := 0
	var failOnce sync.Once
	callbackName := "test:quota-data-operation-lock-release-retry"
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		table := tx.Statement.Table
		if table == "" && tx.Statement.Schema != nil {
			table = tx.Statement.Schema.Table
		}
		if table != quotaDataOperationLockTable {
			return
		}
		attempts++
		failOnce.Do(func() {
			tx.AddError(errors.New("transient quota_data operation lock release failure"))
		})
	}))
	t.Cleanup(func() { db.Callback().Delete().Remove(callbackName) })

	require.NoError(t, releaseQuotaDataOperationLock(holder))
	assert.Equal(t, 2, attempts)
	var remaining int64
	require.NoError(t, db.Model(&quotaDataOperationLock{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestSaveQuotaDataAtomicallyMergesCompetingAggregateInsert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())

	snapshotID := "quota-snapshot-racing-insert"
	snapshot := &QuotaData{
		SnapshotID: &snapshotID,
		UserID:     1, Username: "quota-user", ModelName: "test-model", CreatedAt: 3600,
		UseGroup: "default", TokenID: 2, ChannelID: 3, NodeName: "node-a",
		Count: 1, Quota: 7, TokenUsed: 3,
	}
	aggregateKey := quotaDataAggregateKeyForTest(snapshot)
	var once sync.Once
	callbackName := "test:insert-competing-quota-aggregate"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "quota_data" {
			return
		}
		once.Do(func() {
			err := tx.Session(&gorm.Session{NewDB: true}).Exec(
				"INSERT INTO quota_data (aggregate_key, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				aggregateKey, snapshot.UserID, snapshot.Username, snapshot.ModelName, snapshot.CreatedAt, snapshot.UseGroup,
				snapshot.TokenID, snapshot.ChannelID, snapshot.NodeName, 2, 11, 4,
			).Error
			if err != nil {
				tx.AddError(err)
			}
		})
	}))
	t.Cleanup(func() { db.Callback().Create().Remove(callbackName) })

	require.NoError(t, saveQuotaData(snapshot))
	var rows []QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].Count)
	assert.Equal(t, 18, rows[0].Quota)
	assert.Equal(t, 7, rows[0].TokenUsed)
}

func TestQuotaDataUpsertUsesPortableConflictSyntax(t *testing.T) {
	tests := []struct {
		name         string
		open         func(*testing.T) *gorm.DB
		wantConflict string
	}{
		{
			name: "sqlite",
			open: func(t *testing.T) *gorm.DB {
				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
				require.NoError(t, err)
				return db
			},
			wantConflict: "ON CONFLICT (`aggregate_key`) DO UPDATE SET",
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
			wantConflict: "ON DUPLICATE KEY UPDATE",
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
			wantConflict: `ON CONFLICT ("aggregate_key") DO UPDATE SET`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.open(t)
			quotaData := &QuotaData{UserID: 1, Username: "user", ModelName: "model", CreatedAt: 3600, Count: 1, Quota: 2, TokenUsed: 3}
			statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
				return quotaDataUpsert(tx, quotaData)
			})
			assert.True(t, strings.Contains(statement, tt.wantConflict), statement)
			assert.Contains(t, statement, "count")
			assert.Contains(t, statement, "token_used")
		})
	}
}

func TestQuotaDataOperationLockUsesDatabaseClockForLeaseSQL(t *testing.T) {
	tests := []struct {
		name    string
		open    func(*testing.T) *gorm.DB
		wantNow string
	}{
		{
			name: "sqlite",
			open: func(t *testing.T) *gorm.DB {
				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
				require.NoError(t, err)
				return db
			},
			wantNow: "STRFTIME('%S','NOW')",
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
			wantNow: "UNIX_TIMESTAMP()",
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
			wantNow: "EXTRACT(EPOCH FROM CURRENT_TIMESTAMP)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.open(t)
			nowSQL, err := quotaDataOperationLockNowSQL(db)
			require.NoError(t, err)
			statements := []string{
				db.ToSQL(func(tx *gorm.DB) *gorm.DB {
					return createQuotaDataOperationLockAttempt(tx, "test-owner", nowSQL)
				}),
				db.ToSQL(func(tx *gorm.DB) *gorm.DB {
					return claimExpiredQuotaDataOperationLockAttempt(tx, "test-owner", nowSQL)
				}),
				db.ToSQL(func(tx *gorm.DB) *gorm.DB {
					return renewQuotaDataOperationLockAttempt(tx, "test-owner", nowSQL)
				}),
			}

			for _, statement := range statements {
				upperStatement := strings.ToUpper(statement)
				assert.Contains(t, upperStatement, strings.ToUpper(quotaDataOperationLockTable), statement)
				assert.Contains(t, upperStatement, tt.wantNow, statement)
			}
		})
	}
}

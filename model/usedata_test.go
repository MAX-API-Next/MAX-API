package model

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func quotaDataAggregateKeyForTest(quotaData *QuotaData) string {
	digest := sha256.Sum256([]byte(quotaDataCacheKey(quotaData)))
	return hex.EncodeToString(digest[:])
}

func ensureQuotaDataAggregateKeySchemaForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasColumn("quota_data", "aggregate_key") {
		require.NoError(t, db.Exec("ALTER TABLE quota_data ADD COLUMN aggregate_key TEXT").Error)
	}
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_quota_data_aggregate_key ON quota_data (aggregate_key)").Error)
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
	SaveQuotaDataCache()

	CacheQuotaDataLock.Lock()
	assert.Len(t, CacheQuotaData, 1)
	CacheQuotaDataLock.Unlock()

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	require.NoError(t, migrateQuotaDataAggregateKeys())
	SaveQuotaDataCache()

	CacheQuotaDataLock.Lock()
	assert.Empty(t, CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	var got QuotaData
	require.NoError(t, db.First(&got).Error)
	assert.Equal(t, 1, got.Count)
	assert.Equal(t, 7, got.Quota)
	assert.Equal(t, 3, got.TokenUsed)
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

	assert.True(t, db.Migrator().HasColumn("quota_data", "aggregate_key"))
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
	assert.True(t, db.Migrator().HasColumn("quota_data", "aggregate_key"))
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

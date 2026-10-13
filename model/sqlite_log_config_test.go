package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSQLiteLogConfigTest(t *testing.T) string {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousPath, previousLogType := common.SQLitePath, common.LogSqlType
	previousSQLite, previousMySQL, previousPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	previousMaster, previousRunner := common.IsMasterNode, logRetryMarkerBackfillAsyncRunner
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SQLitePath, common.LogSqlType = previousPath, previousLogType
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = previousSQLite, previousMySQL, previousPostgres
		common.IsMasterNode, logRetryMarkerBackfillAsyncRunner = previousMaster, previousRunner
		initCol()
	})
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("LOG_SQLITE_PATH", "")
	t.Setenv("QUOTA_DATA_AGGREGATE_MIGRATION_ENABLED", "false")
	dir := t.TempDir()
	common.SQLitePath = filepath.Join(dir, "ledger.sqlite")
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	common.LogSqlType = common.DatabaseTypeSQLite
	common.IsMasterNode = true
	logRetryMarkerBackfillAsyncRunner = func(fn func()) { fn() }
	return dir
}

func TestSQLiteLogConfigInitialization(t *testing.T) {
	for _, mode := range []string{"default", "legacy_local", "path_only", "local_and_path"} {
		t.Run(mode, func(t *testing.T) {
			dir := setupSQLiteLogConfigTest(t)
			if mode == "legacy_local" || mode == "local_and_path" {
				t.Setenv("LOG_SQL_DSN", "local")
			}
			logPath := filepath.Join(dir, "logs 中文 space.sqlite")
			separate := mode == "path_only" || mode == "local_and_path"
			if separate {
				t.Setenv("LOG_SQLITE_PATH", logPath)
			}
			require.NoError(t, InitDB())
			t.Cleanup(func() { require.NoError(t, closeDB(DB)) })
			require.NoError(t, InitLogDB())
			if LOG_DB != DB {
				t.Cleanup(func() { require.NoError(t, closeDB(LOG_DB)) })
			}
			require.True(t, LOG_DB.Migrator().HasTable(&Log{}))
			require.True(t, LOG_DB.Migrator().HasTable(&BillingLogReceipt{}))
			require.NoError(t, DB.Create(&User{Id: 9541, Username: "sqlite-config-user", Quota: 1000}).Error)
			log := Log{UserId: 9541, Type: LogTypeConsume, Quota: 7}
			require.NoError(t, LOG_DB.Create(&log).Error)
			var logs int64
			require.NoError(t, DB.Model(&Log{}).Count(&logs).Error)
			if separate {
				require.EqualValues(t, 0, logs, "log writes must not reach the business file")
				require.False(t, LOG_DB.Migrator().HasTable(&User{}))
				require.False(t, LOG_DB.Migrator().HasTable(&BillingSettlement{}))
				require.FileExists(t, logPath)
			} else {
				require.EqualValues(t, 1, logs)
				if mode == "default" {
					require.Same(t, DB, LOG_DB)
				}
			}
			require.NoError(t, CloseDB())
			mainSQL, err := DB.DB()
			require.NoError(t, err)
			require.Error(t, mainSQL.Ping())
			logSQL, err := LOG_DB.DB()
			require.NoError(t, err)
			require.Error(t, logSQL.Ping())
			require.NoError(t, InitDB())
			require.NoError(t, InitLogDB())
			var user User
			require.NoError(t, DB.First(&user, 9541).Error)
			require.Equal(t, int64(1000), user.Quota)
			var persisted Log
			require.NoError(t, LOG_DB.First(&persisted, log.Id).Error)
			require.Equal(t, 7, persisted.Quota)
		})
	}
}

func TestSQLiteLogConfigBackfillIdentity(t *testing.T) {
	dir := setupSQLiteLogConfigTest(t)
	require.Equal(t, logRetryMarkerBackfillOptionKey, logRetryMarkerBackfillCompletionKey())
	t.Setenv("LOG_SQL_DSN", "local")
	legacyKey := logRetryMarkerBackfillOptionKey + ":" + common.Sha1([]byte(common.DatabaseTypeSQLite+"\nlocal\n"+common.SQLitePath))
	require.Equal(t, legacyKey, logRetryMarkerBackfillCompletionKey())
	t.Setenv("LOG_SQLITE_PATH", filepath.Join(dir, "logs-one.sqlite"))
	first := logRetryMarkerBackfillCompletionKey()
	require.NotEqual(t, legacyKey, first)
	require.NotContains(t, first, dir)
	t.Setenv("LOG_SQLITE_PATH", filepath.Join(dir, "logs-two.sqlite"))
	require.NotEqual(t, first, logRetryMarkerBackfillCompletionKey(), "a new file must get its own backfill")
	t.Setenv("LOG_SQL_DSN", "")
	require.NotEqual(t, logRetryMarkerBackfillOptionKey, logRetryMarkerBackfillCompletionKey())
}

func TestSQLiteLogConfigBackfillOnFileSwitch(t *testing.T) {
	dir := setupSQLiteLogConfigTest(t)
	require.NoError(t, InitDB())
	t.Cleanup(func() { require.NoError(t, closeDB(DB)) })
	for _, name := range []string{"logs-one.sqlite", "logs-two.sqlite"} {
		path := filepath.Join(dir, name)
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&Log{}))
		log := Log{UserId: 1, Type: LogTypeConsume, Other: common.MapToJsonStr(map[string]interface{}{"retry_log": true})}
		require.NoError(t, db.Create(&log).Error)
		require.NoError(t, db.Model(&Log{}).Where("id = ?", log.Id).UpdateColumn("is_retry", false).Error)
		require.NoError(t, closeDB(db))
		t.Setenv("LOG_SQLITE_PATH", path)
		require.NoError(t, InitLogDB())
		var actual Log
		require.NoError(t, LOG_DB.First(&actual, log.Id).Error)
		require.True(t, actual.IsRetry, "each configured file requires its own legacy marker backfill")
		require.NoError(t, closeDB(LOG_DB))
	}
}

func TestSQLiteLogConfigOpenFailureDoesNotFallback(t *testing.T) {
	dir := setupSQLiteLogConfigTest(t)
	t.Setenv("LOG_SQLITE_PATH", filepath.Join(dir, "missing-directory", "logs.sqlite"))
	db, err := chooseDB("LOG_SQL_DSN", true)
	if db != nil {
		t.Cleanup(func() { _ = closeDB(db) })
	}
	require.Error(t, err, "an invalid explicit file must not open the business file")
	require.NoFileExists(t, common.SQLitePath)
}

func TestSQLiteLogConfigRemoteConflictIsRedacted(t *testing.T) {
	dir := setupSQLiteLogConfigTest(t)
	t.Setenv("LOG_SQLITE_PATH", filepath.Join(dir, "logs.sqlite"))
	for _, dsn := range []string{"postgres://synthetic:config-test-secret@127.0.0.1:1/logs", "synthetic:config-test-secret@tcp(127.0.0.1:1)/logs"} {
		t.Setenv("LOG_SQL_DSN", dsn)
		db, err := chooseDB("LOG_SQL_DSN", true)
		require.Error(t, err)
		require.Nil(t, db)
		require.Contains(t, err.Error(), "LOG_SQLITE_PATH")
		require.NotContains(t, err.Error(), "config-test-secret")
		require.NotContains(t, err.Error(), "127.0.0.1")
	}
	require.NoFileExists(t, os.Getenv("LOG_SQLITE_PATH"))
}

func TestSQLiteLogConfigInitFailureDoesNotFallback(t *testing.T) {
	for _, mode := range []string{"open", "migration", "remote_conflict"} {
		t.Run(mode, func(t *testing.T) {
			dir := setupSQLiteLogConfigTest(t)
			DB, LOG_DB = nil, nil
			path := filepath.Join(dir, "logs.sqlite")
			if mode == "open" {
				path = filepath.Join(dir, "missing-directory", "logs.sqlite")
			}
			t.Setenv("LOG_SQLITE_PATH", path)
			if mode == "remote_conflict" {
				t.Setenv("LOG_SQL_DSN", "postgres://synthetic:config-test-secret@127.0.0.1:1/logs")
			}
			require.NoError(t, InitDB())
			t.Cleanup(func() { require.NoError(t, closeDB(DB)) })
			if mode == "migration" {
				logDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
				require.NoError(t, err)
				require.NoError(t, logDB.Exec("CREATE VIEW logs AS SELECT 1 AS id").Error)
				require.NoError(t, closeDB(logDB))
			}
			err := InitLogDB()
			require.Error(t, err)
			require.Nil(t, LOG_DB, "failed startup must not silently publish the business connection")
			require.NotContains(t, err.Error(), "config-test-secret")
			mainSQL, sqlErr := DB.DB()
			require.NoError(t, sqlErr)
			require.NoError(t, mainSQL.Ping(), "a failed log connection must not close the business connection")
			if mode == "migration" {
				require.NoError(t, os.Rename(path, path+".failed"), "failed migration must release the SQLite file handle")
			}
		})
	}
}

func TestSQLiteLogConfigUsesLogDialectColumns(t *testing.T) {
	dir := setupSQLiteLogConfigTest(t)
	common.UsingSQLite, common.UsingPostgreSQL = false, true
	common.IsMasterNode = false
	t.Setenv("LOG_SQLITE_PATH", filepath.Join(dir, "logs.sqlite"))
	require.NoError(t, InitLogDB())
	t.Cleanup(func() { require.NoError(t, closeDB(LOG_DB)) })
	require.Equal(t, `"group"`, commonGroupCol)
	require.Equal(t, "`group`", logGroupCol)
	require.Equal(t, "`key`", logKeyCol)
	require.Equal(t, common.DatabaseTypeSQLite, common.LogSqlType)
	require.False(t, common.UsingSQLite, "log initialization must not change the main database flags")
	require.True(t, common.UsingPostgreSQL)
}

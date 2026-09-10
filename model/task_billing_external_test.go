package model

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Package DB handles are global: never use t.Parallel while this fixture is
// active. Only workers within a contract may concurrently use the fixed handle.
func TestTaskBillingSettlementContractSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settlement.db")), taskBillingTestGORMConfig())
	taskBillingTestCheck(t, err, "open SQLite")
	sqlDB, err := db.DB()
	taskBillingTestCheck(t, err, "access SQLite connection")
	t.Cleanup(func() { taskBillingTestCheck(t, sqlDB.Close(), "close SQLite") })
	sqlDB.SetMaxOpenConns(1)
	prepareTaskBillingContractDB(t, db, "sqlite")
	runTaskBillingSettlementContracts(t, db)
}

func TestTaskBillingSettlementContractMySQL(t *testing.T) {
	runExternalTaskBillingSettlementContract(t, "mysql", "TEST_TASK_BILLING_MYSQL_DSN")
}

func TestTaskBillingSettlementContractPostgreSQL(t *testing.T) {
	runExternalTaskBillingSettlementContract(t, "postgres", "TEST_TASK_BILLING_POSTGRES_DSN")
}

var taskBillingTestDatabaseName = regexp.MustCompile(`^maxapi_task_billing_test_[a-z0-9_]{1,32}$`)
var errTaskBillingUnsafeDatabase = errors.New("dedicated disposable task-billing database required")
var taskBillingExternalEnabled = flag.Bool("task-billing-external", false, "run the task-billing contract against dedicated disposable databases")

func taskBillingTestGORMConfig() *gorm.Config {
	// Connection errors and SQL literals can contain credentials. Never emit
	// them from the external harness, even on connection/migration failure.
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

func taskBillingTestCheck(t *testing.T, err error, stage string) {
	t.Helper()
	if err != nil {
		t.Fatalf("task-billing contract stage %s failed (error details withheld)", stage)
	}
}

// Each external invocation needs a freshly provisioned disposable database.
// Nothing is dropped: successful and partial migrations are retained for audit.
// The unique ownership table rejects a second harness targeting the same DB.
type taskBillingTestOwnership struct {
	ID int
}

func (taskBillingTestOwnership) TableName() string { return "task_billing_test_ownership" }

func openTaskBillingTestSQL(dialect, dsn string) (*sql.DB, string, error) {
	switch dialect {
	case "mysql":
		config, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return nil, "", err
		}
		if !taskBillingTestDatabaseName.MatchString(config.DBName) {
			return nil, "", errTaskBillingUnsafeDatabase
		}
		config.Timeout, config.ReadTimeout, config.WriteTimeout = 5*time.Second, 10*time.Second, 10*time.Second
		connector, err := mysqldriver.NewConnector(config)
		if err != nil {
			return nil, "", err
		}
		return sql.OpenDB(connector), config.DBName, nil
	case "postgres":
		config, err := pgx.ParseConfig(dsn)
		if err != nil {
			return nil, "", err
		}
		if !taskBillingTestDatabaseName.MatchString(config.Database) {
			return nil, "", errTaskBillingUnsafeDatabase
		}
		config.ConnectTimeout = 5 * time.Second
		config.RuntimeParams["search_path"] = "public"
		config.RuntimeParams["statement_timeout"] = "10000"
		config.RuntimeParams["lock_timeout"] = "5000"
		return stdlib.OpenDB(*config), config.Database, nil
	default:
		return nil, "", errTaskBillingUnsafeDatabase
	}
}

func runExternalTaskBillingSettlementContract(t *testing.T, dialect, envName string) {
	t.Helper()
	if !*taskBillingExternalEnabled {
		t.Skip("external contract requires -task-billing-external; NOT RUN")
	}
	dsn := strings.TrimSpace(os.Getenv(envName))
	if dsn == "" {
		t.Skip("dedicated " + envName + " is not set; external contract NOT RUN")
	}
	sqlDB, expectedName, err := openTaskBillingTestSQL(dialect, dsn)
	taskBillingTestCheck(t, err, "validate dedicated "+dialect+" DSN and test database name")
	t.Cleanup(func() { taskBillingTestCheck(t, sqlDB.Close(), "close "+dialect) })
	sqlDB.SetMaxOpenConns(4)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	taskBillingTestCheck(t, sqlDB.PingContext(ctx), "connect "+dialect)
	var name string
	var relations int
	if dialect == "mysql" {
		taskBillingTestCheck(t, sqlDB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name), "verify MySQL target")
		taskBillingTestCheck(t, sqlDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&relations), "inspect MySQL tables and views")
	} else {
		taskBillingTestCheck(t, sqlDB.QueryRowContext(ctx, "SELECT current_database()").Scan(&name), "verify PostgreSQL target")
		// Inspect every user schema, not only the search_path. This includes
		// tables, views, sequences and materialized views.
		taskBillingTestCheck(t, sqlDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'").Scan(&relations), "inspect PostgreSQL user relations")
	}
	if name != expectedName || relations != 0 {
		t.Fatal("external target identity mismatch or nonempty database; no migration performed")
	}

	var dialector gorm.Dialector
	if dialect == "mysql" {
		dialector = gormmysql.New(gormmysql.Config{Conn: sqlDB})
	} else {
		dialector = gormpostgres.New(gormpostgres.Config{Conn: sqlDB})
	}
	db, err := gorm.Open(dialector, taskBillingTestGORMConfig())
	taskBillingTestCheck(t, err, "initialize "+dialect+" GORM")
	taskBillingTestCheck(t, db.Migrator().CreateTable(&taskBillingTestOwnership{}), "claim exclusive external test ownership")
	t.Log("external schema and synthetic data are retained for audit; use a fresh disposable database for the next invocation")
	prepareTaskBillingContractDB(t, db, dialect)
	runTaskBillingSettlementContracts(t, db)
}

func prepareTaskBillingContractDB(t *testing.T, db *gorm.DB, dialect string) {
	t.Helper()
	// Create only the real models required by this seam. Do not call shared
	// truncateTables or application startup migrations against an external DB.
	for _, model := range []any{&User{}, &Token{}, &Channel{}, &Task{}, &BillingSettlement{},
		&Log{}, &BillingLogReceipt{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}} {
		taskBillingTestCheck(t, db.Migrator().CreateTable(model), "create contract schema")
	}

	oldDB, oldLogDB := DB, LOG_DB
	oldSQLite, oldMySQL, oldPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	oldRedis, oldBatch, oldExport := common.RedisEnabled, common.BatchUpdateEnabled, common.DataExportEnabled
	oldConsume, oldMemory := common.LogConsumeEnabled, common.MemoryCacheEnabled
	oldGroup, oldKey, oldTrue, oldFalse := commonGroupCol, commonKeyCol, commonTrueVal, commonFalseVal
	oldLogGroup, oldLogKey := logGroupCol, logKeyCol
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = dialect == "sqlite", dialect == "mysql", dialect == "postgres"
	common.RedisEnabled, common.BatchUpdateEnabled, common.DataExportEnabled = false, false, false
	common.LogConsumeEnabled, common.MemoryCacheEnabled = true, false
	initCol()
	// This fixture uses one DB for both handles regardless of LOG_SQL_DSN.
	logGroupCol, logKeyCol = commonGroupCol, commonKeyCol
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = oldSQLite, oldMySQL, oldPostgres
		common.RedisEnabled, common.BatchUpdateEnabled, common.DataExportEnabled = oldRedis, oldBatch, oldExport
		common.LogConsumeEnabled, common.MemoryCacheEnabled = oldConsume, oldMemory
		commonGroupCol, commonKeyCol, commonTrueVal, commonFalseVal = oldGroup, oldKey, oldTrue, oldFalse
		logGroupCol, logKeyCol = oldLogGroup, oldLogKey
	})
}

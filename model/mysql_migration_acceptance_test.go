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
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var mysqlMigrationAcceptanceDatabase = regexp.MustCompile(`^maxapi_mysql_acceptance_[a-z0-9_]{1,32}$`)

// TestMySQLStartupMigrationOnEmptyDatabase is opt-in. It intentionally
// refuses a normal application database: migration acceptance must use a
// fresh, disposable schema so a mistaken DSN cannot alter user data.
func TestMySQLStartupMigrationOnEmptyDatabase(t *testing.T) {
	dsn := strings.TrimSpace(testEnv("TEST_MYSQL_MIGRATION_DSN"))
	if dsn == "" {
		t.Skip("set TEST_MYSQL_MIGRATION_DSN to a dedicated disposable MySQL database")
	}

	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	if !mysqlMigrationAcceptanceDatabase.MatchString(config.DBName) {
		t.Fatalf("refusing MySQL migration target with unsafe database name")
	}
	if config.Net != "unix" && (config.TLS == nil || config.TLS.InsecureSkipVerify) {
		t.Fatalf("refusing MySQL migration target without certificate-validating TLS")
	}

	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(4)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var relationCount int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&relationCount))
	if relationCount != 0 {
		t.Fatalf("refusing non-empty MySQL migration target (%d tables)", relationCount)
	}

	oldDB, oldLogDB := DB, LOG_DB
	oldSQLite, oldMySQL, oldPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	oldRunner := logRetryMarkerBackfillAsyncRunner
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = false, true, false
	logRetryMarkerBackfillAsyncRunner = func(fn func()) { fn() }
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = oldSQLite, oldMySQL, oldPostgres
		logRetryMarkerBackfillAsyncRunner = oldRunner
		initCol()
	})

	require.NoError(t, migrateDB(), "first MySQL startup migration")
	require.NoError(t, migrateDB(), "second MySQL startup migration")
	for _, model := range []any{
		&User{}, &Token{}, &Channel{}, &BillingSettlement{}, &BillingPreConsumeSelection{},
		&SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &Log{}, &BillingLogReceipt{},
	} {
		require.True(t, db.Migrator().HasTable(model), "expected migrated table for %T", model)
	}
	require.True(t, db.Migrator().HasColumn(&User{}, "normalized_email"))
	require.True(t, db.Migrator().HasColumn(&UserSubscription{}, "allow_wallet_overflow"))
	require.True(t, db.Migrator().HasColumn(&SubscriptionPlan{}, "allow_wallet_overflow"))
	require.True(t, db.Migrator().HasIndex(&BillingSettlement{}, "idx_billing_settlement_reconciliation"))
}

func testEnv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

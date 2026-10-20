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
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"errors"
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

func mysqlAcceptanceDatabaseAllowed(name, override string, pattern *regexp.Regexp) bool {
	return pattern.MatchString(name) || (override != "" && name == override)
}

func TestMySQLSchemaCompatibilityReadOnly(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_SCHEMA_AUDIT_DSN"))
	if dsn == "" {
		t.Skip("set TEST_MYSQL_SCHEMA_AUDIT_DSN to run the read-only MySQL schema audit")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	allowedDatabase := strings.TrimSpace(os.Getenv("TEST_MYSQL_SCHEMA_AUDIT_DATABASE"))
	if !mysqlAcceptanceDatabaseAllowed(config.DBName, allowedDatabase, mysqlMigrationAcceptanceDatabase) {
		t.Fatalf("refusing MySQL schema audit target with unsafe database name")
	}

	db, sqlDB := openMySQLAcceptanceDB(t, dsn)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, model := range []any{
		&User{}, &Token{}, &Channel{}, &Option{}, &BillingSettlement{}, &BillingPreConsumeSelection{},
		&SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &Log{}, &BillingLogReceipt{},
	} {
		require.True(t, db.Migrator().HasTable(model), "expected existing table for %T", model)
	}
	require.True(t, db.Migrator().HasColumn(&User{}, "normalized_email"))
	require.True(t, db.Migrator().HasColumn(&UserSubscription{}, "allow_wallet_overflow"))
	require.True(t, db.Migrator().HasColumn(&SubscriptionPlan{}, "allow_wallet_overflow"))
	require.True(t, db.Migrator().HasIndex(&BillingSettlement{}, "idx_billing_settlement_reconciliation"))
}

// TestMySQLStartupMigrationOnEmptyDatabase is opt-in. It intentionally
// refuses a normal application database: migration acceptance must use a
// fresh, disposable schema so a mistaken DSN cannot alter user data. A
// non-standard schema name requires an explicit TEST_MYSQL_MIGRATION_DATABASE
// value in the same process and is still required to be empty.
func TestMySQLStartupMigrationOnEmptyDatabase(t *testing.T) {
	dsn := strings.TrimSpace(testEnv("TEST_MYSQL_MIGRATION_DSN"))
	if dsn == "" {
		t.Skip("set TEST_MYSQL_MIGRATION_DSN to a dedicated disposable MySQL database")
	}

	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	allowedDatabase := strings.TrimSpace(os.Getenv("TEST_MYSQL_MIGRATION_DATABASE"))
	if !mysqlAcceptanceDatabaseAllowed(config.DBName, allowedDatabase, mysqlMigrationAcceptanceDatabase) {
		t.Fatalf("refusing MySQL migration target with unsafe database name")
	}
	if strings.TrimSpace(os.Getenv("TEST_MYSQL_TLS_CERT_SHA256")) == "" && config.Net != "unix" && (config.TLS == nil || config.TLS.InsecureSkipVerify) {
		t.Fatalf("refusing MySQL migration target without certificate-validating TLS")
	}

	db, sqlDB := openMySQLAcceptanceDB(t, dsn)
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

func TestMySQLAcceptanceDatabaseGuardRequiresExplicitOverride(t *testing.T) {
	tests := []struct {
		name    string
		pattern *regexp.Regexp
		valid   string
	}{
		{name: "task billing", pattern: taskBillingTestDatabaseName, valid: "maxapi_task_billing_test_fixture"},
		{name: "migration", pattern: mysqlMigrationAcceptanceDatabase, valid: "maxapi_mysql_acceptance_fixture"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.False(t, mysqlAcceptanceDatabaseAllowed("", "", test.pattern))
			require.False(t, mysqlAcceptanceDatabaseAllowed("production", "", test.pattern))
			require.True(t, mysqlAcceptanceDatabaseAllowed("dedicated_override", "dedicated_override", test.pattern))
			require.True(t, mysqlAcceptanceDatabaseAllowed(test.valid, "", test.pattern))
		})
	}
}

func testEnv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func openMySQLAcceptanceDB(t *testing.T, dsn string) (*gorm.DB, *sql.DB) {
	t.Helper()
	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	if fingerprint := strings.TrimSpace(os.Getenv("TEST_MYSQL_TLS_CERT_SHA256")); fingerprint != "" {
		expected, err := hex.DecodeString(fingerprint)
		require.NoError(t, err)
		require.Len(t, expected, sha256.Size)
		config.TLS = &tls.Config{
			//nolint:gosec // VerifyConnection enforces the configured exact leaf SHA-256 pin.
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return errors.New("mysql server returned no certificate")
				}
				actual := sha256.Sum256(state.PeerCertificates[0].Raw)
				if !hmac.Equal(actual[:], expected) {
					return errors.New("mysql certificate pin mismatch")
				}
				return nil
			},
		}
	} else if config.Net != "unix" && (config.TLS == nil || config.TLS.InsecureSkipVerify) {
		t.Fatalf("refusing MySQL target without certificate-validating TLS")
	}
	connector, err := mysqldriver.NewConnector(config)
	require.NoError(t, err)
	sqlDB := sql.OpenDB(connector)
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db, sqlDB
}

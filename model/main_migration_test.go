package model

import (
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenQuotaFieldsUseInt64(t *testing.T) {
	tokenType := reflect.TypeOf(Token{})
	for _, fieldName := range []string{"RemainQuota", "UsedQuota"} {
		field, ok := tokenType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("Token.%s is missing", fieldName)
		}
		if field.Type.Kind() != reflect.Int64 {
			t.Fatalf("Token.%s kind = %s, want int64", fieldName, field.Type.Kind())
		}
	}
}

func TestIsZeroColumnDefault(t *testing.T) {
	tests := []struct {
		name  string
		value sql.NullString
		want  bool
	}{
		{name: "mysql numeric zero", value: sql.NullString{String: "0", Valid: true}, want: true},
		{name: "postgres bigint zero", value: sql.NullString{String: "0::bigint", Valid: true}, want: true},
		{name: "postgres quoted bigint zero", value: sql.NullString{String: "'0'::bigint", Valid: true}, want: true},
		{name: "missing default", value: sql.NullString{}, want: false},
		{name: "non-zero default", value: sql.NullString{String: "10", Valid: true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isZeroColumnDefault(tt.value); got != tt.want {
				t.Fatalf("isZeroColumnDefault(%q) = %v, want %v", tt.value.String, got, tt.want)
			}
		})
	}
}

func TestStartupMigrationSchemaChecksUseModelValues(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}

	source := string(data)
	forbiddenSnippets := []string{
		`.HasColumn("users",`,
		`.HasColumn(tableName,`,
		`.HasIndex("users",`,
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(source, snippet) {
			t.Fatalf("startup migrations must pass model values to GORM schema checks; found %q", snippet)
		}
	}
}

func TestMigrateDBRunsCoreAutoMigrateBeforeSubscriptionPlanPriceMigrationFailure(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	previousSQLite := common.UsingSQLite
	previousMySQL := common.UsingMySQL
	previousPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.UsingSQLite = previousSQLite
		common.UsingMySQL = previousMySQL
		common.UsingPostgreSQL = previousPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	DB = db
	LOG_DB = db
	common.UsingSQLite = false
	common.UsingMySQL = true
	common.UsingPostgreSQL = false

	if err := db.AutoMigrate(&SubscriptionPlan{}); err != nil {
		t.Fatalf("failed to create subscription plans table: %v", err)
	}

	if err := migrateDB(); err == nil {
		t.Fatal("expected subscription price migration failure to be returned")
	}
	if !db.Migrator().HasTable(&Channel{}) {
		t.Fatal("expected core schema auto-migration to run before subscription price migration failure")
	}
}

func TestUserSubscriptionPolicyMigrationAddsSafeDefaultsForLegacyRows(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.Exec(`CREATE TABLE user_subscriptions (id integer primary key, user_id integer, status varchar(32), end_time bigint, updated_at bigint)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_subscriptions (id, user_id, status, end_time) VALUES (1, 7, 'active', 9999999999)`).Error)

	require.NoError(t, ensureUserSubscriptionPolicyColumns(true, false, false))
	require.NoError(t, migrateUserSubscriptionPolicyDefaults(true, false, false))

	var got struct {
		AllowWalletOverflow bool   `gorm:"column:allow_wallet_overflow"`
		DowngradeGroup      string `gorm:"column:downgrade_group"`
	}
	require.NoError(t, db.Table("user_subscriptions").First(&got, 1).Error)
	assert.True(t, got.AllowWalletOverflow)
	assert.Equal(t, "", got.DowngradeGroup)
}

func TestSubscriptionPolicyMigrationUsesConfiguredDatabaseFlags(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	previousSQLite := common.UsingSQLite
	previousMySQL := common.UsingMySQL
	previousPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.UsingSQLite = previousSQLite
		common.UsingMySQL = previousMySQL
		common.UsingPostgreSQL = previousPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	// Deliberately keep the SQLite driver while selecting the configured MySQL
	// branch. The migration must follow the project flags rather than infer a
	// different dialect from DB.Dialector.Name().
	common.UsingSQLite = false
	common.UsingMySQL = true
	common.UsingPostgreSQL = false
	require.NoError(t, db.Exec(`CREATE TABLE user_subscriptions (id integer primary key)`).Error)
	require.NoError(t, ensureUserSubscriptionPolicyColumns(true, false, false))
	assert.True(t, db.Migrator().HasColumn("user_subscriptions", "allow_wallet_overflow"))
	assert.True(t, db.Migrator().HasColumn("user_subscriptions", "downgrade_group"))
	var tableSQL string
	require.NoError(t, db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'user_subscriptions'`).Scan(&tableSQL).Error)
	assert.Contains(t, tableSQL, "allow_wallet_overflow` boolean NOT NULL DEFAULT TRUE")
	assert.NotContains(t, tableSQL, "allow_wallet_overflow` numeric NOT NULL DEFAULT 1")
}

func TestSubscriptionPolicyMigrationRejectsMissingDatabaseFlags(t *testing.T) {
	previousDB := DB
	previousSQLite := common.UsingSQLite
	previousMySQL := common.UsingMySQL
	previousPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		DB = previousDB
		common.UsingSQLite = previousSQLite
		common.UsingMySQL = previousMySQL
		common.UsingPostgreSQL = previousPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.UsingSQLite = false
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	err = execSubscriptionPolicyColumnDDL("user_subscriptions", "allow_wallet_overflow")
	require.ErrorContains(t, err, "database dialect is not configured")
}

func TestSubscriptionPlanPolicyMigrationIsIdempotentOnSQLite(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	previousSQLite := common.UsingSQLite
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.UsingSQLite = previousSQLite
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	require.NoError(t, db.Exec(`CREATE TABLE subscription_plans (id integer primary key, title varchar(128) NOT NULL, price_amount decimal(10,6) NOT NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO subscription_plans (id, title, price_amount) VALUES (1, 'legacy', 1)`).Error)

	require.NoError(t, ensureSubscriptionPlanTableSQLite())
	require.NoError(t, migrateSubscriptionPlanPolicyDefaults(true, false, false))
	require.NoError(t, ensureSubscriptionPlanTableSQLite())
	require.NoError(t, migrateSubscriptionPlanPolicyDefaults(true, true, true))

	var got struct {
		AllowWalletOverflow *bool  `gorm:"column:allow_wallet_overflow"`
		DowngradeGroup      string `gorm:"column:downgrade_group"`
	}
	require.NoError(t, db.Table("subscription_plans").First(&got, 1).Error)
	require.NotNil(t, got.AllowWalletOverflow)
	assert.True(t, *got.AllowWalletOverflow)
	assert.Equal(t, "", got.DowngradeGroup)
}

func TestUserSubscriptionPolicyMigrationPreservesExplicitFalse(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.Exec(`CREATE TABLE user_subscriptions (
		id integer primary key,
		user_id integer,
		status varchar(32),
		end_time bigint,
		allow_wallet_overflow numeric,
		downgrade_group varchar(64),
		updated_at bigint
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_subscriptions (id, user_id, status, end_time, allow_wallet_overflow, downgrade_group) VALUES
		(1, 7, 'active', 9999999999, 0, NULL),
		(2, 8, 'active', 9999999999, NULL, 'vip')`).Error)

	require.NoError(t, migrateUserSubscriptionPolicyDefaults(true, true, true))

	var rows []struct {
		AllowWalletOverflow *bool  `gorm:"column:allow_wallet_overflow"`
		DowngradeGroup      string `gorm:"column:downgrade_group"`
	}
	require.NoError(t, db.Table("user_subscriptions").Order("id").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.NotNil(t, rows[0].AllowWalletOverflow)
	assert.False(t, *rows[0].AllowWalletOverflow)
	assert.Equal(t, "", rows[0].DowngradeGroup)
	require.NotNil(t, rows[1].AllowWalletOverflow)
	assert.True(t, *rows[1].AllowWalletOverflow)
	assert.Equal(t, "vip", rows[1].DowngradeGroup)
}

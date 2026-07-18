package model

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var commonGroupCol string
var commonKeyCol string
var commonTrueVal string
var commonFalseVal string

var logKeyCol string
var logGroupCol string

const logRetryMarkerBackfillOptionKey = "LogRetryMarkerBackfillCompletedV2"

var logRetryMarkerBackfillMu sync.Mutex

var logRetryMarkerBackfillAsyncRunner = func(fn func()) {
	gopool.Go(fn)
}

type userOAuthIdentityMigration struct {
	column               string
	indexName            string
	mysqlGeneratedColumn string
}

var userOAuthIdentityMigrations = []userOAuthIdentityMigration{
	{column: "wechat_id", indexName: "ux_users_wechat_id", mysqlGeneratedColumn: "wechat_id_unique"},
	{column: "github_id", indexName: "ux_users_github_id", mysqlGeneratedColumn: "github_id_unique"},
	{column: "discord_id", indexName: "ux_users_discord_id", mysqlGeneratedColumn: "discord_id_unique"},
	{column: "oidc_id", indexName: "ux_users_oidc_id", mysqlGeneratedColumn: "oidc_id_unique"},
	{column: "telegram_id", indexName: "ux_users_telegram_id", mysqlGeneratedColumn: "telegram_id_unique"},
	{column: "linux_do_id", indexName: "ux_users_linux_do_id", mysqlGeneratedColumn: "linux_do_id_unique"},
}

func logRetryMarkerBackfillCompletionKey() string {
	logDSN := strings.TrimSpace(os.Getenv("LOG_SQL_DSN"))
	if logDSN == "" {
		return logRetryMarkerBackfillOptionKey
	}

	if strings.HasPrefix(logDSN, "local") {
		logDSN = logDSN + "\n" + common.SQLitePath
	}

	fingerprint := common.Sha1([]byte(common.LogSqlType + "\n" + logDSN))
	return logRetryMarkerBackfillOptionKey + ":" + fingerprint
}

func initCol() {
	// init common column names
	if common.UsingPostgreSQL {
		commonGroupCol = `"group"`
		commonKeyCol = `"key"`
		commonTrueVal = "true"
		commonFalseVal = "false"
	} else {
		commonGroupCol = "`group`"
		commonKeyCol = "`key`"
		commonTrueVal = "1"
		commonFalseVal = "0"
	}
	if os.Getenv("LOG_SQL_DSN") != "" {
		switch common.LogSqlType {
		case common.DatabaseTypePostgreSQL:
			logGroupCol = `"group"`
			logKeyCol = `"key"`
		default:
			logGroupCol = commonGroupCol
			logKeyCol = commonKeyCol
		}
	} else {
		// LOG_SQL_DSN 为空时，日志数据库与主数据库相同
		if common.UsingPostgreSQL {
			logGroupCol = `"group"`
			logKeyCol = `"key"`
		} else {
			logGroupCol = commonGroupCol
			logKeyCol = commonKeyCol
		}
	}
	// log sql type and database type
	//common.SysLog("Using Log SQL Type: " + common.LogSqlType)
}

func quoteDBIdentifier(identifier string) string {
	if common.UsingPostgreSQL {
		return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

var DB *gorm.DB

var LOG_DB *gorm.DB

func createRootAccountIfNeed() error {
	var user User
	//if user.Status != common.UserStatusEnabled {
	if err := DB.First(&user).Error; err != nil {
		common.SysLog("no user exists, create a root user for you: username is root, password is 123456")
		hashedPassword, err := common.Password2Hash("123456")
		if err != nil {
			return err
		}
		rootUser := User{
			Username:    "root",
			Password:    hashedPassword,
			Role:        common.RoleRootUser,
			Status:      common.UserStatusEnabled,
			DisplayName: "Root User",
			AccessToken: nil,
			Quota:       100000000,
		}
		DB.Create(&rootUser)
	}
	return nil
}

func CheckSetup() {
	setup := GetSetup()
	if setup == nil {
		// No setup record exists, check if we have a root user
		if RootUserExists() {
			common.SysLog("system is not initialized, but root user exists")
			// Create setup record
			newSetup := Setup{
				Version:       common.Version,
				InitializedAt: time.Now().Unix(),
			}
			err := DB.Create(&newSetup).Error
			if err != nil {
				common.SysLog("failed to create setup record: " + err.Error())
			}
			constant.Setup = true
		} else {
			common.SysLog("system is not initialized and no root user exists")
			constant.Setup = false
		}
	} else {
		// Setup record exists, system is initialized
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.Setup = true
	}
}

func chooseDB(envName string, isLog bool) (*gorm.DB, error) {
	defer func() {
		initCol()
	}()
	dsn := os.Getenv(envName)
	if dsn != "" {
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			// Use PostgreSQL
			common.SysLog("using PostgreSQL as database")
			if !isLog {
				common.UsingPostgreSQL = true
			} else {
				common.LogSqlType = common.DatabaseTypePostgreSQL
			}
			return gorm.Open(postgres.New(postgres.Config{
				DSN:                  dsn,
				PreferSimpleProtocol: true, // disables implicit prepared statement usage
			}), &gorm.Config{
				PrepareStmt: true, // precompile SQL
			})
		}
		if strings.HasPrefix(dsn, "local") {
			common.SysLog("SQL_DSN not set, using SQLite as database")
			if !isLog {
				common.UsingSQLite = true
			} else {
				common.LogSqlType = common.DatabaseTypeSQLite
			}
			return gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{
				PrepareStmt: true, // precompile SQL
			})
		}
		// Use MySQL
		common.SysLog("using MySQL as database")
		// check parseTime
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		if !isLog {
			common.UsingMySQL = true
		} else {
			common.LogSqlType = common.DatabaseTypeMySQL
		}
		return gorm.Open(mysql.Open(dsn), &gorm.Config{
			PrepareStmt: true, // precompile SQL
		})
	}
	// Use SQLite
	common.SysLog("SQL_DSN not set, using SQLite as database")
	common.UsingSQLite = true
	return gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{
		PrepareStmt: true, // precompile SQL
	})
}

func InitDB() (err error) {
	db, err := chooseDB("SQL_DSN", false)
	if err == nil {
		if common.DebugEnabled {
			db = db.Debug()
		}
		DB = db
		// MySQL charset/collation startup check: ensure Chinese-capable charset
		if common.UsingMySQL {
			if err := checkMySQLChineseSupport(DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		if !common.IsMasterNode {
			return nil
		}
		if common.UsingMySQL {
			//_, _ = sqlDB.Exec("ALTER TABLE channels MODIFY model_mapping TEXT;") // TODO: delete this line when most users have upgraded
		}
		common.SysLog("database migration started")
		err = migrateDB()
		return err
	} else {
		common.FatalLog(err)
	}
	return err
}

func InitLogDB() (err error) {
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		return
	}
	db, err := chooseDB("LOG_SQL_DSN", true)
	if err == nil {
		if common.DebugEnabled {
			db = db.Debug()
		}
		LOG_DB = db
		// If log DB is MySQL, also ensure Chinese-capable charset
		if common.LogSqlType == common.DatabaseTypeMySQL {
			if err := checkMySQLChineseSupport(LOG_DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := LOG_DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		if !common.IsMasterNode {
			return nil
		}
		common.SysLog("database migration started")
		err = migrateLOGDB()
		return err
	} else {
		common.FatalLog(err)
	}
	return err
}

func migrateDB() error {
	// Migrate price_amount column from float/double to decimal for existing tables
	migrateSubscriptionPlanPriceAmount()
	// Migrate model_limits column from varchar to text for existing tables
	if err := migrateTokenModelLimitsToText(); err != nil {
		return err
	}
	if err := migrateUserQuotaColumnsToBigInt(); err != nil {
		return err
	}
	if err := migrateTokenQuotaColumnsToBigInt(); err != nil {
		return err
	}

	err := DB.AutoMigrate(
		&Channel{},
		&Token{},
		&User{},
		&PasskeyCredential{},
		&Option{},
		&Redemption{},
		&Ability{},
		&Log{},
		&Midjourney{},
		&TopUp{},
		&QuotaData{},
		&QuotaDataSnapshot{},
		&Task{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&Setup{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Checkin{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemTask{},
		&SystemInstance{},
	)
	if err != nil {
		return err
	}
	if err := migrateQuotaDataAggregateKeys(); err != nil {
		return err
	}
	if err := migrateUserOAuthIdentityConstraints(); err != nil {
		return err
	}
	if err := backfillUserNormalizedEmails(); err != nil {
		return err
	}
	if common.UsingSQLite {
		if err := ensureSubscriptionPlanTableSQLite(); err != nil {
			return err
		}
	} else {
		if err := DB.AutoMigrate(&SubscriptionPlan{}); err != nil {
			return err
		}
	}
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		scheduleLogRetryMarkerBackfill()
	}
	return nil
}

func migrateDBFast() error {
	if err := migrateUserQuotaColumnsToBigInt(); err != nil {
		return err
	}
	if err := migrateTokenQuotaColumnsToBigInt(); err != nil {
		return err
	}

	var wg sync.WaitGroup

	migrations := []struct {
		model interface{}
		name  string
	}{
		{&Channel{}, "Channel"},
		{&Token{}, "Token"},
		{&User{}, "User"},
		{&PasskeyCredential{}, "PasskeyCredential"},
		{&Option{}, "Option"},
		{&Redemption{}, "Redemption"},
		{&Ability{}, "Ability"},
		{&Log{}, "Log"},
		{&Midjourney{}, "Midjourney"},
		{&TopUp{}, "TopUp"},
		{&QuotaData{}, "QuotaData"},
		{&QuotaDataSnapshot{}, "QuotaDataSnapshot"},
		{&Task{}, "Task"},
		{&Model{}, "Model"},
		{&Vendor{}, "Vendor"},
		{&PrefillGroup{}, "PrefillGroup"},
		{&Setup{}, "Setup"},
		{&TwoFA{}, "TwoFA"},
		{&TwoFABackupCode{}, "TwoFABackupCode"},
		{&Checkin{}, "Checkin"},
		{&SubscriptionOrder{}, "SubscriptionOrder"},
		{&UserSubscription{}, "UserSubscription"},
		{&SubscriptionPreConsumeRecord{}, "SubscriptionPreConsumeRecord"},
		{&CustomOAuthProvider{}, "CustomOAuthProvider"},
		{&UserOAuthBinding{}, "UserOAuthBinding"},
		{&PerfMetric{}, "PerfMetric"},
		{&SystemTask{}, "SystemTask"},
		{&SystemInstance{}, "SystemInstance"},
	}
	// 动态计算migration数量，确保errChan缓冲区足够大
	errChan := make(chan error, len(migrations))

	for _, m := range migrations {
		wg.Add(1)
		go func(model interface{}, name string) {
			defer wg.Done()
			if err := DB.AutoMigrate(model); err != nil {
				errChan <- fmt.Errorf("failed to migrate %s: %v", name, err)
			}
		}(m.model, m.name)
	}

	// Wait for all migrations to complete
	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	if err := migrateQuotaDataAggregateKeys(); err != nil {
		return err
	}
	if err := migrateUserOAuthIdentityConstraints(); err != nil {
		return err
	}
	if err := backfillUserNormalizedEmails(); err != nil {
		return err
	}
	if common.UsingSQLite {
		if err := ensureSubscriptionPlanTableSQLite(); err != nil {
			return err
		}
	} else {
		if err := DB.AutoMigrate(&SubscriptionPlan{}); err != nil {
			return err
		}
	}
	common.SysLog("database migrated")
	return nil
}

func backfillUserNormalizedEmails() error {
	if DB == nil || !DB.Migrator().HasTable(&User{}) || !DB.Migrator().HasColumn(&User{}, "normalized_email") {
		return nil
	}
	result := DB.Model(&User{}).
		Where("email <> ? AND (normalized_email = ? OR normalized_email IS NULL)", "", "").
		Update("normalized_email", gorm.Expr("LOWER(TRIM(email))"))
	if result.Error != nil {
		return fmt.Errorf("failed to backfill user normalized emails: %w", result.Error)
	}
	return nil
}

func migrateUserOAuthIdentityConstraints() error {
	if DB == nil || !DB.Migrator().HasTable(&User{}) {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		for _, identity := range userOAuthIdentityMigrations {
			if !tx.Migrator().HasColumn("users", identity.column) {
				continue
			}
			quotedColumn := quoteDBIdentifier(identity.column)
			if err := tx.Table("users").
				Where(quotedColumn+" = ?", "").
				Update(identity.column, nil).Error; err != nil {
				return fmt.Errorf("failed to null empty users.%s values: %w", identity.column, err)
			}
			if err := clearDuplicateUserOAuthIdentityTx(tx, identity); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, identity := range userOAuthIdentityMigrations {
		if !DB.Migrator().HasColumn("users", identity.column) {
			continue
		}
		if err := ensureUserOAuthIdentityUniqueIndex(identity); err != nil {
			return err
		}
	}
	return nil
}

func clearDuplicateUserOAuthIdentityTx(tx *gorm.DB, identity userOAuthIdentityMigration) error {
	values, err := duplicateUserOAuthIdentityValuesTx(tx, identity.column)
	if err != nil {
		return err
	}
	quotedColumn := quoteDBIdentifier(identity.column)
	for _, value := range values {
		var ids []int
		rows, err := tx.Table("users").
			Select("id").
			Where(quotedColumn+" = ?", value).
			Order("CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END ASC, id ASC").
			Rows()
		if err != nil {
			return fmt.Errorf("failed to load duplicate users.%s ids: %w", identity.column, err)
		}
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(ids) <= 1 {
			continue
		}
		if err := tx.Table("users").
			Where("id IN ?", ids[1:]).
			Update(identity.column, nil).Error; err != nil {
			return fmt.Errorf("failed to clear duplicate users.%s values: %w", identity.column, err)
		}
		common.SysLog(fmt.Sprintf("cleared %d duplicate users.%s OAuth identities during migration", len(ids)-1, identity.column))
	}
	return nil
}

func duplicateUserOAuthIdentityValuesTx(tx *gorm.DB, column string) ([]string, error) {
	quotedColumn := quoteDBIdentifier(column)
	rows, err := tx.Raw(fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s IS NOT NULL AND %s <> ? GROUP BY %s HAVING COUNT(*) > 1",
		quotedColumn,
		quoteDBIdentifier("users"),
		quotedColumn,
		quotedColumn,
		quotedColumn,
	), "").Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect duplicate users.%s values: %w", column, err)
	}
	defer rows.Close()

	values := make([]string, 0)
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		if value.Valid && value.String != "" {
			values = append(values, value.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func ensureUserOAuthIdentityUniqueIndex(identity userOAuthIdentityMigration) error {
	if DB.Migrator().HasIndex("users", identity.indexName) {
		return nil
	}
	if common.UsingMySQL {
		if !DB.Migrator().HasColumn("users", identity.mysqlGeneratedColumn) {
			addColumnSQL := fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s varchar(256) GENERATED ALWAYS AS (NULLIF(%s, '')) STORED",
				quoteDBIdentifier("users"),
				quoteDBIdentifier(identity.mysqlGeneratedColumn),
				quoteDBIdentifier(identity.column),
			)
			if err := DB.Exec(addColumnSQL).Error; err != nil {
				return fmt.Errorf("failed to add users.%s generated column: %w", identity.mysqlGeneratedColumn, err)
			}
		}
		createIndexSQL := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
			quoteDBIdentifier(identity.indexName),
			quoteDBIdentifier("users"),
			quoteDBIdentifier(identity.mysqlGeneratedColumn),
		)
		if err := DB.Exec(createIndexSQL).Error; err != nil {
			return fmt.Errorf("failed to create unique OAuth identity index %s: %w", identity.indexName, err)
		}
		return nil
	}

	quotedColumn := quoteDBIdentifier(identity.column)
	createIndexSQL := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s) WHERE %s IS NOT NULL AND %s <> ''",
		quoteDBIdentifier(identity.indexName),
		quoteDBIdentifier("users"),
		quotedColumn,
		quotedColumn,
		quotedColumn,
	)
	if err := DB.Exec(createIndexSQL).Error; err != nil {
		return fmt.Errorf("failed to create unique OAuth identity index %s: %w", identity.indexName, err)
	}
	return nil
}

func migrateLOGDB() error {
	var err error
	if err = LOG_DB.AutoMigrate(&Log{}); err != nil {
		return err
	}
	scheduleLogRetryMarkerBackfill()
	return nil
}

func scheduleLogRetryMarkerBackfill() {
	if LOG_DB == nil || !LOG_DB.Migrator().HasColumn(&Log{}, "is_retry") {
		return
	}
	if completed, err := isLogRetryMarkerBackfillCompleted(); err == nil && completed {
		return
	} else if err != nil {
		common.SysLog("failed to check log retry marker backfill status: " + err.Error())
	}

	logRetryMarkerBackfillAsyncRunner(func() {
		common.SysLog("log retry marker backfill started")
		if err := backfillLogRetryMarker(); err != nil {
			common.SysLog("log retry marker backfill failed: " + err.Error())
			return
		}
		common.SysLog("log retry marker backfill finished")
	})
}

func backfillLogRetryMarker() error {
	logRetryMarkerBackfillMu.Lock()
	defer logRetryMarkerBackfillMu.Unlock()

	if LOG_DB == nil || !LOG_DB.Migrator().HasColumn(&Log{}, "is_retry") {
		return nil
	}
	if completed, err := isLogRetryMarkerBackfillCompleted(); err == nil && completed {
		return nil
	} else if err != nil {
		common.SysLog("failed to check log retry marker backfill status: " + err.Error())
	}

	const batchSize = 500
	var lastId int
	for {
		var logs []Log
		if err := LOG_DB.
			Select("id", "other", "is_retry", "is_error_retry", "is_empty_retry").
			Where("id > ? AND (is_retry = ? OR is_error_retry = ? OR is_empty_retry = ?)", lastId, false, false, false).
			Order("id asc").
			Limit(batchSize).
			Find(&logs).Error; err != nil {
			return err
		}
		if len(logs) == 0 {
			return markLogRetryMarkerBackfillCompleted()
		}

		for _, log := range logs {
			lastId = log.Id
			errorRetry, emptyRetry := logOtherRetryMarkers(log.Other)
			if log.IsRetry == (errorRetry || emptyRetry) &&
				log.IsErrorRetry == errorRetry &&
				log.IsEmptyRetry == emptyRetry {
				continue
			}
			if err := LOG_DB.Model(&Log{}).
				Where("id = ?", log.Id).
				Updates(map[string]interface{}{
					"is_retry":       errorRetry || emptyRetry,
					"is_error_retry": errorRetry,
					"is_empty_retry": emptyRetry,
				}).Error; err != nil {
				return err
			}
		}
	}
}

func isLogRetryMarkerBackfillCompleted() (bool, error) {
	if DB == nil || !DB.Migrator().HasTable(&Option{}) {
		return false, nil
	}

	var option Option
	err := DB.First(&option, commonKeyCol+" = ?", logRetryMarkerBackfillCompletionKey()).Error
	if err == nil {
		return option.Value == "true", nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func markLogRetryMarkerBackfillCompleted() error {
	if DB == nil || !DB.Migrator().HasTable(&Option{}) {
		return nil
	}

	markerKey := logRetryMarkerBackfillCompletionKey()
	option := Option{Key: markerKey}
	if err := DB.FirstOrCreate(&option, Option{Key: markerKey}).Error; err != nil {
		return err
	}
	option.Value = "true"
	return DB.Save(&option).Error
}

type sqliteColumnDef struct {
	Name string
	DDL  string
}

func ensureSubscriptionPlanTableSQLite() error {
	if !common.UsingSQLite {
		return nil
	}
	tableName := "subscription_plans"
	if !DB.Migrator().HasTable(tableName) {
		createSQL := `CREATE TABLE ` + "`" + tableName + "`" + ` (
` + "`id`" + ` integer,
` + "`title`" + ` varchar(128) NOT NULL,
` + "`subtitle`" + ` varchar(255) DEFAULT '',
` + "`price_amount`" + ` decimal(10,6) NOT NULL,
` + "`currency`" + ` varchar(8) NOT NULL DEFAULT 'USD',
` + "`duration_unit`" + ` varchar(16) NOT NULL DEFAULT 'month',
` + "`duration_value`" + ` integer NOT NULL DEFAULT 1,
` + "`custom_seconds`" + ` bigint NOT NULL DEFAULT 0,
` + "`enabled`" + ` numeric DEFAULT 1,
` + "`sort_order`" + ` integer DEFAULT 0,
` + "`allow_balance_pay`" + ` numeric DEFAULT 1,
` + "`stripe_price_id`" + ` varchar(128) DEFAULT '',
` + "`creem_product_id`" + ` varchar(128) DEFAULT '',
` + "`waffo_pancake_product_id`" + ` varchar(128) DEFAULT '',
` + "`max_purchase_per_user`" + ` integer DEFAULT 0,
` + "`upgrade_group`" + ` varchar(64) DEFAULT '',
` + "`total_amount`" + ` bigint NOT NULL DEFAULT 0,
` + "`quota_reset_period`" + ` varchar(16) DEFAULT 'never',
` + "`quota_reset_custom_seconds`" + ` bigint DEFAULT 0,
` + "`created_at`" + ` bigint,
` + "`updated_at`" + ` bigint,
PRIMARY KEY (` + "`id`" + `)
)`
		return DB.Exec(createSQL).Error
	}
	var cols []struct {
		Name string `gorm:"column:name"`
	}
	if err := DB.Raw("PRAGMA table_info(`" + tableName + "`)").Scan(&cols).Error; err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		existing[c.Name] = struct{}{}
	}
	required := []sqliteColumnDef{
		{Name: "title", DDL: "`title` varchar(128) NOT NULL"},
		{Name: "subtitle", DDL: "`subtitle` varchar(255) DEFAULT ''"},
		{Name: "price_amount", DDL: "`price_amount` decimal(10,6) NOT NULL"},
		{Name: "currency", DDL: "`currency` varchar(8) NOT NULL DEFAULT 'USD'"},
		{Name: "duration_unit", DDL: "`duration_unit` varchar(16) NOT NULL DEFAULT 'month'"},
		{Name: "duration_value", DDL: "`duration_value` integer NOT NULL DEFAULT 1"},
		{Name: "custom_seconds", DDL: "`custom_seconds` bigint NOT NULL DEFAULT 0"},
		{Name: "enabled", DDL: "`enabled` numeric DEFAULT 1"},
		{Name: "sort_order", DDL: "`sort_order` integer DEFAULT 0"},
		{Name: "allow_balance_pay", DDL: "`allow_balance_pay` numeric DEFAULT 1"},
		{Name: "stripe_price_id", DDL: "`stripe_price_id` varchar(128) DEFAULT ''"},
		{Name: "creem_product_id", DDL: "`creem_product_id` varchar(128) DEFAULT ''"},
		{Name: "waffo_pancake_product_id", DDL: "`waffo_pancake_product_id` varchar(128) DEFAULT ''"},
		{Name: "max_purchase_per_user", DDL: "`max_purchase_per_user` integer DEFAULT 0"},
		{Name: "upgrade_group", DDL: "`upgrade_group` varchar(64) DEFAULT ''"},
		{Name: "total_amount", DDL: "`total_amount` bigint NOT NULL DEFAULT 0"},
		{Name: "quota_reset_period", DDL: "`quota_reset_period` varchar(16) DEFAULT 'never'"},
		{Name: "quota_reset_custom_seconds", DDL: "`quota_reset_custom_seconds` bigint DEFAULT 0"},
		{Name: "created_at", DDL: "`created_at` bigint"},
		{Name: "updated_at", DDL: "`updated_at` bigint"},
	}
	for _, col := range required {
		if _, ok := existing[col.Name]; ok {
			continue
		}
		if err := DB.Exec("ALTER TABLE `" + tableName + "` ADD COLUMN " + col.DDL).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateTokenModelLimitsToText migrates model_limits column from varchar(1024) to text
// This is safe to run multiple times - it checks the column type first
func migrateTokenModelLimitsToText() error {
	// SQLite uses type affinity, so TEXT and VARCHAR are effectively the same — no migration needed
	if common.UsingSQLite {
		return nil
	}

	tableName := "tokens"
	columnName := "model_limits"

	if !DB.Migrator().HasTable(tableName) {
		return nil
	}

	if !DB.Migrator().HasColumn(&Token{}, columnName) {
		return nil
	}

	var alterSQL string
	if common.UsingPostgreSQL {
		var dataType string
		if err := DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "text" {
			return nil
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE text`, tableName, columnName)
	} else if common.UsingMySQL {
		var columnType string
		if err := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.ToLower(columnType) == "text" {
			return nil
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s text", tableName, columnName)
	} else {
		return nil
	}

	if alterSQL != "" {
		if err := DB.Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("failed to migrate %s.%s to text: %w", tableName, columnName, err)
		}
		common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to text", tableName, columnName))
	}
	return nil
}

// migrateUserQuotaColumnsToBigInt runs during startup. On MySQL/PostgreSQL,
// changing large users table columns can hold table-level or rewrite locks for
// a noticeable time, so operators with large deployments should schedule this
// upgrade off-peak or run an equivalent online-DDL migration before booting.
func migrateUserQuotaColumnsToBigInt() error {
	return migrateQuotaColumnsToBigInt("users", []string{"quota", "used_quota", "aff_quota", "aff_history"})
}

// migrateTokenQuotaColumnsToBigInt keeps token accounting columns aligned
// with User quota columns on MySQL and PostgreSQL.
func migrateTokenQuotaColumnsToBigInt() error {
	return migrateQuotaColumnsToBigInt("tokens", []string{"remain_quota", "used_quota"})
}

func migrateQuotaColumnsToBigInt(tableName string, columnNames []string) error {
	if DB == nil || common.UsingSQLite || !DB.Migrator().HasTable(tableName) {
		return nil
	}

	for _, columnName := range columnNames {
		if !DB.Migrator().HasColumn(tableName, columnName) {
			continue
		}

		var backfillSQL string
		var alterSQL string
		if common.UsingPostgreSQL {
			var columnMetadata struct {
				DataType      string         `gorm:"column:data_type"`
				IsNullable    string         `gorm:"column:is_nullable"`
				ColumnDefault sql.NullString `gorm:"column:column_default"`
			}
			if err := DB.Raw(`SELECT data_type AS data_type, is_nullable AS is_nullable, column_default AS column_default
				FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
				tableName, columnName).Scan(&columnMetadata).Error; err != nil {
				common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
			} else if strings.EqualFold(strings.TrimSpace(columnMetadata.DataType), "bigint") &&
				strings.EqualFold(columnMetadata.IsNullable, "NO") &&
				isZeroColumnDefault(columnMetadata.ColumnDefault) {
				continue
			}
			backfillSQL = fmt.Sprintf(`UPDATE "%s" SET "%s" = 0 WHERE "%s" IS NULL`,
				tableName, columnName, columnName)
			alterSQL = fmt.Sprintf(`ALTER TABLE "%s" ALTER COLUMN "%s" TYPE bigint USING COALESCE("%s", 0)::bigint, ALTER COLUMN "%s" SET NOT NULL, ALTER COLUMN "%s" SET DEFAULT 0`,
				tableName, columnName, columnName, columnName, columnName)
		} else if common.UsingMySQL {
			var columnMetadata struct {
				ColumnType    string         `gorm:"column:column_type"`
				IsNullable    string         `gorm:"column:is_nullable"`
				ColumnDefault sql.NullString `gorm:"column:column_default"`
			}
			if err := DB.Raw(`SELECT COLUMN_TYPE AS column_type, IS_NULLABLE AS is_nullable, COLUMN_DEFAULT AS column_default
				FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
				tableName, columnName).Scan(&columnMetadata).Error; err != nil {
				common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
			} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(columnMetadata.ColumnType)), "bigint") &&
				strings.EqualFold(columnMetadata.IsNullable, "NO") &&
				isZeroColumnDefault(columnMetadata.ColumnDefault) {
				continue
			}
			backfillSQL = fmt.Sprintf("UPDATE `%s` SET `%s` = 0 WHERE `%s` IS NULL", tableName, columnName, columnName)
			alterSQL = fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN `%s` bigint NOT NULL DEFAULT 0", tableName, columnName)
		}

		if alterSQL == "" {
			continue
		}
		if backfillSQL != "" {
			if err := DB.Exec(backfillSQL).Error; err != nil {
				return fmt.Errorf("failed to backfill null values for %s.%s: %w", tableName, columnName, err)
			}
		}
		if err := DB.Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("failed to migrate %s.%s to bigint: %w", tableName, columnName, err)
		}
		common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to bigint", tableName, columnName))
	}

	return nil
}

func isZeroColumnDefault(defaultValue sql.NullString) bool {
	if !defaultValue.Valid {
		return false
	}

	value := strings.ToLower(strings.TrimSpace(defaultValue.String))
	switch value {
	case "0", "(0)", "'0'", "0::bigint", "0::integer", "'0'::bigint", "'0'::integer":
		return true
	default:
		return false
	}
}

// migrateSubscriptionPlanPriceAmount migrates price_amount column from float/double to decimal(10,6)
// This is safe to run multiple times - it checks the column type first
func migrateSubscriptionPlanPriceAmount() {
	// SQLite doesn't support ALTER COLUMN, and its type affinity handles this automatically
	// Skip early to avoid GORM parsing the existing table DDL which may cause issues
	if common.UsingSQLite {
		return
	}

	tableName := "subscription_plans"
	columnName := "price_amount"

	// Check if table exists first
	if !DB.Migrator().HasTable(tableName) {
		return
	}

	// Check if column exists
	if !DB.Migrator().HasColumn(&SubscriptionPlan{}, columnName) {
		return
	}

	var alterSQL string
	if common.UsingPostgreSQL {
		// PostgreSQL: Check if already decimal/numeric
		var dataType string
		if err := DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "numeric" {
			return // Already decimal/numeric
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE decimal(10,6) USING %s::decimal(10,6)`,
			tableName, columnName, columnName)
	} else if common.UsingMySQL {
		// MySQL: Check if already decimal
		var columnType string
		if err := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.HasPrefix(strings.ToLower(columnType), "decimal") {
			return // Already decimal
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s decimal(10,6) NOT NULL DEFAULT 0",
			tableName, columnName)
	} else {
		return
	}

	if alterSQL != "" {
		if err := DB.Exec(alterSQL).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to migrate %s.%s to decimal: %v", tableName, columnName, err))
		} else {
			common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to decimal(10,6)", tableName, columnName))
		}
	}
}

func closeDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}

func CloseDB() error {
	if LOG_DB != DB {
		err := closeDB(LOG_DB)
		if err != nil {
			return err
		}
	}
	return closeDB(DB)
}

// checkMySQLChineseSupport ensures the MySQL connection and current schema
// default charset/collation can store Chinese characters. It allows common
// Chinese-capable charsets (utf8mb4, utf8, gbk, big5, gb18030) and panics otherwise.
func checkMySQLChineseSupport(db *gorm.DB) error {
	// 仅检测：当前库默认字符集/排序规则 + 各表的排序规则（隐含字符集）

	// Read current schema defaults
	var schemaCharset, schemaCollation string
	err := db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()").Row().Scan(&schemaCharset, &schemaCollation)
	if err != nil {
		return fmt.Errorf("读取当前库默认字符集/排序规则失败 / Failed to read schema default charset/collation: %v", err)
	}

	toLower := func(s string) string { return strings.ToLower(s) }
	// Allowed charsets that can store Chinese text
	allowedCharsets := map[string]string{
		"utf8mb4": "utf8mb4_",
		"utf8":    "utf8_",
		"gbk":     "gbk_",
		"big5":    "big5_",
		"gb18030": "gb18030_",
	}
	isChineseCapable := func(cs, cl string) bool {
		csLower := toLower(cs)
		clLower := toLower(cl)
		if prefix, ok := allowedCharsets[csLower]; ok {
			if clLower == "" {
				return true
			}
			return strings.HasPrefix(clLower, prefix)
		}
		// 如果仅提供了排序规则，尝试按排序规则前缀判断
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(clLower, prefix) {
				return true
			}
		}
		return false
	}

	// 1) 当前库默认值必须支持中文
	if !isChineseCapable(schemaCharset, schemaCollation) {
		return fmt.Errorf("当前库默认字符集/排序规则不支持中文：schema(%s/%s)。请将库设置为 utf8mb4/utf8/gbk/big5/gb18030 / Schema default charset/collation is not Chinese-capable: schema(%s/%s). Please set to utf8mb4/utf8/gbk/big5/gb18030",
			schemaCharset, schemaCollation, schemaCharset, schemaCollation)
	}

	// 2) 所有物理表的排序规则（隐含字符集）必须支持中文
	type tableInfo struct {
		Name      string
		Collation *string
	}
	var tables []tableInfo
	if err := db.Raw("SELECT TABLE_NAME, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("读取表排序规则失败 / Failed to read table collations: %v", err)
	}

	var badTables []string
	for _, t := range tables {
		// NULL 或空表示继承库默认设置，已在上面校验库默认，视为通过
		if t.Collation == nil || *t.Collation == "" {
			continue
		}
		cl := *t.Collation
		// 仅凭排序规则判断是否中文可用
		ok := false
		lower := strings.ToLower(cl)
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(lower, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			badTables = append(badTables, fmt.Sprintf("%s(%s)", t.Name, cl))
		}
	}

	if len(badTables) > 0 {
		// 限制输出数量以避免日志过长
		maxShow := 20
		shown := badTables
		if len(shown) > maxShow {
			shown = shown[:maxShow]
		}
		return fmt.Errorf(
			"存在不支持中文的表，请修复其排序规则/字符集。示例（最多展示 %d 项）：%v / Found tables not Chinese-capable. Please fix their collation/charset. Examples (showing up to %d): %v",
			maxShow, shown, maxShow, shown,
		)
	}
	return nil
}

var (
	lastPingTime time.Time
	pingMutex    sync.Mutex
)

func PingDB() error {
	pingMutex.Lock()
	defer pingMutex.Unlock()

	if time.Since(lastPingTime) < time.Second*10 {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Printf("Error getting sql.DB from GORM: %v", err)
		return err
	}

	err = sqlDB.Ping()
	if err != nil {
		log.Printf("Error pinging DB: %v", err)
		return err
	}

	lastPingTime = time.Now()
	common.SysLog("Database pinged successfully")
	return nil
}

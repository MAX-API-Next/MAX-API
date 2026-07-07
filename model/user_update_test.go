package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func setupUserUpdateTestState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)

	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
}

func useFailingUserUpdateRedis(t *testing.T) {
	t.Helper()
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	client := redis.NewClient(&redis.Options{
		Addr:       "cache-unavailable",
		MaxRetries: 0,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("cache unavailable")
		},
	})
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
	})
}

func openUserUpdateMySQLTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(gormmysql.Open(mysqlDSNWithClientFoundRowsFalse(dsn)), &gorm.Config{})
	require.NoError(t, err)
	if db.Migrator().HasTable(&User{}) {
		t.Skip("refusing to run mysql user update test against external database because users table already exists")
	}
	require.NoError(t, db.AutoMigrate(&User{}))
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&User{})
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestNormalizedEmailLockNameIsStableAndShort(t *testing.T) {
	lockName := normalizedEmailLockName(" User@Example.COM ")

	assert.Equal(t, normalizedEmailLockName("user@example.com"), lockName)
	assert.LessOrEqual(t, len(lockName), 64)
	assert.Contains(t, lockName, "maxapi:user-email:")
}

func mysqlDSNWithClientFoundRowsFalse(dsn string) string {
	base, rawQuery, ok := strings.Cut(dsn, "?")
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		if ok {
			return dsn + "&clientFoundRows=false"
		}
		return dsn + "?clientFoundRows=false"
	}
	for key := range values {
		if strings.EqualFold(key, "clientFoundRows") {
			delete(values, key)
		}
	}
	values.Set("clientFoundRows", "false")
	if !ok {
		return base + "?" + values.Encode()
	}
	return base + "?" + values.Encode()
}

func useUserUpdateTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()

	oldDB := DB
	oldLOGDB := LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled

	DB = db
	LOG_DB = db
	common.UsingSQLite = false
	common.UsingMySQL = true
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	initCol()

	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLOGDB
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		initCol()
	})
}

func TestUserUpdateDoesNotOverwriteAccountingFields(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:           1,
		Username:     "quota-race-user",
		Password:     "password",
		DisplayName:  "before",
		Status:       common.UserStatusEnabled,
		Quota:        1000,
		UsedQuota:    20,
		RequestCount: 3,
	}
	require.NoError(t, DB.Create(&user).Error)

	staleUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota - ?", 400),
		"used_quota":    gorm.Expr("used_quota + ?", 400),
		"request_count": gorm.Expr("request_count + ?", 1),
	}).Error)

	staleUser.DisplayName = "after"
	require.NoError(t, staleUser.Update(false))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "after", got.DisplayName)
	assert.Equal(t, 600, got.Quota)
	assert.Equal(t, 420, got.UsedQuota)
	assert.Equal(t, 4, got.RequestCount)
}

func TestUserUpdatePersistsZeroValueProfileFields(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:           5,
		Username:     "zero-value-user",
		Password:     "password",
		DisplayName:  "display",
		Status:       common.UserStatusEnabled,
		AffCount:     7,
		Quota:        1000,
		UsedQuota:    20,
		RequestCount: 3,
	}
	require.NoError(t, DB.Create(&user).Error)

	loaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	loaded.DisplayName = ""
	loaded.AffCount = 0
	loaded.Quota = 1
	loaded.UsedQuota = 1
	loaded.RequestCount = 1

	require.NoError(t, loaded.UpdateFields(false, UserUpdateFieldDisplayName, UserUpdateFieldAffCount))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Empty(t, got.DisplayName)
	assert.Zero(t, got.AffCount)
	assert.Equal(t, 1000, got.Quota)
	assert.Equal(t, 20, got.UsedQuota)
	assert.Equal(t, 3, got.RequestCount)
}

func TestUserUpdateDoesNotClearEmailFromLoadedPartialUpdate(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:          6,
		Username:    "email-preserve-user",
		Password:    "password",
		DisplayName: "before",
		Email:       "Keep@Example.COM",
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	loaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	loaded.Email = ""
	loaded.NormalizedEmail = ""
	loaded.DisplayName = "after"

	require.NoError(t, loaded.Update(false))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "after", got.DisplayName)
	assert.Equal(t, "Keep@Example.COM", got.Email)
	assert.Equal(t, "keep@example.com", got.NormalizedEmail)
}

func TestUserUpdateIgnoresCacheWriteFailure(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:          3,
		Username:    "cache-failure-user",
		Password:    "password",
		DisplayName: "before",
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)
	useFailingUserUpdateRedis(t)

	user.DisplayName = "after"
	require.NoError(t, user.Update(false))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "after", got.DisplayName)
}

func TestUpdateUserSettingOnlyUpdatesSetting(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:           2,
		Username:     "setting-user",
		Password:     "password",
		Status:       common.UserStatusEnabled,
		Quota:        1000,
		UsedQuota:    20,
		RequestCount: 3,
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota - ?", 250),
		"used_quota":    gorm.Expr("used_quota + ?", 250),
		"request_count": gorm.Expr("request_count + ?", 1),
	}).Error)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 750, got.Quota)
	assert.Equal(t, 270, got.UsedQuota)
	assert.Equal(t, 4, got.RequestCount)
	assert.Equal(t, "zh", got.GetSetting().Language)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))
}

func TestUpdateUserSettingOnlyUpdatesSettingMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run mysql UpdateUserSetting coverage")
	}
	db := openUserUpdateMySQLTestDB(t, dsn)
	useUserUpdateTestDB(t, db)

	user := User{
		Id:           2,
		Username:     "mysql-setting-user",
		Password:     "password",
		Status:       common.UserStatusEnabled,
		Quota:        1000,
		UsedQuota:    20,
		RequestCount: 3,
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota - ?", 250),
		"used_quota":    gorm.Expr("used_quota + ?", 250),
		"request_count": gorm.Expr("request_count + ?", 1),
	}).Error)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 750, got.Quota)
	assert.Equal(t, 270, got.UsedQuota)
	assert.Equal(t, 4, got.RequestCount)
	assert.Equal(t, "zh", got.GetSetting().Language)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))
}

func TestInsertRejectsConcurrentDuplicateEmailMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run mysql concurrent email insert coverage")
	}
	db := openUserUpdateMySQLTestDB(t, dsn)
	useUserUpdateTestDB(t, db)

	oldQuotaForNewUser := common.QuotaForNewUser
	common.QuotaForNewUser = 0
	t.Cleanup(func() {
		common.QuotaForNewUser = oldQuotaForNewUser
	})

	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := range 2 {
		go func(i int) {
			<-start
			user := &User{
				Username: fmt.Sprintf("mysql-race-user-%d", i),
				Email:    "Race@Example.COM",
				Role:     common.RoleCommonUser,
				Status:   common.UserStatusEnabled,
			}
			errs <- user.Insert(0)
		}(i)
	}
	close(start)

	var successCount int
	var duplicateCount int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrEmailAlreadyTaken):
			duplicateCount++
		default:
			require.NoError(t, err)
		}
	}

	require.Equal(t, 1, successCount)
	require.Equal(t, 1, duplicateCount)
	count, err := CountUsersByEmail("race@example.com")
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestUpdateUserSettingIgnoresCacheWriteFailure(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:       4,
		Username: "setting-cache-failure-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)
	useFailingUserUpdateRedis(t)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "zh", got.GetSetting().Language)
}

func TestUpdateUserSettingMissingUserReturnsError(t *testing.T) {
	setupUserUpdateTestState(t)

	err := UpdateUserSetting(999999, dto.UserSetting{Language: "zh"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "用户不存在")
}

func TestEnsureEmailAvailableRejectsExistingEmailCaseInsensitive(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "existing",
		Password: "old-password",
		Email:    "Taken@Example.com",
		Status:   common.UserStatusEnabled,
	}).Error)

	err := EnsureEmailAvailable(" taken@example.COM ", 0)
	require.ErrorIs(t, err, ErrEmailAlreadyTaken)

	user, err := GetUniqueUserByEmail("TAKEN@example.com")
	require.NoError(t, err)
	assert.Equal(t, "existing", user.Username)

	require.NoError(t, EnsureEmailAvailable("taken@example.com", user.Id))
}

func TestBackfillUserNormalizedEmails(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Exec(
		"INSERT INTO users (id, username, password, email, status) VALUES (?, ?, ?, ?, ?)",
		21,
		"legacy-email-user",
		"password",
		"Legacy@Example.COM",
		common.UserStatusEnabled,
	).Error)

	require.NoError(t, backfillUserNormalizedEmails())

	var got User
	require.NoError(t, DB.First(&got, 21).Error)
	assert.Equal(t, "Legacy@Example.COM", got.Email)
	assert.Equal(t, "legacy@example.com", got.NormalizedEmail)

	require.ErrorIs(t, EnsureEmailAvailable(" legacy@example.com ", 0), ErrEmailAlreadyTaken)
	require.NoError(t, EnsureEmailAvailable("legacy@example.com", got.Id))
}

func TestInsertRejectsDuplicateEmailWithoutUniqueIndex(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "existing",
		Password: "old-password",
		Email:    "taken@example.com",
		Status:   common.UserStatusEnabled,
	}).Error)

	user := &User{
		Username: "oauth-user",
		Email:    "TAKEN@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}

	err := user.Insert(0)
	require.ErrorIs(t, err, ErrEmailAlreadyTaken)

	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "oauth-user").Count(&count).Error)
	assert.Zero(t, count)
}

func TestInsertKeepsBlankPasswordForPasswordlessUser(t *testing.T) {
	setupUserUpdateTestState(t)

	user := &User{
		Username: "passwordless-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}

	require.NoError(t, user.Insert(0))

	var stored User
	require.NoError(t, DB.Where("username = ?", user.Username).First(&stored).Error)
	assert.Empty(t, stored.Password)
}

func TestValidateAndFillRejectsPasswordlessUser(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "passwordless-user",
		Password: "",
		Status:   common.UserStatusEnabled,
	}).Error)

	loginUser := User{
		Username: "passwordless-user",
		Password: "NewPassword123",
	}
	err := loginUser.ValidateAndFill()
	require.ErrorIs(t, err, ErrInvalidCredentials)

	var stored User
	require.NoError(t, DB.Where("username = ?", "passwordless-user").First(&stored).Error)
	assert.Empty(t, stored.Password)
}

func TestResetUserPasswordByEmailRequiresSingleActiveMatch(t *testing.T) {
	setupUserUpdateTestState(t)

	require.NoError(t, DB.Create(&User{
		Username: "duplicate-1",
		Password: "old-1",
		Email:    "legacy@example.com",
		AffCode:  "dupe1",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&User{
		Username: "duplicate-2",
		Password: "old-2",
		Email:    "LEGACY@example.com",
		AffCode:  "dupe2",
		Status:   common.UserStatusEnabled,
	}).Error)

	err := ResetUserPasswordByEmail("legacy@example.com", "NewPassword123")
	require.ErrorIs(t, err, ErrEmailAmbiguous)

	var duplicates []User
	require.NoError(t, DB.Where("LOWER(email) = ?", "legacy@example.com").Order("username asc").Find(&duplicates).Error)
	require.Len(t, duplicates, 2)
	assert.Equal(t, "old-1", duplicates[0].Password)
	assert.Equal(t, "old-2", duplicates[1].Password)

	require.NoError(t, DB.Create(&User{
		Username: "unique",
		Password: "old",
		Email:    "unique@example.com",
		AffCode:  "unique",
		Status:   common.UserStatusEnabled,
	}).Error)

	require.NoError(t, ResetUserPasswordByEmail("UNIQUE@example.com", "NewPassword123"))

	var unique User
	require.NoError(t, DB.Where("username = ?", "unique").First(&unique).Error)
	assert.True(t, common.ValidatePasswordAndHash("NewPassword123", unique.Password))

	err = ResetUserPasswordByEmail("missing@example.com", "NewPassword123")
	require.True(t, errors.Is(err, ErrEmailNotFound))
}

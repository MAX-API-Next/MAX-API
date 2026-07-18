package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestConcurrentUserQuotaDeductionCannotOverdraw(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{Id: 31, Username: "quota-guard-user", Quota: 10, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if DecreaseUserQuota(user.Id, 7, false) == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.EqualValues(t, 1, successes.Load())
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.EqualValues(t, 3, got.Quota)
}

func TestConcurrentTokenQuotaDeductionCannotOverdraw(t *testing.T) {
	setupUserUpdateTestState(t)

	token := Token{Id: 32, UserId: 31, Key: "quota-guard-token", RemainQuota: 10}
	require.NoError(t, DB.Create(&token).Error)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if DecreaseTokenQuota(token.Id, token.Key, 7) == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.EqualValues(t, 1, successes.Load())
	var got Token
	require.NoError(t, DB.First(&got, token.Id).Error)
	assert.EqualValues(t, 3, got.RemainQuota)
	assert.EqualValues(t, 7, got.UsedQuota)
}

func TestUnlimitedTokenQuotaDeductionRemainsUnbounded(t *testing.T) {
	setupUserUpdateTestState(t)

	token := Token{Id: 34, UserId: 31, Key: "unlimited-token", RemainQuota: 0, UnlimitedQuota: true}
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 7))

	var got Token
	require.NoError(t, DB.First(&got, token.Id).Error)
	assert.EqualValues(t, -7, got.RemainQuota)
	assert.EqualValues(t, 7, got.UsedQuota)
}

func TestQuotaMutationDoesNotTouchCacheAfterDatabaseFailure(t *testing.T) {
	setupUserUpdateTestState(t)

	oldRDB := common.RDB
	var dialAttempts atomic.Int32
	client := redis.NewClient(&redis.Options{
		Addr:       "cache-unavailable",
		MaxRetries: 0,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialAttempts.Add(1)
			return nil, errors.New("cache unavailable")
		},
	})
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
	})

	require.Error(t, DecreaseUserQuota(999999, 1, false))
	require.Error(t, DecreaseTokenQuota(999999, "missing-token", 1))
	assert.Zero(t, dialAttempts.Load())
}

func TestInviteUserUpdatesOnlyAffiliateCounters(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{
		Id:              33,
		Username:        "inviter",
		Quota:           100,
		Status:          common.UserStatusEnabled,
		AffCount:        2,
		AffQuota:        10,
		AffHistoryQuota: 20,
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, inviteUser(user.Id))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 3, got.AffCount)
	assert.EqualValues(t, 10+common.QuotaForInviter, got.AffQuota)
	assert.EqualValues(t, 20+common.QuotaForInviter, got.AffHistoryQuota)
	assert.EqualValues(t, 100, got.Quota)
	assert.Equal(t, common.UserStatusEnabled, got.Status)
}

func TestFillUserMethodsReturnDatabaseErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	tests := []struct {
		name string
		user User
		call func(*User) error
	}{
		{name: "id", user: User{Id: 1}, call: (*User).FillUserById},
		{name: "email", user: User{Email: "user@example.com"}, call: (*User).FillUserByEmail},
		{name: "github", user: User{GitHubId: "github-id"}, call: (*User).FillUserByGitHubId},
		{name: "discord", user: User{DiscordId: "discord-id"}, call: (*User).FillUserByDiscordId},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := tt.user
			require.Error(t, tt.call(&user))
		})
	}
}

func TestFillOAuthUserMethodsDistinguishSoftDeletedUsers(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{
		Id: 35, Username: "deleted-oauth-user", GitHubId: "deleted-github",
		DiscordId: "deleted-discord", OidcId: "deleted-oidc", WeChatId: "deleted-wechat",
		TelegramId: "deleted-telegram", LinuxDOId: "deleted-linuxdo",
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Delete(&user).Error)

	tests := []struct {
		name string
		user User
		call func(*User) error
	}{
		{name: "github", user: User{GitHubId: user.GitHubId}, call: (*User).FillUserByGitHubId},
		{name: "discord", user: User{DiscordId: user.DiscordId}, call: (*User).FillUserByDiscordId},
		{name: "oidc", user: User{OidcId: user.OidcId}, call: (*User).FillUserByOidcId},
		{name: "wechat", user: User{WeChatId: user.WeChatId}, call: (*User).FillUserByWeChatId},
		{name: "telegram", user: User{TelegramId: user.TelegramId}, call: (*User).FillUserByTelegramId},
		{name: "linuxdo", user: User{LinuxDOId: user.LinuxDOId}, call: (*User).FillUserByLinuxDOId},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := tt.user
			require.ErrorIs(t, tt.call(&lookup), ErrUserDeleted)
		})
	}
}

type blockingCacheFillHook struct {
	started    chan struct{}
	release    chan struct{}
	finished   chan struct{}
	beforeOnce sync.Once
	afterOnce  sync.Once
}

func newBlockingCacheFillHook() *blockingCacheFillHook {
	return &blockingCacheFillHook{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (h *blockingCacheFillHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (h *blockingCacheFillHook) AfterProcess(context.Context, redis.Cmder) error { return nil }

func (h *blockingCacheFillHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	if cacheFillPipeline(cmds) {
		h.beforeOnce.Do(func() {
			close(h.started)
			<-h.release
		})
	}
	return ctx, nil
}

func (h *blockingCacheFillHook) AfterProcessPipeline(_ context.Context, cmds []redis.Cmder) error {
	if cacheFillPipeline(cmds) {
		h.afterOnce.Do(func() { close(h.finished) })
	}
	return nil
}

func cacheFillPipeline(cmds []redis.Cmder) bool {
	for _, cmd := range cmds {
		if strings.EqualFold(cmd.Name(), "hset") {
			return true
		}
	}
	return false
}

func useBlockingCacheFillRedis(t *testing.T) (*blockingCacheFillHook, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	hook := newBlockingCacheFillHook()
	client.AddHook(hook)

	oldRDB := common.RDB
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
	})
	return hook, client
}

func waitForCacheHook(t *testing.T, ch <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for cache %s", stage)
	}
}

func TestUserQuotaInvalidationRejectsOlderAsyncRefill(t *testing.T) {
	setupUserUpdateTestState(t)
	hook, client := useBlockingCacheFillRedis(t)
	user := User{Id: 36, Username: "user-cache-fence", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	quota, err := GetUserQuota(user.Id, true)
	require.NoError(t, err)
	assert.EqualValues(t, 100, quota)
	waitForCacheHook(t, hook.started, "fill start")

	require.NoError(t, DecreaseUserQuota(user.Id, 10, false))
	close(hook.release)
	waitForCacheHook(t, hook.finished, "fill completion")

	quota, err = GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.EqualValues(t, 90, quota)
	require.Eventually(t, func() bool {
		cached, err := client.HGet(context.Background(), getUserCacheKey(user.Id), "Quota").Int64()
		return err == nil && cached == 90
	}, 3*time.Second, 10*time.Millisecond)
}

func TestTokenQuotaInvalidationRejectsOlderAsyncRefill(t *testing.T) {
	setupUserUpdateTestState(t)
	hook, client := useBlockingCacheFillRedis(t)
	token := Token{Id: 37, UserId: 36, Key: "token-cache-fence", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)

	got, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	assert.EqualValues(t, 100, got.RemainQuota)
	waitForCacheHook(t, hook.started, "fill start")

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 10))
	close(hook.release)
	waitForCacheHook(t, hook.finished, "fill completion")

	got, err = GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.EqualValues(t, 90, got.RemainQuota)
	require.Eventually(t, func() bool {
		cached, err := client.HGet(context.Background(), getTokenCacheKey(token.Key), "RemainQuota").Int64()
		return err == nil && cached == 90
	}, 3*time.Second, 10*time.Millisecond)
}

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

func TestScanNullableInt64UsesSQLRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() {
			_ = sqlDB.Close()
		})
	}

	got, err := scanNullableInt64(db.Raw("SELECT ?", 1).Row())
	require.NoError(t, err)
	require.True(t, got.Valid)
	assert.EqualValues(t, 1, got.Int64)

	nullValue, err := scanNullableInt64(db.Raw("SELECT NULL").Row())
	require.NoError(t, err)
	assert.False(t, nullValue.Valid)
}

func TestMySQLNamedLockResultSuccessRequiresOne(t *testing.T) {
	assert.True(t, isMySQLNamedLockSuccess(sql.NullInt64{Int64: 1, Valid: true}))
	assert.False(t, isMySQLNamedLockSuccess(sql.NullInt64{Int64: 0, Valid: true}))
	assert.False(t, isMySQLNamedLockSuccess(sql.NullInt64{Valid: false}))
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
	assert.EqualValues(t, 600, got.Quota)
	assert.EqualValues(t, 420, got.UsedQuota)
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
	assert.EqualValues(t, 1000, got.Quota)
	assert.EqualValues(t, 20, got.UsedQuota)
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
	assert.EqualValues(t, 750, got.Quota)
	assert.EqualValues(t, 270, got.UsedQuota)
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
	assert.EqualValues(t, 750, got.Quota)
	assert.EqualValues(t, 270, got.UsedQuota)
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

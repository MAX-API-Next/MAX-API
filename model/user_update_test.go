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

func TestAtomicTokenAndUserPreConsumeRollsBackTokenOnUserFailure(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 38, Username: "atomic-preconsume-user", Quota: 5, Status: common.UserStatusEnabled}
	token := Token{Id: 39, UserId: user.Id, Key: "atomic-preconsume-token", RemainQuota: 10}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	require.ErrorIs(t, DecreaseTokenAndUserQuota(token.Id, user.Id, token.Key, 7), ErrUserQuotaInsufficient)

	var storedToken Token
	require.NoError(t, DB.First(&storedToken, token.Id).Error)
	assert.EqualValues(t, 10, storedToken.RemainQuota)
	assert.Zero(t, storedToken.UsedQuota)
	var storedUser User
	require.NoError(t, DB.First(&storedUser, user.Id).Error)
	assert.EqualValues(t, 5, storedUser.Quota)
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

func TestIsOidcIdAlreadyTakenHandlesDuplicateLegacyRows(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Create(&User{Id: 36, Username: "oidc-duplicate-a", OidcId: "duplicate-oidc", AffCode: "oidc-a"}).Error)
	require.NoError(t, DB.Create(&User{Id: 37, Username: "oidc-duplicate-b", OidcId: "duplicate-oidc", AffCode: "oidc-b"}).Error)

	taken, err := IsOidcIdAlreadyTaken("duplicate-oidc")
	require.NoError(t, err)
	require.True(t, taken)
}

func TestOAuthIDTakenChecksReturnDatabaseErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })

	checks := []struct {
		name string
		call func() (bool, error)
	}{
		{name: "wechat", call: func() (bool, error) { return IsWeChatIdAlreadyTaken("wechat-id") }},
		{name: "github", call: func() (bool, error) { return IsGitHubIdAlreadyTaken("github-id") }},
		{name: "discord", call: func() (bool, error) { return IsDiscordIdAlreadyTaken("discord-id") }},
		{name: "oidc", call: func() (bool, error) { return IsOidcIdAlreadyTaken("oidc-id") }},
		{name: "telegram", call: func() (bool, error) { return IsTelegramIdAlreadyTaken("telegram-id") }},
		{name: "linuxdo", call: func() (bool, error) { return IsLinuxDOIdAlreadyTaken("linuxdo-id") }},
		{name: "generic", call: func() (bool, error) { return IsProviderUserIdTaken(1, "provider-user-id") }},
	}

	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			taken, err := tt.call()
			assert.False(t, taken)
			require.Error(t, err)
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

type failCacheMutationHook struct {
	seen   atomic.Int32
	failAt int32
}

type recoverableCacheMutationHook struct {
	seen      atomic.Int32
	available atomic.Bool
}

type overlappingCacheMutationHook struct {
	client    *redis.Client
	cacheKey  string
	started   chan struct{}
	release   chan struct{}
	afterErr  chan error
	seen      atomic.Int32
	afterOnce sync.Once
}

func (h *overlappingCacheMutationHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	name := strings.ToLower(cmd.Name())
	if name != "eval" && name != "evalsha" {
		return ctx, nil
	}
	if h.seen.Add(1) == 1 {
		close(h.started)
		<-h.release
	}
	return ctx, nil
}

func (h *overlappingCacheMutationHook) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	name := strings.ToLower(cmd.Name())
	if name != "eval" && name != "evalsha" || h.seen.Load() != 1 {
		return nil
	}
	h.afterOnce.Do(func() {
		h.afterErr <- h.client.HSet(context.Background(), h.cacheKey, "Status", common.TokenStatusEnabled).Err()
	})
	return nil
}

func (*overlappingCacheMutationHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*overlappingCacheMutationHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func (h *recoverableCacheMutationHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	name := strings.ToLower(cmd.Name())
	if name != "eval" && name != "evalsha" {
		return ctx, nil
	}
	h.seen.Add(1)
	if !h.available.Load() {
		return ctx, errors.New("injected cache outage")
	}
	return ctx, nil
}

func (*recoverableCacheMutationHook) AfterProcess(context.Context, redis.Cmder) error { return nil }

func (*recoverableCacheMutationHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*recoverableCacheMutationHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func (h *failCacheMutationHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	name := strings.ToLower(cmd.Name())
	if name != "eval" && name != "evalsha" {
		return ctx, nil
	}
	if h.seen.Add(1) == h.failAt {
		return ctx, errors.New("injected cache mutation failure")
	}
	return ctx, nil
}

func (*failCacheMutationHook) AfterProcess(context.Context, redis.Cmder) error { return nil }

func (*failCacheMutationHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*failCacheMutationHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func useFailingCacheMutationRedis(t *testing.T, failAt int32) (*redis.Client, *failCacheMutationHook) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	// Load the script before installing the hook so each later invalidation is
	// represented by one EVALSHA command and failAt is deterministic.
	require.NoError(t, common.RedisInvalidateVersionedHash("cache-retry-warmup", "cache-retry-warmup-version"))
	hook := &failCacheMutationHook{failAt: failAt}
	client.AddHook(hook)
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	})
	return client, hook
}

func cacheUserForRetryTest(t *testing.T, client *redis.Client, user User) {
	t.Helper()
	require.NoError(t, client.HSet(context.Background(), getUserCacheKey(user.Id), map[string]interface{}{
		"Id":       user.Id,
		"Group":    user.Group,
		"Quota":    user.Quota,
		"Role":     common.RoleCommonUser,
		"Status":   common.UserStatusEnabled,
		"Username": user.Username,
	}).Err())
}

func cacheTokenForRetryTest(t *testing.T, client *redis.Client, token Token) {
	t.Helper()
	require.NoError(t, client.HSet(context.Background(), getTokenCacheKey(token.Key), map[string]interface{}{
		"Id":          token.Id,
		"UserId":      token.UserId,
		"Status":      token.Status,
		"RemainQuota": token.RemainQuota,
	}).Err())
}

func requireCacheKeyDeletedEventually(t *testing.T, client *redis.Client, key string) {
	t.Helper()
	require.Eventually(t, func() bool {
		exists, err := client.Exists(context.Background(), key).Result()
		return err == nil && exists == 0
	}, time.Second, 10*time.Millisecond)
}

func TestUserQuotaMutationsRetryCacheInvalidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(int) error
	}{
		{name: "increase", mutate: func(id int) error { return IncreaseUserQuota(id, 1, false) }},
		{name: "decrease", mutate: func(id int) error { return DecreaseUserQuota(id, 1, false) }},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupUserUpdateTestState(t)
			user := User{Id: 80 + index, Username: "cache-retry-user-" + tt.name, Quota: 10, Status: common.UserStatusEnabled}
			require.NoError(t, DB.Create(&user).Error)
			client, _ := useFailingCacheMutationRedis(t, 1)
			cacheUserForRetryTest(t, client, user)

			require.NoError(t, tt.mutate(user.Id))
			requireCacheKeyDeletedEventually(t, client, getUserCacheKey(user.Id))
		})
	}
}

func TestAtomicPreConsumeRetriesBothCacheInvalidations(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 83, Username: "atomic-cache-retry-user", Quota: 10, Status: common.UserStatusEnabled}
	token := Token{Id: 84, UserId: user.Id, Key: "atomic-cache-retry-token", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	client, _ := useFailingCacheMutationRedis(t, 2)
	cacheUserForRetryTest(t, client, user)
	cacheTokenForRetryTest(t, client, token)

	require.NoError(t, DecreaseTokenAndUserQuota(token.Id, user.Id, token.Key, 1))
	requireCacheKeyDeletedEventually(t, client, getTokenCacheKey(token.Key))
	requireCacheKeyDeletedEventually(t, client, getUserCacheKey(user.Id))
}

func TestTokenQuotaMutationRetriesCacheInvalidation(t *testing.T) {
	setupUserUpdateTestState(t)
	token := Token{Id: 85, UserId: 86, Key: "token-quota-cache-retry", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	client, _ := useFailingCacheMutationRedis(t, 1)
	cacheTokenForRetryTest(t, client, token)

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 1))
	requireCacheKeyDeletedEventually(t, client, getTokenCacheKey(token.Key))
}

func TestTokenDeleteRetriesCacheDeletion(t *testing.T) {
	setupUserUpdateTestState(t)
	token := Token{Id: 87, UserId: 88, Key: "token-delete-cache-retry", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	client, _ := useFailingCacheMutationRedis(t, 1)
	cacheTokenForRetryTest(t, client, token)

	require.NoError(t, token.Delete())
	requireCacheKeyDeletedEventually(t, client, getTokenCacheKey(token.Key))
}

func TestUpdateUserSettingRetriesCacheInvalidation(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 86, Username: "setting-cache-retry-user", Quota: 10, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	client, _ := useFailingCacheMutationRedis(t, 1)
	cacheUserForRetryTest(t, client, user)

	require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{Language: "zh"}))
	requireCacheKeyDeletedEventually(t, client, getUserCacheKey(user.Id))
}

func TestUserDeletesRetryCacheDeletionAfterDatabaseCommit(t *testing.T) {
	tests := []struct {
		name       string
		userID     int
		deleteUser func(*User) error
		wantRows   int64
	}{
		{
			name:       "soft delete",
			userID:     93,
			deleteUser: (*User).Delete,
			wantRows:   1,
		},
		{
			name:       "hard delete",
			userID:     94,
			deleteUser: (*User).HardDelete,
			wantRows:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupUserUpdateTestState(t)
			user := User{Id: tt.userID, Username: "delete-cache-retry-user-" + tt.name, Status: common.UserStatusEnabled}
			require.NoError(t, DB.Create(&user).Error)
			client, _ := useFailingCacheMutationRedis(t, 1)
			cacheUserForRetryTest(t, client, user)

			require.NoError(t, tt.deleteUser(&user))
			var count int64
			require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Count(&count).Error)
			assert.Equal(t, tt.wantRows, count)
			requireCacheKeyDeletedEventually(t, client, getUserCacheKey(user.Id))
		})
	}
}

func TestTokenDeleteRetriesUntilCacheRecovers(t *testing.T) {
	setupUserUpdateTestState(t)
	token := Token{Id: 91, UserId: 92, Key: "token-delete-cache-recovery", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	require.NoError(t, common.RedisInvalidateVersionedHash("cache-recovery-warmup", "cache-recovery-warmup-version"))
	hook := &recoverableCacheMutationHook{}
	client.AddHook(hook)
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	})
	cacheTokenForRetryTest(t, client, token)

	require.NoError(t, token.Delete())
	require.Eventually(t, func() bool { return hook.seen.Load() >= 4 }, 2*time.Second, 10*time.Millisecond)
	exists, err := client.Exists(context.Background(), getTokenCacheKey(token.Key)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, exists)

	hook.available.Store(true)
	requireCacheKeyDeletedEventually(t, client, getTokenCacheKey(token.Key))
}

func TestTokenCacheRetryPreservesNewerRequestDuringInflightSuccess(t *testing.T) {
	setupUserUpdateTestState(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	require.NoError(t, common.RedisDeleteVersionedHash("cache-overlap-warmup", "cache-overlap-warmup-version"))

	tokenKey := "overlapping-token-cache-retry"
	cacheKey := getTokenCacheKey(tokenKey)
	require.NoError(t, client.HSet(context.Background(), cacheKey, "Status", common.TokenStatusEnabled).Err())
	hook := &overlappingCacheMutationHook{
		client: client, cacheKey: cacheKey, started: make(chan struct{}), release: make(chan struct{}), afterErr: make(chan error, 1),
	}
	client.AddHook(hook)
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	})

	enqueueTokenCacheRetry(tokenKey, true, nil)
	waitForCacheHook(t, hook.started, "retry start")
	enqueueTokenCacheRetry(tokenKey, true, errors.New("newer deletion request"))
	close(hook.release)
	require.NoError(t, <-hook.afterErr)
	requireCacheKeyDeletedEventually(t, client, cacheKey)
}

func TestBatchTokenDeleteRetriesCacheDeletion(t *testing.T) {
	setupUserUpdateTestState(t)
	token := Token{Id: 89, UserId: 90, Key: "batch-token-delete-cache-retry", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	client, _ := useFailingCacheMutationRedis(t, 1)
	cacheTokenForRetryTest(t, client, token)

	deleted, err := BatchDeleteTokens([]int{token.Id}, token.UserId)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	requireCacheKeyDeletedEventually(t, client, getTokenCacheKey(token.Key))
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

func TestUserGroupInvalidationRejectsOlderNarrowRefill(t *testing.T) {
	setupUserUpdateTestState(t)
	hook, _ := useBlockingCacheFillRedis(t)
	user := User{Id: 40, Username: "user-group-cache-fence", Group: "old", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	group, err := GetUserGroup(user.Id, true)
	require.NoError(t, err)
	require.Equal(t, "old", group)
	waitForCacheHook(t, hook.started, "narrow fill start")

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "new").Error)
	require.NoError(t, invalidateUserCache(user.Id))
	close(hook.release)
	waitForCacheHook(t, hook.finished, "narrow fill completion")

	group, err = GetUserGroup(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, "new", group)
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

func TestUserOAuthIdentityGeneratedColumnUsesIdentityMaxLength(t *testing.T) {
	oldUsingPostgreSQL := common.UsingPostgreSQL
	common.UsingPostgreSQL = false
	t.Cleanup(func() { common.UsingPostgreSQL = oldUsingPostgreSQL })

	statement := userOAuthIdentityGeneratedColumnSQL(userOAuthIdentityMigrations[1])

	assert.Contains(t, statement, fmt.Sprintf("varchar(%d)", userOAuthIdentityMaxLength))
	assert.NotContains(t, statement, "varchar(256)")
}

func TestUserOAuthIdentityLengthValidation(t *testing.T) {
	exactLimit := strings.Repeat("x", userOAuthIdentityMaxLength)
	tooLong := exactLimit + "x"

	assert.NoError(t, (&User{GitHubId: exactLimit}).ValidateOAuthIdentityLengths())
	err := (&User{GitHubId: tooLong}).ValidateOAuthIdentityLengths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "users.github_id")
	assert.Contains(t, err.Error(), fmt.Sprint(userOAuthIdentityMaxLength))
}

func TestFinishUserOAuthIdentityLockRetriesReleaseAndJoinsErrors(t *testing.T) {
	callbackErr := errors.New("callback failed")
	releaseErr := errors.New("release failed")

	released := false
	err := finishUserOAuthIdentityLock(callbackErr, releaseErr, &released)
	require.Error(t, err)
	assert.ErrorIs(t, err, callbackErr)
	assert.ErrorIs(t, err, releaseErr)
	assert.False(t, released)

	err = finishUserOAuthIdentityLock(callbackErr, nil, &released)
	require.ErrorIs(t, err, callbackErr)
	assert.True(t, released)
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

func useSQLiteUserMigrationTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()

	oldDB := DB
	oldLOGDB := LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL

	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()

	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLOGDB
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
		initCol()
	})
}

func TestMigrateUserOAuthIdentityConstraintsBackfillsAndEnforcesUniqueness(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	useSQLiteUserMigrationTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&User{}))

	require.NoError(t, DB.Create(&User{Id: 101, Username: "oauth-empty-a", AffCode: "oea", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Id: 102, Username: "oauth-empty-b", AffCode: "oeb", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Id: 103, Username: "oauth-duplicate-a", OidcId: "duplicate-oidc", AffCode: "oda", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Id: 104, Username: "oauth-duplicate-b", OidcId: "duplicate-oidc", AffCode: "odb", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Delete(&User{Id: 103}).Error)

	require.NoError(t, migrateUserOAuthIdentityConstraints())
	assert.True(t, DB.Migrator().HasIndex(&User{}, "ux_users_oidc_id"))

	var duplicateUsers []User
	require.NoError(t, DB.Unscoped().Where("id IN ?", []int{103, 104}).Order("id asc").Find(&duplicateUsers).Error)
	require.Len(t, duplicateUsers, 2)
	assert.Empty(t, duplicateUsers[0].OidcId)
	assert.Equal(t, "duplicate-oidc", duplicateUsers[1].OidcId)

	var nullGitHubIDs int64
	require.NoError(t, DB.Table("users").Where("github_id IS NULL").Count(&nullGitHubIDs).Error)
	assert.GreaterOrEqual(t, nullGitHubIDs, int64(2))

	err = DB.Create(&User{Id: 105, Username: "oauth-duplicate-c", OidcId: "duplicate-oidc", AffCode: "odc", Status: common.UserStatusEnabled}).Error
	require.Error(t, err)

	require.NoError(t, DB.Create(&User{Id: 106, Username: "oauth-empty-c", AffCode: "oec", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Id: 107, Username: "oauth-empty-d", AffCode: "oed", Status: common.UserStatusEnabled}).Error)
}

func assertWorkerBlocksUntilOAuthIdentityLockReleased(t *testing.T, run func() error, blockedMessage string) {
	t.Helper()
	userOAuthIdentityLockMu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			userOAuthIdentityLockMu.Unlock()
		}
	})

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- run()
	}()
	<-started

	select {
	case err := <-done:
		require.NoError(t, err)
		t.Fatal(blockedMessage)
	case <-time.After(50 * time.Millisecond):
	}

	userOAuthIdentityLockMu.Unlock()
	locked = false
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("operation did not complete after releasing the OAuth identity lock")
	}
}

func TestMigrateUserOAuthIdentityConstraintsWaitsForOAuthIdentityLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	useSQLiteUserMigrationTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&User{}))
	require.NoError(t, DB.Create(&User{Id: 201, Username: "oauth-lock-a", OidcId: "lock-oidc", AffCode: "ola", Status: common.UserStatusEnabled}).Error)

	assertWorkerBlocksUntilOAuthIdentityLockReleased(t, migrateUserOAuthIdentityConstraints, "migration completed while OAuth identity lock was held")
}

func TestOAuthIdentityUpdateWaitsForMigrationLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	useSQLiteUserMigrationTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&User{}))
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := User{Id: 211, Username: "oauth-update-lock", GitHubId: "before", AffCode: "oul", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	user.GitHubId = "after"

	assertWorkerBlocksUntilOAuthIdentityLockReleased(t, func() error {
		return user.UpdateFields(false, UserUpdateFieldGitHubId)
	}, "OAuth identity update completed while migration lock was held")

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "after", stored.GitHubId)
}

func TestUserFieldUpdateWaitsForOAuthIdentityMigrationLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	useSQLiteUserMigrationTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&User{}))
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := User{Id: 221, Username: "user-update-lock", DisplayName: "before", GitHubId: "existing", AffCode: "uul", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	user.DisplayName = "after"

	assertWorkerBlocksUntilOAuthIdentityLockReleased(t, func() error {
		return user.UpdateFields(false, UserUpdateFieldDisplayName)
	}, "user field update completed while OAuth identity migration lock was held")

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "after", stored.DisplayName)
	assert.Equal(t, "existing", stored.GitHubId)
}

func TestAccessTokenUpdateDoesNotWaitForOAuthIdentityMigrationLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	useSQLiteUserMigrationTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&User{}))
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := User{Id: 231, Username: "access-token-lock", AffCode: "atl", Status: common.UserStatusEnabled}
	user.SetAccessToken("before-token")
	require.NoError(t, DB.Create(&user).Error)
	user.SetAccessToken("after-token")

	userOAuthIdentityLockMu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			userOAuthIdentityLockMu.Unlock()
		}
	})

	done := make(chan error, 1)
	go func() {
		done <- user.UpdateFields(false, UserUpdateFieldAccessToken)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		userOAuthIdentityLockMu.Unlock()
		locked = false
		err := <-done
		require.NoError(t, err)
		t.Fatal("access token update waited for OAuth identity migration lock")
	}

	userOAuthIdentityLockMu.Unlock()
	locked = false
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, "after-token", stored.GetAccessToken())
}

func TestPartialUserUpdateValuesDoNotWriteUnspecifiedAccessToken(t *testing.T) {
	current := User{Id: 232, Username: "partial-token-current", DisplayName: "before", Status: common.UserStatusEnabled}
	current.SetAccessToken("current-token")
	newUser := User{Id: current.Id, DisplayName: "after"}

	updates := buildUserUpdateValues(current, newUser, false, UserUpdateFieldDisplayName)

	assert.Equal(t, "after", updates["display_name"])
	assert.NotContains(t, updates, "access_token")
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
	user.SetAccessToken("stale-token")
	require.NoError(t, DB.Create(&user).Error)

	staleUser, err := GetUserById(user.Id, true)
	require.NoError(t, err)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota - ?", 400),
		"used_quota":    gorm.Expr("used_quota + ?", 400),
		"request_count": gorm.Expr("request_count + ?", 1),
		"access_token":  "fresh-token",
	}).Error)

	staleUser.DisplayName = "after"
	require.NoError(t, staleUser.Update(false))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "after", got.DisplayName)
	assert.EqualValues(t, 600, got.Quota)
	assert.EqualValues(t, 420, got.UsedQuota)
	assert.Equal(t, 4, got.RequestCount)
	assert.Equal(t, "fresh-token", got.GetAccessToken())
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

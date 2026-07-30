package model

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestCacheInvalidationTargetIndexFitsMySQL57CompactUtf8mb4Limit(t *testing.T) {
	parsed, err := schema.Parse(&CacheInvalidationTask{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	kind := parsed.LookUpField("Kind")
	entityKey := parsed.LookUpField("EntityKey")
	require.NotNil(t, kind)
	require.NotNil(t, entityKey)

	// MySQL 5.7.8 with utf8mb4 and COMPACT row format limits index keys to
	// 767 bytes. DeleteEntry is a one-byte boolean column.
	indexBytes := (kind.Size+entityKey.Size)*4 + 1
	require.LessOrEqual(t, indexBytes, 767)
}

func useCacheMutationRedis(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	})
	return client
}

func useRecoverableCacheMutationRedis(t *testing.T) (*redis.Client, *recoverableCacheMutationHook) {
	t.Helper()
	client := useCacheMutationRedis(t)
	require.NoError(t, common.RedisInvalidateVersionedHash("persistent-retry-warmup", "persistent-retry-warmup-version"))
	hook := &recoverableCacheMutationHook{}
	client.AddHook(hook)
	return client, hook
}

func failCacheOutboxInserts(t *testing.T) {
	t.Helper()
	if !common.UsingSQLite {
		t.Skip("cache outbox trigger failure injection uses SQLite trigger syntax")
	}
	require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS cache_outbox_insert_failure").Error)
	require.NoError(t, DB.Exec("CREATE TRIGGER cache_outbox_insert_failure BEFORE INSERT ON cache_invalidation_tasks BEGIN SELECT RAISE(FAIL, 'cache outbox unavailable'); END").Error)
	t.Cleanup(func() { _ = DB.Exec("DROP TRIGGER IF EXISTS cache_outbox_insert_failure").Error })
}

func clearTokenCacheRetryMemory(t *testing.T) {
	t.Helper()
	tokenCacheRetries.Lock()
	tokenCacheRetries.pending = make(map[string]*tokenCacheRetryState)
	tokenCacheRetries.Unlock()

	require.Eventually(t, func() bool {
		tokenCacheRetries.Lock()
		defer tokenCacheRetries.Unlock()
		return !tokenCacheRetries.running
	}, time.Second, 10*time.Millisecond)
}

func TestApplyBillingSettlementOnceResolvesTokenKeyForCacheInvalidation(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{Id: 301, Username: "settlement-cache-user", Quota: 100, Status: common.UserStatusEnabled}
	token := Token{Id: 302, UserId: user.Id, Key: "settlement-cache-token", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	client := useCacheMutationRedis(t)
	cacheTokenForRetryTest(t, client, token)

	_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
		OperationKey: "request:resolve-token-key:settle",
		Source:       BillingSettlementSourceWallet,
		UserID:       user.Id,
		TokenID:      token.Id,
		FundingDelta: 10,
		TokenDelta:   10,
	})
	require.NoError(t, err)

	exists, err := client.Exists(context.Background(), getTokenCacheKey(token.Key)).Result()
	require.NoError(t, err)
	assert.Zero(t, exists)
}

func TestPersistedTokenCacheInvalidationSurvivesRetryWorkerRestart(t *testing.T) {
	setupUserUpdateTestState(t)
	token := Token{Id: 303, UserId: 304, Key: "persistent-token-cache-retry", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	client, hook := useRecoverableCacheMutationRedis(t)
	cacheTokenForRetryTest(t, client, token)

	enqueueTokenCacheRetry(token.Key, false, errors.New("injected cache outage"))
	require.Eventually(t, func() bool { return hook.seen.Load() > 0 }, time.Second, 10*time.Millisecond)
	clearTokenCacheRetryMemory(t)

	var task CacheInvalidationTask
	require.NoError(t, DB.First(&task).Error)
	assert.Equal(t, cacheInvalidationKindToken, task.Kind)
	assert.Equal(t, getTokenCacheKey(token.Key), task.EntityKey)
	assert.NotContains(t, task.EntityKey, token.Key)

	hook.available.Store(true)
	processCacheInvalidationTasks()

	var count int64
	require.NoError(t, DB.Model(&CacheInvalidationTask{}).Count(&count).Error)
	assert.Zero(t, count)
	exists, err := client.Exists(context.Background(), getTokenCacheKey(token.Key)).Result()
	require.NoError(t, err)
	assert.Zero(t, exists)
}

func TestPersistedCacheInvalidationRevisionProtectsNewerTask(t *testing.T) {
	setupUserUpdateTestState(t)
	token := Token{Id: 305, UserId: 306, Key: "persistent-revision-cache-retry", RemainQuota: 10, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	client, hook := useRecoverableCacheMutationRedis(t)
	cacheTokenForRetryTest(t, client, token)

	enqueueTokenCacheRetry(token.Key, false, errors.New("first cache outage"))
	require.Eventually(t, func() bool { return hook.seen.Load() > 0 }, time.Second, 10*time.Millisecond)
	clearTokenCacheRetryMemory(t)

	var stale CacheInvalidationTask
	require.NoError(t, DB.First(&stale).Error)
	staleRevision := stale.Revision
	persistCacheInvalidationTask(cacheInvalidationKindToken, stale.EntityKey, stale.VersionKey, stale.DeleteEntry, errors.New("newer cache outage"))

	result := DB.Where("id = ? AND revision = ?", stale.ID, staleRevision).Delete(&CacheInvalidationTask{})
	require.NoError(t, result.Error)
	assert.Zero(t, result.RowsAffected)

	hook.available.Store(true)
	processCacheInvalidationTasks()
	var count int64
	require.NoError(t, DB.Model(&CacheInvalidationTask{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestCacheInvalidationRestagePreservesDurableBackoff(t *testing.T) {
	setupUserUpdateTestState(t)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	task, err := upsertCacheInvalidationTask(DB, cacheInvalidationKindUser, "301", getUserCacheVersionKey(301), false, errors.New("first outage"))
	require.NoError(t, err)
	nextAttempt := time.Now().Add(time.Minute).Unix()
	require.NoError(t, DB.Model(&CacheInvalidationTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"attempts":     4,
		"next_attempt": nextAttempt,
		"last_error":   "first outage",
	}).Error)

	restaged, err := upsertCacheInvalidationTask(DB, cacheInvalidationKindUser, "301", "new-version", false, errors.New("second outage"))
	require.NoError(t, err)

	assert.EqualValues(t, 4, restaged.Attempts)
	assert.EqualValues(t, nextAttempt, restaged.NextAttempt)
	assert.Equal(t, "first outage", restaged.LastError)
	assert.Equal(t, "new-version", restaged.VersionKey)
	assert.Greater(t, restaged.Revision, task.Revision)
}

func TestProcessCacheInvalidationTasksDropsPermanentFailures(t *testing.T) {
	setupUserUpdateTestState(t)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&CacheInvalidationTask{
		Kind:        "unknown",
		EntityKey:   "entity",
		VersionKey:  "version",
		NextAttempt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Revision:    1,
	}).Error)
	require.NoError(t, DB.Create(&CacheInvalidationTask{
		Kind:        cacheInvalidationKindUser,
		EntityKey:   "not-a-user-id",
		VersionKey:  "version",
		NextAttempt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Revision:    1,
	}).Error)

	processCacheInvalidationTasks()

	var count int64
	require.NoError(t, DB.Model(&CacheInvalidationTask{}).Count(&count).Error)
	assert.Zero(t, count)
}

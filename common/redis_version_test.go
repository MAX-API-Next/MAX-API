package common

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestVersionedHashDeletionRejectsStaleRefillsAndReleasesEntityKeys(t *testing.T) {
	server := miniredis.RunT(t)
	oldRDB := RDB
	RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, RDB.Close())
		RDB = oldRDB
	})

	cacheKey := "cache:test-entity"
	versionKey := "cache-version:test-entity"
	version, err := RedisGetCacheVersion(versionKey)
	require.NoError(t, err)
	require.Positive(t, version)

	stored, err := RedisHSetFieldIfVersion(cacheKey, versionKey, version, "Quota", 10, time.Minute)
	require.NoError(t, err)
	require.True(t, stored)

	require.NoError(t, RedisInvalidateVersionedHash(cacheKey, versionKey))
	newVersion, err := RedisGetCacheVersion(versionKey)
	require.NoError(t, err)
	require.Greater(t, newVersion, version)
	stored, err = RedisHSetFieldIfVersion(cacheKey, versionKey, version, "Quota", 20, time.Minute)
	require.NoError(t, err)
	require.False(t, stored)

	require.NoError(t, RedisDeleteVersionedHash(cacheKey, versionKey))
	require.False(t, server.Exists(cacheKey))
	require.False(t, server.Exists(versionKey))
	stored, err = RedisHSetFieldIfVersion(cacheKey, versionKey, newVersion, "Quota", 30, time.Minute)
	require.NoError(t, err)
	require.False(t, stored)

	recreatedVersion, err := RedisGetCacheVersion(versionKey)
	require.NoError(t, err)
	require.Greater(t, recreatedVersion, newVersion)
}

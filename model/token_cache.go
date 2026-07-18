package model

import (
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/bytedance/gopkg/util/gopool"
)

func getTokenCacheKey(key string) string {
	return fmt.Sprintf("token:%s", common.GenerateHMAC(key))
}

func getTokenCacheVersionKey(key string) string {
	return fmt.Sprintf("cache-version:token:%s", common.GenerateHMAC(key))
}

func cacheSetTokenIfVersion(token Token, version int64) error {
	key := token.Key
	token.Clean()
	_, err := common.RedisHSetObjIfVersion(
		getTokenCacheKey(key),
		getTokenCacheVersionKey(key),
		version,
		&token,
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
	return err
}

func invalidateTokenCache(key string) error {
	if !common.RedisEnabled || key == "" {
		return nil
	}
	return common.RedisInvalidateVersionedHash(getTokenCacheKey(key), getTokenCacheVersionKey(key))
}

func deleteTokenCache(key string) error {
	if !common.RedisEnabled || key == "" {
		return nil
	}
	return common.RedisDeleteVersionedHash(getTokenCacheKey(key), getTokenCacheVersionKey(key))
}

func enqueueTokenCacheRetry(key string, deleteEntry bool, cause error) {
	if key == "" {
		return
	}
	gopool.Go(func() {
		delay := 50 * time.Millisecond
		operation := "invalidation"
		cacheOperation := invalidateTokenCache
		if deleteEntry {
			operation = "deletion"
			cacheOperation = deleteTokenCache
		}
		for attempt := 1; attempt <= 3; attempt++ {
			time.Sleep(delay)
			if err := cacheOperation(key); err == nil {
				return
			} else {
				common.SysLog(fmt.Sprintf("token cache %s retry %d/3 failed (initial_error=%v): %v",
					operation, attempt, cause, err))
			}
			delay *= 2
		}
	})
}

func cacheSetTokenField(key string, field string, value string) error {
	key = common.GenerateHMAC(key)
	err := common.RedisHSetField(fmt.Sprintf("token:%s", key), field, value)
	if err != nil {
		return err
	}
	return nil
}

// CacheGetTokenByKey 从缓存中获取 token，如果缓存中不存在，则从数据库中获取
func cacheGetTokenByKey(key string) (*Token, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var token Token
	err := common.RedisHGetObj(getTokenCacheKey(key), &token)
	if err != nil {
		return nil, err
	}
	token.Key = key
	return &token, nil
}

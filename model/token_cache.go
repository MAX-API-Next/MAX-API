package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	tokenCacheRetryInitialDelay = 50 * time.Millisecond
	tokenCacheRetryMaxDelay     = 5 * time.Second
)

type tokenCacheRetryState struct {
	cacheKey    string
	versionKey  string
	revision    uint64
	deleteEntry bool
	cause       error
	attempts    int
	delay       time.Duration
	nextAttempt time.Time
	deadline    time.Time
}

type tokenCacheRetryAttempt struct {
	cacheKey    string
	versionKey  string
	revision    uint64
	deleteEntry bool
	cause       error
	attempt     int
}

var tokenCacheRetries = struct {
	sync.Mutex
	pending map[string]*tokenCacheRetryState
	running bool
	wake    chan struct{}
}{
	pending: make(map[string]*tokenCacheRetryState),
	wake:    make(chan struct{}, 1),
}

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

func stageTokenCacheInvalidationTx(tx *gorm.DB, key string, deleteEntry bool) (CacheInvalidationTask, error) {
	if key == "" {
		return CacheInvalidationTask{}, errors.New("token key is required for cache invalidation")
	}
	return stageCacheInvalidationTaskTx(
		tx,
		cacheInvalidationKindToken,
		getTokenCacheKey(key),
		getTokenCacheVersionKey(key),
		deleteEntry,
	)
}

func stageUserTokenCacheInvalidationsTx(tx *gorm.DB, userID int, deleteEntry bool) ([]CacheInvalidationTask, error) {
	if userID <= 0 {
		return nil, errors.New("user id is required for token cache invalidation")
	}
	if !common.RedisEnabled {
		return nil, nil
	}
	var tokens []Token
	if err := tx.Unscoped().Where("user_id = ?", userID).Find(&tokens).Error; err != nil {
		return nil, err
	}
	tasks := make([]CacheInvalidationTask, 0, len(tokens))
	for _, token := range tokens {
		if token.Key == "" {
			continue
		}
		task, err := stageTokenCacheInvalidationTx(tx, token.Key, deleteEntry)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func tokenCacheRetryWindow() time.Duration {
	window := time.Duration(common.RedisKeyCacheSeconds()) * time.Second
	if window <= 0 {
		window = time.Minute
	}
	return window
}

func enqueueTokenCacheRetry(key string, deleteEntry bool, cause error) {
	if key == "" {
		return
	}
	cacheKey := getTokenCacheKey(key)
	versionKey := getTokenCacheVersionKey(key)
	enqueueTokenCacheRetryByTarget(cacheKey, versionKey, deleteEntry, cause)
}

func enqueueTokenCacheRetryByTarget(cacheKey string, versionKey string, deleteEntry bool, cause error) {
	if cacheKey == "" || versionKey == "" {
		return
	}
	now := time.Now()
	persistCacheInvalidationTask(cacheInvalidationKindToken, cacheKey, versionKey, deleteEntry, cause)

	tokenCacheRetries.Lock()
	state, exists := tokenCacheRetries.pending[cacheKey]
	if !exists {
		state = &tokenCacheRetryState{
			cacheKey:    cacheKey,
			versionKey:  versionKey,
			revision:    1,
			deleteEntry: deleteEntry,
			cause:       cause,
			delay:       tokenCacheRetryInitialDelay,
			nextAttempt: now,
		}
		if cause != nil {
			state.nextAttempt = now.Add(tokenCacheRetryInitialDelay)
		}
		state.deadline = now.Add(tokenCacheRetryWindow())
		tokenCacheRetries.pending[cacheKey] = state
	} else {
		state.revision++
		if cause != nil {
			state.cause = cause
		}
		if deleteEntry && !state.deleteEntry {
			state.deleteEntry = true
			state.deadline = now.Add(tokenCacheRetryWindow())
			state.delay = tokenCacheRetryInitialDelay
			state.nextAttempt = now
		}
	}
	startWorker := !tokenCacheRetries.running
	if startWorker {
		tokenCacheRetries.running = true
	}
	tokenCacheRetries.Unlock()

	select {
	case tokenCacheRetries.wake <- struct{}{}:
	default:
	}
	if startWorker {
		gopool.Go(runTokenCacheRetryWorker)
	}
}

func runTokenCacheRetryWorker() {
	for {
		attempts, wait, done := claimTokenCacheRetryAttempts()
		if done {
			return
		}
		if len(attempts) == 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-tokenCacheRetries.wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			continue
		}
		for _, attempt := range attempts {
			err := runTokenCacheRetryAttempt(attempt)
			completeTokenCacheRetryAttempt(attempt, err)
		}
	}
}

func claimTokenCacheRetryAttempts() ([]tokenCacheRetryAttempt, time.Duration, bool) {
	now := time.Now()
	tokenCacheRetries.Lock()
	defer tokenCacheRetries.Unlock()

	if len(tokenCacheRetries.pending) == 0 {
		tokenCacheRetries.running = false
		return nil, 0, true
	}

	attempts := make([]tokenCacheRetryAttempt, 0)
	var nextAttempt time.Time
	for cacheKey, state := range tokenCacheRetries.pending {
		if !state.deadline.IsZero() && !now.Before(state.deadline) {
			operation := "invalidation"
			if state.deleteEntry {
				operation = "deletion"
			}
			common.SysLog(fmt.Sprintf("token cache %s retry expired after %d attempts (cache_key=%s, initial_error=%v)",
				operation, state.attempts, cacheKey, state.cause))
			delete(tokenCacheRetries.pending, cacheKey)
			continue
		}
		if now.Before(state.nextAttempt) {
			if nextAttempt.IsZero() || state.nextAttempt.Before(nextAttempt) {
				nextAttempt = state.nextAttempt
			}
			continue
		}

		state.attempts++
		attempts = append(attempts, tokenCacheRetryAttempt{
			cacheKey: state.cacheKey, versionKey: state.versionKey, deleteEntry: state.deleteEntry,
			revision: state.revision, cause: state.cause, attempt: state.attempts,
		})
		state.nextAttempt = now.Add(state.delay)
		state.delay *= 2
		if state.delay > tokenCacheRetryMaxDelay {
			state.delay = tokenCacheRetryMaxDelay
		}
	}

	if len(attempts) > 0 {
		return attempts, 0, false
	}
	if len(tokenCacheRetries.pending) == 0 {
		tokenCacheRetries.running = false
		return nil, 0, true
	}
	if nextAttempt.IsZero() {
		nextAttempt = now.Add(tokenCacheRetryInitialDelay)
	}
	return nil, time.Until(nextAttempt), false
}

func runTokenCacheRetryAttempt(attempt tokenCacheRetryAttempt) error {
	if !common.RedisEnabled {
		return nil
	}
	if attempt.deleteEntry {
		return common.RedisDeleteVersionedHash(attempt.cacheKey, attempt.versionKey)
	}
	return common.RedisInvalidateVersionedHash(attempt.cacheKey, attempt.versionKey)
}

func completeTokenCacheRetryAttempt(attempt tokenCacheRetryAttempt, err error) {
	tokenCacheRetries.Lock()
	defer tokenCacheRetries.Unlock()
	state, exists := tokenCacheRetries.pending[attempt.cacheKey]
	if !exists {
		return
	}
	if err != nil && state.cause == nil {
		state.cause = err
	}
	if state.revision != attempt.revision {
		state.nextAttempt = time.Now()
		state.delay = tokenCacheRetryInitialDelay
		return
	}
	if err == nil {
		if state.deleteEntry && !attempt.deleteEntry {
			state.nextAttempt = time.Now()
			state.delay = tokenCacheRetryInitialDelay
			return
		}
		delete(tokenCacheRetries.pending, attempt.cacheKey)
		return
	}

	if attempt.attempt <= 3 || attempt.attempt%10 == 0 {
		operation := "invalidation"
		if attempt.deleteEntry {
			operation = "deletion"
		}
		initialErr := attempt.cause
		if initialErr == nil {
			initialErr = err
		}
		common.SysLog(fmt.Sprintf("token cache %s retry %d failed (initial_error=%v): %v",
			operation, attempt.attempt, initialErr, err))
	}
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

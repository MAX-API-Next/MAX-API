package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/bytedance/gopkg/util/gopool"
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

func enqueueTokenCacheRetry(key string, deleteEntry bool, cause error) {
	if key == "" {
		return
	}
	now := time.Now()
	cacheKey := getTokenCacheKey(key)
	versionKey := getTokenCacheVersionKey(key)

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
		if !deleteEntry {
			window := time.Duration(common.RedisKeyCacheSeconds()) * time.Second
			if window <= 0 {
				window = time.Minute
			}
			state.deadline = now.Add(window)
		}
		tokenCacheRetries.pending[cacheKey] = state
	} else {
		state.revision++
		if cause != nil {
			state.cause = cause
		}
		if deleteEntry && !state.deleteEntry {
			state.deleteEntry = true
			state.deadline = time.Time{}
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
		if !state.deleteEntry && !state.deadline.IsZero() && !now.Before(state.deadline) {
			common.SysLog(fmt.Sprintf("token cache invalidation retry expired after %d attempts (initial_error=%v)", state.attempts, state.cause))
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

package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	userCacheRetryInitialDelay = 50 * time.Millisecond
	userCacheRetryMaxDelay     = 5 * time.Second
)

type userCacheRetryState struct {
	userId      int
	revision    uint64
	deleteEntry bool
	cause       error
	attempts    int
	delay       time.Duration
	nextAttempt time.Time
	deadline    time.Time
}

type userCacheRetryAttempt struct {
	userId      int
	revision    uint64
	deleteEntry bool
	cause       error
	attempt     int
}

var userCacheRetries = struct {
	sync.Mutex
	pending map[int]*userCacheRetryState
	running bool
	wake    chan struct{}
}{
	pending: make(map[int]*userCacheRetryState),
	wake:    make(chan struct{}, 1),
}

// UserBase struct remains the same as it represents the cached data structure
type UserBase struct {
	Id       int    `json:"id"`
	Group    string `json:"group"`
	Email    string `json:"email"`
	Quota    int64  `json:"quota"`
	Role     int    `json:"role"`
	Status   int    `json:"status"`
	Username string `json:"username"`
	Setting  string `json:"setting"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

func getUserCacheVersionKey(userId int) string {
	return fmt.Sprintf("cache-version:user:%d", userId)
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisInvalidateVersionedHash(getUserCacheKey(userId), getUserCacheVersionKey(userId))
}

func enqueueUserCacheInvalidationRetry(userId int, cause error) {
	enqueueUserCacheRetry(userId, false, cause)
}

func enqueueUserCacheDeletionRetry(userId int, cause error) {
	enqueueUserCacheRetry(userId, true, cause)
}

func deleteUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDeleteVersionedHash(getUserCacheKey(userId), getUserCacheVersionKey(userId))
}

func deleteUserCacheAfterCommittedDelete(userId int) {
	if err := deleteUserCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("failed to delete user cache after user deletion: user_id=%d, error=%v", userId, err))
		enqueueUserCacheDeletionRetry(userId, err)
	}
}

func userCacheRetryWindow() time.Duration {
	window := time.Duration(common.RedisKeyCacheSeconds()) * time.Second
	if window <= 0 {
		window = time.Minute
	}
	return window
}

func enqueueUserCacheRetry(userId int, deleteEntry bool, cause error) {
	if userId <= 0 {
		return
	}
	now := time.Now()
	userCacheRetries.Lock()
	state, exists := userCacheRetries.pending[userId]
	if !exists {
		state = &userCacheRetryState{
			userId:      userId,
			revision:    1,
			deleteEntry: deleteEntry,
			cause:       cause,
			delay:       userCacheRetryInitialDelay,
			nextAttempt: now,
			deadline:    now.Add(userCacheRetryWindow()),
		}
		if cause != nil {
			state.nextAttempt = now.Add(userCacheRetryInitialDelay)
		}
		userCacheRetries.pending[userId] = state
	} else {
		state.revision++
		if cause != nil {
			state.cause = cause
		}
		if deleteEntry && !state.deleteEntry {
			state.deleteEntry = true
			state.deadline = now.Add(userCacheRetryWindow())
			state.delay = userCacheRetryInitialDelay
			state.nextAttempt = now
		}
	}
	startWorker := !userCacheRetries.running
	if startWorker {
		userCacheRetries.running = true
	}
	userCacheRetries.Unlock()

	select {
	case userCacheRetries.wake <- struct{}{}:
	default:
	}
	if startWorker {
		gopool.Go(runUserCacheRetryWorker)
	}
}

func runUserCacheRetryWorker() {
	for {
		attempts, wait, done := claimUserCacheRetryAttempts()
		if done {
			return
		}
		if len(attempts) == 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-userCacheRetries.wake:
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
			err := runUserCacheRetryAttempt(attempt)
			completeUserCacheRetryAttempt(attempt, err)
		}
	}
}

func claimUserCacheRetryAttempts() ([]userCacheRetryAttempt, time.Duration, bool) {
	now := time.Now()
	userCacheRetries.Lock()
	defer userCacheRetries.Unlock()

	if len(userCacheRetries.pending) == 0 {
		userCacheRetries.running = false
		return nil, 0, true
	}

	attempts := make([]userCacheRetryAttempt, 0)
	var nextAttempt time.Time
	for userId, state := range userCacheRetries.pending {
		if !state.deadline.IsZero() && !now.Before(state.deadline) {
			operation := "invalidation"
			if state.deleteEntry {
				operation = "deletion"
			}
			common.SysLog(fmt.Sprintf("user cache %s retry expired after %d attempts (user_id=%d, initial_error=%v)",
				operation, state.attempts, userId, state.cause))
			delete(userCacheRetries.pending, userId)
			continue
		}
		if now.Before(state.nextAttempt) {
			if nextAttempt.IsZero() || state.nextAttempt.Before(nextAttempt) {
				nextAttempt = state.nextAttempt
			}
			continue
		}

		state.attempts++
		attempts = append(attempts, userCacheRetryAttempt{
			userId: state.userId, deleteEntry: state.deleteEntry, revision: state.revision,
			cause: state.cause, attempt: state.attempts,
		})
		state.nextAttempt = now.Add(state.delay)
		state.delay *= 2
		if state.delay > userCacheRetryMaxDelay {
			state.delay = userCacheRetryMaxDelay
		}
	}

	if len(attempts) > 0 {
		return attempts, 0, false
	}
	if len(userCacheRetries.pending) == 0 {
		userCacheRetries.running = false
		return nil, 0, true
	}
	if nextAttempt.IsZero() {
		nextAttempt = now.Add(userCacheRetryInitialDelay)
	}
	return nil, time.Until(nextAttempt), false
}

func runUserCacheRetryAttempt(attempt userCacheRetryAttempt) error {
	if !common.RedisEnabled {
		return nil
	}
	if attempt.deleteEntry {
		return deleteUserCache(attempt.userId)
	}
	return invalidateUserCache(attempt.userId)
}

func completeUserCacheRetryAttempt(attempt userCacheRetryAttempt, err error) {
	userCacheRetries.Lock()
	defer userCacheRetries.Unlock()
	state, exists := userCacheRetries.pending[attempt.userId]
	if !exists {
		return
	}
	if err != nil && state.cause == nil {
		state.cause = err
	}
	if state.revision != attempt.revision {
		state.nextAttempt = time.Now()
		state.delay = userCacheRetryInitialDelay
		return
	}
	if err == nil {
		if state.deleteEntry && !attempt.deleteEntry {
			state.nextAttempt = time.Now()
			state.delay = userCacheRetryInitialDelay
			return
		}
		delete(userCacheRetries.pending, attempt.userId)
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
		common.SysLog(fmt.Sprintf("user cache %s retry %d failed (user_id=%d, initial_error=%v): %v",
			operation, attempt.attempt, attempt.userId, initialErr, err))
	}
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// 供 controller 等上层包在用户状态变更（如禁用、删除、角色变更）后主动清理缓存。
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

func populateUserCacheIfVersion(user User, version int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := common.RedisHSetObjIfVersion(
		getUserCacheKey(user.Id),
		getUserCacheVersionKey(user.Id),
		version,
		user.ToBaseUser(),
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
	return err
}

// updateUserCache refreshes non-quota user cache fields.
// Quota is maintained by atomic quota delta paths and must not be overwritten
// by stale user snapshots from profile/settings updates.
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	// User mutations must advance the generation before any later DB refill can
	// write a stale snapshot. The next read repopulates the complete hash.
	return invalidateUserCache(user.Id)
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (userCache *UserBase, err error) {
	// Try getting from Redis first
	userCache, err = cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}

	// If Redis fails, get from DB
	var cacheVersion int64
	cacheVersionValid := false
	if common.RedisEnabled {
		cacheVersion, err = common.RedisGetCacheVersion(getUserCacheVersionKey(userId))
		cacheVersionValid = err == nil
	}
	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err // Return nil and error if DB lookup fails
	}

	// Create cache object from user data
	userCache = &UserBase{
		Id:       user.Id,
		Group:    user.Group,
		Quota:    user.Quota,
		Role:     user.Role,
		Status:   user.Status,
		Username: user.Username,
		Setting:  user.Setting,
		Email:    user.Email,
	}
	if cacheVersionValid {
		userSnapshot := *user
		gopool.Go(func() {
			if err := populateUserCacheIfVersion(userSnapshot, cacheVersion); err != nil {
				common.SysLog("failed to update user status cache: " + err.Error())
			}
		})
	}

	return userCache, nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id == 0 {
		userCache.Id = userId
	}
	if userCache.Role < common.RoleCommonUser || userCache.Status == 0 {
		return nil, fmt.Errorf("stale user cache")
	}
	return &userCache, nil
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int64, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserStatusCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Status, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	if !common.RedisEnabled {
		return nil
	}
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return updateUserCacheField(userId, "Status", fmt.Sprintf("%d", statusInt))
}

func updateUserQuotaCacheIfVersion(userId int, quota int64, version int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := common.RedisHSetFieldIfVersion(
		getUserCacheKey(userId),
		getUserCacheVersionKey(userId),
		version,
		"Quota",
		quota,
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
	return err
}

func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheField(userId, "Group", group)
}

func updateUserRoleCache(userId int, role int) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheField(userId, "Role", fmt.Sprintf("%d", role))
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserEmailCache(userId int, email string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheField(userId, "Email", email)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheField(userId, "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return updateUserCacheField(userId, "Setting", setting)
}

func updateUserCacheField(userId int, field string, value interface{}) error {
	if err := invalidateUserCache(userId); err != nil {
		return err
	}
	version, err := common.RedisGetCacheVersion(getUserCacheVersionKey(userId))
	if err != nil {
		return err
	}
	return updateUserCacheFieldIfVersion(userId, field, value, version)
}

func updateUserCacheFieldIfVersion(userId int, field string, value interface{}, version int64) error {
	_, err := common.RedisHSetFieldIfVersion(
		getUserCacheKey(userId),
		getUserCacheVersionKey(userId),
		version,
		field,
		value,
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
	return err
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}

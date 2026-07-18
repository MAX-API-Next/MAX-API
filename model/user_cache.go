package model

import (
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
)

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
	if err := updateUserRoleCache(user.Id, user.Role); err != nil {
		return err
	}
	if err := updateUserGroupCache(user.Id, user.Group); err != nil {
		return err
	}
	if err := updateUserEmailCache(user.Id, user.Email); err != nil {
		return err
	}
	if err := updateUserStatusCache(user.Id, user.Status == common.UserStatusEnabled); err != nil {
		return err
	}
	if err := updateUserNameCache(user.Id, user.Username); err != nil {
		return err
	}
	return updateUserSettingCache(user.Id, user.Setting)
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
	return common.RedisHSetField(getUserCacheKey(userId), "Status", fmt.Sprintf("%d", statusInt))
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
	return common.RedisHSetField(getUserCacheKey(userId), "Group", group)
}

func updateUserRoleCache(userId int, role int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Role", fmt.Sprintf("%d", role))
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserEmailCache(userId int, email string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Email", email)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Setting", setting)
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

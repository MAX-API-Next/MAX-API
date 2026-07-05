package model

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedAdminStatusUsers(t *testing.T) {
	t.Helper()
	setupUserUpdateTestState(t)
	for _, user := range []User{
		{Id: 501, Username: "status-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
		{Id: 502, Username: "status-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 503, Username: "status-peer", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 504, Username: "status-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 12345, UsedQuota: 678, RequestCount: 9},
	} {
		user.AffCode = user.Username
		require.NoError(t, DB.Create(&user).Error)
	}
}

func TestAdminUserStatusChangesOnlyStatusAndIsRepeatSafe(t *testing.T) {
	seedAdminStatusUsers(t)
	for _, status := range []int{common.UserStatusDisabled, common.UserStatusEnabled} {
		expected := common.UserStatusEnabled
		if status == common.UserStatusEnabled {
			expected = common.UserStatusDisabled
		}
		changed, err := SetUserStatusByAdmin(context.Background(), 502, 504, expected, status)
		require.NoError(t, err)
		require.True(t, changed.Changed)
		again, err := SetUserStatusByAdmin(context.Background(), 502, 504, expected, status)
		require.NoError(t, err)
		require.False(t, again.Changed)
		var user User
		require.NoError(t, DB.First(&user, 504).Error)
		require.Equal(t, status, user.Status)
		require.EqualValues(t, 12345, user.Quota)
		require.EqualValues(t, 678, user.UsedQuota)
		require.Equal(t, 9, user.RequestCount)
		require.Equal(t, common.RoleCommonUser, user.Role)
		require.False(t, user.DeletedAt.Valid)
	}
}

func TestAdminUserStatusRejectsProtectedAndStaleTargets(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		actor, target, expected int
		want                    error
	}{
		{"self", 502, 502, 1, ErrUserStatusForbidden},
		{"root", 501, 501, 1, ErrUserStatusForbidden},
		{"peer", 502, 503, 1, ErrUserStatusForbidden},
		{"higher", 502, 501, 1, ErrUserStatusForbidden},
		{"non-admin", 504, 503, 1, ErrUserStatusForbidden},
		{"missing-actor", 999, 504, 1, ErrUserStatusForbidden},
		{"missing-target", 502, 999, 1, gorm.ErrRecordNotFound},
		{"stale-status", 502, 504, 2, ErrUserStatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seedAdminStatusUsers(t)
			_, err := SetUserStatusByAdmin(context.Background(), tc.actor, tc.target, tc.expected, 2)
			require.ErrorIs(t, err, tc.want)
			var user User
			require.NoError(t, DB.First(&user, 504).Error)
			require.Equal(t, common.UserStatusEnabled, user.Status)
		})
	}
}

func TestAdminUserStatusRechecksActorAndTargetFromDatabase(t *testing.T) {
	seedAdminStatusUsers(t)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 502).Update("status", 2).Error)
	_, err := SetUserStatusByAdmin(context.Background(), 502, 504, 1, 2)
	require.ErrorIs(t, err, ErrUserStatusForbidden)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 504).Update("role", common.RoleRootUser).Error)
	_, err = SetUserStatusByAdmin(context.Background(), 501, 504, 1, 2)
	require.ErrorIs(t, err, ErrUserStatusForbidden)
	require.NoError(t, DB.Delete(&User{}, 503).Error)
	_, err = SetUserStatusByAdmin(context.Background(), 501, 503, 1, 2)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestAdminUserStatusConcurrentDuplicatesChangeOnce(t *testing.T) {
	seedAdminStatusUsers(t)
	var changed atomic.Int32
	var wg sync.WaitGroup
	errorsCh := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := SetUserStatusByAdmin(context.Background(), 502, 504, 1, 2)
			if result.Changed {
				changed.Add(1)
			}
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, changed.Load())
}

func TestAdminUserStatusAllowsRootToManageAdminAndRejectsCancelledWork(t *testing.T) {
	seedAdminStatusUsers(t)
	result, err := SetUserStatusByAdmin(context.Background(), 501, 502, 1, 2)
	require.NoError(t, err)
	require.True(t, result.Changed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = SetUserStatusByAdmin(ctx, 501, 504, 1, 2)
	require.ErrorIs(t, err, context.Canceled)
	var user User
	require.NoError(t, DB.First(&user, 504).Error)
	require.Equal(t, common.UserStatusEnabled, user.Status)
	for _, values := range [][2]int{{0, 2}, {1, -1}, {1, 100}} {
		_, err = SetUserStatusByAdmin(context.Background(), 501, 504, values[0], values[1])
		require.ErrorIs(t, err, ErrUserStatusConflict)
	}
}

func TestAdminUserStatusRollsBackWhenCacheIntentFails(t *testing.T) {
	seedAdminStatusUsers(t)
	useFailingUserUpdateRedis(t)
	const callback = "test:status-outbox-error"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_invalidation_tasks" {
			tx.AddError(errors.New("outbox unavailable"))
		}
	}))
	t.Cleanup(func() { DB.Callback().Create().Remove(callback) })
	_, err := SetUserStatusByAdmin(context.Background(), 502, 504, 1, 2)
	require.ErrorContains(t, err, "outbox unavailable")
	var user User
	require.NoError(t, DB.First(&user, 504).Error)
	require.Equal(t, common.UserStatusEnabled, user.Status)
}

func TestAdminUserStatusKeepsDurableCacheIntentOnRedisFailure(t *testing.T) {
	seedAdminStatusUsers(t)
	useFailingUserUpdateRedis(t)
	result, err := SetUserStatusByAdmin(context.Background(), 502, 504, 1, 2)
	require.NoError(t, err)
	require.True(t, result.Changed)
	var task CacheInvalidationTask
	require.NoError(t, DB.Where("kind = ? AND entity_key = ?", cacheInvalidationKindUser, "504").First(&task).Error)
	cached, err := GetUserCache(504)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusDisabled, cached.Status)
}

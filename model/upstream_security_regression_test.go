package model

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func assertSecurityUpdateColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:security_columns", func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]interface{}); ok && tx.Statement.Schema != nil {
			for column := range values {
				if _, ok := tx.Statement.Schema.FieldsByDBName[column]; !ok {
					t.Errorf("unknown emitted update column: %s", column)
					tx.AddError(errors.New("invalid update column"))
				}
			}
		}
	}))
}

func isolateSecurityChannelCache(t *testing.T) {
	t.Helper()
	channelSyncLock.Lock()
	oldChannels, oldGroups := channelsIDM, group2model2channels
	oldCustom, oldErrors := channel2advancedCustomConfig, channel2advancedCustomConfigError
	channelsIDM, group2model2channels = nil, nil
	channel2advancedCustomConfig, channel2advancedCustomConfigError = nil, nil
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		defer channelSyncLock.Unlock()
		channelsIDM, group2model2channels = oldChannels, oldGroups
		channel2advancedCustomConfig, channel2advancedCustomConfigError = oldCustom, oldErrors
	})
}

func TestTwoFAUsageWriteCannotRestoreCredentialSnapshot(t *testing.T) {
	setupAuthFlowTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TwoFA{}))
	assertSecurityUpdateColumns(t, DB)
	factor := TwoFA{UserId: 1, Secret: "old", IsEnabled: true}
	require.NoError(t, DB.Create(&factor).Error)
	require.NoError(t, DB.Model(&TwoFA{}).Where("id = ?", factor.Id).Updates(map[string]interface{}{"secret": "rotated", "is_enabled": false}).Error)
	now := time.Now()
	factor.LastUsedAt = &now
	require.NoError(t, factor.updateUsageState())
	var stored TwoFA
	require.NoError(t, DB.First(&stored, factor.Id).Error)
	require.Equal(t, "rotated", stored.Secret)
	require.False(t, stored.IsEnabled)
}

func TestTwoFAFailureWindowRestartsAfterExpiry(t *testing.T) {
	setupAuthFlowTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TwoFA{}))
	assertSecurityUpdateColumns(t, DB)
	expired := time.Now().Add(-time.Minute)
	factor := TwoFA{UserId: 1, Secret: "synthetic", IsEnabled: true, FailedAttempts: 5, LockedUntil: &expired}
	require.NoError(t, DB.Create(&factor).Error)
	require.NoError(t, factor.IncrementFailedAttempts())
	var stored TwoFA
	require.NoError(t, DB.First(&stored, factor.Id).Error)
	require.Equal(t, 1, stored.FailedAttempts)
	require.Nil(t, stored.LockedUntil)
	// A stale caller must increment the authoritative value, never overwrite it.
	stale := TwoFA{Id: factor.Id}
	require.NoError(t, stale.IncrementFailedAttempts())
	require.Equal(t, 2, stale.FailedAttempts)
}

func TestTwoFAFailureCountUsesStoredStateAndDoesNotExtendLock(t *testing.T) {
	setupAuthFlowTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TwoFA{}))
	assertSecurityUpdateColumns(t, DB)
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	factor := TwoFA{UserId: 1, Secret: "synthetic", IsEnabled: true}
	require.NoError(t, DB.Create(&factor).Error)
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- (&TwoFA{Id: factor.Id}).IncrementFailedAttempts() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var stored TwoFA
	require.NoError(t, DB.First(&stored, factor.Id).Error)
	require.Equal(t, 3, stored.FailedAttempts)
	require.Equal(t, "synthetic", stored.Secret)
	deadline := time.Now().Add(time.Minute)
	require.NoError(t, DB.Model(&stored).Updates(map[string]interface{}{"failed_attempts": 5, "locked_until": deadline}).Error)
	require.NoError(t, factor.IncrementFailedAttempts())
	require.Equal(t, 5, factor.FailedAttempts)
	require.WithinDuration(t, deadline, *factor.LockedUntil, time.Millisecond)
}

func TestAccountBindingChecksAuthoritativeSession(t *testing.T) {
	setupAuthFlowTestDB(t)
	require.NoError(t, DB.AutoMigrate(&CacheInvalidationTask{}))
	user := User{Username: "session-identity", Email: "old@example.com", Status: common.UserStatusEnabled, SessionGeneration: 2}
	require.NoError(t, DB.Create(&user).Error)
	for _, bind := range []func(int64) error{
		func(gen int64) error { return BindEmailToUserWithSession(user.Id, gen, "new@example.com") },
		func(gen int64) error { return BindWeChatToUserWithSession(user.Id, gen, "synthetic-wechat") },
	} {
		require.ErrorIs(t, bind(1), ErrUserSessionInvalid)
		require.NoError(t, DB.Model(&user).Update("status", common.UserStatusDisabled).Error)
		require.ErrorIs(t, bind(2), ErrUserSessionInvalid)
		require.NoError(t, DB.Model(&user).Update("status", common.UserStatusEnabled).Error)
		require.NoError(t, bind(2))
	}
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, "new@example.com", stored.Email)
	require.Equal(t, "synthetic-wechat", stored.WeChatId)
}

func TestTwoFAChallengeRechecksGenerationAtConsumption(t *testing.T) {
	setupAuthFlowTestDB(t)
	user := User{Username: "challenge", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	token, err := CreateTwoFALoginChallenge(&user, "session-a")
	require.NoError(t, err)
	_, err = GetTwoFALoginChallenge(token, "session-b")
	require.Error(t, err)
	_, err = GetTwoFALoginChallenge(token, "session-a")
	require.NoError(t, err)
	// Revocation after preflight must still defeat final challenge consumption.
	require.NoError(t, DB.Model(&user).Update("session_generation", 1).Error)
	_, err = CompleteTwoFALoginChallenge(token, "session-a")
	require.ErrorIs(t, err, ErrUserSessionInvalid)
}

func TestChannelStatusRollbackDoesNotPublishCache(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	isolateSecurityChannelCache(t)
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	channel.ChannelInfo.MultiKeyPollingIndex = 2
	CacheUpdateChannel(channel)
	assertSecurityUpdateColumns(t, db)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:reject_status", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			tx.AddError(errors.New("synthetic rejected write"))
		}
	}))
	require.False(t, UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic"))
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, cached.Status)
	require.Equal(t, 2, cached.ChannelInfo.MultiKeyPollingIndex)
	require.NotContains(t, cached.ChannelInfo.MultiKeyStatusList, 0)
}

func TestChannelStatusPublicationReleasesPollingLockAndPreservesLatestCursor(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	isolateSecurityChannelCache(t)
	channel.ChannelInfo.MultiKeyMode = constant.MultiKeyModePolling
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{}
	channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{}
	channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{}
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", channel.ChannelInfo).Error)
	channel.ChannelInfo.MultiKeyPollingIndex = 1
	CacheUpdateChannel(channel)
	updated := make(chan struct{}, 1)
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:status_reached_write", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			updated <- struct{}{}
		}
	}))
	// A cache reader prevents publication. The updater must release polling
	// before waiting for the cache writer, or cache -> polling readers deadlock.
	channelSyncLock.RLock()
	completed := make(chan bool, 1)
	go func() {
		completed <- UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic")
	}()
	<-updated
	advanced := make(chan struct{})
	go func() {
		lock := GetChannelPollingLock(channel.Id)
		lock.Lock()
		// Simulate the last in-flight request advancing while publication waits.
		channel.ChannelInfo.MultiKeyPollingIndex = 2
		lock.Unlock()
		close(advanced)
	}()
	select {
	case <-advanced:
	case <-time.After(time.Second):
		t.Error("status publication held polling while waiting for the cache lock")
	}
	channelSyncLock.RUnlock()
	require.True(t, <-completed)
	<-advanced
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, 2, cached.ChannelInfo.MultiKeyPollingIndex, "publication must preserve the latest cursor, not a pre-unlock snapshot")
	require.Equal(t, common.ChannelStatusAutoDisabled, cached.ChannelInfo.MultiKeyStatusList[0])
	_, index, keyErr := cached.GetNextEnabledKey()
	require.Nil(t, keyErr)
	require.Equal(t, 2, index)
}

func TestDelayedStatusPublicationCannotRestoreAdministrativeConfig(t *testing.T) {
	for _, queryFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-config", true: "query-failed"}[queryFailed], func(t *testing.T) {
			db, channel := setupChannelUpdateFieldsTestDB(t)
			oldMemory := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = true
			t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
			isolateSecurityChannelCache(t)
			CacheUpdateChannel(channel)
			require.True(t, UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic"))
			var delayed Channel
			require.NoError(t, db.First(&delayed, channel.Id).Error)
			// The administrative write/publication finishes after the status SQL
			// transaction, before its delayed cache publication resumes.
			admin := cloneChannelForUpdate(&delayed)
			baseURL := "https://new-provider.invalid"
			admin.Key, admin.BaseURL, admin.Status = "new-key", &baseURL, common.ChannelStatusManuallyDisabled
			require.NoError(t, admin.UpdateFields("key", "base_url", "status"))
			InitChannelCache()
			if queryFailed {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:status_publish_query_failed", func(tx *gorm.DB) {
					if tx.Statement.Table == "channels" {
						tx.AddError(errors.New("synthetic publication read failed"))
					}
				}))
			}
			cacheUpdateChannel(&delayed, true)
			cached, err := CacheGetChannel(channel.Id)
			require.NoError(t, err)
			require.Equal(t, "new-key", cached.Key)
			require.Equal(t, "https://new-provider.invalid", *cached.BaseURL)
			require.Equal(t, common.ChannelStatusManuallyDisabled, cached.Status)
		})
	}
}

func TestChannelRefreshCannotPublishOlderSnapshotLast(t *testing.T) {
	for _, kind := range []string{"full", "status"} {
		t.Run(kind, func(t *testing.T) {
			db, channel := setupChannelUpdateFieldsTestDB(t)
			oldMemory := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = true
			t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
			isolateSecurityChannelCache(t)
			CacheUpdateChannel(channel)
			read, release := make(chan struct{}), make(chan struct{})
			var snapshotReads atomic.Int32
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:pause_old_channel_snapshot", func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*[]*Channel); ok && snapshotReads.Add(1) == 1 {
					close(read)
					<-release
				}
			}))
			firstDone, secondDone := make(chan struct{}), make(chan struct{})
			go func() { defer close(firstDone); InitChannelCache() }()
			<-read
			newInfo := cloneChannelForUpdate(channel).ChannelInfo
			newInfo.MultiKeyStatusList[1] = common.ChannelStatusManuallyDisabled
			require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", newInfo).Error)
			go func() {
				defer close(secondDone)
				if kind == "full" {
					InitChannelCache()
				} else {
					UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic")
				}
			}()
			publishedEarly := false
			select {
			case <-secondDone:
				publishedEarly = true
			case <-time.After(time.Second):
			}
			close(release)
			<-firstDone
			<-secondDone
			require.False(t, publishedEarly, "a later publication must wait for the paused full refresh")
			cached, err := CacheGetChannel(channel.Id)
			require.NoError(t, err)
			require.Equal(t, common.ChannelStatusManuallyDisabled, cached.ChannelInfo.MultiKeyStatusList[1])
			if kind == "status" {
				require.Equal(t, common.ChannelStatusAutoDisabled, cached.ChannelInfo.MultiKeyStatusList[0])
			}
		})
	}
}

func TestFailedChannelRefreshPreservesPublishedCache(t *testing.T) {
	for _, failedTable := range []string{"channels", "abilities"} {
		t.Run(failedTable, func(t *testing.T) {
			db, channel := setupChannelUpdateFieldsTestDB(t)
			oldMemory := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = true
			t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
			isolateSecurityChannelCache(t)
			InitChannelCache()
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:reject_channel_refresh_read", func(tx *gorm.DB) {
				if tx.Statement.Table == failedTable {
					tx.AddError(errors.New("synthetic full refresh read failed"))
				}
			}))
			InitChannelCache()
			cached, err := CacheGetChannel(channel.Id)
			require.NoError(t, err)
			require.Equal(t, channel.Key, cached.Key)
			require.Equal(t, channel.Status, cached.Status)
		})
	}
}

func TestIncompleteChannelRefreshKeepsCacheUntilAbilitiesCatchUp(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	isolateSecurityChannelCache(t)
	InitChannelCache()
	// Equivalent to reads straddling an admin transaction: the channel's new
	// group is visible, but its matching abilities are not in this snapshot.
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("group", "new-group").Error)
	InitChannelCache()
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, "default", cached.Group)
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("group", "new-group").Error)
	InitChannelCache()
	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, "new-group", cached.Group)
}

func TestRecoveredChannelImmediatelyReturnsToRoutingCandidates(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	isolateSecurityChannelCache(t)
	InitChannelCache()
	require.True(t, UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic"))
	require.True(t, UpdateChannelStatus(channel.Id, "second-key", common.ChannelStatusAutoDisabled, "synthetic"))
	selected, err := GetRandomSatisfiedChannelExcluding("default", "gpt-test", 0, "", nil)
	require.NoError(t, err)
	require.Nil(t, selected)
	require.True(t, RecoverChannelStatus(channel.Id, "first-key"))
	selected, err = GetRandomSatisfiedChannelExcluding("default", "gpt-test", 0, "", nil)
	require.NoError(t, err)
	require.NotNil(t, selected, "recovery must restore the only routing candidate without a full refresh")
	require.Equal(t, channel.Id, selected.Id)
	var ability Ability
	require.NoError(t, db.First(&ability, "channel_id = ?", channel.Id).Error)
	require.True(t, ability.Enabled)
}

func TestStatusPublicationRebuildsRoutingMembershipWithoutDuplicates(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	isolateSecurityChannelCache(t)
	InitChannelCache()
	low, high := int64(1), int64(20)
	peer := &Channel{Name: "peer", Key: "peer-key", Models: "gpt-test", Group: "default", Status: common.ChannelStatusEnabled, Priority: &low}
	require.NoError(t, db.Create(peer).Error)
	require.NoError(t, peer.AddAbilities(nil))
	InitChannelCache()
	// This snapshot is visible before the administrator's full refresh; a
	// status publication must repair group/model membership along with the row.
	channel.Group, channel.Models, channel.Priority = "default,new-group", "gpt-test,new-model", &high
	require.NoError(t, channel.UpdateFields("group", "models", "priority"))
	for range 2 {
		require.True(t, UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic"))
		require.True(t, RecoverChannelStatus(channel.Id, "first-key"))
	}
	func() {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		require.Equal(t, []int{channel.Id, peer.Id}, group2model2channels["default"]["gpt-test"])
		require.Equal(t, []int{channel.Id}, group2model2channels["new-group"]["new-model"])
	}()
	channel.Group, channel.Models = "moved-group", "moved-model"
	require.NoError(t, channel.UpdateFields("group", "models"))
	require.True(t, UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "synthetic"))
	require.True(t, RecoverChannelStatus(channel.Id, "first-key"))
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	require.Equal(t, []int{peer.Id}, group2model2channels["default"]["gpt-test"])
	require.Empty(t, group2model2channels["new-group"]["new-model"])
	require.Equal(t, []int{channel.Id}, group2model2channels["moved-group"]["moved-model"])
}

func TestLateAutomaticDisablePreservesManualChannel(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "stale-cache"}[memory], func(t *testing.T) {
			db, channel := setupChannelUpdateFieldsTestDB(t)
			oldMemory := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = memory
			t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
			if memory {
				isolateSecurityChannelCache(t)
				CacheUpdateChannel(channel)
			}
			const reason = `{"status_reason":"administrator pause"}`
			require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]interface{}{"status": common.ChannelStatusManuallyDisabled, "other_info": reason}).Error)
			if memory {
				cached, err := CacheGetChannel(channel.Id)
				require.NoError(t, err)
				require.Equal(t, common.ChannelStatusEnabled, cached.Status, "fixture must really retain an old enabled cache")
			}
			require.False(t, UpdateChannelStatus(channel.Id, "first-key", common.ChannelStatusAutoDisabled, "late failure"))
			var stored Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
			require.Equal(t, reason, stored.OtherInfo)
		})
	}
}

func TestAutomaticRecoveryCannotUndoManualDisableWithEmptyKey(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
	for _, key := range []string{"", "first-key"} {
		require.False(t, RecoverChannelStatus(channel.Id, key))
	}
	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	// Explicit manual state changes keep the original API semantics.
	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "administrator enable"))
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
}

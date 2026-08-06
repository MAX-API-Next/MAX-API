package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelUpdateFieldsTestDB(t *testing.T) (*gorm.DB, *Channel) {
	t.Helper()

	originalDB := DB
	originalLogDB := LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		_ = sqlDB.Close()
	})

	priority := int64(10)
	weight := uint(20)
	tag := "stable"
	channel := &Channel{
		Name:     "channel-update-fields",
		Key:      "first-key\nsecond-key\nthird-key",
		Status:   common.ChannelStatusEnabled,
		Models:   "gpt-test",
		Group:    "default",
		Priority: &priority,
		Weight:   &weight,
		Tag:      &tag,
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           3,
			MultiKeyStatusList:     map[int]int{2: 2},
			MultiKeyDisabledTime:   map[int]int64{2: 123},
			MultiKeyDisabledReason: map[int]string{2: "failed"},
		},
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	return db, channel
}

func installFailingAbilityDeleteTrigger(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_ability_delete BEFORE DELETE ON abilities BEGIN SELECT RAISE(ABORT, 'ability delete blocked'); END`).Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS fail_ability_delete").Error })
}

func TestUpdateFieldsSkipsAbilityRebuildForChannelInfoOnly(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	installFailingAbilityDeleteTrigger(t, db)

	channel.ChannelInfo.MultiKeyPollingIndex = 1
	require.NoError(t, channel.UpdateFields("channel_info"))

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	require.EqualValues(t, 1, abilityCount)
}

func TestUpdateFieldsRollsBackChannelWhenAbilityRebuildFails(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	installFailingAbilityDeleteTrigger(t, db)

	channel.Status = common.ChannelStatusManuallyDisabled
	require.Error(t, channel.UpdateFields("status"))

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
}

func TestUpdateFieldsPersistsMultiKeyInfoWhenOnlyKeyChanges(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)

	channel.Key = "first-key\nsecond-key"
	require.NoError(t, channel.UpdateFields("key"))

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, "first-key\nsecond-key", stored.Key)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
	_, exists := stored.ChannelInfo.MultiKeyStatusList[2]
	require.False(t, exists)
	_, exists = stored.ChannelInfo.MultiKeyDisabledTime[2]
	require.False(t, exists)
	_, exists = stored.ChannelInfo.MultiKeyDisabledReason[2]
	require.False(t, exists)
}

func TestUpdateFieldsClearsFinalMultiKeyAndMetadata(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)

	channel.Key = ""
	require.NoError(t, channel.UpdateFields("key"))

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Empty(t, stored.Key)
	require.Zero(t, stored.ChannelInfo.MultiKeySize)
	require.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	require.Empty(t, stored.ChannelInfo.MultiKeyDisabledTime)
	require.Empty(t, stored.ChannelInfo.MultiKeyDisabledReason)
}

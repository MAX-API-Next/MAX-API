package model

import (
	"fmt"
	"maps"
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

func populatedChannelForEditableUpdateMap() *Channel {
	stringPtr := func(value string) *string { return &value }
	priority := int64(1)
	weight := uint(2)
	autoBan := 1
	return &Channel{
		Type:               3,
		Key:                "key",
		OpenAIOrganization: stringPtr("org"),
		TestModel:          stringPtr("gpt-test"),
		Status:             common.ChannelStatusEnabled,
		Name:               "schema-check-channel",
		Weight:             &weight,
		BaseURL:            stringPtr("https://example.com"),
		Other:              "{}",
		Models:             "gpt-test",
		Group:              "default",
		ModelMapping:       stringPtr("{}"),
		StatusCodeMapping:  stringPtr("{}"),
		Priority:           &priority,
		AutoBan:            &autoBan,
		Tag:                stringPtr("tag"),
		Setting:            stringPtr("{}"),
		ParamOverride:      stringPtr("{}"),
		HeaderOverride:     stringPtr("{}"),
		Remark:             stringPtr("remark"),
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         1,
			MultiKeyPollingIndex: 0,
		},
		OtherSettings: "{}",
	}
}

func TestEditableUpdateMapUsesOnlyChannelSchemaColumns(t *testing.T) {
	db, _ := setupChannelUpdateFieldsTestDB(t)

	stmt := &gorm.Statement{DB: db}
	require.NoError(t, stmt.Parse(&Channel{}))

	schemaColumns := make(map[string]struct{}, len(stmt.Schema.DBNames))
	for _, column := range stmt.Schema.DBNames {
		schemaColumns[column] = struct{}{}
	}

	channel := populatedChannelForEditableUpdateMap()
	for _, field := range channelEditableUpdateFields {
		updates := channel.editableUpdateMap([]string{field})
		require.NotEmpty(t, updates, "editable field %q is not handled by editableUpdateMap or the test fixture lacks its value", field)
		for column := range updates {
			require.Contains(t, schemaColumns, column, "editable field %q maps to unknown channel schema column %q", field, column)
			require.True(t, db.Migrator().HasColumn(&Channel{}, column), "editable field %q maps to missing database column %q", field, column)
		}
	}
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

func TestUpdateFieldsPersistsOpenAIOrganizationUsingSchemaColumn(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)

	organization := "org-update-fields"
	channel.OpenAIOrganization = &organization

	require.NoError(t, channel.UpdateFields("openai_organization"))

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.NotNil(t, stored.OpenAIOrganization)
	require.Equal(t, organization, *stored.OpenAIOrganization)
}

func TestUpdateFieldsRollsBackChannelWhenAbilityRebuildFails(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)
	installFailingAbilityDeleteTrigger(t, db)

	channel.Key = "first-key\nsecond-key"
	channel.Status = common.ChannelStatusManuallyDisabled
	priority := int64(99)
	channel.Priority = &priority
	expected := *channel
	expectedPriority := *channel.Priority
	expected.Priority = &expectedPriority
	expected.ChannelInfo.MultiKeyStatusList = maps.Clone(channel.ChannelInfo.MultiKeyStatusList)
	expected.ChannelInfo.MultiKeyDisabledTime = maps.Clone(channel.ChannelInfo.MultiKeyDisabledTime)
	expected.ChannelInfo.MultiKeyDisabledReason = maps.Clone(channel.ChannelInfo.MultiKeyDisabledReason)
	expected.Keys = append([]string(nil), channel.Keys...)

	require.Error(t, channel.UpdateFields("key", "status", "priority"))
	require.Equal(t, expected, *channel)

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	require.NotNil(t, stored.Priority)
	require.EqualValues(t, 10, *stored.Priority)
	require.Equal(t, "first-key\nsecond-key\nthird-key", stored.Key)
	require.Equal(t, 3, stored.ChannelInfo.MultiKeySize)
	require.Equal(t, map[int]int{2: 2}, stored.ChannelInfo.MultiKeyStatusList)
	require.Equal(t, map[int]int64{2: 123}, stored.ChannelInfo.MultiKeyDisabledTime)
	require.Equal(t, map[int]string{2: "failed"}, stored.ChannelInfo.MultiKeyDisabledReason)
}

func TestUpdateFieldsPersistsMultiKeyInfoWhenOnlyKeyChanges(t *testing.T) {
	db, channel := setupChannelUpdateFieldsTestDB(t)

	channel.Keys = channel.GetKeys()
	require.Equal(t, []string{"first-key", "second-key", "third-key"}, channel.Keys)
	channel.Key = "first-key\nsecond-key"
	require.NoError(t, channel.UpdateFields("key"))
	require.Equal(t, []string{"first-key", "second-key"}, channel.GetKeys())
	require.Equal(t, 2, channel.ChannelInfo.MultiKeySize)
	require.NotContains(t, channel.ChannelInfo.MultiKeyStatusList, 2)
	require.NotContains(t, channel.ChannelInfo.MultiKeyDisabledTime, 2)
	require.NotContains(t, channel.ChannelInfo.MultiKeyDisabledReason, 2)

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
	require.Zero(t, channel.ChannelInfo.MultiKeySize)
	require.Empty(t, channel.ChannelInfo.MultiKeyStatusList)
	require.Empty(t, channel.ChannelInfo.MultiKeyDisabledTime)
	require.Empty(t, channel.ChannelInfo.MultiKeyDisabledReason)

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Empty(t, stored.Key)
	require.Zero(t, stored.ChannelInfo.MultiKeySize)
	require.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	require.Empty(t, stored.ChannelInfo.MultiKeyDisabledTime)
	require.Empty(t, stored.ChannelInfo.MultiKeyDisabledReason)
}

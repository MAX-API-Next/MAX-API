package model

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func advancedCustomSettingsForSelectionTest(t *testing.T, routes []dto.AdvancedCustomRoute) string {
	t.Helper()
	data, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: routes},
	})
	require.NoError(t, err)
	return string(data)
}

func insertChannelSelectionCandidate(
	t *testing.T,
	channelID int,
	channelType int,
	otherSettings string,
	modelName string,
	group string,
	priority int64,
) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{
		Id:            channelID,
		Type:          channelType,
		Key:           fmt.Sprintf("key-%d", channelID),
		Status:        common.ChannelStatusEnabled,
		Name:          fmt.Sprintf("selection-channel-%d", channelID),
		Models:        modelName,
		Group:         group,
		OtherSettings: otherSettings,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
		Priority:  &priority,
		Weight:    100,
	}).Error)
}

func TestGetChannelFiltersRequestPathBeforePrioritySelection(t *testing.T) {
	clearPreferredOwnerTables(t)
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
	})

	const (
		groupName = "default"
		modelName = "gpt-selection"
	)
	insertChannelSelectionCandidate(
		t,
		1,
		constant.ChannelTypeAdvancedCustom,
		advancedCustomSettingsForSelectionTest(t, []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/responses",
				Converter:    dto.AdvancedCustomConverterNone,
			},
		}),
		modelName,
		groupName,
		10,
	)
	insertChannelSelectionCandidate(t, 2, constant.ChannelTypeOpenAI, "", modelName, groupName, 1)

	channel, err := GetChannel(groupName, modelName, 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
}

func TestGetChannelSkipsInvalidAdvancedCustomConfig(t *testing.T) {
	clearPreferredOwnerTables(t)
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
	})

	const (
		groupName = "default"
		modelName = "gpt-invalid-advanced"
	)
	invalidButMatchingSettings := advancedCustomSettingsForSelectionTest(t, []dto.AdvancedCustomRoute{
		{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
			Converter:    dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions,
		},
	})
	insertChannelSelectionCandidate(t, 1, constant.ChannelTypeAdvancedCustom, invalidButMatchingSettings, modelName, groupName, 10)
	insertChannelSelectionCandidate(t, 2, constant.ChannelTypeOpenAI, "", modelName, groupName, 1)

	channel, err := GetChannel(groupName, modelName, 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})
	assert.False(t, ChannelSupportsRequestPath(&Channel{
		Id:            1,
		Type:          constant.ChannelTypeAdvancedCustom,
		OtherSettings: invalidButMatchingSettings,
	}, "/v1/chat/completions"))
}

func TestMemoryCacheSelectionSkipsInvalidAdvancedCustomConfig(t *testing.T) {
	clearPreferredOwnerTables(t)
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
		channelSyncLock.Lock()
		group2model2channels = nil
		channelsIDM = nil
		channel2advancedCustomConfig = nil
		channel2advancedCustomConfigError = nil
		channelSyncLock.Unlock()
	})

	const (
		groupName = "default"
		modelName = "gpt-invalid-cache"
	)
	invalidButMatchingSettings := advancedCustomSettingsForSelectionTest(t, []dto.AdvancedCustomRoute{
		{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
			Converter:    dto.AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions,
		},
	})
	insertChannelSelectionCandidate(t, 1, constant.ChannelTypeAdvancedCustom, invalidButMatchingSettings, modelName, groupName, 10)
	insertChannelSelectionCandidate(t, 2, constant.ChannelTypeOpenAI, "", modelName, groupName, 1)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})
	InitChannelCache()

	channel, err := GetRandomSatisfiedChannel(groupName, modelName, 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
}

func TestAlphaSearchSelectionUsesOnlySupportedChannelTypes(t *testing.T) {
	clearPreferredOwnerTables(t)
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
		channelSyncLock.Lock()
		group2model2channels = nil
		channelsIDM = nil
		channel2advancedCustomConfig = nil
		channel2advancedCustomConfigError = nil
		channelSyncLock.Unlock()
	})

	const (
		groupName         = "default"
		modelName         = "gpt-alpha-selection"
		advancedModelName = "gpt-alpha-advanced-selection"
	)
	insertChannelSelectionCandidate(t, 1, constant.ChannelTypeOpenAI, "", modelName, groupName, 10)
	insertChannelSelectionCandidate(t, 2, constant.ChannelTypeCodex, "", modelName, groupName, 1)
	advancedSettings := advancedCustomSettingsForSelectionTest(t, []dto.AdvancedCustomRoute{
		{
			IncomingPath: "/v1/alpha/search",
			UpstreamPath: "/v1/alpha/search",
			Converter:    dto.AdvancedCustomConverterNone,
		},
	})
	insertChannelSelectionCandidate(t, 3, constant.ChannelTypeOpenAI, "", advancedModelName, groupName, 10)
	insertChannelSelectionCandidate(t, 4, constant.ChannelTypeAdvancedCustom, advancedSettings, advancedModelName, groupName, 1)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})

	common.MemoryCacheEnabled = false
	channel, err := GetChannel(groupName, modelName, 0, "/v1/alpha/search")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
	assert.False(t, ChannelSupportsRequestPath(&Channel{Type: constant.ChannelTypeOpenAI}, "/v1/alpha/search"))
	assert.True(t, ChannelSupportsRequestPath(&Channel{Type: constant.ChannelTypeCodex}, "/v1/alpha/search"))
	channel, err = GetChannel(groupName, advancedModelName, 0, "/v1/alpha/search")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 4, channel.Id)
	assert.True(t, ChannelSupportsRequestPath(&Channel{
		Id:            4,
		Type:          constant.ChannelTypeAdvancedCustom,
		OtherSettings: advancedSettings,
	}, "/v1/alpha/search"))

	common.MemoryCacheEnabled = true
	InitChannelCache()
	channel, err = GetRandomSatisfiedChannel(groupName, modelName, 0, "/v1/alpha/search")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
	channel, err = GetRandomSatisfiedChannel(groupName, advancedModelName, 0, "/v1/alpha/search")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 4, channel.Id)
}

func TestChannelSelectionExcludesPreviouslyTriedChannel(t *testing.T) {
	clearPreferredOwnerTables(t)
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
		channelSyncLock.Lock()
		group2model2channels = nil
		channelsIDM = nil
		channel2advancedCustomConfig = nil
		channel2advancedCustomConfigError = nil
		channelSyncLock.Unlock()
	})

	const (
		groupName = "default"
		modelName = "gpt-exclude-failed-channel"
	)
	insertChannelSelectionCandidate(t, 1, constant.ChannelTypeOpenAI, "", modelName, groupName, 10)
	insertChannelSelectionCandidate(t, 2, constant.ChannelTypeOpenAI, "", modelName, groupName, 10)
	excluded := map[int]struct{}{1: {}}

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})

	common.MemoryCacheEnabled = false
	channel, err := GetChannelExcluding(groupName, modelName, 0, "/v1/chat/completions", excluded)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	channel, err = GetRandomSatisfiedChannelExcluding(groupName, modelName, 0, "/v1/chat/completions", excluded)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
}

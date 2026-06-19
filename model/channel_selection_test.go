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

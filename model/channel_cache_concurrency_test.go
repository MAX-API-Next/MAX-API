package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/stretchr/testify/require"
)

func TestMultiKeyPollingRemainsSynchronizedDuringCacheRefresh(t *testing.T) {
	clearPreferredOwnerTables(t)
	const channelID = 9811

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	previousGroup2Model2Channels := group2model2channels
	previousChannelsIDM := channelsIDM
	previousAdvancedConfig := channel2advancedCustomConfig
	previousAdvancedConfigError := channel2advancedCustomConfigError
	group2model2channels = nil
	channelsIDM = nil
	channel2advancedCustomConfig = nil
	channel2advancedCustomConfigError = nil
	channelSyncLock.Unlock()
	channelPollingLocks.Delete(channelID)
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
		channelPollingLocks.Delete(channelID)
		channelSyncLock.Lock()
		group2model2channels = previousGroup2Model2Channels
		channelsIDM = previousChannelsIDM
		channel2advancedCustomConfig = previousAdvancedConfig
		channel2advancedCustomConfigError = previousAdvancedConfigError
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})

	common.MemoryCacheEnabled = true
	key, index, keyErr := (&Channel{Id: channelID + 1, Key: "single-key"}).GetNextEnabledKey()
	require.Nil(t, keyErr)
	require.Equal(t, "single-key", key)
	require.Zero(t, index)

	channel := &Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "first-key\nsecond-key",
		Status: common.ChannelStatusEnabled,
		Name:   "cache-refresh-polling-channel",
		Models: "gpt-cache-refresh",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	priority := int64(0)
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "gpt-cache-refresh", ChannelId: channelID,
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)
	InitChannelCache()

	start := make(chan struct{})
	errs := make(chan error, 4)
	var waitGroup sync.WaitGroup
	for range 3 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			for i := 0; i < 100; i++ {
				if _, _, err := channel.GetNextEnabledKey(); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		<-start
		for i := 0; i < 40; i++ {
			InitChannelCache()
		}
	}()
	close(start)
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, fmt.Sprintf("polling during cache refresh failed: %v", err))
	}
}

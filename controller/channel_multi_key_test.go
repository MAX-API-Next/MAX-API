package controller

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManageMultiKeysValidatesThenReloadsChannelUnderPollingLock(t *testing.T) {
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Log{}, &model.User{}))

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})

	channel := model.Channel{
		Name:   "multi-key-lock-test",
		Key:    "first-key\nsecond-key",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, db.Create(&channel).Error)

	var channelReads atomic.Int32
	bothReads := make(chan struct{})
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:observe-multi-key-channel-load", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" && channelReads.Add(1) == 2 {
			close(bothReads)
		}
	}))

	lock := model.GetChannelPollingLock(channel.Id)
	lock.Lock()
	results := make(chan tokenAPIResponse, 2)
	for _, keyIndex := range []int{0, 1} {
		keyIndex := keyIndex
		go func() {
			ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/channel/multi-key", MultiKeyManageRequest{
				ChannelId: channel.Id,
				Action:    "disable_key",
				KeyIndex:  &keyIndex,
			}, 1)
			ManageMultiKeys(ctx)
			results <- decodeAPIResponse(t, recorder)
		}()
	}

	validatedBeforeLock := false
	select {
	case <-bothReads:
		validatedBeforeLock = true
	case <-time.After(250 * time.Millisecond):
	}
	lock.Unlock()
	require.True(t, validatedBeforeLock, "multi-key handlers must validate channel existence before allocating a polling lock")

	for range 2 {
		select {
		case result := <-results:
			require.True(t, result.Success, "multi-key mutation failed: %s", result.Message)
		case <-time.After(2 * time.Second):
			t.Fatal("multi-key mutation did not finish")
		}
	}

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, 2, stored.ChannelInfo.MultiKeyStatusList[1])
}

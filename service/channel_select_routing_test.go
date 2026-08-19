package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenRoutePlanAdvancesGroupsWithoutResettingRetryBudget(t *testing.T) {
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = false

	modelName := "gpt-route-plan"
	for index, group := range []string{"base", "deluxe"} {
		channelID := index + 1
		priority := int64(0)
		require.NoError(t, db.Create(&model.Channel{
			Id:     channelID,
			Type:   1,
			Key:    fmt.Sprintf("key-%d", channelID),
			Status: common.ChannelStatusEnabled,
			Name:   group,
			Models: modelName,
			Group:  group,
		}).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     group,
			Model:     modelName,
			ChannelId: channelID,
			Enabled:   true,
			Priority:  &priority,
			Weight:    100,
		}).Error)
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyTokenRoutePlan, &TokenRoutePlan{
		Mode:           "manual",
		OrderedGroups:  []string{"base", "deluxe"},
		RetryOnFailure: true,
	})
	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "base",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}

	first, firstGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, "base", firstGroup)
	require.Equal(t, 0, param.GetRetry())
	require.Equal(t, "base", common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))

	param.ExcludeChannel(first.Id)
	param.SetRetry(1)
	second, secondGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, "deluxe", secondGroup)
	require.Equal(t, 1, param.GetRetry())
	require.Equal(t, "deluxe", common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
}

func TestTokenRoutePlanDoesNotAdvanceAfterFailureWhenRetryDisabled(t *testing.T) {
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = false

	modelName := "gpt-route-plan-no-cross-group-retry"
	for index, group := range []string{"base", "deluxe"} {
		channelID := index + 1
		priority := int64(0)
		require.NoError(t, db.Create(&model.Channel{
			Id:     channelID,
			Type:   1,
			Key:    fmt.Sprintf("key-%d", channelID),
			Status: common.ChannelStatusEnabled,
			Name:   group,
			Models: modelName,
			Group:  group,
		}).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     group,
			Model:     modelName,
			ChannelId: channelID,
			Enabled:   true,
			Priority:  &priority,
			Weight:    100,
		}).Error)
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyTokenRoutePlan, &TokenRoutePlan{
		Mode:           "manual",
		OrderedGroups:  []string{"base", "deluxe"},
		RetryOnFailure: false,
	})
	retry := 1
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "base",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}
	param.ExcludeChannel(1)

	channel, group, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.Nil(t, channel)
	require.Equal(t, "base", group)
	_, advanced := common.GetContextKey(ctx, constant.ContextKeyAutoGroupIndex)
	require.False(t, advanced)
}

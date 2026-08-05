package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func TestGetModelFromJSONBodyPreservesProviderRoutingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model":"gpt-test",
		"routing_strategy":"cost",
		"provider":{"sort":"price","order":["openai"]}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	request, err := getModelFromJSONBody(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-test", request.Model)

	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	body, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "cost", gjson.GetBytes(body, "routing_strategy").String())
	require.Equal(t, "price", gjson.GetBytes(body, "provider.sort").String())
	require.Equal(t, "openai", gjson.GetBytes(body, "provider.order.0").String())
}

func TestPlaygroundExplicitGroupBypassesStoredTokenRoutePlan(t *testing.T) {
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	usableGroups := setting.GetUserUsableGroupsCopy()
	usableGroupsJSON, err := common.Marshal(usableGroups)
	require.NoError(t, err)
	groupRatiosJSON := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(string(usableGroupsJSON)))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatiosJSON))
	})

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.MemoryCacheEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","base":"Base","deluxe":"Deluxe"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"base":1,"deluxe":2}`))

	modelName := "gpt-playground-route"
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
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions", bytes.NewBufferString(fmt.Sprintf(`{"model":%q,"group":"deluxe"}`, modelName)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "base")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "base")
	common.SetContextKey(ctx, constant.ContextKeyTokenRoutingPolicy, model.TokenRoutingPolicy{
		Version:        model.TokenRoutingPolicyVersion,
		Mode:           model.TokenRoutingModeManual,
		Groups:         []string{"base"},
		RetryOnFailure: true,
	})
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	Distribute()(ctx)

	require.False(t, ctx.IsAborted())
	require.Equal(t, "deluxe", common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
	require.Equal(t, 2, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	_, hasPlan := common.GetContextKey(ctx, constant.ContextKeyTokenRoutePlan)
	require.False(t, hasPlan)
}

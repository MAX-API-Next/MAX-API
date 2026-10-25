package controller

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelRoutingHealthController(t *testing.T) (*gorm.DB, *model.Channel, *miniredis.Miniredis) {
	t.Helper()
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	oldSQLite, oldMySQL, oldPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	oldSetting := *operation_setting.GetMonitorSetting()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RDB, common.RedisEnabled = oldClient, oldEnabled
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = oldSQLite, oldMySQL, oldPostgres
		*operation_setting.GetMonitorSetting() = oldSetting
	})
	initModelListColumnNames(t)
	db, channel := setupChannelUpdateControllerTestDB(t)
	channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "fixture reason"}
	channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 123}
	require.NoError(t, db.Save(channel).Error)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { _ = client.Close() })
	setting := operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled: true, StreamingFirstResultTimeoutSeconds: 20,
		PriorityDeduction: 1, PenaltyCooldownSeconds: 600, RecoveryMode: operation_setting.RecoveryModeAutomatic,
	}
	*operation_setting.GetMonitorSetting() = setting
	result, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
		ChannelID: channel.Id, AttemptID: t.Name(), RequestMode: channelhealth.RequestModeStreaming,
		TimeoutKind: channelhealth.TimeoutKindStreamingFirstResult, TimeoutEligible: true, ObservedAt: time.Now().UTC(),
	}, &setting)
	require.NoError(t, err)
	require.True(t, result.Applied)
	return db, channel, server
}

func TestChannelRoutingHealthManagementReadEndpointsPreserveConfiguration(t *testing.T) {
	db, primary, server := setupChannelRoutingHealthController(t)
	secondary := &model.Channel{
		Name: "secondary-channel", Type: 1, Key: "fixture-secondary", Status: common.ChannelStatusEnabled,
		Models: "gpt-origin", Group: "default", Tag: primary.Tag, Priority: common.GetPointer(int64(9)),
	}
	require.NoError(t, db.Create(secondary).Error)
	before := server.Dump()
	for _, test := range []struct {
		name    string
		url     string
		handler gin.HandlerFunc
		detail  bool
		count   int
		total   int
	}{
		{"list page", "/api/channel/?page_size=1&sort_by=priority&sort_order=desc", GetAllChannels, false, 1, 2},
		{"search page", "/api/channel/search?keyword=channel&page_size=1&sort_by=priority&sort_order=desc", SearchChannels, false, 1, 2},
		{"tag children", "/api/channel/?tag_mode=true&page_size=1&sort_by=priority&sort_order=desc", GetAllChannels, false, 2, 1},
		{"tag search", "/api/channel/search?tag_mode=true&keyword=channel&page_size=20&sort_by=priority&sort_order=desc", SearchChannels, false, 2, 2},
		{"detail", "/api/channel/" + strconv.Itoa(primary.Id), GetChannel, true, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newAuthenticatedContext(t, http.MethodGet, test.url, nil, 1)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(primary.Id)}}
			test.handler(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)
			var item map[string]interface{}
			if test.detail {
				require.NoError(t, common.Unmarshal(response.Data, &item))
			} else {
				var page struct {
					Items []map[string]interface{} `json:"items"`
					Total int                      `json:"total"`
				}
				require.NoError(t, common.Unmarshal(response.Data, &page))
				require.Len(t, page.Items, test.count)
				require.Equal(t, test.total, page.Total)
				item = page.Items[0]
				if len(page.Items) > 1 {
					require.Equal(t, dto.ChannelRoutingHealthNormal, page.Items[1]["routing_health"].(map[string]interface{})["status"])
				}
			}
			require.EqualValues(t, primary.Id, item["id"])
			require.EqualValues(t, 10, item["priority"])
			health := item["routing_health"].(map[string]interface{})
			require.Equal(t, dto.ChannelRoutingHealthPenalized, health["status"])
			require.EqualValues(t, 9, health["effective_priority"])
			require.EqualValues(t, 1, health["active_penalty"])
			require.Empty(t, item["key"])
			require.NotContains(t, item["channel_info"], "multi_key_disabled_reason")
			require.NotContains(t, item["channel_info"], "multi_key_disabled_time")
			delete(item, "routing_health")
			original, err := model.GetChannelById(primary.Id, false)
			require.NoError(t, err)
			clearChannelInfo(original)
			encoded, err := common.Marshal(original)
			require.NoError(t, err)
			var expected map[string]interface{}
			require.NoError(t, common.Unmarshal(encoded, &expected))
			require.Equal(t, expected, item)
			require.Equal(t, before, server.Dump())
		})
	}
	stored, err := model.GetChannelById(primary.Id, true)
	require.NoError(t, err)
	require.EqualValues(t, 10, stored.GetPriority())
	require.Equal(t, primary.Key, stored.Key)
	require.Equal(t, primary.ChannelInfo.MultiKeyDisabledReason, stored.ChannelInfo.MultiKeyDisabledReason)
	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).Count(&logCount).Error)
	require.Zero(t, logCount)
}

func TestChannelRoutingHealthManagementReadSurvivesRedisFailure(t *testing.T) {
	_, channel, server := setupChannelRoutingHealthController(t)
	server.SetError("ERR private-diagnostic")
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/channel/", nil, 1)
	GetAllChannels(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	var page struct {
		Items []struct {
			ID            int                      `json:"id"`
			Priority      int64                    `json:"priority"`
			RoutingHealth dto.ChannelRoutingHealth `json:"routing_health"`
		} `json:"items"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, channel.Id, page.Items[0].ID)
	require.EqualValues(t, 10, page.Items[0].Priority)
	require.Equal(t, dto.ChannelRoutingHealthUnknown, page.Items[0].RoutingHealth.Status)
	require.Nil(t, page.Items[0].RoutingHealth.EffectivePriority)
	require.Equal(t, "redis_read_failed", *page.Items[0].RoutingHealth.UnavailableReason)
	require.NotContains(t, recorder.Body.String(), "private-diagnostic")
}

func TestChannelRoutingHealthCannotCreatePhantomChannelOrBeMassAssigned(t *testing.T) {
	db, channel, server := setupChannelRoutingHealthController(t)
	// Keep the existing update audit on a cache hit so this test does not leave
	// unrelated asynchronous user-cache refill work after its Redis is closed.
	require.NoError(t, db.AutoMigrate(&model.CacheInvalidationTask{}))
	require.NoError(t, common.RedisHSetObj("user:1", &model.UserBase{
		Id: 1, Username: "fixture-admin", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
	}, time.Hour))
	require.NoError(t, common.RDB.HSet(context.Background(), channelhealth.RuntimeStateKey(99999), map[string]interface{}{
		"penalty": "1", "runtime_disabled": "false",
	}).Err())
	before := server.Dump()
	runtimeBefore, err := common.RDB.HGetAll(context.Background(), channelhealth.RuntimeStateKey(channel.Id)).Result()
	require.NoError(t, err)
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/channel/99999", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: "99999"}}
	GetChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Equal(t, before, server.Dump())

	ctx, recorder = newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]interface{}{
		"id": channel.Id, "priority": 11,
		"routing_health": map[string]interface{}{
			"effective_priority": -999, "stored_penalty": 0, "runtime_disabled": true,
		},
	}, 1)
	UpdateChannel(ctx)
	response = decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	stored, err := model.GetChannelById(channel.Id, false)
	require.NoError(t, err)
	require.EqualValues(t, 11, stored.GetPriority())
	runtimeAfter, err := common.RDB.HGetAll(context.Background(), channelhealth.RuntimeStateKey(channel.Id)).Result()
	require.NoError(t, err)
	require.Equal(t, runtimeBefore, runtimeAfter)
	ctx, recorder = newAuthenticatedContext(t, http.MethodGet, "/api/channel/"+strconv.Itoa(channel.Id), nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	GetChannel(ctx)
	response = decodeAPIResponse(t, recorder)
	var view struct {
		RoutingHealth dto.ChannelRoutingHealth `json:"routing_health"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &view))
	require.EqualValues(t, 10, *view.RoutingHealth.EffectivePriority)
	require.False(t, *view.RoutingHealth.RuntimeDisabled)
}

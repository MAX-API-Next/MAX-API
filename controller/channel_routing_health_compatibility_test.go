package controller

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// The P4.4 recipe can run this unchanged against the current controller or a
// pinned historical controller supplied by Go's file overlay. No live DB/Redis
// is used. Optional files join the Go and historical frontend contract tests.
func TestP44ChannelCompatibilityContract(t *testing.T) {
	oldRedis, oldEnabled := common.RDB, common.RedisEnabled
	oldSetting := *operation_setting.GetMonitorSetting()
	oldMemory := common.MemoryCacheEnabled
	oldDB, oldLogDB := model.DB, model.LOG_DB
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RDB, common.RedisEnabled = oldRedis, oldEnabled
		common.MemoryCacheEnabled = oldMemory
		*operation_setting.GetMonitorSetting() = oldSetting
	})
	initModelListColumnNames(t)
	db, channel := setupChannelUpdateControllerTestDB(t)
	common.MemoryCacheEnabled = false
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(context.Background()).Err())
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled: true, PriorityDeduction: 2,
		RecoveryMode: operation_setting.RecoveryModeManual,
	}
	// Raw legacy-compatible fields are the input, not the current encoder's
	// output. A missing deadline represents a manual penalty of exactly two.
	key := channelhealth.RuntimeStateKey(channel.Id)
	server.HSet(key, "penalty", "2", "runtime_disabled", "false", "future_extension", "opaque")
	require.NoError(t, db.AutoMigrate(&model.CacheInvalidationTask{}))
	require.NoError(t, common.RedisHSetObj("user:1", &model.UserBase{
		Id: 1, Username: "synthetic-p44-admin", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
	}, time.Hour))

	legacy := os.Getenv("MAXAPI_P44_MODE") == "legacy"
	read := func(name, path string, handler gin.HandlerFunc, detail bool, base, effective int64) map[string]any {
		t.Helper()
		before := server.Dump()
		ctx, recorder := newAuthenticatedContext(t, http.MethodGet, path, nil, 1)
		if detail {
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
		}
		handler(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		response := decodeAPIResponse(t, recorder)
		require.True(t, response.Success, response.Message)
		var envelope map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
		var item map[string]any
		if detail {
			item = envelope["data"].(map[string]any)
		} else {
			items := envelope["data"].(map[string]any)["items"].([]any)
			require.Len(t, items, 1)
			item = items[0].(map[string]any)
		}
		require.EqualValues(t, channel.Id, item["id"])
		require.EqualValues(t, base, item["priority"])
		require.Empty(t, item["key"])
		if legacy {
			require.NotContains(t, item, "routing_health")
		} else {
			health := item["routing_health"].(map[string]any)
			require.Equal(t, "penalized", health["status"])
			require.EqualValues(t, effective, health["effective_priority"])
			require.EqualValues(t, 2, health["active_penalty"])
		}
		require.Equal(t, before, server.Dump(), "management reads, including historical reads, must not rewrite Redis")
		if legacy {
			t.Logf("%s legacy=true configured=%d projection=absent", name, base)
		} else {
			t.Logf("%s legacy=false configured=%d effective=%d", name, base, effective)
		}
		return envelope
	}
	detailPath := "/api/channel/" + strconv.Itoa(channel.Id)
	output := map[string]any{
		"mode": legacy, "id": channel.Id,
		"list":   read("list", "/api/channel/?page_size=20", GetAllChannels, false, 10, 8),
		"search": read("search", "/api/channel/search?keyword=origin&page_size=20", SearchChannels, false, 10, 8),
		"detail": read("detail", detailPath, GetChannel, true, 10, 8),
	}
	// A legacy frontend's actual serialized PUT can be supplied by the recipe.
	payload := map[string]any{"id": channel.Id, "priority": 20}
	if input := os.Getenv("MAXAPI_P44_INPUT"); input != "" {
		encoded, err := os.ReadFile(input)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(encoded, &payload))
		require.EqualValues(t, channel.Id, payload["id"])
		require.EqualValues(t, 20, payload["priority"])
	}
	for _, field := range []string{"routing_health", "effective_priority", "observation"} {
		require.NotContains(t, payload, field)
	}
	beforeSave, err := client.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", payload, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	stored, err := model.GetChannelById(channel.Id, false)
	require.NoError(t, err)
	require.EqualValues(t, 20, stored.GetPriority())
	afterSave, err := client.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	require.Equal(t, beforeSave, afterSave, "configuration PUT must preserve the existing penalty and extension")
	output["saved"] = read("saved", detailPath, GetChannel, true, 20, 18)
	if destination := os.Getenv("MAXAPI_P44_OUTPUT"); destination != "" {
		encoded, err := common.Marshal(output)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(destination, encoded, 0600))
	}
}

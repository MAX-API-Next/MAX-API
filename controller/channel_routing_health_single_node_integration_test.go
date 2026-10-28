package controller

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// P4.2 uses the existing isolated SQLite/miniredis controller fixture to
// verify the complete single-node seam without connecting to live services.
func TestP42SingleNodeSaveProjectionAndRouting(t *testing.T) {
	db, primary, redisServer := setupChannelRoutingHealthController(t)
	oldMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })

	modelName := "gpt-origin"
	// The setup fixture starts this channel disabled. Save through the real
	// controller so the database and its ability row change together.
	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id": primary.Id, "status": common.ChannelStatusEnabled, "priority": 10,
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	secondaryPriority := int64(9)
	secondaryWeight := uint(100)
	secondary := &model.Channel{
		Name: "secondary-channel", Type: 1, Key: "fixture-secondary",
		Status: common.ChannelStatusEnabled, Models: modelName, Group: "default",
		Priority: &secondaryPriority, Weight: &secondaryWeight,
	}
	require.NoError(t, db.Create(secondary).Error)
	require.NoError(t, secondary.AddAbilities(nil))

	setting := *operation_setting.GetMonitorSetting()
	setting.PriorityDeduction = 2
	*operation_setting.GetMonitorSetting() = setting
	transition, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
		ChannelID: primary.Id, AttemptID: "p42-single-node-penalty-two",
		RequestMode:     channelhealth.RequestModeStreaming,
		TimeoutKind:     channelhealth.TimeoutKindStreamingFirstResult,
		TimeoutEligible: true, ObservedAt: time.Now().UTC(),
	}, &setting)
	require.NoError(t, err)
	require.True(t, transition.Applied)
	require.Equal(t, int64(2), transition.State.Penalty)

	beforeRuntime := redisServer.Dump()
	getList := func() map[string]any {
		ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/channel/?page_size=20", nil, 1)
		GetAllChannels(ctx)
		response := decodeAPIResponse(t, recorder)
		require.True(t, response.Success, response.Message)
		var page map[string]any
		require.NoError(t, common.Unmarshal(response.Data, &page))
		items := page["items"].([]any)
		for _, raw := range items {
			item := raw.(map[string]any)
			if int(item["id"].(float64)) != primary.Id {
				continue
			}
			require.EqualValues(t, 10, item["priority"])
			health := item["routing_health"].(map[string]any)
			require.Equal(t, string(dto.ChannelRoutingHealthPenalized), health["status"])
			require.EqualValues(t, 8, health["effective_priority"])
			require.EqualValues(t, 2, health["active_penalty"])
			return item
		}
		t.Fatalf("primary channel %d missing from management response", primary.Id)
		return nil
	}
	item := getList()
	require.NotNil(t, item)
	require.Equal(t, beforeRuntime, redisServer.Dump(), "management GET must not mutate runtime state")

	stored, err := model.GetChannelById(primary.Id, false)
	require.NoError(t, err)
	require.EqualValues(t, 10, stored.GetPriority(), "configured priority remains the database value")

	// Exercise the database-backed candidate path first.
	common.MemoryCacheEnabled = false
	selected, err := model.GetRandomSatisfiedChannel("default", modelName, 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, secondary.Id, selected.Id, "8 must lose to the configured-9 secondary")

	// Then exercise the node's cached candidate path after a fresh cache load.
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	selected, err = model.GetRandomSatisfiedChannel("default", modelName, 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, secondary.Id, selected.Id, "cached routing must apply the same runtime penalty")

	// A second real save must still carry configuration fields only and must
	// leave the Redis runtime observation unchanged.
	ctx, recorder = newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id": primary.Id, "priority": 10,
	}, 1)
	UpdateChannel(ctx)
	response = decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	require.Equal(t, beforeRuntime, redisServer.Dump(), "configuration save must not rewrite runtime state")
	stored, err = model.GetChannelById(primary.Id, false)
	require.NoError(t, err)
	require.EqualValues(t, 10, stored.GetPriority())
	item = getList()
	require.EqualValues(t, 8, item["routing_health"].(map[string]any)["effective_priority"])

	// The response must not expose a locally computed or writable projection
	// field in the configuration save path.
	ctx, recorder = newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id": primary.Id, "priority": 10,
		"routing_health": map[string]any{"effective_priority": -999},
	}, 1)
	UpdateChannel(ctx)
	response = decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	require.Equal(t, beforeRuntime, redisServer.Dump())
}

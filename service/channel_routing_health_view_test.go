package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func withChannelRoutingHealthRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	oldSetting := *operation_setting.GetMonitorSetting()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB, common.RedisEnabled = oldClient, oldEnabled
		*operation_setting.GetMonitorSetting() = oldSetting
	})
	return server
}

func TestBuildChannelRoutingHealthViewsObservesPenaltyWithoutMutations(t *testing.T) {
	server := withChannelRoutingHealthRedis(t)
	setting := operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled: true, StreamingFirstResultTimeoutSeconds: 20,
		PriorityDeduction: 1, PenaltyCooldownSeconds: 600, RecoveryMode: operation_setting.RecoveryModeAutomatic,
	}
	*operation_setting.GetMonitorSetting() = setting
	observed := time.Now().UTC()
	result, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
		ChannelID: 1086, AttemptID: "display-integration", RequestMode: channelhealth.RequestModeStreaming,
		TimeoutKind: channelhealth.TimeoutKindStreamingFirstResult, TimeoutEligible: true, ObservedAt: observed,
	}, &setting)
	require.NoError(t, err)
	require.Equal(t, "priority_demotion_firing", result.Transition)
	before := server.Dump()
	base := int64(10)
	channel := &model.Channel{Id: 1086, Priority: &base}
	views := BuildChannelRoutingHealthViews(context.Background(), []*model.Channel{channel, {Id: 2}, nil})
	require.Len(t, views, 3)
	require.Same(t, channel, views[0].Channel)
	require.Equal(t, int64(10), channel.GetPriority())
	require.Equal(t, before, server.Dump())
	health := views[0].RoutingHealth
	require.Equal(t, dto.ChannelRoutingHealthPenalized, health.Status)
	require.EqualValues(t, 9, *health.EffectivePriority)
	require.EqualValues(t, 1, *health.ActivePenalty)
	require.Equal(t, result.State.PenaltyUntil, *health.PenaltyUntil)
	require.Equal(t, operation_setting.RecoveryModeAutomatic, *health.PenaltyRecoveryMode)
	require.Equal(t, dto.ChannelRoutingHealthNormal, views[1].RoutingHealth.Status)
	require.EqualValues(t, 0, *views[1].RoutingHealth.EffectivePriority)
	require.False(t, *views[1].RoutingHealth.RuntimeDisabled)
	require.Equal(t, health.EvaluatedAt, views[1].RoutingHealth.EvaluatedAt)
	require.Nil(t, views[2])

	encoded, err := common.Marshal(views)
	require.NoError(t, err)
	var decoded []map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.EqualValues(t, 10, decoded[0]["priority"])
	known := decoded[1]["routing_health"].(map[string]interface{})
	require.EqualValues(t, 0, known["effective_priority"])
	require.Equal(t, false, known["runtime_disabled"])
	require.Contains(t, known, "penalty_until")
	require.Nil(t, known["penalty_until"])
}

func TestChannelRoutingHealthProjectionMatchesExistingRoutingRules(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name     string
		base     int64
		state    channelhealth.RuntimeState
		autoBan  bool
		status   string
		priority *int64
		active   int64
	}{
		{"active", 10, channelhealth.RuntimeState{Penalty: 1, PenaltyUntil: now.Add(time.Minute)}, false, dto.ChannelRoutingHealthPenalized, common.GetPointer(int64(9)), 1},
		{"floor", 0, channelhealth.RuntimeState{Penalty: 1}, false, dto.ChannelRoutingHealthPenalized, common.GetPointer(int64(0)), 1},
		{"negative base", -3, channelhealth.RuntimeState{Penalty: 1}, false, dto.ChannelRoutingHealthPenalized, common.GetPointer(int64(-4)), 1},
		{"expired", 10, channelhealth.RuntimeState{Penalty: 1, PenaltyUntil: now}, false, dto.ChannelRoutingHealthNormal, common.GetPointer(int64(10)), 0},
		{"manual", 10, channelhealth.RuntimeState{Penalty: 1}, false, dto.ChannelRoutingHealthPenalized, common.GetPointer(int64(9)), 1},
		{"disabled waiting", 10, channelhealth.RuntimeState{RuntimeDisabled: true, DisabledUntil: now.Add(time.Minute)}, true, dto.ChannelRoutingHealthRuntimeDisabled, nil, 0},
		{"disabled due", 10, channelhealth.RuntimeState{RuntimeDisabled: true, DisabledUntil: now.Add(-time.Minute)}, true, dto.ChannelRoutingHealthRuntimeDisabled, nil, 0},
		{"stored disable inactive", 10, channelhealth.RuntimeState{RuntimeDisabled: true, Penalty: 1}, false, dto.ChannelRoutingHealthNormal, common.GetPointer(int64(10)), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := projectChannelRoutingHealth(test.base, test.state, operation_setting.MonitorSetting{
				AutoPriorityDemotionEnabled: true, TimeoutAutoDisableEnabled: test.autoBan,
				TimeoutAutoDisableRecoveryMode: operation_setting.TimeoutAutoDisableRecoveryManual,
			}, now)
			require.Equal(t, test.status, view.Status)
			require.Equal(t, test.priority, view.EffectivePriority)
			require.Equal(t, test.active, *view.ActivePenalty)
			require.Equal(t, test.state.RuntimeDisabled, *view.StoredRuntimeDisabled)
			if test.priority != nil {
				require.Equal(t, channelhealth.EffectivePriority(test.base, test.state, now), *view.EffectivePriority)
			}
			if test.name == "manual" {
				require.Equal(t, operation_setting.RecoveryModeManual, *view.PenaltyRecoveryMode)
				require.Nil(t, view.PenaltyUntil)
			}
			require.Equal(t, operation_setting.TimeoutAutoDisableRecoveryManual, *view.DisableRecoveryMode)
		})
	}
}

func TestBuildChannelRoutingHealthViewsHonorsAllPolicyCombinations(t *testing.T) {
	withChannelRoutingHealthRedis(t)
	for _, demotion := range []bool{false, true} {
		for _, disable := range []bool{false, true} {
			calls := 0
			views := buildChannelRoutingHealthViews(context.Background(), []*model.Channel{{Id: 1, Priority: common.GetPointer(int64(10))}},
				operation_setting.MonitorSetting{AutoPriorityDemotionEnabled: demotion, TimeoutAutoDisableEnabled: disable},
				func(context.Context, []int) (map[int]channelhealth.RuntimeStateReadResult, error) {
					calls++
					return map[int]channelhealth.RuntimeStateReadResult{1: {State: channelhealth.RuntimeState{Penalty: 1}}}, nil
				})
			health := views[0].RoutingHealth
			if !demotion && !disable {
				require.Zero(t, calls)
				require.Equal(t, dto.ChannelRoutingHealthPolicyOff, health.Status)
				require.EqualValues(t, 10, *health.EffectivePriority)
				require.Nil(t, health.StoredPenalty)
			} else {
				require.Equal(t, 1, calls)
				require.Equal(t, dto.ChannelRoutingHealthPenalized, health.Status)
				require.EqualValues(t, 9, *health.EffectivePriority)
			}
		}
	}
}

func TestBuildChannelRoutingHealthViewsUnknownIsNotKnownZero(t *testing.T) {
	withChannelRoutingHealthRedis(t)
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{AutoPriorityDemotionEnabled: true}
	common.RedisEnabled = false
	views := BuildChannelRoutingHealthViews(context.Background(), []*model.Channel{{Id: 1}})
	health := views[0].RoutingHealth
	require.Equal(t, dto.ChannelRoutingHealthUnknown, health.Status)
	require.Equal(t, "redis_unconfigured", *health.UnavailableReason)
	encoded, err := common.Marshal(health)
	require.NoError(t, err)
	var decoded map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	for _, field := range []string{"effective_priority", "active_penalty", "stored_penalty", "runtime_disabled", "stored_runtime_disabled"} {
		require.Contains(t, decoded, field)
		require.Nil(t, decoded[field])
	}
	common.RedisEnabled = true
	for _, test := range []struct {
		name   string
		result map[int]channelhealth.RuntimeStateReadResult
		err    error
		reason string
	}{
		{"transport", nil, errors.New("private connection detail"), "redis_read_failed"},
		{"corrupt", map[int]channelhealth.RuntimeStateReadResult{1: {Err: channelhealth.ErrInvalidRuntimeState}}, nil, "redis_invalid_state"},
	} {
		t.Run(test.name, func(t *testing.T) {
			views := buildChannelRoutingHealthViews(context.Background(), []*model.Channel{{Id: 1}}, *operation_setting.GetMonitorSetting(),
				func(context.Context, []int) (map[int]channelhealth.RuntimeStateReadResult, error) {
					return test.result, test.err
				})
			require.Equal(t, dto.ChannelRoutingHealthUnknown, views[0].RoutingHealth.Status)
			require.Equal(t, test.reason, *views[0].RoutingHealth.UnavailableReason)
			require.Nil(t, views[0].RoutingHealth.EffectivePriority)
		})
	}
}

func TestBuildChannelRoutingHealthViewsBoundsAndDeduplicatesReads(t *testing.T) {
	withChannelRoutingHealthRedis(t)
	channels := make([]*model.Channel, 1003)
	for index := range channels {
		channels[index] = &model.Channel{Id: index + 1}
	}
	channels[1002] = channels[0]
	calls := 0
	seen := make(map[int]bool)
	views := buildChannelRoutingHealthViews(context.Background(), channels, operation_setting.MonitorSetting{AutoPriorityDemotionEnabled: true},
		func(_ context.Context, ids []int) (map[int]channelhealth.RuntimeStateReadResult, error) {
			calls++
			require.LessOrEqual(t, len(ids), 200)
			results := make(map[int]channelhealth.RuntimeStateReadResult)
			for _, id := range ids {
				require.False(t, seen[id])
				seen[id] = true
				results[id] = channelhealth.RuntimeStateReadResult{State: channelhealth.RuntimeState{ChannelID: id}}
			}
			return results, nil
		})
	require.Equal(t, 5, calls)
	require.Len(t, seen, 1000)
	require.Len(t, views, len(channels))
	require.Equal(t, dto.ChannelRoutingHealthUnknown, views[1000].RoutingHealth.Status)
	require.Equal(t, "projection_budget_exceeded", *views[1000].RoutingHealth.UnavailableReason)
	require.Equal(t, dto.ChannelRoutingHealthNormal, views[1002].RoutingHealth.Status)
}

func TestBuildChannelRoutingHealthViewsPreservesSuccessfulBatchesOnTimeout(t *testing.T) {
	withChannelRoutingHealthRedis(t)
	channels := make([]*model.Channel, 201)
	for index := range channels {
		channels[index] = &model.Channel{Id: index + 1}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	views := buildChannelRoutingHealthViews(ctx, channels, operation_setting.MonitorSetting{AutoPriorityDemotionEnabled: true},
		func(ctx context.Context, ids []int) (map[int]channelhealth.RuntimeStateReadResult, error) {
			calls++
			if calls == 1 {
				results := make(map[int]channelhealth.RuntimeStateReadResult)
				for _, id := range ids {
					results[id] = channelhealth.RuntimeStateReadResult{}
				}
				return results, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
	require.Equal(t, dto.ChannelRoutingHealthNormal, views[0].RoutingHealth.Status)
	require.Equal(t, dto.ChannelRoutingHealthUnknown, views[200].RoutingHealth.Status)
	require.Nil(t, views[200].RoutingHealth.EffectivePriority)
}

func TestBuildChannelRoutingHealthViewsDistinguishesContextAndChannelBudgets(t *testing.T) {
	withChannelRoutingHealthRedis(t)
	for _, test := range []struct {
		name        string
		deadline    bool
		cancelAfter bool
		reason      string
	}{
		{name: "canceled_before_read", reason: "context_canceled"},
		{name: "expired_before_read", deadline: true, reason: "redis_read_timeout"},
		{name: "canceled_after_successful_batch", cancelAfter: true, reason: "context_canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if test.deadline {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
			} else if !test.cancelAfter {
				cancel()
			}
			defer cancel()
			channels := make([]*model.Channel, 1001)
			for index := range channels {
				channels[index] = &model.Channel{Id: index + 1}
			}
			calls := 0
			views := buildChannelRoutingHealthViews(ctx, channels, operation_setting.MonitorSetting{AutoPriorityDemotionEnabled: true},
				func(_ context.Context, ids []int) (map[int]channelhealth.RuntimeStateReadResult, error) {
					calls++
					states := make(map[int]channelhealth.RuntimeStateReadResult)
					for _, id := range ids {
						states[id] = channelhealth.RuntimeStateReadResult{}
					}
					cancel()
					return states, nil
				})
			if test.cancelAfter {
				require.Equal(t, 1, calls)
				require.Equal(t, dto.ChannelRoutingHealthNormal, views[0].RoutingHealth.Status)
			} else {
				require.Zero(t, calls)
			}
			health := views[999].RoutingHealth
			require.Equal(t, dto.ChannelRoutingHealthUnknown, health.Status)
			require.Nil(t, health.EffectivePriority)
			require.Equal(t, test.reason, *health.UnavailableReason)
			require.Equal(t, "projection_budget_exceeded", *views[1000].RoutingHealth.UnavailableReason)
		})
	}
}

func TestBuildChannelRoutingHealthViewsClassifiesReadFailures(t *testing.T) {
	withChannelRoutingHealthRedis(t)
	for _, test := range []struct {
		name   string
		err    error
		reason string
	}{
		{"deadline", errors.Join(errors.New("private diagnostic"), context.DeadlineExceeded), "redis_read_timeout"},
		{"canceled", errors.Join(errors.New("private diagnostic"), context.Canceled), "context_canceled"},
		{"transport", errors.New("private diagnostic"), "redis_read_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			views := buildChannelRoutingHealthViews(context.Background(), []*model.Channel{{Id: 1}}, operation_setting.MonitorSetting{AutoPriorityDemotionEnabled: true},
				func(context.Context, []int) (map[int]channelhealth.RuntimeStateReadResult, error) {
					return nil, test.err
				})
			health := views[0].RoutingHealth
			require.Equal(t, dto.ChannelRoutingHealthUnknown, health.Status)
			require.Nil(t, health.EffectivePriority)
			require.Equal(t, test.reason, *health.UnavailableReason)
		})
	}
}

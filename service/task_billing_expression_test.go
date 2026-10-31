package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This wire fixture is independent of the snapshot builder and money oracle.
// Estimate: 4 credits * $0.10 * 1000 quota/$ * 1.5 = 600; floor = 700.
// Final: 2.5 credits * $0.10 * 1000 * 1.5 = 375; refund = 325.
func goldenExpressionTask(t *testing.T) (*model.Task, *relaycommon.TaskInfo) {
	t.Helper()
	contract, ok := taskpricing.KnownContract("kling", "kling-v1")
	require.True(t, ok)
	digest, err := taskpricing.ContractDigest(contract)
	require.NoError(t, err)
	expression := `u("units") * 0.1`
	encoded, err := common.Marshal(map[string]any{
		"profile": taskpricing.NewAPITask, "source": "task_expression", "rule_key": "kling::kling-v1",
		"schema_version": 1, "mode": "bounded_actual", "config_hash": "price-v1", "currency": "USD",
		"group_ratio": 1.5, "quota_per_unit": 1000, "estimate_quota": 600, "reserve_quota": 700,
		"usage_producer_kind": "task_plugin", "usage_source_id": "kling", "usage_schema_version": 1, "usage_contract_digest": digest,
		"expression_plan": map[string]any{
			"version": 1, "original_model": "kling-v1", "mapped_model": "kling-v1", "selected_model": "kling-v1",
			"selected_plugin_key": "kling",
			"driver":              "task_plugin", "plugin_key": "kling", "plugin_version": "1.0.0", "package_digest": strings.Repeat("a", 64),
			"channel_id": 902, "action": "generate", "binding_version": "binding-v1",
			"request_id": "request-h3-terminal",
			"expression": expression, "expression_hash": billingexpr.ExprHashString(expression),
			"pricing_at": "2026-10-31T12:00:00Z", "rounding": "half_away_from_zero@1", "reservation_floor": 700,
			"contract": contract, "submit_facts": map[string]any{"units": 4},
		},
	})
	require.NoError(t, err)
	var plan types.TaskBillingPlan
	require.NoError(t, common.Unmarshal(encoded, &plan))
	task := makeServiceH3Task(t, 700, &plan)
	task.Properties.OriginModelName, task.Properties.UpstreamModelName = "kling-v1", "kling-v1"
	task.PrivateData.BillingContext.OriginModelName = "kling-v1"
	task.Action = "generate"
	resultJSON, err := common.Marshal(map[string]any{
		"status": "SUCCESS", "total_tokens": 99999,
		"pricing_usage_envelope": map[string]any{
			"producer_kind": "task_plugin", "source_id": "kling", "schema_version": 1, "contract_digest": digest,
			"plugin_version": "1.0.0", "package_digest": strings.Repeat("a", 64), "channel_id": 902,
			"request_id": "request-h3-terminal", "action": "generate",
			"stage": "provider_response", "completeness": "complete", "facts": map[string]any{"units": 2.5},
		},
	})
	require.NoError(t, err)
	var result relaycommon.TaskInfo
	require.NoError(t, common.Unmarshal(resultJSON, &result))
	return task, &result
}

func TestTaskExpressionGoldenWireUsesCreditInsteadOfH3OrTokenBilling(t *testing.T) {
	task, result := goldenExpressionTask(t)
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, result, constant.ChannelTypeKling)
	require.True(t, decision.UsesPlan)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)
	require.EqualValues(t, 375, decision.Settlement.TaskQuotaTarget)
	require.EqualValues(t, -325, decision.Settlement.FundingDelta)
	require.EqualValues(t, -325, decision.Settlement.TokenDelta)
}

func TestTaskExpressionVersionedSnapshotReachesTerminalSettlement(t *testing.T) {
	task, result := goldenExpressionTask(t)
	p := task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan
	p.Expression = "v1:" + p.Expression
	p.ExpressionHash = fmt.Sprintf("%x", sha256.Sum256([]byte(p.Expression)))
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, result, constant.ChannelTypeKling)
	require.True(t, decision.UsesPlan)
	require.Empty(t, decision.ManualReason)
	require.EqualValues(t, 375, decision.Settlement.TaskQuotaTarget)
	require.EqualValues(t, -325, decision.Settlement.FundingDelta)
	require.EqualValues(t, -325, decision.Settlement.TokenDelta)
	require.Equal(t, p.ExpressionHash, decision.Settlement.Effect.Other["task_expression_hash"])
}

func TestTaskExpressionLogUsesFrozenTermsInsteadOfLegacyFields(t *testing.T) {
	task, result := goldenExpressionTask(t)
	bc := task.PrivateData.BillingContext
	bc.ModelPrice, bc.GroupRatio = 99, 20
	bc.OtherRatios = map[string]float64{"duration": 999}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, result, constant.ChannelTypeKling)
	require.Empty(t, decision.ManualReason)
	other := decision.Settlement.Effect.Other
	require.Equal(t, 1.5, other["group_ratio"])
	require.Equal(t, float64(1000), other["quota_per_unit"])
	require.NotContains(t, other, "model_price")
	require.NotContains(t, other, "duration")
	require.Equal(t, "price-v1", other["task_price_version"])
}

func TestTaskExpressionRejectsDifferentProducerPackage(t *testing.T) {
	task, result := goldenExpressionTask(t)
	encoded, err := common.Marshal(result.PricingUsageEnvelope)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, common.Unmarshal(encoded, &wire))
	wire["plugin_version"] = "2.0.0"
	wire["package_digest"] = strings.Repeat("b", 64)
	wire["channel_id"] = 902
	wire["request_id"] = "request-h3-terminal"
	encoded, err = common.Marshal(wire)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encoded, result.PricingUsageEnvelope))
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, result, constant.ChannelTypeKling)
	require.NotEmpty(t, decision.ManualReason, "same key/schema from a different package cannot settle an old task")
	require.Zero(t, decision.Settlement.FundingDelta)
}

type expressionPollingFixture struct {
	result *relaycommon.TaskInfo
	calls  atomic.Int32
}

func (a *expressionPollingFixture) Init(*relaycommon.RelayInfo) {}
func (a *expressionPollingFixture) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	a.calls.Add(1)
	encoded, err := common.Marshal(a.result)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
}
func (a *expressionPollingFixture) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var result relaycommon.TaskInfo
	return &result, common.Unmarshal(body, &result)
}
func (a *expressionPollingFixture) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	panic("legacy completion billing must not run for a task-expression plan")
}

// Exercise the existing financial seam with an actual isolated reservation.
// This is a harness, not a newly enabled public submit/provider route.
func reserveExpressionTask(t *testing.T) (*model.Task, *expressionPollingFixture) {
	t.Helper()
	truncate(t)
	seedUser(t, 901, 2000)
	seedToken(t, 903, 901, "synthetic-expression-fixture", 2000)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 903).Update("used_quota", 0).Error)
	seedChannel(t, 902)
	task, result := goldenExpressionTask(t)
	p := task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan
	digest := task.PrivateData.BillingContext.TaskBillingPlan.UsageContractDigest
	guards := taskpricing.Guards{PriceVersion: "price-v1", ContractDigest: digest, BindingVersion: "binding-v1"}
	plan, err := taskpricing.Freeze(taskpricing.FreezeInput{
		PreviewInput: taskpricing.PreviewInput{Profile: taskpricing.NewAPITask,
			Prices: types.TaskPricingMaps{PluginBillingExpr: map[string]string{"kling::kling-v1": p.Expression}},
			Source: taskpricing.Source{Driver: "task_plugin", PluginKey: "kling"}, OriginalModel: "kling-v1", MappedModel: "kling-v1",
			Contract: p.Contract, Expected: guards, Current: guards, Submit: p.SubmitFacts,
			PricingAt: p.PricingAt, GroupRatio: 1.5, QuotaPerUnit: 1000, ReservationFloor: 700},
		ChannelID: 902, Action: "generate", RequestID: p.RequestID, SourceID: "kling", PluginVersion: "1.0.0", PackageDigest: strings.Repeat("a", 64),
	})
	require.NoError(t, err)
	require.Equal(t, 600, plan.EstimateQuota)
	require.Equal(t, 700, plan.ReserveQuota)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/isolated-task-fixture", nil)
	c.Set(common.RequestIdKey, p.RequestID)
	info := &relaycommon.RelayInfo{RequestId: p.RequestID, UserId: 901, TokenId: 903, TokenKey: "synthetic-expression-fixture",
		OriginModelName: "kling-v1", UsingGroup: "default", ForcePreConsume: true, StartTime: time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 902}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "generate"},
		TaskBillingPlan: plan, UserSetting: dto.UserSetting{BillingPreference: "wallet_only", QuotaWarningThreshold: 1},
		PriceData: types.PriceData{ModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1.5}}}
	require.Nil(t, PreConsumeBilling(c, plan.ReserveQuota, info))
	require.EqualValues(t, 1300, getUserQuota(t, 901))
	require.Equal(t, 1300, getTokenRemainQuota(t, 903))
	task.Quota = info.FinalPreConsumedQuota
	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.BillingContext.TaskBillingPlan = plan
	// Conflicting legacy context must never become a second price source.
	task.PrivateData.BillingContext.ModelPrice = 99
	task.PrivateData.BillingContext.GroupRatio = 20
	task.PrivateData.BillingContext.PerCallBilling = true
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{"duration": 999}
	persistTask(t, task)
	require.Nil(t, BuildTaskSubmissionSettlementEffect(c, info, plan.ReserveQuota))
	require.NoError(t, info.Billing.Settle(plan.ReserveQuota))
	require.Zero(t, countLogs(t), "submission must not project final usage")
	return task, &expressionPollingFixture{result: result}
}

func pollExpressionFixture(t *testing.T, task *model.Task, adaptor *expressionPollingFixture) *model.Task {
	t.Helper()
	ch := &model.Channel{Id: 902, Type: constant.ChannelTypeKling, Key: "synthetic-fixture"}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	return &reloaded
}

func assertExpressionLedger(t *testing.T, task *model.Task, target int) {
	t.Helper()
	require.Equal(t, target, task.Quota)
	require.EqualValues(t, 2000-target, getUserQuota(t, 901))
	require.Equal(t, 2000-target, getTokenRemainQuota(t, 903))
	require.Equal(t, target, getTokenUsedQuota(t, 903))
	var user model.User
	require.NoError(t, model.DB.First(&user, 901).Error)
	require.EqualValues(t, target, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
	require.EqualValues(t, 1, countLogs(t))
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, 902).Error)
	require.EqualValues(t, target, channel.UsedQuota)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
	require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
	require.EqualValues(t, target-700, settlement.AppliedFundingDelta)
}

func TestTaskExpressionLifecycleReservesPollsAndReloadsExactlyOnce(t *testing.T) {
	task, adaptor := reserveExpressionTask(t)
	stale := *task
	stored := pollExpressionFixture(t, task, adaptor)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.Equal(t, 2.5, stored.PrivateData.BillingContext.PricingUsageEnvelope.Facts["units"])
	assertExpressionLedger(t, stored, 375)
	pollExpressionFixture(t, &stale, adaptor)
	pollExpressionFixture(t, stored, adaptor)
	assertExpressionLedger(t, stored, 375)
	require.EqualValues(t, 3, adaptor.calls.Load())
}

func TestTaskExpressionLifecycleZeroAndMissingRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		name    string
		units   any
		present bool
		target  int
	}{
		{"zero", 0, true, 0}, {"estimate-below-floor", 4, true, 600}, {"same-reservation", float64(14) / 3, true, 700}, {"missing", nil, false, 700}, {"over-reservation", 5, true, 700},
	} {
		t.Run(test.name, func(t *testing.T) {
			task, adaptor := reserveExpressionTask(t)
			adaptor.result.PricingUsageEnvelope.Facts = types.TaskPricingFacts{}
			if test.present {
				adaptor.result.PricingUsageEnvelope.Facts["units"] = test.units
			}
			stored := pollExpressionFixture(t, task, adaptor)
			if test.name == "missing" || test.name == "over-reservation" {
				require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
				require.EqualValues(t, 1300, getUserQuota(t, 901))
				require.Zero(t, countLogs(t))
				var settlement model.BillingSettlement
				require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
				require.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
				if test.name == "over-reservation" {
					adaptor.result.PricingUsageEnvelope.Facts["units"] = 2.5
					retry := pollExpressionFixture(t, stored, adaptor)
					require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), retry.Status)
					require.Equal(t, 700, retry.Quota, "a different manual reason must not be reopened automatically")
				}
				return
			}
			assertExpressionLedger(t, stored, test.target)
		})
	}
}

func TestTaskExpressionRejectsChangedTaskIdentityAndInvalidEvidence(t *testing.T) {
	for name, corrupt := range map[string]func(*model.Task, *relaycommon.TaskInfo){
		"channel": func(task *model.Task, _ *relaycommon.TaskInfo) { task.ChannelId++ },
		"request": func(task *model.Task, _ *relaycommon.TaskInfo) { task.PrivateData.BillingRequestId += "-other" },
		"action":  func(task *model.Task, _ *relaycommon.TaskInfo) { task.Action = "other" },
		"model":   func(task *model.Task, _ *relaycommon.TaskInfo) { task.Properties.UpstreamModelName = "other" },
		"profile": func(task *model.Task, _ *relaycommon.TaskInfo) {
			task.PrivateData.BillingContext.TaskBillingPlan.Profile = "unknown@1"
		},
		"source": func(_ *model.Task, result *relaycommon.TaskInfo) { result.PricingUsageEnvelope.SourceID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			task, result := goldenExpressionTask(t)
			corrupt(task, result)
			decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, result, constant.ChannelTypeKling)
			require.True(t, decision.UsesPlan)
			require.NotEmpty(t, decision.ManualReason)
			require.Zero(t, decision.Settlement.FundingDelta)
		})
	}
}

func TestTaskExpressionLifecycleMissingUsageRecoversAfterReload(t *testing.T) {
	task, adaptor := reserveExpressionTask(t)
	complete := types.CloneTaskPricingUsageEnvelope(adaptor.result.PricingUsageEnvelope)
	adaptor.result.PricingUsageEnvelope = nil
	stored := pollExpressionFixture(t, task, adaptor)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	require.Zero(t, countLogs(t))
	adaptor.result.PricingUsageEnvelope = complete
	stored = pollExpressionFixture(t, stored, adaptor)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	assertExpressionLedger(t, stored, 375)
}

func TestTaskExpressionSettlementConcurrentReplayUsesRecordedResult(t *testing.T) {
	task, adaptor := reserveExpressionTask(t)
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, adaptor.result, constant.ChannelTypeKling)
	require.Empty(t, decision.ManualReason)
	task.PrivateData.BillingContext.PricingUsageEnvelope = types.CloneTaskPricingUsageEnvelope(decision.PricingUsageEnvelope)
	won, err := task.UpdateWithStatusAndSettlementIntent(task.Status, task.UpdatedAt, *decision.Settlement)
	require.NoError(t, err)
	require.True(t, won)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	deltas := make(chan int64, 2)
	for range 2 {
		wg.Go(func() {
			delta, _, err := model.ApplyBillingSettlementOnce(*decision.Settlement)
			deltas <- delta
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	close(deltas)
	for delta := range deltas {
		require.EqualValues(t, -325, delta, "every replay returns the recorded applied result")
	}
	for err := range errs {
		require.NoError(t, err)
	}
	// Simulate interruption after funding but before effect/provider projection.
	require.Zero(t, countLogs(t))
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	require.Equal(t, 375, stored.Quota)
	result := pollExpressionFixture(t, &stored, adaptor)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.Zero(t, countLogs(t), "effect-only recovery is separate from provider polling")
	require.NoError(t, model.ProcessBillingSettlementEffect(decision.Settlement.OperationKey))
	require.NoError(t, model.ProcessBillingSettlementEffect(decision.Settlement.OperationKey))
	assertExpressionLedger(t, result, 375)
}

func TestTaskExpressionSubscriptionKeepsOriginalFundingAndPeriod(t *testing.T) {
	for _, scenario := range []string{"normal", "zero", "reset-period"} {
		t.Run(scenario, func(t *testing.T) {
			truncate(t)
			seedUser(t, 901, 2000)
			seedToken(t, 903, 901, "synthetic-expression-subscription", 1300)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 903).Update("used_quota", 700).Error)
			seedChannel(t, 902)
			seedSubscription(t, 905, 901, 2000, 700)
			task, result := goldenExpressionTask(t)
			task.PrivateData.BillingSource, task.PrivateData.SubscriptionId = BillingSourceSubscription, 905
			require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
				RequestId: task.PrivateData.BillingRequestId, UserId: 901, TokenId: 903, UserSubscriptionId: 905,
				PreConsumed: 700, Status: "consumed", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
			}).Error)
			persistTask(t, task)
			// A more attractive subscription must not receive this refund.
			seedSubscription(t, 906, 901, 9000, 0)
			if scenario == "reset-period" {
				require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", 905).UpdateColumn("last_reset_time", 99).Error)
			}
			target := 375
			if scenario == "zero" {
				result.PricingUsageEnvelope.Facts["units"] = 0
				target = 0
			}
			adaptor := &expressionPollingFixture{result: result}
			stored := pollExpressionFixture(t, task, adaptor)
			var original, other model.UserSubscription
			require.NoError(t, model.DB.First(&original, 905).Error)
			require.NoError(t, model.DB.First(&other, 906).Error)
			require.EqualValues(t, 2000, getUserQuota(t, 901), "wallet is not the original funding source")
			require.Zero(t, other.AmountUsed)
			if scenario == "reset-period" {
				require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
				require.EqualValues(t, 700, original.AmountUsed)
				require.Zero(t, countLogs(t))
				return
			}
			require.EqualValues(t, target, original.AmountUsed)
			require.Equal(t, 2000-target, getTokenRemainQuota(t, 903))
			require.Equal(t, target, getTokenUsedQuota(t, 903))
			require.EqualValues(t, 1, countLogs(t))
			pollExpressionFixture(t, stored, adaptor)
			require.EqualValues(t, 1, countLogs(t))
		})
	}
}

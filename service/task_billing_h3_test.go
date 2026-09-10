package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

type h3TerminalPollingAdaptor struct {
	result   *relaycommon.TaskInfo
	parseErr error
}

func (a *h3TerminalPollingAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *h3TerminalPollingAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"task":{"id":"provider-h3"}}`)),
	}, nil
}

func (a *h3TerminalPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	if a.parseErr != nil {
		return nil, a.parseErr
	}
	if a.result == nil || a.result.UsageEnvelope != nil {
		return a.result, nil
	}
	result := *a.result
	usage := types.CloneTaskUsage(result.Usage)
	if usage != nil && usage.Source == "" {
		usage.Source = types.TaskUsageSourceProviderResponse
	}
	envelope, err := taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.MiniMaxH3Contract(),
		types.TaskUsageSourceProviderResponse,
		usage,
	)
	if err != nil {
		return nil, err
	}
	result.Usage = usage
	result.UsageEnvelope = envelope
	return &result, nil
}

func (a *h3TerminalPollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	panic("legacy completion billing must not run for a frozen H3 plan")
}

func h3UsageInt64(value int64) *int64 {
	return &value
}

func serviceH3TaskInfo(t *testing.T, status string, usage *types.TaskUsage) *relaycommon.TaskInfo {
	t.Helper()
	cloned := types.CloneTaskUsage(usage)
	if cloned != nil && cloned.Source == "" {
		cloned.Source = types.TaskUsageSourceProviderResponse
	}
	envelope, err := taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.MiniMaxH3Contract(),
		types.TaskUsageSourceProviderResponse,
		cloned,
	)
	require.NoError(t, err)
	return &relaycommon.TaskInfo{Status: status, Usage: cloned, UsageEnvelope: envelope}
}

func buildServiceH3Plan(t *testing.T, input task_billing_setting.H3BillingInput) *types.TaskBillingPlan {
	t.Helper()
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	plan, err := task_billing_setting.BuildH3BillingPlan(input, 1)
	require.NoError(t, err)
	return plan
}

func makeServiceH3Task(t *testing.T, quota int, plan *types.TaskBillingPlan) *model.Task {
	t.Helper()
	task := makeTask(901, 902, quota, 903, BillingSourceWallet, 0)
	task.ID = 904
	task.TaskID = "task_h3_terminal"
	task.Properties.OriginModelName = constant.TaskModelMiniMaxH3
	task.Properties.UpstreamModelName = constant.TaskModelMiniMaxH3
	task.PrivateData.UpstreamTaskID = "provider-h3"
	task.PrivateData.BillingRequestId = "request-h3-terminal"
	task.PrivateData.BillingContext.TaskBillingPlan = types.CloneTaskBillingPlan(plan)
	return task
}

func TestH3TerminalDecisionUsesFrozenPlanForNegativeAndZeroDelta(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	usage := &types.TaskUsage{
		OutputDurationMs:     h3UsageInt64(5_000),
		InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount:      h3UsageInt64(0),
		Source:               types.TaskUsageSourceProviderResponse,
		Completeness:         types.TaskUsageCompletenessComplete,
	}

	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage),
		constant.ChannelTypeMiniMax,
	)

	require.True(t, decision.UsesPlan)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)
	require.EqualValues(t, -800, decision.Settlement.FundingDelta)
	require.EqualValues(t, 800, decision.Settlement.TaskQuotaTarget)
	require.NotNil(t, decision.Settlement.Effect)
	require.True(t, decision.Settlement.Effect.UpdateUsage)
	require.True(t, decision.Settlement.Effect.QuotaIsActual)
	require.EqualValues(t, 800, decision.Settlement.Effect.Quota)
	require.Equal(t, usage, decision.Usage)
	require.NotSame(t, usage, decision.Usage)
	require.NotSame(t, usage.OutputDurationMs, decision.Usage.OutputDurationMs)
	require.NotSame(t, usage.InputImageCount, decision.Usage.InputImageCount)

	zeroDeltaTask := makeServiceH3Task(t, plan.ReserveQuota, plan)
	reserveUsage := types.CloneTaskUsage(usage)
	reserveUsage.InputVideoDurationMs = h3UsageInt64(15_000)
	zeroDeltaDecision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, zeroDeltaTask,
		serviceH3TaskInfo(t, string(model.TaskStatusFailure), reserveUsage),
		constant.ChannelTypeMiniMax,
	)
	require.True(t, zeroDeltaDecision.UsesPlan)
	require.Empty(t, zeroDeltaDecision.ManualReason)
	require.NotNil(t, zeroDeltaDecision.Settlement)
	require.Zero(t, zeroDeltaDecision.Settlement.FundingDelta)
	require.EqualValues(t, plan.ReserveQuota, zeroDeltaDecision.Settlement.TaskQuotaTarget)
}

func TestH3TerminalDecisionPreservesExplicitZeroFinalQuota(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	for i := range plan.Components {
		plan.Components[i].UnitPrice = "0"
	}
	plan.EstimateQuota = 0
	plan.ReserveQuota = 0
	task := makeServiceH3Task(t, 100, plan)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000),
		InputImageCount:  h3UsageInt64(0),
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}

	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage),
		constant.ChannelTypeMiniMax,
	)

	require.True(t, decision.UsesPlan)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)
	require.EqualValues(t, -100, decision.Settlement.FundingDelta)
	require.Zero(t, decision.Settlement.TaskQuotaTarget)
	require.NotNil(t, decision.Settlement.Effect)
	require.True(t, decision.Settlement.Effect.QuotaIsActual)
	require.Zero(t, decision.Settlement.Effect.Quota)
}

func TestH3TerminalDecisionFailsClosedOnUsageEnvelopeIdentityDrift(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}
	result := serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage)
	result.UsageEnvelope.SourceID = "unexpected-source"

	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, result, constant.ChannelTypeMiniMax)
	require.True(t, decision.UsesPlan)
	require.ErrorContains(t, errors.New(decision.ManualReason), "usage envelope identity")
	require.NotNil(t, decision.Settlement)
	require.Zero(t, decision.Settlement.FundingDelta)
	require.Nil(t, decision.Settlement.Effect)
}

func TestFrozenTaskUsageEnvelopeKeepsMissingUntilReplacementMatchesPlan(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	missing, err := taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.MiniMaxH3Contract(),
		types.TaskUsageSourceProviderResponse,
		nil,
	)
	require.NoError(t, err)
	task.PrivateData.BillingContext.TaskUsageEnvelope = missing

	completeUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000),
		InputImageCount:  h3UsageInt64(0),
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}
	mismatched := serviceH3TaskInfo(t, string(model.TaskStatusSuccess), completeUsage).UsageEnvelope
	mismatched.SourceID = "unexpected-source"
	selected := frozenTaskUsageEnvelope(task, &relaycommon.TaskInfo{UsageEnvelope: mismatched})
	require.Equal(t, missing.EvidenceDigest, selected.EvidenceDigest)
	require.Equal(t, types.TaskUsageCompletenessMissing, selected.Completeness)

	valid := serviceH3TaskInfo(t, string(model.TaskStatusSuccess), completeUsage).UsageEnvelope
	selected = frozenTaskUsageEnvelope(task, &relaycommon.TaskInfo{UsageEnvelope: valid})
	require.Equal(t, valid.EvidenceDigest, selected.EvidenceDigest)
	require.Equal(t, types.TaskUsageCompletenessComplete, selected.Completeness)
}

func TestH3HistoricalPlanWithoutUsageIdentityKeepsLegacyCompatibility(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	plan.UsageProducerKind = ""
	plan.UsageSourceID = ""
	plan.UsageSchemaVersion = 0
	plan.UsageContractDigest = ""
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}

	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess), Usage: usage,
	}, constant.ChannelTypeMiniMax)
	require.True(t, decision.UsesPlan)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)
	require.Nil(t, decision.UsageEnvelope)
}

func TestH3HistoricalPlanKeepsPersistedLegacyUsageWhenNewResultHasEnvelope(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	plan.UsageProducerKind = ""
	plan.UsageSourceID = ""
	plan.UsageSchemaVersion = 0
	plan.UsageContractDigest = ""
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.PrivateData.BillingContext.TaskUsage = &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}
	newResultUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(15_000), InputImageCount: h3UsageInt64(9),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}

	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), newResultUsage),
		constant.ChannelTypeMiniMax,
	)
	require.Empty(t, decision.ManualReason)
	require.Nil(t, decision.UsageEnvelope)
	require.EqualValues(t, 5_000, *decision.Usage.OutputDurationMs)
	require.EqualValues(t, 0, *decision.Usage.InputImageCount)
}

func TestH3TerminalDecisionRejectsNonProviderUsageStage(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceManualReconcile, Completeness: types.TaskUsageCompletenessComplete,
	}
	envelope, err := taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.MiniMaxH3Contract(),
		types.TaskUsageSourceManualReconcile,
		usage,
	)
	require.NoError(t, err)

	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task, &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess), Usage: usage, UsageEnvelope: envelope,
	}, constant.ChannelTypeMiniMax)
	require.ErrorContains(t, errors.New(decision.ManualReason), "provider_response")
	require.NotNil(t, decision.Settlement)
	require.Zero(t, decision.Settlement.FundingDelta)
}

func TestH3TerminalDecisionUsesValidatedEnvelopeOverDivergentCompatibilityUsage(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	envelopeUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}
	envelope, err := taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.MiniMaxH3Contract(),
		types.TaskUsageSourceProviderResponse,
		envelopeUsage,
	)
	require.NoError(t, err)
	task.PrivateData.BillingContext.TaskUsageEnvelope = envelope
	task.PrivateData.BillingContext.TaskUsage = &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(15_000), InputImageCount: h3UsageInt64(9),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}

	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), task.PrivateData.BillingContext.TaskUsage),
		constant.ChannelTypeMiniMax,
	)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Usage)
	require.EqualValues(t, 5_000, *decision.Usage.OutputDurationMs)
	require.EqualValues(t, 0, *decision.Usage.InputImageCount)
	expected, err := task_billing_setting.QuoteH3Final(plan, envelopeUsage)
	require.NoError(t, err)
	require.EqualValues(t, expected.Quota, decision.Settlement.TaskQuotaTarget)
}

func TestH3SettlementEffectStoresOnlyCanonicalEnvelopeMetadata(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}
	result := serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage)
	result.Url = "https://cdn.example.com/private-result.mp4"

	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task, result, constant.ChannelTypeMiniMax,
	)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)
	require.NotNil(t, decision.Settlement.Effect)
	metadata, ok := decision.Settlement.Effect.Other["task_usage_envelope"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, types.TaskUsageCompletenessComplete, metadata["completeness"])
	require.NotContains(t, metadata, "usage")
	require.NotContains(t, metadata, "payload")
	require.NotContains(t, metadata, "url")
	encoded, err := common.Marshal(decision.Settlement.Effect.Other)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-result.mp4")
}

func TestH3TerminalDecisionRoutesUnsafeUsageToManual(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	tests := []struct {
		name  string
		usage *types.TaskUsage
	}{
		{name: "missing", usage: nil},
		{name: "partial", usage: &types.TaskUsage{OutputDurationMs: h3UsageInt64(5_000), Completeness: types.TaskUsageCompletenessPartial}},
		{name: "invalid", usage: &types.TaskUsage{Completeness: types.TaskUsageCompletenessInvalid}},
		{name: "ambiguous", usage: &types.TaskUsage{Completeness: types.TaskUsageCompletenessAmbiguous}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := makeServiceH3Task(t, plan.ReserveQuota, plan)
			decision := prepareTaskTerminalBillingDecision(
				context.Background(), nil, task,
				serviceH3TaskInfo(t, string(model.TaskStatusFailure), test.usage),
				constant.ChannelTypeMiniMax,
			)
			require.True(t, decision.UsesPlan)
			require.NotEmpty(t, decision.ManualReason)
			require.NotNil(t, decision.Settlement)
			require.Zero(t, decision.Settlement.FundingDelta)
			require.EqualValues(t, task.Quota, decision.Settlement.TaskQuotaTarget)
			require.Nil(t, decision.Settlement.Effect)
		})
	}
}

func TestH3TerminalDecisionRoutesChangedFrozenTotalsToManual(t *testing.T) {
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	plan.EstimateQuota++
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	usage := &types.TaskUsage{
		OutputDurationMs:     h3UsageInt64(5_000),
		InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount:      h3UsageInt64(0),
		Source:               types.TaskUsageSourceProviderResponse,
		Completeness:         types.TaskUsageCompletenessComplete,
	}

	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage),
		constant.ChannelTypeMiniMax,
	)

	require.True(t, decision.UsesPlan)
	require.Contains(t, decision.ManualReason, "frozen totals")
	require.NotNil(t, decision.Settlement)
	require.Zero(t, decision.Settlement.FundingDelta)
}

func TestH3SubmissionProjectionIsDeferredUntilTerminalUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 1000)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	info := &relaycommon.RelayInfo{
		UserId:          901,
		OriginModelName: constant.TaskModelMiniMaxH3,
		UsingGroup:      "default",
		TaskBillingPlan: plan,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 902},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	require.Nil(t, BuildTaskSubmissionSettlementEffect(nil, info, plan.ReserveQuota))
	LogTaskConsumption(nil, info)
	require.Zero(t, countLogs(t))
	var user model.User
	require.NoError(t, model.DB.First(&user, 901).Error)
	require.Zero(t, user.UsedQuota)
	require.Zero(t, user.RequestCount)
}

func TestUpdateVideoSingleTaskKeepsManualH3TaskPollable(t *testing.T) {
	truncate(t)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.Status = model.TaskStatusInProgress
	task.UpdatedAt = time.Now().Unix()
	persistTask(t, task)
	ch := &model.Channel{Id: task.ChannelId, Type: constant.ChannelTypeMiniMax, Key: "test"}
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusFailure), Reason: "provider failed",
		Usage: &types.TaskUsage{Completeness: types.TaskUsageCompletenessMissing},
	}}

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	require.Equal(t, plan.ReserveQuota, stored.Quota)
	require.NotNil(t, stored.PrivateData.BillingContext.TaskUsage)
	require.Equal(t, types.TaskUsageCompletenessMissing, stored.PrivateData.BillingContext.TaskUsage.Completeness)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
	require.Zero(t, settlement.FundingDelta)

	firstUpdatedAt := stored.UpdatedAt
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, stored.GetUpstreamTaskID(), map[string]*model.Task{
		stored.GetUpstreamTaskID(): &stored,
	}))
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, firstUpdatedAt, stored.UpdatedAt)
	var settlementCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).
		Count(&settlementCount).Error)
	require.EqualValues(t, 1, settlementCount)
}

func TestUpdateVideoSingleTaskLeavesBillingUntouchedOnTransientQueryError(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedToken(t, 903, 901, "h3-transient-query", 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)

	var before model.Task
	require.NoError(t, model.DB.First(&before, task.ID).Error)
	adaptor := &h3TerminalPollingAdaptor{parseErr: errors.New("MiniMax temporary query error: retry later")}
	ch := &model.Channel{Id: task.ChannelId, Type: constant.ChannelTypeMiniMax, Key: "test"}

	err := updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	})
	require.ErrorContains(t, err, "temporary query error")

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, before.Status, stored.Status)
	require.Equal(t, before.Progress, stored.Progress)
	require.Equal(t, before.Quota, stored.Quota)
	require.Equal(t, before.UpdatedAt, stored.UpdatedAt)
	require.EqualValues(t, 900, getUserQuota(t, task.UserId))
	require.Equal(t, 900, getTokenRemainQuota(t, task.PrivateData.TokenId))
	require.Zero(t, countLogs(t))

	var settlementCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key IN ?", []string{
			model.BillingTaskFinalizeOperationKey(task.ID),
			fmt.Sprintf("task:%d:refund", task.ID),
		}).Count(&settlementCount).Error)
	require.Zero(t, settlementCount)
}

func TestUpdateVideoSingleTaskRejectsNilParsedResultWithoutBillingMutation(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedToken(t, 903, 901, "h3-nil-query-result", 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)

	var before model.Task
	require.NoError(t, model.DB.First(&before, task.ID).Error)
	adaptor := &h3TerminalPollingAdaptor{}
	ch := &model.Channel{Id: task.ChannelId, Type: constant.ChannelTypeMiniMax, Key: "test"}

	err := updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	})
	require.ErrorContains(t, err, "parseTaskResult returned no result")

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, before.Status, stored.Status)
	require.Equal(t, before.Progress, stored.Progress)
	require.Equal(t, before.Quota, stored.Quota)
	require.Equal(t, before.UpdatedAt, stored.UpdatedAt)
	require.EqualValues(t, 900, getUserQuota(t, task.UserId))
	require.Equal(t, 900, getTokenRemainQuota(t, task.PrivateData.TokenId))
	require.Zero(t, countLogs(t))

	var settlementCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key IN ?", []string{
			model.BillingTaskFinalizeOperationKey(task.ID),
			fmt.Sprintf("task:%d:refund", task.ID),
		}).Count(&settlementCount).Error)
	require.Zero(t, settlementCount)
}

func TestUpdateVideoSingleTaskRecoversManualH3WhenLaterPollHasCompleteUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedToken(t, 903, 901, "h3-manual-recovery", 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputImageCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)
	missingDecision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), &types.TaskUsage{Completeness: types.TaskUsageCompletenessMissing}),
		constant.ChannelTypeMiniMax)
	require.NotEmpty(t, missingDecision.ManualReason)
	won, err := persistTaskManualBillingDecision(task, task.Status, task.UpdatedAt, missingDecision)
	require.NoError(t, err)
	require.True(t, won)

	completeUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000),
		InputImageCount:  h3UsageInt64(1),
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}
	finalQuote, err := task_billing_setting.QuoteH3Final(plan, completeUsage)
	require.NoError(t, err)
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess),
		Usage:  completeUsage,
	}}
	ch := &model.Channel{Id: task.ChannelId, Type: constant.ChannelTypeMiniMax, Key: "test"}

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.EqualValues(t, finalQuote.Quota, stored.Quota)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
	require.EqualValues(t, finalQuote.Quota-plan.ReserveQuota, settlement.FundingDelta)
	require.Contains(t, settlement.ReconciliationReviewNote, "original manual reason")
	require.Contains(t, settlement.ReconciliationReviewNote, "H3 terminal usage requires manual reconciliation")
	require.EqualValues(t, 1, countLogs(t))
	// A repeated provider poll must replay the same settlement without a
	// second balance mutation or receipt.
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, stored.GetUpstreamTaskID(), map[string]*model.Task{
		stored.GetUpstreamTaskID(): &stored,
	}))
	require.EqualValues(t, 1, countLogs(t))
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
}

func TestRecoverManualH3PersistsTerminalEvidenceBeforeFunding(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputImageCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	persistTask(t, task)

	missingDecision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), &types.TaskUsage{Completeness: types.TaskUsageCompletenessMissing}),
		constant.ChannelTypeMiniMax)
	require.NotEmpty(t, missingDecision.ManualReason)
	won, err := persistTaskManualBillingDecision(task, task.Status, task.UpdatedAt, missingDecision)
	require.NoError(t, err)
	require.True(t, won)

	completeUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000),
		InputImageCount:  h3UsageInt64(1),
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), completeUsage), constant.ChannelTypeMiniMax)
	require.Empty(t, decision.ManualReason)
	providerResult := serviceH3TaskInfo(t, string(model.TaskStatusSuccess), completeUsage)
	providerResult.Url = "https://cdn.example.com/manual-recovered.mp4"
	expectedUpdatedAt := task.UpdatedAt
	recovered, err := recoverManualTaskBillingSettlement(
		context.Background(), task, decision, providerResult, model.TaskStatusInProgress, expectedUpdatedAt,
	)
	require.NoError(t, err)
	require.True(t, recovered)

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.PrivateData.PendingTerminalStatus)
	require.Equal(t, "https://cdn.example.com/manual-recovered.mp4", stored.PrivateData.PendingTerminalResultURL)
	require.NotNil(t, stored.PrivateData.BillingContext.TaskUsage)
	require.Equal(t, types.TaskUsageCompletenessComplete, stored.PrivateData.BillingContext.TaskUsage.Completeness)

	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
	require.EqualValues(t, decision.Settlement.TaskQuotaTarget, stored.Quota)

}

func TestUpdateVideoSingleTaskAppliesAlreadyPromotedManualH3Settlement(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedToken(t, 903, 901, "h3-promoted-recovery", 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputImageCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)

	missingDecision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), &types.TaskUsage{Completeness: types.TaskUsageCompletenessMissing}),
		constant.ChannelTypeMiniMax)
	require.NotEmpty(t, missingDecision.ManualReason)
	won, err := persistTaskManualBillingDecision(task, task.Status, task.UpdatedAt, missingDecision)
	require.NoError(t, err)
	require.True(t, won)

	completeUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000),
		InputImageCount:  h3UsageInt64(1),
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}
	providerResult := serviceH3TaskInfo(t, string(model.TaskStatusSuccess), completeUsage)
	providerResult.Url = "https://cdn.example.com/promoted-recovery.mp4"
	decision := prepareTaskTerminalBillingDecision(
		context.Background(), nil, task, providerResult, constant.ChannelTypeMiniMax,
	)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)

	candidate := *task
	billingContext := *task.PrivateData.BillingContext
	candidate.PrivateData.BillingContext = &billingContext
	candidate.PrivateData.BillingContext.TaskUsage = types.CloneTaskUsage(completeUsage)
	candidate.PrivateData.BillingContext.TaskUsageEnvelope = types.CloneTaskUsageEnvelope(decision.UsageEnvelope)
	persistPendingTaskTerminalEvidence(&candidate, providerResult, time.Now().Unix())
	won, err = candidate.UpdateWithStatusAndPendingTerminalEvidence(task.Status, task.UpdatedAt)
	require.NoError(t, err)
	require.True(t, won)
	*task = candidate

	ready, err := model.PromoteManualTaskBillingSettlement(
		*decision.Settlement,
		"H3 terminal usage requires manual reconciliation:",
		"H3 task settlement automatically recovered after a complete provider usage response",
	)
	require.NoError(t, err)
	require.True(t, ready)
	status, found, err := model.GetBillingSettlementStatus(model.BillingTaskFinalizeOperationKey(task.ID))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model.BillingSettlementStatusPending, status)

	beforeUserQuota := getUserQuota(t, task.UserId)
	beforeTokenQuota := getTokenRemainQuota(t, task.PrivateData.TokenId)
	adaptor := &h3TerminalPollingAdaptor{result: providerResult}
	ch := &model.Channel{Id: task.ChannelId, Type: constant.ChannelTypeMiniMax, Key: "test"}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.EqualValues(t, decision.Settlement.TaskQuotaTarget, stored.Quota)
	require.EqualValues(t, beforeUserQuota-decision.Settlement.FundingDelta, getUserQuota(t, task.UserId))
	require.EqualValues(t, beforeTokenQuota-int(decision.Settlement.TokenDelta), getTokenRemainQuota(t, task.PrivateData.TokenId))
	require.EqualValues(t, 1, countLogs(t))
	var logOther map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(getLastLog(t).Other, &logOther))
	planMetadata, ok := logOther["task_billing_plan"].(map[string]interface{})
	require.True(t, ok)
	require.NotContains(t, planMetadata, "config_hash")
	require.NotContains(t, planMetadata, "group_ratio")
	require.NotContains(t, planMetadata, "quota_per_unit")
	require.NotContains(t, planMetadata, "components")
	usageMetadata, ok := logOther["task_usage"].(map[string]interface{})
	require.True(t, ok)
	require.EqualValues(t, 5_000, usageMetadata["output_duration_ms"])
	require.EqualValues(t, 1, usageMetadata["input_image_count"])
	require.Equal(t, types.TaskUsageCompletenessComplete, usageMetadata["completeness"])
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)

	userQuotaAfterFirstPoll := getUserQuota(t, task.UserId)
	tokenQuotaAfterFirstPoll := getTokenRemainQuota(t, task.PrivateData.TokenId)
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, stored.GetUpstreamTaskID(), map[string]*model.Task{
		stored.GetUpstreamTaskID(): &stored,
	}))
	require.EqualValues(t, userQuotaAfterFirstPoll, getUserQuota(t, task.UserId))
	require.EqualValues(t, tokenQuotaAfterFirstPoll, getTokenRemainQuota(t, task.PrivateData.TokenId))
	require.EqualValues(t, 1, countLogs(t))
}

func TestUpdateVideoSingleTaskRecoversManualH3SubscriptionFullRefund(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 905, 906, 907, 908
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "h3-manual-subscription-recovery", 100)
	seedChannel(t, channelID)
	seedSubscription(t, subscriptionID, userID, 10_000, 5_000)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{Resolution: "768P", OutputDurationSeconds: 5})
	for i := range plan.Components {
		plan.Components[i].UnitPrice = "0"
	}
	plan.EstimateQuota = 0
	plan.ReserveQuota = 0
	task := makeServiceH3Task(t, 100, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.PrivateData.BillingSource = BillingSourceSubscription
	task.PrivateData.SubscriptionId = subscriptionID
	task.PrivateData.BillingRequestId = ""
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)

	missingDecision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), &types.TaskUsage{Completeness: types.TaskUsageCompletenessMissing}),
		constant.ChannelTypeMiniMax)
	require.NotEmpty(t, missingDecision.ManualReason)
	won, err := persistTaskManualBillingDecision(task, task.Status, task.UpdatedAt, missingDecision)
	require.NoError(t, err)
	require.True(t, won)

	completeUsage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000),
		InputImageCount:  h3UsageInt64(0),
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), completeUsage), constant.ChannelTypeMiniMax)
	require.Empty(t, decision.ManualReason)
	require.NotNil(t, decision.Settlement)
	require.EqualValues(t, -100, decision.Settlement.FundingDelta)
	require.True(t, decision.Settlement.FinalizeSubscriptionPreConsume)
	require.True(t, decision.Settlement.AllowMissingToken)

	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess), Usage: completeUsage,
	}}
	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeMiniMax, Key: "test"}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.Zero(t, stored.Quota)
	require.EqualValues(t, 4_900, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 200, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, 0, getTokenUsedQuota(t, tokenID))
	require.EqualValues(t, 1, countLogs(t))
	var preConsume model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", stored.PrivateData.BillingRequestId).First(&preConsume).Error)
	require.Equal(t, "refunded", preConsume.Status)

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, stored.GetUpstreamTaskID(), map[string]*model.Task{
		stored.GetUpstreamTaskID(): &stored,
	}))
	require.EqualValues(t, 4_900, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 200, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, 1, countLogs(t))
}

func TestUpdateVideoSingleTaskDoesNotRecoverManualH3BeforeSubmissionSettlement(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedToken(t, 903, 901, "h3-manual-submission-pending", 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{Resolution: "768P", OutputDurationSeconds: 5, InputImageCount: 1})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	persistTask(t, task)
	requestKey := model.BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId)
	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: requestKey, Source: model.BillingSettlementSourceWallet, UserID: task.UserId,
		Status: model.BillingSettlementStatusPending, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Revision: 1,
	}).Error)
	missingDecision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), &types.TaskUsage{Completeness: types.TaskUsageCompletenessMissing}),
		constant.ChannelTypeMiniMax)
	_, err := persistTaskManualBillingDecision(task, task.Status, task.UpdatedAt, missingDecision)
	require.NoError(t, err)

	completeUsage := &types.TaskUsage{OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(1), Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete}
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), Usage: completeUsage}}
	ch := &model.Channel{Id: task.ChannelId, Type: constant.ChannelTypeMiniMax, Key: "test"}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	var finalSettlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&finalSettlement).Error)
	require.Equal(t, model.BillingSettlementStatusManual, finalSettlement.Status)
}

func TestUpdateVideoSingleTaskKeepsH3TaskPollableWhenSubscriptionPeriodChanged(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 911, 912, 913, 914
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "h3-period-change", 5_000)
	seedChannel(t, channelID)
	seedSubscription(t, subscriptionID, userID, 10_000, 5_000)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.PrivateData.BillingSource = BillingSourceSubscription
	task.PrivateData.SubscriptionId = subscriptionID
	task.PrivateData.BillingRequestId = ""
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("id = ?", subscriptionID).
		UpdateColumn("last_reset_time", 99).Error)

	usage := &types.TaskUsage{
		OutputDurationMs:     h3UsageInt64(5_000),
		InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount:      h3UsageInt64(0),
		Source:               types.TaskUsageSourceProviderResponse,
		Completeness:         types.TaskUsageCompletenessComplete,
	}
	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeMiniMax, Key: "test"}
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess), Usage: usage,
	}}

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	require.Equal(t, plan.ReserveQuota, stored.Quota)
	require.NotNil(t, stored.PrivateData.BillingContext.TaskUsage)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
	require.ErrorContains(t, fmt.Errorf("%s", settlement.LastError), "crossed a quota reset period")
	require.Zero(t, countLogs(t))
}

func TestUpdateVideoSingleTaskSettlesH3BeforeTerminalTransition(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 951, 952, 953
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "h3-terminal-order", 900)
	seedChannel(t, channelID)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount: h3UsageInt64(0), Source: types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessComplete,
	}
	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeMiniMax, Key: "test"}
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess), Usage: usage,
	}}

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.EqualValues(t, 800, stored.Quota)
	require.EqualValues(t, 1_700, getUserQuota(t, userID))
	require.Equal(t, 1_700, getTokenRemainQuota(t, tokenID))
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.EqualValues(t, 800, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
	require.EqualValues(t, 1, countLogs(t))
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
	require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
}

func TestUpdateVideoSingleTaskRecoversAppliedH3FundingWithPendingEffect(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 961, 962, 963
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "h3-effect-recovery", 900)
	seedChannel(t, channelID)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.Status = model.TaskStatusInProgress
	persistTask(t, task)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount: h3UsageInt64(0), Source: types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage), constant.ChannelTypeMiniMax)
	require.NotNil(t, decision.Settlement)
	task.PrivateData.BillingContext.TaskUsage = types.CloneTaskUsage(usage)
	task.PrivateData.BillingContext.TaskUsageEnvelope = types.CloneTaskUsageEnvelope(decision.UsageEnvelope)
	won, err := task.UpdateWithStatusAndSettlementIntent(task.Status, task.UpdatedAt, *decision.Settlement)
	require.NoError(t, err)
	require.True(t, won)
	_, _, err = model.ApplyBillingSettlementOnce(*decision.Settlement)
	require.NoError(t, err)

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
	require.EqualValues(t, 800, stored.Quota)
	require.Zero(t, countLogs(t))
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Zero(t, user.UsedQuota)
	require.Zero(t, user.RequestCount)
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(task.ID)).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
	require.Equal(t, model.BillingSettlementEffectPending, settlement.EffectStatus)

	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeMiniMax, Key: "test"}
	adaptor := &h3TerminalPollingAdaptor{result: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess), Usage: usage,
	}}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, stored.GetUpstreamTaskID(), map[string]*model.Task{
		stored.GetUpstreamTaskID(): &stored,
	}))
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	require.EqualValues(t, 800, stored.Quota)
	require.Zero(t, countLogs(t))

	require.NoError(t, model.ProcessBillingSettlementEffect(decision.Settlement.OperationKey))
	require.NoError(t, model.ProcessBillingSettlementEffect(decision.Settlement.OperationKey))
	require.EqualValues(t, 1, countLogs(t))
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.EqualValues(t, 800, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
	require.EqualValues(t, 1_700, getUserQuota(t, userID))
	require.Equal(t, 1_700, getTokenRemainQuota(t, tokenID))
}

func TestH3ZeroFinalSettlementRefundsAndProjectsReceiptExactlyOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 900)
	seedToken(t, 903, 901, "h3-zero-final", 900)
	seedChannel(t, 902)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	})
	for i := range plan.Components {
		plan.Components[i].UnitPrice = "0"
	}
	plan.EstimateQuota = 0
	plan.ReserveQuota = 0
	task := makeServiceH3Task(t, 100, plan)
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputImageCount: h3UsageInt64(0),
		Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage), constant.ChannelTypeMiniMax)
	require.NotNil(t, decision.Settlement)

	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.EqualValues(t, 1000, getUserQuota(t, 901))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, 903))
	require.Zero(t, task.Quota)
	var user model.User
	require.NoError(t, model.DB.First(&user, 901).Error)
	require.Zero(t, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
	require.EqualValues(t, 1, countLogs(t))
	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, model.LogTypeConsume, log.Type)
	require.Zero(t, log.Quota)
}

func TestH3SettlementRefundsWalletWhenTokenWasDeleted(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 921, 922, 923
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "h3-deleted-token", 900)
	seedChannel(t, channelID)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)
	require.NoError(t, model.DB.Delete(&model.Token{}, tokenID).Error)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount: h3UsageInt64(0), Source: types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage), constant.ChannelTypeMiniMax)
	require.NotNil(t, decision.Settlement)

	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.EqualValues(t, 1_700, getUserQuota(t, userID))
	require.EqualValues(t, 800, task.Quota)
	require.EqualValues(t, 1, countLogs(t))
}

func TestH3SettlementRefundsUnlimitedTokenExactlyOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 931, 932, 933
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "h3-unlimited-token", 900)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("unlimited_quota", true).Error)
	seedChannel(t, channelID)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputVideoDurationMs: h3UsageInt64(5_000),
		InputImageCount: h3UsageInt64(0), Source: types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage), constant.ChannelTypeMiniMax)
	require.NotNil(t, decision.Settlement)

	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.EqualValues(t, 1_700, getUserQuota(t, userID))
	require.Equal(t, 1_700, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 100, getTokenUsedQuota(t, tokenID))
	require.EqualValues(t, 1, countLogs(t))
}

func TestH3SubscriptionZeroDeltaFinalizesWithoutTouchingSubscriptionPeriod(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 941, 942, 943, 944
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "h3-subscription-zero-delta", 5_000)
	seedChannel(t, channelID)
	seedSubscription(t, subscriptionID, userID, 10_000, 5_000)
	plan := buildServiceH3Plan(t, task_billing_setting.H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	})
	task := makeServiceH3Task(t, plan.ReserveQuota, plan)
	task.UserId = userID
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID
	task.PrivateData.BillingSource = BillingSourceSubscription
	task.PrivateData.SubscriptionId = subscriptionID
	task.PrivateData.BillingRequestId = ""
	task.Status = model.TaskStatusSuccess
	persistTask(t, task)
	const stableUpdatedAt int64 = 123
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("id = ?", subscriptionID).
		UpdateColumn("updated_at", stableUpdatedAt).Error)
	usage := &types.TaskUsage{
		OutputDurationMs: h3UsageInt64(5_000), InputVideoDurationMs: h3UsageInt64(15_000),
		InputImageCount: h3UsageInt64(0), Source: types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessComplete,
	}
	decision := prepareTaskTerminalBillingDecision(context.Background(), nil, task,
		serviceH3TaskInfo(t, string(model.TaskStatusSuccess), usage), constant.ChannelTypeMiniMax)
	require.NotNil(t, decision.Settlement)
	require.Zero(t, decision.Settlement.FundingDelta)

	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	require.True(t, ApplyTaskBillingSettlement(context.Background(), task, decision.Settlement))
	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	require.EqualValues(t, 5_000, subscription.AmountUsed)
	require.Equal(t, stableUpdatedAt, subscription.UpdatedAt)
	require.EqualValues(t, 1, countLogs(t))
}

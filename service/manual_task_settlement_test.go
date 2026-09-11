package service

import (
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createManualTaskFinalizeSettlement(t *testing.T, task *model.Task, reason string) model.BillingSettlement {
	t.Helper()
	now := time.Now().Unix()
	settlement := model.BillingSettlement{
		OperationKey:                    model.BillingTaskFinalizeOperationKey(task.ID),
		Source:                          model.BillingSettlementSourceWallet,
		UserID:                          task.UserId,
		SubscriptionID:                  task.PrivateData.SubscriptionId,
		TokenID:                         task.PrivateData.TokenId,
		TaskID:                          task.ID,
		TaskQuota:                       int64(task.Quota),
		TaskQuotaTarget:                 int64(task.Quota),
		SubscriptionPreConsumeRequestID: task.PrivateData.BillingRequestId,
		Status:                          model.BillingSettlementStatusManual,
		LastError:                       reason,
		CreatedAt:                       now,
		UpdatedAt:                       now,
		Revision:                        1,
	}
	if taskIsSubscription(task) {
		settlement.Source = model.BillingSettlementSourceSubscription
	}
	require.NoError(t, model.DB.Create(&settlement).Error)
	return settlement
}

func TestCompleteManualTaskBillingSettlementRefundsWalletAndReplays(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 801, 802, 803
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-wallet", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage was incomplete")
	actualQuota := int64(40)

	result, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9001,
		&actualQuota,
		"Verified provider artifacts and calculated the exact final quota.",
	)

	require.NoError(t, err)
	assert.False(t, result.AlreadyApplied)
	assert.EqualValues(t, -60, result.AppliedFundingDelta)
	assert.Equal(t, model.BillingTaskManualCompletionOperationKey(task.ID), result.OperationKey)
	assert.EqualValues(t, 960, getUserQuota(t, userID))
	assert.Equal(t, 160, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 40, getTokenUsedQuota(t, tokenID))
	var storedTask model.Task
	require.NoError(t, model.DB.First(&storedTask, task.ID).Error)
	assert.Equal(t, 40, storedTask.Quota)
	pending, applied, stateErr := taskTerminalSettlementState(&storedTask, true)
	require.NoError(t, stateErr)
	assert.False(t, pending)
	assert.True(t, applied)
	var storedManual model.BillingSettlement
	require.NoError(t, model.DB.First(&storedManual, manual.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, storedManual.Status)
	assert.Equal(t, "provider usage was incomplete", storedManual.LastError)
	assert.Equal(t, 9001, storedManual.ReconciliationReviewedBy)
	assert.Contains(t, storedManual.ReconciliationReviewNote, "Verified provider artifacts")
	assert.EqualValues(t, manual.Revision+1, storedManual.Revision)
	assert.EqualValues(t, 1, countLogs(t))

	replayed, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9001,
		&actualQuota,
		"Verified provider artifacts and calculated the exact final quota.",
	)
	require.NoError(t, err)
	assert.True(t, replayed.AlreadyApplied)
	assert.EqualValues(t, -60, replayed.AppliedFundingDelta)
	assert.EqualValues(t, 960, getUserQuota(t, userID))
	assert.Equal(t, 160, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 1, countLogs(t))

	// The current UI no longer submits an audit note. A retry of a settlement
	// created by the previous UI must retain its durable note rather than fail
	// solely because the generated default differs.
	replayedWithoutNote, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9001,
		&actualQuota,
		"",
	)
	require.NoError(t, err)
	assert.True(t, replayedWithoutNote.AlreadyApplied)
	assert.EqualValues(t, -60, replayedWithoutNote.AppliedFundingDelta)
	assert.EqualValues(t, 1, countLogs(t))

	differentQuota := int64(30)
	_, err = CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9001,
		&differentQuota,
		"Verified provider artifacts and calculated a different final quota.",
	)
	require.Error(t, err)
	assert.EqualValues(t, 960, getUserQuota(t, userID))
	assert.Equal(t, 160, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 1, countLogs(t))
}

func TestCompleteManualTaskBillingSettlementPreservesExplicitZero(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 811, 812, 813
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-zero", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider confirmed no billable output")
	actualQuota := int64(0)

	_, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9011,
		&actualQuota,
		"",
	)

	require.NoError(t, err)
	assert.EqualValues(t, 1000, getUserQuota(t, userID))
	assert.Equal(t, 200, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	var storedTask model.Task
	require.NoError(t, model.DB.First(&storedTask, task.ID).Error)
	assert.Zero(t, storedTask.Quota)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Zero(t, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	assert.EqualValues(t, 1, countLogs(t))
	var storedManual model.BillingSettlement
	require.NoError(t, model.DB.First(&storedManual, manual.ID).Error)
	assert.Equal(t, "Administrator-approved manual task usage settlement", storedManual.ReconciliationReviewNote)
}

func TestCompleteManualTaskBillingSettlementsZeroIsIdempotent(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 816, 817, 818
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-zero-batch", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	// The client model may be an alias while the persisted channel mapping
	// records the effective MiniMax-H3 upstream model.
	task.Properties.OriginModelName = "minimax-h3-alias"
	task.Properties.UpstreamModelName = constant.TaskModelMiniMaxH3
	task.PrivateData.BillingContext.OriginModelName = "minimax-h3-alias"
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage requires manual review")

	result, err := CompleteManualTaskBillingSettlementsZero(
		[]model.BillingSettlementReviewTarget{{ID: manual.ID, Revision: manual.Revision}},
		9016,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.CompletedCount)
	assert.Zero(t, result.FailedCount)
	assert.Equal(t, []int64{manual.ID}, result.SettlementIDs)
	assert.EqualValues(t, 1000, getUserQuota(t, userID))
	assert.Equal(t, 200, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))

	replay, err := CompleteManualTaskBillingSettlementsZero(
		[]model.BillingSettlementReviewTarget{{ID: manual.ID, Revision: manual.Revision}},
		9016,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, replay.CompletedCount)
	assert.Zero(t, replay.FailedCount)
	assert.EqualValues(t, 1000, getUserQuota(t, userID))
	assert.Equal(t, 200, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 1, countLogs(t))
	var completion model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskManualCompletionOperationKey(task.ID)).First(&completion).Error)
	var effect model.BillingSettlementEffect
	require.NoError(t, common.Unmarshal([]byte(completion.EffectPayload), &effect))
	assert.Equal(t, manualTaskBillingZeroNote, effect.Content)
}

func TestCompleteManualTaskBillingSettlementsAppliesDifferentExactQuotas(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 8171, 8172, 8173
	seedUser(t, userID, 2000)
	seedToken(t, tokenID, userID, "manual-completion-exact-batch", 200)
	seedChannel(t, channelID)
	first := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	second := makeTask(userID, channelID, 80, tokenID, BillingSourceWallet, 0)
	persistTask(t, first)
	persistTask(t, second)
	firstManual := createManualTaskFinalizeSettlement(t, first, "provider usage requires exact review")
	secondManual := createManualTaskFinalizeSettlement(t, second, "provider usage requires exact review")
	firstQuota := int64(25)
	secondQuota := int64(60)

	result, err := CompleteManualTaskBillingSettlements([]ManualTaskBillingCompletionTarget{
		{ID: firstManual.ID, Revision: firstManual.Revision, ActualQuota: &firstQuota},
		{ID: secondManual.ID, Revision: secondManual.Revision, ActualQuota: &secondQuota},
	}, 9181)

	require.NoError(t, err)
	assert.Equal(t, 2, result.CompletedCount)
	assert.Zero(t, result.FailedCount)
	assert.ElementsMatch(t, []int64{firstManual.ID, secondManual.ID}, result.SettlementIDs)
	assert.EqualValues(t, 2095, getUserQuota(t, userID))
	assert.Equal(t, 295, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 105, getTokenUsedQuota(t, tokenID))
	var storedFirst, storedSecond model.Task
	require.NoError(t, model.DB.First(&storedFirst, first.ID).Error)
	require.NoError(t, model.DB.First(&storedSecond, second.ID).Error)
	assert.Equal(t, 25, storedFirst.Quota)
	assert.Equal(t, 60, storedSecond.Quota)

	replay, err := CompleteManualTaskBillingSettlements([]ManualTaskBillingCompletionTarget{
		{ID: firstManual.ID, Revision: firstManual.Revision, ActualQuota: &firstQuota},
		{ID: secondManual.ID, Revision: secondManual.Revision, ActualQuota: &secondQuota},
	}, 9181)
	require.NoError(t, err)
	assert.Equal(t, 2, replay.CompletedCount)
	assert.Zero(t, replay.FailedCount)
	assert.EqualValues(t, 2, countLogs(t))
}

func TestCompleteManualTaskBillingSettlementsPrevalidatesEveryItem(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 8181, 8182, 8183
	seedUser(t, userID, 2000)
	seedToken(t, tokenID, userID, "manual-completion-exact-batch-validation", 200)
	seedChannel(t, channelID)
	first := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	second := makeTask(userID, channelID, 80, tokenID, BillingSourceWallet, 0)
	persistTask(t, first)
	persistTask(t, second)
	firstManual := createManualTaskFinalizeSettlement(t, first, "provider usage requires exact review")
	secondManual := createManualTaskFinalizeSettlement(t, second, "provider usage requires exact review")
	firstQuota := int64(25)
	invalidQuota := int64(81)

	result, err := CompleteManualTaskBillingSettlements([]ManualTaskBillingCompletionTarget{
		{ID: firstManual.ID, Revision: firstManual.Revision, ActualQuota: &firstQuota},
		{ID: secondManual.ID, Revision: secondManual.Revision, ActualQuota: &invalidQuota},
	}, 9182)

	require.NoError(t, err)
	assert.Zero(t, result.CompletedCount)
	assert.Equal(t, 1, result.FailedCount)
	assert.Empty(t, result.SettlementIDs)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, secondManual.ID, result.Failed[0].SettlementID)
	assert.EqualValues(t, 2000, getUserQuota(t, userID))
	assert.Equal(t, 200, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, countLogs(t))
}

func TestCompleteManualTaskBillingSettlementsPrevalidatesReplayReviewer(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 8184, 8185, 8186
	seedUser(t, userID, 2000)
	seedToken(t, tokenID, userID, "manual-completion-replay-validation", 200)
	seedChannel(t, channelID)
	first := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	second := makeTask(userID, channelID, 80, tokenID, BillingSourceWallet, 0)
	persistTask(t, first)
	persistTask(t, second)
	firstManual := createManualTaskFinalizeSettlement(t, first, "provider usage requires exact review")
	secondManual := createManualTaskFinalizeSettlement(t, second, "provider usage requires exact review")
	secondQuota := int64(40)
	_, err := CompleteManualTaskBillingSettlement(
		secondManual.ID,
		secondManual.Revision,
		9300,
		&secondQuota,
		"Approved by the original reviewer.",
	)
	require.NoError(t, err)

	firstQuota := int64(25)
	result, err := CompleteManualTaskBillingSettlements([]ManualTaskBillingCompletionTarget{
		{ID: firstManual.ID, Revision: firstManual.Revision, ActualQuota: &firstQuota},
		{ID: secondManual.ID, Revision: secondManual.Revision, ActualQuota: &secondQuota},
	}, 9301)

	require.NoError(t, err)
	assert.Zero(t, result.CompletedCount)
	assert.Equal(t, 1, result.FailedCount)
	assert.Empty(t, result.SettlementIDs)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, secondManual.ID, result.Failed[0].SettlementID)
	assert.EqualValues(t, 2040, getUserQuota(t, userID), "the earlier completed settlement is the only funding mutation")
	var storedFirst model.Task
	var storedSecond model.Task
	require.NoError(t, model.DB.First(&storedFirst, first.ID).Error)
	require.NoError(t, model.DB.First(&storedSecond, second.ID).Error)
	assert.Equal(t, 100, storedFirst.Quota)
	assert.Equal(t, 40, storedSecond.Quota)
}

func TestValidateManualTaskBillingCompletionReplay(t *testing.T) {
	original := model.BillingSettlement{
		ReconciliationReviewedBy: 9302,
		ReconciliationReviewNote: "durable review note",
	}
	eligibility := manualTaskBillingEligibility{
		original:                original,
		originalAlreadyResolved: true,
	}

	note, err := validateManualTaskBillingCompletionReplay(
		eligibility,
		9302,
		manualTaskBillingDefaultNote,
	)
	require.NoError(t, err)
	assert.Equal(t, original.ReconciliationReviewNote, note)

	_, err = validateManualTaskBillingCompletionReplay(
		eligibility,
		9303,
		manualTaskBillingDefaultNote,
	)
	assert.ErrorIs(t, err, model.ErrBillingSettlementReviewConflict)

	eligibility.original.ReconciliationReviewNote = ""
	_, err = validateManualTaskBillingCompletionReplay(
		eligibility,
		9302,
		manualTaskBillingDefaultNote,
	)
	assert.ErrorIs(t, err, model.ErrBillingSettlementReviewConflict)
}

func TestCompleteManualTaskBillingSettlementsZeroPrevalidatesSelection(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 819, 820, 821
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-zero-partial", 100)
	seedChannel(t, channelID)
	h3Task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	h3Task.Properties.OriginModelName = constant.TaskModelMiniMaxH3
	h3Task.Properties.UpstreamModelName = constant.TaskModelMiniMaxH3
	h3Task.PrivateData.BillingContext.OriginModelName = constant.TaskModelMiniMaxH3
	persistTask(t, h3Task)
	h3Manual := createManualTaskFinalizeSettlement(t, h3Task, "provider usage requires manual review")

	ordinaryTask := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	ordinaryTask.Properties.OriginModelName = constant.TaskModelMiniMaxH3
	ordinaryTask.Properties.UpstreamModelName = "legacy-video-model"
	persistTask(t, ordinaryTask)
	ordinaryManual := createManualTaskFinalizeSettlement(t, ordinaryTask, "provider usage requires manual review")

	result, err := CompleteManualTaskBillingSettlementsZero(
		[]model.BillingSettlementReviewTarget{
			{ID: h3Manual.ID, Revision: h3Manual.Revision},
			{ID: ordinaryManual.ID, Revision: ordinaryManual.Revision},
		},
		9019,
	)
	require.NoError(t, err)
	assert.Zero(t, result.CompletedCount)
	assert.Equal(t, 1, result.FailedCount)
	assert.Empty(t, result.SettlementIDs)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, ordinaryManual.ID, result.Failed[0].SettlementID)
	assert.Contains(t, result.Failed[0].Message, "MiniMax-H3")
	assert.EqualValues(t, 900, getUserQuota(t, userID), "no target may settle before the complete selection validates")
	assert.Equal(t, 100, getTokenRemainQuota(t, tokenID))
}

func TestZeroTaskSettlementRechecksMiniMaxH3AtCompletionBoundary(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 825, 826, 827
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-zero-boundary", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.Properties.OriginModelName = constant.TaskModelMiniMaxH3
	task.Properties.UpstreamModelName = "legacy-video-model"
	task.PrivateData.BillingContext.OriginModelName = constant.TaskModelMiniMaxH3
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage requires manual review")
	actualQuota := int64(0)

	_, err := completeManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9020,
		&actualQuota,
		manualTaskBillingZeroNote,
		true,
	)

	assert.ErrorIs(t, err, model.ErrBillingSettlementReviewConflict)
	assert.ErrorIs(t, err, errManualTaskBillingZeroQuotaRequiresMiniMaxH3)
	assert.EqualValues(t, 900, getUserQuota(t, userID))
	assert.Equal(t, 100, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 100, getTokenUsedQuota(t, tokenID))
}

func TestCompleteManualTaskBillingSettlementRefundsSubscriptionWithDeletedToken(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 821, 822, 823, 824
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "manual-completion-subscription", 100)
	seedChannel(t, channelID)
	seedSubscription(t, subscriptionID, userID, 1000, 100)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceSubscription, subscriptionID)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider confirmed an empty result")
	require.NoError(t, model.DB.Delete(&model.Token{}, tokenID).Error)
	actualQuota := int64(0)

	_, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9021,
		&actualQuota,
		"Verified the empty provider result and approved a full refund.",
	)

	require.NoError(t, err)
	assert.Zero(t, getSubscriptionUsed(t, subscriptionID))
	var preConsume model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.BillingRequestId).First(&preConsume).Error)
	assert.Equal(t, "refunded", preConsume.Status)
	var storedTask model.Task
	require.NoError(t, model.DB.First(&storedTask, task.ID).Error)
	assert.Zero(t, storedTask.Quota)
	assert.EqualValues(t, 1, countLogs(t))
}

func TestCompleteManualTaskBillingSettlementRecoversAfterChildWasApplied(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 831, 832, 833
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-recovery", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage needed manual recovery")
	actualQuota := int64(40)
	completionTask := *task
	completionTask.Quota = int(manual.TaskQuota)
	input := buildTaskExactFinalSettlementInput(
		&completionTask,
		int(actualQuota),
		types.CloneTaskUsage(task.PrivateData.BillingContext.TaskUsage),
		"Administrator manual task usage settlement: recovery fixture",
	)
	require.NotNil(t, input)
	input.OperationKey = model.BillingTaskManualCompletionOperationKey(task.ID)
	_, err := model.EnsureManualTaskBillingCompletion(*input, 9031, "Recovered the already-applied child settlement after restart.")
	require.NoError(t, err)
	_, _, err = model.ApplyBillingSettlementOnce(*input)
	require.NoError(t, err)

	var unresolved model.BillingSettlement
	require.NoError(t, model.DB.First(&unresolved, manual.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, unresolved.Status)
	assert.EqualValues(t, 960, getUserQuota(t, userID))

	result, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9031,
		&actualQuota,
		"Recovered the already-applied child settlement after restart.",
	)

	require.NoError(t, err)
	assert.True(t, result.AlreadyApplied)
	assert.EqualValues(t, 960, getUserQuota(t, userID))
	assert.Equal(t, 160, getTokenRemainQuota(t, tokenID))
	require.NoError(t, model.DB.First(&unresolved, manual.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, unresolved.Status)
	assert.EqualValues(t, manual.Revision+1, unresolved.Revision)
	require.NoError(t, model.ProcessBillingSettlementEffect(input.OperationKey))
	require.NoError(t, model.ProcessBillingSettlementEffect(input.OperationKey))
	assert.EqualValues(t, 1, countLogs(t))

	pending, applied, stateErr := taskTerminalSettlementState(task, true)
	require.NoError(t, stateErr)
	assert.False(t, pending)
	assert.True(t, applied)
}

func TestCompleteManualTaskBillingSettlementValidatesExplicitAmount(t *testing.T) {
	truncate(t)
	const userID = 841
	seedUser(t, userID, 900)
	task := makeTask(userID, 0, 100, 0, BillingSourceWallet, 0)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage needed manual review")

	_, err := CompleteManualTaskBillingSettlement(manual.ID, manual.Revision, 9041, nil, "Verified provider evidence.")
	require.Error(t, err)
	aboveReservation := int64(101)
	_, err = CompleteManualTaskBillingSettlement(manual.ID, manual.Revision, 9041, &aboveReservation, "Verified provider evidence.")
	require.Error(t, err)
	negative := int64(-1)
	_, err = CompleteManualTaskBillingSettlement(manual.ID, manual.Revision, 9041, &negative, "Verified provider evidence.")
	require.Error(t, err)
	assert.EqualValues(t, 900, getUserQuota(t, userID))
	var childCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", model.BillingTaskManualCompletionOperationKey(task.ID)).
		Count(&childCount).Error)
	assert.Zero(t, childCount)
}

func TestCompleteManualTaskBillingSettlementRejectsTaskIdentityDrift(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 851, 852, 853
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-identity", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage needed manual review")
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("user_id", userID+1).Error)
	actualQuota := int64(40)

	_, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9051,
		&actualQuota,
		"Verified provider evidence and exact usage.",
	)

	require.ErrorIs(t, err, model.ErrBillingSettlementReviewConflict)
	assert.EqualValues(t, 900, getUserQuota(t, userID))
	assert.Equal(t, 100, getTokenRemainQuota(t, tokenID))
	var childCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", model.BillingTaskManualCompletionOperationKey(task.ID)).
		Count(&childCount).Error)
	assert.Zero(t, childCount)
}

func TestCompleteManualTaskBillingSettlementRejectsClampedSubscriptionRefund(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 861, 862, 863, 864
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "manual-completion-clamped-subscription", 100)
	seedChannel(t, channelID)
	seedSubscription(t, subscriptionID, userID, 1000, 50)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceSubscription, subscriptionID)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider confirmed no billable usage")
	actualQuota := int64(0)

	_, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9061,
		&actualQuota,
		"Verified provider evidence for a full refund.",
	)

	require.ErrorIs(t, err, model.ErrSubscriptionRefundClamped)
	assert.EqualValues(t, 50, getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, 100, getTokenRemainQuota(t, tokenID))
	var storedManual model.BillingSettlement
	require.NoError(t, model.DB.First(&storedManual, manual.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, storedManual.Status)
	var child model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskManualCompletionOperationKey(task.ID)).First(&child).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, child.Status)
	assert.Equal(t, 9061, child.ReconciliationReviewedBy)
	assert.Equal(t, "Verified provider evidence for a full refund.", child.ReconciliationReviewNote)
}

func TestCompleteManualTaskBillingSettlementReapprovesManualChildAfterRecovery(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 871, 872, 873, 874
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "manual-completion-reapproval", 100)
	seedChannel(t, channelID)
	seedSubscription(t, subscriptionID, userID, 1000, 50)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceSubscription, subscriptionID)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider confirmed no billable usage")
	actualQuota := int64(0)
	note := "Verified provider evidence for a full refund."

	_, err := CompleteManualTaskBillingSettlement(manual.ID, manual.Revision, 9071, &actualQuota, note)
	require.Error(t, err)

	var failedChild model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskManualCompletionOperationKey(task.ID)).First(&failedChild).Error)
	require.Equal(t, model.BillingSettlementStatusManual, failedChild.Status)
	require.Equal(t, 9071, failedChild.ReconciliationReviewedBy)
	require.Equal(t, note, failedChild.ReconciliationReviewNote)
	failedRevision := failedChild.Revision

	// An operator repairs the inconsistent subscription mirror before
	// re-approving the exact same immutable child operation.
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("id = ?", subscriptionID).
		Update("amount_used", 100).Error)

	_, err = CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9072,
		&actualQuota,
		"A different administrator attempted to replace the original approval.",
	)
	require.ErrorIs(t, err, model.ErrBillingSettlementReviewConflict)

	result, err := CompleteManualTaskBillingSettlement(manual.ID, manual.Revision, 9071, &actualQuota, note)
	require.NoError(t, err)
	assert.False(t, result.AlreadyApplied)
	assert.EqualValues(t, -100, result.AppliedFundingDelta)
	assert.Zero(t, getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, 200, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	assert.EqualValues(t, 1, countLogs(t))

	var recoveredChild model.BillingSettlement
	require.NoError(t, model.DB.First(&recoveredChild, failedChild.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, recoveredChild.Status)
	assert.Greater(t, recoveredChild.Revision, failedRevision)
	assert.Equal(t, 9071, recoveredChild.ReconciliationReviewedBy)
	assert.Equal(t, note, recoveredChild.ReconciliationReviewNote)
	var recoveredParent model.BillingSettlement
	require.NoError(t, model.DB.First(&recoveredParent, manual.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, recoveredParent.Status)
}

func TestCompleteManualTaskBillingSettlementPropagatesFinalTaskReadFailure(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 881, 882, 883
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "manual-completion-final-read", 100)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	persistTask(t, task)
	manual := createManualTaskFinalizeSettlement(t, task, "provider usage needed manual verification")
	actualQuota := int64(40)
	finalReadErr := errors.New("final task read unavailable")
	childSettlementApplied := false
	beforeCallbackName := "test:manual-completion-final-task-read-before"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(beforeCallbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "tasks" && childSettlementApplied {
			_ = tx.AddError(finalReadErr)
			childSettlementApplied = false
		}
	}))
	updateCallbackName := "test:manual-completion-final-task-read-update"
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register(updateCallbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Name != "BillingSettlement" {
			return
		}
		fields, ok := tx.Statement.Dest.(map[string]interface{})
		if status, statusOK := fields["status"].(string); ok && statusOK && status == model.BillingSettlementStatusApplied {
			childSettlementApplied = true
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Query().Remove(beforeCallbackName))
		require.NoError(t, model.DB.Callback().Update().Remove(updateCallbackName))
	})

	_, err := CompleteManualTaskBillingSettlement(
		manual.ID,
		manual.Revision,
		9081,
		&actualQuota,
		"Verified provider evidence and exact usage.",
	)

	require.ErrorIs(t, err, finalReadErr)
	require.False(t, errors.Is(err, model.ErrBillingSettlementTaskConflict))
	assert.EqualValues(t, 960, getUserQuota(t, userID))
	assert.Equal(t, 160, getTokenRemainQuota(t, tokenID))
	var storedTask model.Task
	require.NoError(t, model.DB.First(&storedTask, task.ID).Error)
	assert.Equal(t, 40, storedTask.Quota)
	var child model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", model.BillingTaskManualCompletionOperationKey(task.ID)).First(&child).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, child.Status)
	var parent model.BillingSettlement
	require.NoError(t, model.DB.First(&parent, manual.ID).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, parent.Status)
}

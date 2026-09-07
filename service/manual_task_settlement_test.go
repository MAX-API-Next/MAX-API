package service

import (
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		"Verified that the provider produced no billable output.",
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

	require.Error(t, err)
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

package model

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMidjourneyBillingTask(t *testing.T, suffix string, quota int) *Task {
	t.Helper()
	task := &Task{
		TaskID:    "task_midjourney_" + suffix,
		Platform:  constant.TaskPlatformMidjourney,
		UserId:    501,
		ChannelId: 601,
		Quota:     quota,
		Action:    constant.MjActionImagine,
		Status:    TaskStatusNotStart,
		Progress:  "0%",
		PrivateData: TaskPrivateData{
			AwaitingUpstreamID: true,
			BillingSource:      BillingSettlementSourceWallet,
			BillingRequestId:   "mj-request-" + suffix,
			TokenId:            701,
		},
	}
	insertTask(t, task)
	return task
}

func TestFinalizeMidjourneySubmissionClaimsUpstreamTaskOnce(t *testing.T) {
	truncateTables(t)
	firstTask := newMidjourneyBillingTask(t, "first", 20)
	secondTask := newMidjourneyBillingTask(t, "second", 20)

	first := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-1", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}
	created, refundDuplicate, err := FinalizeMidjourneySubmission(first, firstTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-first:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		&BillingSettlementInput{
			OperationKey: "request:mj-request-first:refund", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701, FundingDelta: -20, TokenDelta: -20,
		},
	)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	duplicate := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-1", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}
	created, refundDuplicate, err = FinalizeMidjourneySubmission(duplicate, secondTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-second:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		&BillingSettlementInput{
			OperationKey: "request:mj-request-second:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701, FundingDelta: -20, TokenDelta: -20,
		},
	)
	require.NoError(t, err)
	require.False(t, created)
	require.True(t, refundDuplicate)

	var count int64
	require.NoError(t, DB.Model(&Midjourney{}).Where("user_id = ? AND mj_id = ?", 501, "provider-mj-1").Count(&count).Error)
	assert.EqualValues(t, 1, count)

	var claim MidjourneyBillingClaim
	require.NoError(t, DB.Where("channel_id = ? AND mj_id = ?", 601, "provider-mj-1").First(&claim).Error)
	assert.Equal(t, firstTask.ID, claim.BillingTaskID)

	var reloadedSecond Task
	require.NoError(t, DB.First(&reloadedSecond, secondTask.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloadedSecond.Status)
	assert.Zero(t, reloadedSecond.Quota)

	var refund BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "request:mj-request-second:finalize").First(&refund).Error)
	assert.Equal(t, BillingSettlementStatusPending, refund.Status)
}

func TestFinalizeMidjourneySubmissionDoesNotRefundSameRequestReplay(t *testing.T) {
	truncateTables(t)
	firstTask := newMidjourneyBillingTask(t, "replay-first", 20)
	firstTask.PrivateData.BillingRequestId = "mj-request-replay"
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", firstTask.ID).Update("private_data", firstTask.PrivateData).Error)
	mj := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-replay", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}
	created, refundDuplicate, err := FinalizeMidjourneySubmission(mj, firstTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-replay:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		}, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	replayTask := newMidjourneyBillingTask(t, "replay-second", 20)
	replayTask.PrivateData.BillingRequestId = "mj-request-replay"
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", replayTask.ID).Update("private_data", replayTask.PrivateData).Error)
	created, refundDuplicate, err = FinalizeMidjourneySubmission(&Midjourney{
		UserId: 501, ChannelId: 601, MjId: mj.MjId, Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 20,
	}, replayTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-replay:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		&BillingSettlementInput{
			OperationKey: "request:mj-request-replay:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701, FundingDelta: -20, TokenDelta: -20,
		},
	)
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, refundDuplicate)

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", "request:mj-request-replay:finalize").First(&settlement).Error)
	assert.Zero(t, settlement.FundingDelta)
}

func TestMidjourneyFailureCommitsRefundIntentWithTerminalState(t *testing.T) {
	truncateTables(t)
	billingTask := newMidjourneyBillingTask(t, "failure", 30)
	mj := &Midjourney{
		UserId: 501, ChannelId: 601, MjId: "provider-mj-failure", Action: constant.MjActionImagine,
		Status: "", Progress: "0%", Quota: 30,
	}
	created, refundDuplicate, err := FinalizeMidjourneySubmission(mj, billingTask,
		&BillingSettlementInput{
			OperationKey: "request:mj-request-failure:finalize", Source: BillingSettlementSourceWallet,
			UserID: 501, TokenID: 701,
		},
		nil,
	)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	loadedTask, err := GetMidjourneyBillingTask(mj.UserId, mj.ChannelId, mj.MjId)
	require.NoError(t, err)
	require.NotNil(t, loadedTask)

	fromStatus := mj.Status
	mj.Status = "FAILURE"
	mj.Progress = "100%"
	mj.FailReason = "provider failed"
	loadedTask.Status = TaskStatusFailure
	loadedTask.Progress = "100%"
	loadedTask.FailReason = mj.FailReason
	operationKey := fmt.Sprintf("task:%d:refund", loadedTask.ID)
	won, err := mj.UpdateWithBillingTaskAndSettlement(fromStatus, loadedTask, &BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 501, TokenID: 701, FundingDelta: -30, TokenDelta: -30,
		TaskID: loadedTask.ID, TaskQuota: 30, TaskQuotaTarget: 0,
	})
	require.NoError(t, err)
	require.True(t, won)

	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", operationKey).First(&settlement).Error)
	assert.Equal(t, BillingSettlementStatusPending, settlement.Status)

	var storedMJ Midjourney
	require.NoError(t, DB.First(&storedMJ, mj.Id).Error)
	assert.Equal(t, "FAILURE", storedMJ.Status)
	var storedTask Task
	require.NoError(t, DB.First(&storedTask, loadedTask.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, storedTask.Status)
}

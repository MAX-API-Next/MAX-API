package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateWithStatusAndManualSettlementKeepsTaskNonTerminal(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:     "manual-h3",
		Status:     TaskStatusInProgress,
		Quota:      800,
		Progress:   "35%",
		StartTime:  11,
		FinishTime: 0,
		FailReason: "",
	}
	task.SetData(map[string]any{"phase": "running"})
	insertTask(t, task)
	expectedUpdatedAt := task.UpdatedAt
	task.PrivateData.BillingContext = &TaskBillingContext{
		OriginModelName: "MiniMax-H3",
		PerCallBilling:  true,
	}
	task.SetData(map[string]any{"phase": "provider-terminal-evidence"})
	task.Progress = "100%"
	task.StartTime = 22
	task.FinishTime = 33
	task.FailReason = "must remain reconciliation-only"
	task.UpdatedAt++
	operationKey := BillingTaskFinalizeOperationKey(task.ID)

	won, err := task.UpdateWithStatusAndManualSettlement(TaskStatusInProgress, expectedUpdatedAt, BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 800,
	}, "H3 terminal usage is missing")

	require.NoError(t, err)
	require.True(t, won)
	var stored Task
	require.NoError(t, DB.First(&stored, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), stored.Status)
	require.Equal(t, 800, stored.Quota)
	require.Equal(t, "35%", stored.Progress)
	require.EqualValues(t, 11, stored.StartTime)
	require.Zero(t, stored.FinishTime)
	require.Empty(t, stored.FailReason)
	var storedData map[string]any
	require.NoError(t, stored.GetData(&storedData))
	require.Equal(t, "provider-terminal-evidence", storedData["phase"])
	require.NotNil(t, stored.PrivateData.BillingContext)
	require.Equal(t, "MiniMax-H3", stored.PrivateData.BillingContext.OriginModelName)
	require.True(t, stored.PrivateData.BillingContext.PerCallBilling)
	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", operationKey).First(&settlement).Error)
	require.Equal(t, BillingSettlementStatusManual, settlement.Status)
	require.Zero(t, settlement.FundingDelta)
	require.Contains(t, settlement.LastError, "usage is missing")
}

func TestUpdateWithStatusAndManualSettlementCASLossLeavesNoIntent(t *testing.T) {
	truncateTables(t)
	task := &Task{TaskID: "manual-h3-cas-loss", Status: TaskStatusQueued, Quota: 800}
	insertTask(t, task)
	operationKey := BillingTaskFinalizeOperationKey(task.ID)
	task.Status = TaskStatusInProgress
	task.UpdatedAt++

	won, err := task.UpdateWithStatusAndManualSettlement(TaskStatusInProgress, task.UpdatedAt-1, BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 800,
	}, "H3 terminal usage is missing")

	require.NoError(t, err)
	require.False(t, won)
	var count int64
	require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", operationKey).Count(&count).Error)
	require.Zero(t, count)
}

func TestUpdateWithStatusAndSettlementIntentKeepsTaskNonTerminal(t *testing.T) {
	truncateTables(t)
	task := &Task{TaskID: "pending-h3", Status: TaskStatusInProgress, Quota: 800}
	insertTask(t, task)
	expectedUpdatedAt := task.UpdatedAt
	task.PrivateData.BillingContext = &TaskBillingContext{}
	task.UpdatedAt++
	operationKey := BillingTaskFinalizeOperationKey(task.ID)

	won, err := task.UpdateWithStatusAndSettlementIntent(TaskStatusInProgress, expectedUpdatedAt, BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 500,
		FundingDelta: -300,
	})

	require.NoError(t, err)
	require.True(t, won)
	var stored Task
	require.NoError(t, DB.First(&stored, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), stored.Status)
	require.Equal(t, 800, stored.Quota)
	require.NotNil(t, stored.PrivateData.BillingContext)
	var settlement BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", operationKey).First(&settlement).Error)
	require.Equal(t, BillingSettlementStatusPending, settlement.Status)
	require.EqualValues(t, -300, settlement.FundingDelta)
}

func TestUpdateWithStatusAndSettlementIntentCASLossLeavesNoIntent(t *testing.T) {
	truncateTables(t)
	task := &Task{TaskID: "pending-h3-cas-loss", Status: TaskStatusQueued, Quota: 800}
	insertTask(t, task)
	operationKey := BillingTaskFinalizeOperationKey(task.ID)
	task.Status = TaskStatusInProgress
	task.UpdatedAt++

	won, err := task.UpdateWithStatusAndSettlementIntent(TaskStatusInProgress, task.UpdatedAt-1, BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 500,
		FundingDelta: -300,
	})

	require.NoError(t, err)
	require.False(t, won)
	var count int64
	require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", operationKey).Count(&count).Error)
	require.Zero(t, count)
}

package model

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateWithStatusAndPendingTerminalEvidenceUsesCAS(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:   "pending-terminal-evidence",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		PrivateData: TaskPrivateData{
			PendingTerminalStatus:     TaskStatusSuccess,
			PendingTerminalProgress:   "100%",
			PendingTerminalFinishTime: time.Now().Unix(),
			PendingTerminalResultURL:  "https://cdn.example.com/pending.mp4",
		},
	}
	insertTask(t, task)
	expectedUpdatedAt := task.UpdatedAt

	won, err := task.UpdateWithStatusAndPendingTerminalEvidence(TaskStatusInProgress, expectedUpdatedAt)
	require.NoError(t, err)
	require.True(t, won)

	var stored Task
	require.NoError(t, DB.First(&stored, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), stored.Status)
	require.Equal(t, expectedUpdatedAt+1, stored.UpdatedAt)
	require.Equal(t, TaskStatus(TaskStatusSuccess), stored.PrivateData.PendingTerminalStatus)
	require.Equal(t, "https://cdn.example.com/pending.mp4", stored.PrivateData.PendingTerminalResultURL)

	stale := stored
	stale.UpdatedAt = expectedUpdatedAt
	won, err = stale.UpdateWithStatusAndPendingTerminalEvidence(TaskStatusInProgress, expectedUpdatedAt)
	require.NoError(t, err)
	require.False(t, won)
	var afterCASLoss Task
	require.NoError(t, DB.First(&afterCASLoss, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), afterCASLoss.Status)
	require.Equal(t, TaskStatus(TaskStatusSuccess), afterCASLoss.PrivateData.PendingTerminalStatus)
}

func TestUpdateWithStatusAndPendingTerminalEvidencePropagatesError(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID: "pending-terminal-evidence-error",
		Status: TaskStatusInProgress,
	}
	insertTask(t, task)
	expectedUpdatedAt := task.UpdatedAt
	task.PrivateData.PendingTerminalStatus = TaskStatusSuccess
	task.PrivateData.PendingTerminalProgress = "100%"
	task.PrivateData.PendingTerminalResultURL = "https://cdn.example.com/should-not-persist.mp4"

	callbackName := "test:pending-terminal-evidence-update-error"
	forcedErr := errors.New("forced pending terminal evidence update error")
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		_ = tx.AddError(forcedErr)
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

	won, err := task.UpdateWithStatusAndPendingTerminalEvidence(TaskStatusInProgress, expectedUpdatedAt)
	require.ErrorIs(t, err, forcedErr)
	require.False(t, won)
	require.Equal(t, expectedUpdatedAt, task.UpdatedAt)
	var stored Task
	require.NoError(t, DB.First(&stored, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), stored.Status)
	require.Empty(t, stored.PrivateData.PendingTerminalStatus)
	require.Empty(t, stored.PrivateData.PendingTerminalResultURL)
}

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
	require.Equal(t, expectedUpdatedAt+1, task.UpdatedAt)
	require.Equal(t, task.UpdatedAt, stored.UpdatedAt)
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
	expectedUpdatedAt := task.UpdatedAt
	task.Status = TaskStatusInProgress

	won, err := task.UpdateWithStatusAndManualSettlement(TaskStatusInProgress, expectedUpdatedAt, BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 800,
	}, "H3 terminal usage is missing")

	require.NoError(t, err)
	require.False(t, won)
	require.Equal(t, expectedUpdatedAt, task.UpdatedAt)
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
	require.Equal(t, expectedUpdatedAt+1, task.UpdatedAt)
	require.Equal(t, task.UpdatedAt, stored.UpdatedAt)
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
	expectedUpdatedAt := task.UpdatedAt
	task.Status = TaskStatusInProgress

	won, err := task.UpdateWithStatusAndSettlementIntent(TaskStatusInProgress, expectedUpdatedAt, BillingSettlementInput{
		OperationKey: operationKey, Source: BillingSettlementSourceWallet,
		UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 500,
		FundingDelta: -300,
	})

	require.NoError(t, err)
	require.False(t, won)
	require.Equal(t, expectedUpdatedAt, task.UpdatedAt)
	var count int64
	require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", operationKey).Count(&count).Error)
	require.Zero(t, count)
}

func TestTaskSettlementIntentErrorsKeepCallerTimestamp(t *testing.T) {
	tests := []struct {
		name string
		call func(*Task, int64, BillingSettlementInput) (bool, error)
	}{
		{
			name: "pending settlement intent",
			call: func(task *Task, expectedUpdatedAt int64, input BillingSettlementInput) (bool, error) {
				return task.UpdateWithStatusAndSettlementIntent(TaskStatusInProgress, expectedUpdatedAt, input)
			},
		},
		{
			name: "manual settlement intent",
			call: func(task *Task, expectedUpdatedAt int64, input BillingSettlementInput) (bool, error) {
				input.TaskQuotaTarget = input.TaskQuota
				input.FundingDelta = 0
				return task.UpdateWithStatusAndManualSettlement(TaskStatusInProgress, expectedUpdatedAt, input, "provider usage is missing")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			task := &Task{TaskID: "settlement-intent-error", Status: TaskStatusInProgress, Quota: 800}
			insertTask(t, task)
			expectedUpdatedAt := task.UpdatedAt
			operationKey := BillingTaskFinalizeOperationKey(task.ID)

			callbackName := "test:settlement-intent-error-" + test.name
			forcedErr := errors.New("forced settlement create error")
			require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
				_ = tx.AddError(forcedErr)
			}))
			t.Cleanup(func() { _ = DB.Callback().Create().Remove(callbackName) })

			won, err := test.call(task, expectedUpdatedAt, BillingSettlementInput{
				OperationKey: operationKey, Source: BillingSettlementSourceWallet,
				UserID: 1, TaskID: task.ID, TaskQuota: 800, TaskQuotaTarget: 500,
				FundingDelta: -300,
			})
			require.ErrorContains(t, err, forcedErr.Error())
			require.False(t, won)
			require.Equal(t, expectedUpdatedAt, task.UpdatedAt)

			var stored Task
			require.NoError(t, DB.First(&stored, task.ID).Error)
			require.Equal(t, expectedUpdatedAt, stored.UpdatedAt)
			var count int64
			require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", operationKey).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

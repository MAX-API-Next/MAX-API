package model

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPlaceholderRewriteMakesStaleManualDecisionLose(t *testing.T) {
	truncateTables(t)
	fixed := time.Now().Unix() + 100000
	row := &Task{TaskID: "same-second-retry", Status: TaskStatusSubmitted, Quota: 20, UpdatedAt: fixed, ChannelId: 1, PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: "original"}}
	insertTask(t, row)
	require.NoError(t, DB.Model(row).UpdateColumn("updated_at", fixed).Error)
	for _, request := range []string{"retry-one", "retry-two"} {
		var observed, retry Task
		require.NoError(t, DB.First(&observed, row.ID).Error)
		retry = observed
		retry.ChannelId++
		retry.PrivateData.BillingRequestId = request
		require.NoError(t, retry.RewritePlaceholder(observed), "exercise the actual placeholder rewrite method used by ensureTaskPlaceholder")
		input := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 20, TaskQuotaTarget: 20}
		won, err := observed.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, observed.UpdatedAt, input, "old plan needs review")
		require.NoError(t, err)
		require.False(t, won, "a rewrite within the same Unix second must invalidate the old manual decision")
		var fresh Task
		require.NoError(t, DB.First(&fresh, row.ID).Error)
		require.Equal(t, observed.UpdatedAt+1, fresh.UpdatedAt)
		require.Equal(t, retry.UpdatedAt, fresh.UpdatedAt)
		require.Equal(t, request, fresh.PrivateData.BillingRequestId)
		require.Equal(t, retry.ChannelId, fresh.ChannelId)
		var count int64
		require.NoError(t, DB.Model(&BillingSettlement{}).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestPlaceholderRewriteLosesAfterNewerManualDecision(t *testing.T) {
	truncateTables(t)
	row := &Task{TaskID: "manual-before-retry", Status: TaskStatusSubmitted, Quota: 20, PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: "original"}}
	insertTask(t, row)
	var retry Task
	require.NoError(t, DB.First(&retry, row.ID).Error)
	observed := retry
	input := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 20, TaskQuotaTarget: 20}
	won, err := row.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, row.UpdatedAt, input, "requires reconciliation")
	require.NoError(t, err)
	require.True(t, won)
	retry.ChannelId = 99
	retry.PrivateData.BillingRequestId = "stale-retry"
	require.ErrorIs(t, retry.RewritePlaceholder(observed), ErrTaskFinalizeOwned)
	require.Equal(t, observed.UpdatedAt, retry.UpdatedAt)
	var fresh Task
	require.NoError(t, DB.First(&fresh, row.ID).Error)
	require.Equal(t, "original", fresh.PrivateData.BillingRequestId)
	require.Equal(t, row.UpdatedAt, fresh.UpdatedAt)
	require.Equal(t, row.ChannelId, fresh.ChannelId)
	reloaded := fresh
	fresh.PrivateData.BillingRequestId = "reloaded-retry"
	require.ErrorIs(t, fresh.RewritePlaceholder(reloaded), ErrTaskFinalizeOwned, "reloading does not authorize replacing an owned finalize decision")
	var afterReloadedAttempt Task
	require.NoError(t, DB.First(&afterReloadedAttempt, row.ID).Error)
	require.Equal(t, "original", afterReloadedAttempt.PrivateData.BillingRequestId)
	require.Equal(t, row.ChannelId, afterReloadedAttempt.ChannelId)
	require.Equal(t, row.UpdatedAt, afterReloadedAttempt.UpdatedAt)
	var record BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", input.OperationKey).First(&record).Error)
	require.Equal(t, BillingSettlementStatusManual, record.Status)
	require.Zero(t, record.AppliedFundingDelta)
}

func TestPlaceholderRewriteRejectsSameVersionLifecycleChanges(t *testing.T) {
	for _, change := range []string{"none", "quota", "quota-nullness", "upstream", "awaiting"} {
		t.Run(change, func(t *testing.T) {
			truncateTables(t)
			row := &Task{TaskID: "same-version-" + change, Status: TaskStatusSubmitted, UpdatedAt: 100, ChannelId: 1,
				PrivateData: TaskPrivateData{AwaitingUpstreamID: true}}
			insertTask(t, row)
			observed, err := GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			wantPrivate := row.PrivateData
			var wantQuota any = 0
			switch change {
			case "quota":
				wantQuota = 20
			case "quota-nullness":
				wantQuota = nil
			case "upstream":
				wantPrivate.UpstreamTaskID = "confirmed"
			case "awaiting":
				wantPrivate.AwaitingUpstreamID = false
			}
			require.NoError(t, DB.Model(row).UpdateColumns(map[string]any{"quota": wantQuota, "private_data": wantPrivate}).Error)
			committed, err := GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			candidate := *observed
			candidate.Quota, candidate.ChannelId = 10, 2
			err = candidate.RewritePlaceholder(*observed)
			fresh, readErr := GetTaskPlaceholderByID(row.ID)
			require.NoError(t, readErr)
			if change == "none" {
				require.NoError(t, err)
				require.Equal(t, candidate.Quota, fresh.Quota)
				require.Equal(t, candidate.ChannelId, fresh.ChannelId)
				require.Equal(t, candidate.UpdatedAt, fresh.UpdatedAt)
				return
			}
			require.ErrorIs(t, err, ErrTaskPlaceholderConflict)
			require.True(t, fresh.HasSameQuotaSnapshot(committed))
			require.Equal(t, wantPrivate, fresh.PrivateData)
			require.Equal(t, observed.ChannelId, fresh.ChannelId)
			require.Equal(t, observed.UpdatedAt, fresh.UpdatedAt)
		})
	}
}

func TestSubmitFailurePreservesOpenTaskFinalizeDecision(t *testing.T) {
	truncateTables(t)
	row := &Task{TaskID: "manual-before-failure-cleanup", Status: TaskStatusSubmitted, Quota: 20, PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: "original"}}
	insertTask(t, row)
	input := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 20, TaskQuotaTarget: 20}
	won, err := row.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, row.UpdatedAt, input, "requires review")
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, MarkTaskSubmitFailed(row.ID, "retry rewrite was refused"))
	var fresh Task
	require.NoError(t, DB.First(&fresh, row.ID).Error)
	require.EqualValues(t, TaskStatusSubmitted, fresh.Status)
	require.Equal(t, 20, fresh.Quota)
	var record BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", input.OperationKey).First(&record).Error)
	require.Equal(t, BillingSettlementStatusManual, record.Status)
	require.Zero(t, record.AppliedFundingDelta)
}

func TestSubmitFailureAndManualDecisionHaveOneOwner(t *testing.T) {
	const child = "MAX_API_TEST_COMPETING_TASK_CLAIM_CHILD"
	if os.Getenv(child) != "1" {
		exe, err := os.Executable()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSubmitFailureAndManualDecisionHaveOneOwner$", "-test.timeout=15s")
		cmd.Env = append(os.Environ(), child+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	truncateTables(t)
	row := &Task{TaskID: "failure-manual-race", Status: TaskStatusSubmitted, Quota: 20, PrivateData: TaskPrivateData{AwaitingUpstreamID: true, BillingRequestId: "original"}}
	insertTask(t, row)
	input := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 20, TaskQuotaTarget: 20}
	type outcome struct {
		won bool
		err error
	}
	start, failed, manual := make(chan struct{}), make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		<-start
		won, err := ClaimTaskSubmitFailure(row.ID, "definite rejection")
		failed <- outcome{won, err}
	}()
	go func() {
		<-start
		observed := *row
		won, err := observed.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, row.UpdatedAt, input, "manual decision")
		manual <- outcome{won, err}
	}()
	close(start)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var failureResult, manualResult outcome
	select {
	case failureResult = <-failed:
	case <-deadline.C:
		t.Fatal("failure claim did not finish")
	}
	select {
	case manualResult = <-manual:
	case <-deadline.C:
		t.Fatal("manual decision did not finish")
	}
	require.NoError(t, failureResult.err)
	require.NoError(t, manualResult.err)
	require.NotEqual(t, failureResult.won, manualResult.won)
	var fresh Task
	require.NoError(t, DB.First(&fresh, row.ID).Error)
	var count int64
	require.NoError(t, DB.Model(&BillingSettlement{}).Where("operation_key = ?", input.OperationKey).Count(&count).Error)
	if manualResult.won {
		require.EqualValues(t, TaskStatusSubmitted, fresh.Status)
		require.EqualValues(t, 1, count)
		require.Equal(t, 20, fresh.Quota)
	} else {
		require.EqualValues(t, TaskStatusFailure, fresh.Status)
		require.Zero(t, count)
		require.Zero(t, fresh.Quota)
	}
}

func TestPlaceholderRewriteRollsBackWithoutPublishedVersion(t *testing.T) {
	for _, mode := range []string{"overflow", "readback-failure"} {
		t.Run(mode, func(t *testing.T) {
			truncateTables(t)
			row := &Task{TaskID: mode, Status: TaskStatusSubmitted, Quota: 20, UpdatedAt: 100, PrivateData: TaskPrivateData{BillingRequestId: "original"}}
			insertTask(t, row)
			version := int64(1<<63 - 1)
			if mode == "readback-failure" {
				version = time.Now().Unix() + 1000
			}
			require.NoError(t, DB.Model(&Task{}).Where("id = ?", row.ID).UpdateColumn("updated_at", version).Error)
			require.NoError(t, DB.First(row, row.ID).Error)
			observed := *row
			row.PrivateData.BillingRequestId = "retry"
			reached := false
			if mode == "readback-failure" {
				require.NoError(t, DB.Callback().Query().Before("gorm:query").Register("fail-rewrite-version-read", func(tx *gorm.DB) {
					if tx.Statement.Table == "tasks" && len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == "updated_at" {
						reached = true
						tx.AddError(errors.New("synthetic rewrite version read failure"))
					}
				}))
				t.Cleanup(func() { _ = DB.Callback().Query().Remove("fail-rewrite-version-read") })
			}
			require.Error(t, row.RewritePlaceholder(observed))
			if mode == "readback-failure" {
				require.True(t, reached)
			}
			var fresh Task
			require.NoError(t, DB.First(&fresh, row.ID).Error)
			require.Equal(t, "original", fresh.PrivateData.BillingRequestId)
			require.Equal(t, version, fresh.UpdatedAt)
			require.Equal(t, observed.UpdatedAt, row.UpdatedAt)
		})
	}
}

func TestManualSettlementRejectsChangedQuotaSnapshot(t *testing.T) {
	truncateTables(t)
	row := &Task{TaskID: "manual-quota-snapshot", Status: TaskStatusSubmitted, Quota: 20}
	insertTask(t, row)
	version := row.UpdatedAt
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", row.ID).UpdateColumn("quota", 25).Error)
	input := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 20, TaskQuotaTarget: 20}
	won, err := row.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, version, input, "provider usage requires review")
	require.NoError(t, err)
	require.False(t, won)
	var count int64
	require.NoError(t, DB.Model(&BillingSettlement{}).Count(&count).Error)
	require.Zero(t, count)
	require.Equal(t, version, row.UpdatedAt)
	var fresh Task
	require.NoError(t, DB.First(&fresh, row.ID).Error)
	require.Equal(t, 25, fresh.Quota)
	require.Equal(t, version, fresh.UpdatedAt, "losing attempt must not advance the stored version")
	input.TaskQuota, input.TaskQuotaTarget = 25, 25
	won, err = fresh.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, fresh.UpdatedAt, input, "provider usage requires review")
	require.NoError(t, err)
	require.True(t, won)
	var record BillingSettlement
	require.NoError(t, DB.First(&record).Error)
	require.EqualValues(t, 25, record.TaskQuota)
	require.Equal(t, BillingSettlementStatusManual, record.Status)
	require.Zero(t, record.AppliedFundingDelta)
	require.Zero(t, record.AppliedTokenDelta)
}

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

func TestTaskPrivateDataPersistsTaskUsageEnvelope(t *testing.T) {
	truncateTables(t)
	zero := int64(0)
	task := &Task{
		TaskID: "usage-envelope-round-trip",
		Status: TaskStatusInProgress,
		PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{
			TaskUsage: &types.TaskUsage{
				OutputDurationMs: &zero,
				Source:           types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessPartial,
			},
			TaskUsageEnvelope: &types.TaskUsageEnvelope{
				ProducerKind: types.TaskUsageProducerKindGoAdapter,
				SourceID:     "minimax", SchemaVersion: 1,
				ContractDigest: "contract-digest", Stage: types.TaskUsageSourceProviderResponse,
				Presence: types.TaskUsagePresencePartial, Completeness: types.TaskUsageCompletenessPartial,
				EvidenceDigest: "evidence-digest",
				Usage: &types.TaskUsage{
					OutputDurationMs: &zero,
					Source:           types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessPartial,
				},
			},
		}},
	}
	insertTask(t, task)

	var stored Task
	require.NoError(t, DB.First(&stored, task.ID).Error)
	require.NotNil(t, stored.PrivateData.BillingContext)
	envelope := stored.PrivateData.BillingContext.TaskUsageEnvelope
	require.NotNil(t, envelope)
	require.Equal(t, types.TaskUsageProducerKindGoAdapter, envelope.ProducerKind)
	require.Equal(t, "minimax", envelope.SourceID)
	require.Equal(t, 1, envelope.SchemaVersion)
	require.Equal(t, "contract-digest", envelope.ContractDigest)
	require.Equal(t, types.TaskUsageSourceProviderResponse, envelope.Stage)
	require.Equal(t, types.TaskUsagePresencePartial, envelope.Presence)
	require.Equal(t, types.TaskUsageCompletenessPartial, envelope.Completeness)
	require.Equal(t, "evidence-digest", envelope.EvidenceDigest)
	require.NotNil(t, envelope.Usage)
	require.NotNil(t, envelope.Usage.OutputDurationMs)
	require.Zero(t, *envelope.Usage.OutputDurationMs)
	require.Equal(t, types.TaskUsageSourceProviderResponse, envelope.Usage.Source)
	require.Equal(t, types.TaskUsageCompletenessPartial, envelope.Usage.Completeness)
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

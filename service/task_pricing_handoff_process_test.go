// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const taskPricingHandoffProcessEnv = "MAXAPI_TASK_PRICING_HANDOFF_PROCESS"

type taskPricingHandoffProcessConfig struct {
	Database, Ready, Release, Phase string
}

func startTaskPricingHandoffProcess(t *testing.T, dir, phase, release string) *billingTestProcess {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	file, err := os.CreateTemp(dir, "handoff-process-*.json")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	cfg := taskPricingHandoffProcessConfig{Database: filepath.Join(dir, "ledger.sqlite"), Ready: file.Name() + ".ready", Phase: phase, Release: release}
	writeBillingProcessJSON(t, file.Name(), cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestTaskPricingHandoffProcessHelper$", "-test.timeout=40s", "-test.v")
	cmd.Env = append(os.Environ(), taskPricingHandoffProcessEnv+"="+file.Name())
	p := &billingTestProcess{cmd: cmd, done: make(chan error, 1), exited: make(chan struct{}), output: &billingProcessOutput{}, ready: cfg.Ready}
	cmd.Stdout, cmd.Stderr = p.output, p.output
	require.NoError(t, cmd.Start())
	go func() { p.done <- cmd.Wait(); close(p.exited) }()
	t.Cleanup(func() { cancel(); <-p.exited })
	return p
}

// Only this disposable child opens its configured SQLite file. No production
// failpoints, route, provider call or graceful shutdown simulate the crashes.
func TestTaskPricingHandoffProcessHelper(t *testing.T) {
	config := os.Getenv(taskPricingHandoffProcessEnv)
	if config == "" {
		t.Skip("subprocess helper; requires its isolated parent fixture")
	}
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	var cfg taskPricingHandoffProcessConfig
	require.NoError(t, common.Unmarshal(data, &cfg))
	require.Equal(t, filepath.Join(filepath.Dir(config), "ledger.sqlite"), cfg.Database)
	require.Equal(t, config+".ready", cfg.Ready)
	marker, err := os.ReadFile(filepath.Join(filepath.Dir(config), "handoff-isolated"))
	require.NoError(t, err)
	require.Equal(t, "MAX isolated handoff fixture", string(marker))
	db := openBillingProcessDB(t, cfg.Database)
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled = true, false, false
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	actor := taskplugin.Actor{UserID: 901, TokenID: 903, ChannelID: 902, RequestID: "bounded-request"}
	pause := func(inTransaction bool) {
		pauseBillingProcess(t, billingProcessConfig{Ready: cfg.Ready}, billingProcessBarrier{Phase: cfg.Phase, InsideTransaction: inTransaction})
	}
	if cfg.Phase == "unbound_before_task" || cfg.Phase == "unbound_recover" {
		var user model.User
		require.NoError(t, db.First(&user, actor.UserID).Error)
		require.Equal(t, "MAX isolated handoff fixture", string(marker))
		if cfg.Phase == "unbound_recover" {
			model.ProcessPendingBillingSettlementsOnce()
			model.ProcessPendingBillingSettlementsOnce()
			return
		}
		fixture := &boundedPricingFixture{actor: actor}
		c, info := fixture.billingInfo(&types.TaskBillingPlan{GroupRatio: 1})
		require.Nil(t, PreConsumeBilling(c, 1500, info))
		// Deliberately stop the original request-only funding path before
		// creating any task. This is a counterexample, not the new writer.
		pause(false)
	}
	var task model.Task
	require.NoError(t, db.First(&task, 904).Error)
	require.Equal(t, "bounded-request", task.PrivateData.BillingRequestId)
	require.Equal(t, 901, task.UserId)
	quota := int64(task.PrivateData.BillingContext.TaskBillingPlan.ReserveQuota)
	input := model.BillingSettlementInput{
		OperationKey: "request:bounded-request:pre-consume", Source: model.BillingSettlementSourceWallet,
		UserID: 901, TokenID: 903, FundingDelta: quota, TokenDelta: quota, TaskID: task.ID, TaskQuotaTarget: quota,
		PreConsumeRequestID: actor.RequestID, PreConsumeModelName: "alias", PreConsumeRequestedQuota: quota, PreConsumeEffectiveQuota: quota,
	}
	if cfg.Phase == "prepared" {
		pause(false)
	}
	if cfg.Phase == "recover" {
		require.Equal(t, filepath.Join(filepath.Dir(config), "release"), cfg.Release)
		writeBillingProcessJSON(t, cfg.Ready, billingProcessBarrier{PID: os.Getpid(), Phase: "recover-ready"})
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(cfg.Release); err == nil {
				break
			} else {
				require.True(t, os.IsNotExist(err))
			}
			select {
			case <-deadline.C:
				t.Fatal("parent did not release recovery")
			case <-ticker.C:
			}
		}
		model.ProcessPendingBillingSettlementsOnce()
		model.ProcessPendingBillingSettlementsOnce()
		return
	}
	// This is the existing production transaction primitive. Staging pending
	// is already a funding decision: the restart runner is allowed to apply it.
	require.NoError(t, task.UpdateWithSettlementIntent(&input))
	if cfg.Phase == "intent_committed" || cfg.Phase == "intent_backoff" {
		pause(false)
	}
	if cfg.Phase == "funding_before_commit" {
		var once sync.Once
		require.NoError(t, db.Callback().Update().After("gorm:update").Register("handoff-crash-before-commit", func(tx *gorm.DB) {
			updates, ok := tx.Statement.Dest.(map[string]interface{})
			if ok && tx.Error == nil && tx.Statement.Table == "billing_settlements" && updates["status"] == model.BillingSettlementStatusApplied {
				once.Do(func() { pause(billingProcessHasSQLTransaction(tx.Statement.ConnPool)) })
			}
		}))
	}
	_, _, err = model.ApplyBillingSettlementOnce(input)
	require.NoError(t, err)
	require.Equal(t, "funding_committed", cfg.Phase)
	pause(false)
}

func TestTaskPricingHandoffProcessUnboundFundingCannotInventMissingTask(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handoff-isolated"), []byte("MAX isolated handoff fixture"), 0600))
	f := newBoundedPricingFixtureAt(t, filepath.Join(dir, "ledger.sqlite"))
	p := startTaskPricingHandoffProcess(t, dir, "unbound_before_task", "")
	awaitBillingProcessBarrier(t, p)
	require.NoError(t, p.cmd.Process.Kill())
	require.Error(t, <-p.done, p.output.text())
	var receipt model.BillingSettlement
	require.NoError(t, f.db.Where("operation_key = ?", "request:bounded-request:pre-consume").First(&receipt).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, receipt.Status)
	require.Zero(t, receipt.TaskID)
	require.EqualValues(t, 1500, receipt.AppliedFundingDelta)
	recovery := startTaskPricingHandoffProcess(t, dir, "unbound_recover", "")
	require.NoError(t, <-recovery.done, recovery.output.text())
	var tasks int64
	require.NoError(t, f.db.Model(&model.Task{}).Count(&tasks).Error)
	require.Zero(t, tasks, "a funding receipt cannot reconstruct the missing frozen plan/task")
	require.EqualValues(t, 1500, getUserQuota(t, 901))
	require.Equal(t, 1500, getTokenRemainQuota(t, 903))
	observed, err := ReadTaskPricingWalletHandoff(t.Context(), 904, f.actor)
	require.Error(t, err)
	require.Nil(t, observed)
}

func TestTaskPricingHandoffProcessCrashAndConcurrentRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "intent_committed", "intent_backoff", "funding_before_commit", "funding_committed"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "handoff-isolated"), []byte("MAX isolated handoff fixture"), 0600))
			f := newBoundedPricingFixtureAt(t, filepath.Join(dir, "ledger.sqlite"))
			task, input := seedTaskPricingHandoff(t, f)
			p := startTaskPricingHandoffProcess(t, dir, phase, "")
			event := awaitBillingProcessBarrier(t, p)
			require.Equal(t, phase == "funding_before_commit", event.InsideTransaction)
			require.NoError(t, p.cmd.Process.Kill())
			require.Error(t, <-p.done, p.output.text())
			observed, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
			require.NoError(t, err)
			expected := TaskPricingHandoffPending
			balance := int64(3000)
			if phase == "prepared" {
				expected = TaskPricingHandoffPrepared
			}
			if phase == "funding_committed" {
				expected, balance = TaskPricingHandoffApplied, 1500
			}
			require.Equal(t, expected, observed.State)
			require.Equal(t, balance, getUserQuota(t, 901))
			require.EqualValues(t, balance, getTokenRemainQuota(t, 903))
			if phase == "prepared" {
				require.Nil(t, observed.AppliedReservation)
				return
			}
			// Changing live prices after the crash cannot rewrite the old plan.
			require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "billing_setting.plugin_billing_expr").Update("value", `{"hailuo::alias":"99"}`).Error)
			release := filepath.Join(dir, "release")
			a := startTaskPricingHandoffProcess(t, dir, "recover", release)
			b := startTaskPricingHandoffProcess(t, dir, "recover", release)
			awaitBillingProcessBarrier(t, a)
			awaitBillingProcessBarrier(t, b)
			if phase == "intent_backoff" {
				// Synthetic scheduling fixture: both real workers must leave a
				// not-yet-due durable intent untouched after the release barrier.
				require.NoError(t, f.db.Model(&model.BillingSettlement{}).
					Where("operation_key = ?", input.OperationKey).
					Updates(map[string]interface{}{
						"attempts": 1, "last_error": "SQLITE_BUSY: isolated pending schedule",
						"next_attempt": time.Now().Add(15 * time.Second).Unix(),
					}).Error)
			}
			require.NoError(t, os.WriteFile(release, nil, 0600))
			require.NoError(t, <-a.done, a.output.text())
			require.NoError(t, <-b.done, b.output.text())
			var recovery model.BillingSettlement
			require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&recovery).Error)
			t.Logf("post-contention funding=%s attempts=%d next_attempt=%d cause=%s", recovery.Status, recovery.Attempts, recovery.NextAttempt, recovery.LastError)
			if phase == "intent_backoff" {
				require.Equal(t, model.BillingSettlementStatusPending, recovery.Status, "the synthetic deadline must exercise deferred recovery")
			}
			if recovery.Status == model.BillingSettlementStatusPending {
				require.Positive(t, recovery.Attempts)
				require.True(t, strings.Contains(recovery.LastError, "SQLITE_BUSY") || strings.Contains(recovery.LastError, "database is locked"),
					"only transient SQLite writer contention may defer recovery")
				pending, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
				require.NoError(t, err)
				require.Equal(t, TaskPricingHandoffPending, pending.State)
				require.Nil(t, pending.AppliedReservation)
				require.Zero(t, pending.PlanReceiptBound)
				require.Zero(t, pending.AdmissionReady)
				require.EqualValues(t, 3000, getUserQuota(t, 901))
				require.Equal(t, 3000, getTokenRemainQuota(t, 903))
				require.Zero(t, countLogs(t))
				// Honor the persisted retry deadline after all contenders exit.
				// One new process gets one scheduled run; another failure fails
				// this test instead of looping until the balances look correct.
				require.Positive(t, recovery.NextAttempt)
				wait := time.Until(time.Unix(recovery.NextAttempt, 0))
				require.LessOrEqual(t, wait, 64*time.Second)
				if wait > 0 {
					timer := time.NewTimer(wait)
					<-timer.C
				}
				retry := startTaskPricingHandoffProcess(t, dir, "recover", release)
				require.NoError(t, <-retry.done, retry.output.text())
			}
			observed, err = ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
			require.NoError(t, err)
			require.Equal(t, TaskPricingHandoffApplied, observed.State)
			require.Equal(t, 1500, observed.Reservation)
			require.EqualValues(t, 1500, *observed.AppliedReservation)
			require.False(t, observed.PlanReceiptBound)
			require.False(t, observed.AdmissionReady)
			applied, duplicate, err := model.ApplyBillingSettlementOnce(input)
			require.NoError(t, err)
			require.True(t, duplicate)
			require.EqualValues(t, 1500, applied)
			require.EqualValues(t, 1500, getUserQuota(t, 901))
			require.Equal(t, 1500, getTokenRemainQuota(t, 903))
			require.Zero(t, countLogs(t))
			settleTaskPricingHandoffFixture(t, f, task.ID)
		})
	}
}

func settleTaskPricingHandoffFixture(t *testing.T, f *boundedPricingFixture, id int64) {
	t.Helper()
	var task model.Task
	require.NoError(t, f.db.First(&task, id).Error)
	task.PrivateData.AwaitingUpstreamID = false
	task.PrivateData.UpstreamTaskID = "isolated-handoff-provider-task"
	require.NoError(t, task.Update())
	p := task.PrivateData.BillingContext.TaskBillingPlan
	result := &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), PricingUsageEnvelope: &types.TaskPricingUsageEnvelope{
		ProducerKind: "task_plugin", SourceID: "hailuo", PluginVersion: p.ExpressionPlan.PluginVersion, PackageDigest: p.ExpressionPlan.PackageDigest,
		ChannelID: 902, RequestID: f.actor.RequestID, Action: "generate", SchemaVersion: 1, ContractDigest: p.UsageContractDigest,
		Stage: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete,
		Facts: types.TaskPricingFacts{"seconds": 8, "input_images": 0, "input_video_seconds": 0},
	}}
	adaptor := &expressionPollingFixture{result: result}
	channel := &model.Channel{Id: 902, Type: constant.ChannelTypeMiniMax, Key: "synthetic"}
	for range 2 {
		require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): &task}))
		require.NoError(t, f.db.First(&task, id).Error)
	}
	// Independent arithmetic: 8 seconds * USD0.1 * 1000 = 800; refund700.
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, 800, task.Quota)
	require.EqualValues(t, 2200, getUserQuota(t, 901))
	require.Equal(t, 2200, getTokenRemainQuota(t, 903))
	require.EqualValues(t, 1, countLogs(t))
	var final model.BillingSettlement
	require.NoError(t, f.db.Where("operation_key = ?", model.BillingTaskFinalizeOperationKey(id)).First(&final).Error)
	require.EqualValues(t, -700, final.AppliedFundingDelta)
	require.Equal(t, model.BillingSettlementEffectApplied, final.EffectStatus)
}

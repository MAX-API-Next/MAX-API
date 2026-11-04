// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"errors"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedTaskPricingHandoff(t *testing.T, f *boundedPricingFixture) (*model.Task, model.BillingSettlementInput) {
	t.Helper()
	prepared, err := f.freeze(t, f.observe(t))
	require.NoError(t, err)
	task := makeServiceH3Task(t, 0, prepared.Plan)
	task.Status = model.TaskStatusNotStart
	task.Group = "default"
	task.Action = "generate"
	task.PrivateData.UpstreamTaskID = ""
	task.PrivateData.AwaitingUpstreamID = true
	task.PrivateData.BillingRequestId = f.actor.RequestID
	task.Properties.OriginModelName = "alias"
	task.Properties.UpstreamModelName = "MiniMax-H3"
	task.PrivateData.BillingContext.OriginModelName = "alias"
	task.PrivateData.BillingContext.GroupRatio = prepared.Plan.GroupRatio
	require.NoError(t, AttachTaskPluginBinding(f.catalog.Host(), task, prepared.Binding))
	persistTask(t, task)
	quota := int64(prepared.Plan.ReserveQuota)
	return task, model.BillingSettlementInput{
		OperationKey: "request:" + f.actor.RequestID + ":pre-consume", Source: model.BillingSettlementSourceWallet,
		UserID: f.actor.UserID, TokenID: f.actor.TokenID, FundingDelta: quota, TokenDelta: quota,
		TaskID: task.ID, TaskQuota: 0, TaskQuotaTarget: quota,
		PreConsumeRequestID: f.actor.RequestID, PreConsumeModelName: "alias",
		PreConsumeRequestedQuota: quota, PreConsumeEffectiveQuota: quota,
	}
}

func TestTaskPricingHandoffObservesDurableStatesWithoutWrites(t *testing.T) {
	for _, state := range []string{"prepared", "pending", "manual", "applied", "applied-zero"} {
		t.Run(state, func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			if state == "applied-zero" {
				require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "GroupRatio").Update("value", `{"default":0}`).Error)
			}
			task, input := seedTaskPricingHandoff(t, f)
			if state != "prepared" {
				require.NoError(t, task.UpdateWithSettlementIntent(&input))
			}
			if state == "manual" {
				require.NoError(t, f.db.Model(&model.BillingSettlement{}).Where("operation_key = ?", input.OperationKey).Update("status", model.BillingSettlementStatusManual).Error)
			}
			if state == "applied" || state == "applied-zero" {
				_, _, err := model.ApplyBillingSettlementOnce(input)
				require.NoError(t, err)
			}
			// Live prices and node units must not reprice this persisted sample.
			require.NoError(t, f.db.Model(&model.Option{}).Where("key = ?", "billing_setting.plugin_billing_expr").Update("value", `{"hailuo::alias":"99"}`).Error)
			writes := 0
			require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("handoff-no-create", func(*gorm.DB) { writes++ }))
			require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("handoff-no-update", func(*gorm.DB) { writes++ }))
			require.NoError(t, f.db.Callback().Delete().Before("gorm:delete").Register("handoff-no-delete", func(*gorm.DB) { writes++ }))
			observed, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
			require.NoError(t, err)
			require.Zero(t, writes)
			require.False(t, observed.AdmissionReady)
			require.False(t, observed.PlanReceiptBound, "existing funding rows do not anchor a plan digest")
			require.NotEmpty(t, observed.PlanDigest)
			require.Equal(t, "u(\"seconds\") * 0.1", observed.Plan.ExpressionPlan.Expression)
			if state == "applied-zero" {
				require.Equal(t, TaskPricingHandoffApplied, observed.State)
				require.NotNil(t, observed.AppliedReservation)
				require.Zero(t, *observed.AppliedReservation)
			} else {
				require.Equal(t, TaskPricingHandoffState(state), observed.State)
				if state == "applied" {
					require.NotNil(t, observed.AppliedReservation)
					require.EqualValues(t, 1500, *observed.AppliedReservation)
				} else {
					require.Nil(t, observed.AppliedReservation)
				}
			}
			require.Equal(t, state == "pending", observed.AutomaticRetryEligible)
			observed.Plan.ExpressionPlan.SubmitFacts["seconds"] = 15
			again, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
			require.NoError(t, err)
			require.EqualValues(t, 6, again.Plan.ExpressionPlan.SubmitFacts["seconds"])
		})
	}
}

func TestTaskPricingHandoffRejectsUnboundAndMismatchedEvidence(t *testing.T) {
	for _, change := range []string{"unbound", "user", "token", "request", "channel", "source", "amount", "quota", "binding", "legacy", "legacy-compatible", "submitted", "query", "receipt-query"} {
		t.Run(change, func(t *testing.T) {
			f := newBoundedPricingFixture(t)
			task, input := seedTaskPricingHandoff(t, f)
			switch change {
			case "unbound":
				// This is the existing ordinary PreConsumeBilling call, not a
				// synthetic claim that its durable receipt binds a TaskID.
				c, info := f.billingInfo(task.PrivateData.BillingContext.TaskBillingPlan)
				require.Nil(t, PreConsumeBilling(c, 1500, info))
			case "amount":
				input.FundingDelta, input.TokenDelta, input.TaskQuotaTarget = 1499, 1499, 1499
				require.NoError(t, task.UpdateWithSettlementIntent(&input))
			case "quota":
				require.NoError(t, f.db.Model(&model.Task{}).Where("id = ?", task.ID).Update("quota", 1500).Error)
			case "source":
				task.PrivateData.BillingSource = model.BillingSettlementSourceSubscription
				require.NoError(t, task.Update())
			case "binding":
				task.PrivateData.PluginBinding.PackageDigest = "changed"
				require.NoError(t, task.Update())
			case "legacy":
				task.PrivateData.BillingContext.TaskBillingPlan = &types.TaskBillingPlan{}
				require.NoError(t, task.Update())
			case "legacy-compatible":
				task.PrivateData.BillingContext.TaskBillingPlan.Profile = ""
				task.PrivateData.BillingContext.TaskBillingPlan.ExpressionPlan = nil
				task.PrivateData.PluginBinding.Profile = ""
				task.PrivateData.PluginBinding.Action = ""
				task.PrivateData.PluginBinding.PhaseApprovalID = ""
				require.NoError(t, task.Update())
			case "submitted":
				require.NoError(t, f.db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusSubmitted).Error)
			case "query":
				require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("handoff-query-fault", func(tx *gorm.DB) { tx.AddError(errors.New("isolated read failure")) }))
			case "receipt-query":
				require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("handoff-receipt-fault", func(tx *gorm.DB) {
					if tx.Statement.Table == "billing_settlements" {
						tx.AddError(errors.New("isolated late receipt read failure"))
					}
				}))
			}
			actor := f.actor
			switch change {
			case "user":
				actor.UserID++
			case "token":
				actor.TokenID++
			case "request":
				actor.RequestID += "-different"
			case "channel":
				actor.ChannelID++
			}
			observed, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, actor)
			require.Error(t, err)
			require.Nil(t, observed, "failed reads must not return a stale/partially validated observation")
		})
	}
}

func TestTaskPricingHandoffSameReservationDoesNotProveOriginalPlan(t *testing.T) {
	f := newBoundedPricingFixture(t)
	task, input := seedTaskPricingHandoff(t, f)
	_, _, err := model.ApplyBillingSettlementOnce(input)
	require.NoError(t, err)
	before, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
	require.NoError(t, err)
	// A second self-consistent task snapshot has a different submit estimate
	// but the same15-second cap. Current funding rows cannot distinguish it.
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	plan.ExpressionPlan.SubmitFacts["seconds"] = 7
	plan.EstimateQuota = 700 // 7 *0.1 USD *1000, independent of the validator.
	task.Quota = 1500
	require.NoError(t, task.Update())
	after, err := ReadTaskPricingWalletHandoff(t.Context(), task.ID, f.actor)
	require.NoError(t, err)
	require.Equal(t, 1500, before.Reservation)
	require.Equal(t, 1500, after.Reservation)
	require.NotEqual(t, before.PlanDigest, after.PlanDigest)
	require.False(t, after.PlanReceiptBound)
	require.False(t, after.AdmissionReady)
	require.EqualValues(t, 1500, getUserQuota(t, 901))
}

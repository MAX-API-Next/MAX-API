package service

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
)

func TestBillingPlaygroundProcessCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	funding := billingProcessFundingCases()[0]
	phases := []string{"intent_committed", "funding_before_commit", "funding_committed", "receipt_before_log",
		"log_before_commit", "log_committed", "usage_before_commit", "complete"}
	for _, quota := range []int{130, 100, 6, 0} {
		for _, phase := range phases {
			t.Run(fmt.Sprintf("quota_%d/%s", quota, phase), func(t *testing.T) {
				dir, db := seedBillingProcessDB(t, funding)
				cfg := billingProcessConfig{Role: "work", Phase: phase, Preference: funding.preference, Source: funding.source, Quota: quota, Playground: true}
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
				assertBillingProcessLedger(t, db, funding, quota, phase, false, true)
				original := billingProcessRecord(t, db)
				cfg.Role, cfg.Phase = "recover", ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessLedger(t, db, funding, quota, "complete", false, true)
				require.Equal(t, original.EffectPayload, billingProcessRecord(t, db).EffectPayload)
				snapshot := billingProcessSnapshot(t, db)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessSnapshot(t, db), "fresh PG recovery cannot repeat funding or effects")
			})
		}
	}
}

func TestBillingPlaygroundProcessRefundRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	funding := billingProcessFundingCases()[0]
	for _, phase := range []string{"intent_committed", "funding_before_commit", "complete"} {
		t.Run(phase, func(t *testing.T) {
			dir, db := seedBillingProcessDB(t, funding)
			cfg := billingProcessConfig{Role: "work", Phase: phase, Preference: funding.preference, Source: funding.source, Refund: true, Playground: true}
			killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
			assertBillingProcessLedger(t, db, funding, 0, phase, true, true)
			cfg.Role, cfg.Phase = "recover", ""
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			assertBillingProcessLedger(t, db, funding, 0, "complete", true, true)
			snapshot := billingProcessSnapshot(t, db)
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.Equal(t, snapshot, billingProcessSnapshot(t, db))
		})
	}
}

func TestBillingPlaygroundProcessAdmissionRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	funding := billingProcessFundingCases()[0]
	for _, phase := range []string{"intent_committed", "funding_before_commit", "complete"} {
		t.Run(phase, func(t *testing.T) {
			dir, db := seedBillingProcessDB(t, funding)
			cfg := billingProcessConfig{Role: "work", Phase: phase, Preference: funding.preference, Source: funding.source, Playground: true, AdmissionOnly: true}
			killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
			var initial model.BillingSettlement
			require.NoError(t, db.Where("operation_key = ?", "request:process-recovery-request:pre-consume").First(&initial).Error)
			require.EqualValues(t, 100, initial.FundingDelta)
			require.Zero(t, initial.TokenDelta)
			var user model.User
			var token model.Token
			require.NoError(t, db.First(&user, 951).Error)
			require.NoError(t, db.First(&token, 952).Error)
			want := int64(1000)
			if phase == "complete" {
				want = 900
			}
			require.Equal(t, want, user.Quota)
			require.EqualValues(t, 1000, token.RemainQuota)
			cfg.Role, cfg.Phase = "recover", ""
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.NoError(t, db.First(&user, 951).Error)
			require.EqualValues(t, 900, user.Quota)
			// The original admitted reservation is reconstructable after a lost
			// process; a refund uses the durable result, never a guessed amount.
			cfg.Role = "refund_admission"
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.NoError(t, db.First(&user, 951).Error)
			require.EqualValues(t, 1000, user.Quota)
			require.NoError(t, db.First(&token, 952).Error)
			require.EqualValues(t, 1000, token.RemainQuota)
			var refund model.BillingSettlement
			require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey("process-recovery-request")).First(&refund).Error)
			require.EqualValues(t, -100, refund.AppliedFundingDelta)
			require.Zero(t, refund.AppliedTokenDelta)
			snapshot := billingProcessSnapshot(t, db)
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.Equal(t, snapshot, billingProcessSnapshot(t, db))
		})
	}
}

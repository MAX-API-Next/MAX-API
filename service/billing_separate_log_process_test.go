package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
)

type billingSeparateLogCase struct {
	name       string
	funding    billingProcessFunding
	playground bool
}

func billingSeparateLogCases() []billingSeparateLogCase {
	var cases []billingSeparateLogCase
	for _, funding := range billingProcessFundingCases() {
		cases = append(cases, billingSeparateLogCase{name: funding.name, funding: funding})
	}
	return append(cases, billingSeparateLogCase{name: "playground_wallet", funding: billingProcessFundingCases()[0], playground: true})
}

func (c billingSeparateLogCase) config(quota int, phase string) billingProcessConfig {
	return billingProcessConfig{Role: "work", Phase: phase, Preference: c.funding.preference, Source: c.funding.source,
		Quota: quota, Playground: c.playground, SeparateLogDB: true}
}

func TestBillingSeparateLogProcessCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	// The first three barriers belong to the independent log transaction; the
	// last two belong to the business transaction that applies usage counters.
	phases := []string{"funding_committed", "receipt_before_log", "log_before_commit", "log_committed", "usage_before_commit"}
	for _, c := range billingSeparateLogCases() {
		for _, quota := range []int{130, 100, 6, 0} {
			for _, phase := range phases {
				t.Run(fmt.Sprintf("%s/quota_%d/%s", c.name, quota, phase), func(t *testing.T) {
					dir, db, logDB := seedBillingProcessDatabases(t, c.funding, true)
					cfg := c.config(quota, phase)
					event := killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
					wantDatabase := "logs"
					if phase == "log_committed" || phase == "usage_before_commit" {
						wantDatabase = "ledger"
					}
					require.Equal(t, wantDatabase, event.Database, "interrupt the transaction in the intended database")
					assertBillingProcessDatabases(t, db, logDB, c.funding, quota, phase, false, c.playground)
					original := billingProcessRecord(t, db)
					cfg.Role, cfg.Phase = "recover", ""
					finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
					assertBillingProcessDatabases(t, db, logDB, c.funding, quota, "complete", false, c.playground)
					require.Equal(t, original.EffectPayload, billingProcessRecord(t, db).EffectPayload)
					snapshot := billingProcessDatabasesSnapshot(t, db, logDB)
					finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
					require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB), "neither database may change on duplicate recovery")
				})
			}
		}
	}
}

func TestBillingSeparateLogProcessRepeatedCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, c := range billingSeparateLogCases()[:2] {
		for _, quota := range []int{130, 0} {
			t.Run(fmt.Sprintf("%s/quota_%d", c.name, quota), func(t *testing.T) {
				dir, db, logDB := seedBillingProcessDatabases(t, c.funding, true)
				cfg := c.config(quota, "funding_committed")
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
				original := billingProcessRecord(t, db)
				cfg.Role = "recover"
				for _, phase := range []string{"log_committed", "usage_before_commit"} {
					cfg.Phase = phase
					killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
					assertBillingProcessDatabases(t, db, logDB, c.funding, quota, phase, false)
				}
				cfg.Phase = ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessDatabases(t, db, logDB, c.funding, quota, "complete", false)
				require.Equal(t, original.EffectPayload, billingProcessRecord(t, db).EffectPayload)
				snapshot := billingProcessDatabasesSnapshot(t, db, logDB)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB))
			})
		}
	}
}

func TestBillingSeparateLogProcessUnavailableRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, c := range billingSeparateLogCases() {
		for _, quota := range []int{130, 0} {
			t.Run(fmt.Sprintf("%s/quota_%d", c.name, quota), func(t *testing.T) {
				dir, db, logDB := seedBillingProcessDatabases(t, c.funding, true)
				cfg := c.config(quota, "funding_committed")
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
				original := billingProcessRecord(t, db)
				snapshot := billingProcessDatabasesSnapshot(t, db, logDB)
				cfg.Role, cfg.Phase, cfg.LogUnavailable = "recover", "", true
				// Close only the child's independent sql.DB. The original funding
				// is already applied, and failed effects must stay durably pending.
				for attempt := 0; attempt < 2; attempt++ {
					finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
					assertBillingProcessDatabases(t, db, logDB, c.funding, quota, "funding_committed", false, c.playground)
					require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB), "a log outage cannot change either ledger or drop the intent")
				}
				cfg.LogUnavailable = false
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessDatabases(t, db, logDB, c.funding, quota, "complete", false, c.playground)
				require.Equal(t, original.EffectPayload, billingProcessRecord(t, db).EffectPayload)
				snapshot = billingProcessDatabasesSnapshot(t, db, logDB)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB))
			})
		}
	}
}

func TestBillingSeparateLogProcessConcurrentRecoveryAndRefund(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	cases := billingSeparateLogCases()
	for _, c := range []billingSeparateLogCase{cases[0], cases[1], cases[4]} {
		for _, quota := range []int{130, 0} {
			t.Run(fmt.Sprintf("%s/quota_%d", c.name, quota), func(t *testing.T) {
				dir, db, logDB := seedBillingProcessDatabases(t, c.funding, true)
				cfg := c.config(quota, "funding_committed")
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
				original := billingProcessRecord(t, db)
				cfg.Phase, cfg.Release = "", filepath.Join(dir, "release")
				var workers []*billingTestProcess
				for _, role := range []string{"recover", "recover", "refund_competitor"} {
					cfg.Role = role
					p := startBillingTestProcess(t, dir, cfg)
					workers = append(workers, p)
					awaitBillingProcessBarrier(t, p)
				}
				require.NoError(t, os.WriteFile(cfg.Release, []byte("release"), 0600))
				for _, p := range workers {
					finishBillingTestProcess(t, p)
				}
				cfg.Role, cfg.Release = "recover", ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessDatabases(t, db, logDB, c.funding, quota, "complete", false, c.playground)
				require.Equal(t, original.EffectPayload, billingProcessRecord(t, db).EffectPayload, "the competing refund must not replace finalized usage")
				snapshot := billingProcessDatabasesSnapshot(t, db, logDB)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB))
			})
		}
	}
}

func TestBillingSeparateLogProcessRefundRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, c := range billingSeparateLogCases() {
		for _, phase := range []string{"intent_committed", "funding_before_commit", "complete"} {
			t.Run(c.name+"/"+phase, func(t *testing.T) {
				dir, db, logDB := seedBillingProcessDatabases(t, c.funding, true)
				cfg := c.config(0, phase)
				cfg.Refund = true
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
				assertBillingProcessDatabases(t, db, logDB, c.funding, 0, phase, true, c.playground)
				cfg.Role, cfg.Phase = "recover", ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessDatabases(t, db, logDB, c.funding, 0, "complete", true, c.playground)
				snapshot := billingProcessDatabasesSnapshot(t, db, logDB)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB))
			})
		}
	}
}

func TestBillingSeparateLogProcessSubscriptionPeriodRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	c := billingSeparateLogCases()[1]
	for _, quota := range []int{130, 0} {
		t.Run(fmt.Sprintf("quota_%d", quota), func(t *testing.T) {
			dir, db, logDB := seedBillingProcessDatabases(t, c.funding, true)
			cfg := c.config(quota, "funding_committed")
			killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
			original := billingProcessRecord(t, db)
			cfg.Role, cfg.Phase = "reset", ""
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			cfg.Role = "recover"
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			var sub model.UserSubscription
			var user model.User
			var token model.Token
			var channel model.Channel
			for _, target := range []struct {
				row any
				id  int
			}{{&sub, 955}, {&user, 951}, {&token, 952}, {&channel, 953}} {
				require.NoError(t, db.First(target.row, target.id).Error)
			}
			require.Zero(t, sub.AmountUsed, "effect recovery must not charge the new subscription period")
			require.EqualValues(t, 1000, user.Quota)
			require.EqualValues(t, quota, token.UsedQuota)
			require.EqualValues(t, 1000-quota, token.RemainQuota)
			require.EqualValues(t, quota, user.UsedQuota)
			require.EqualValues(t, 1, user.RequestCount)
			require.EqualValues(t, quota, channel.UsedQuota)
			record := billingProcessRecord(t, db)
			require.Equal(t, original.EffectPayload, record.EffectPayload)
			require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
			require.Equal(t, model.BillingSettlementEffectApplied, record.EffectStatus)
			require.EqualValues(t, quota-100, record.AppliedFundingDelta)
			require.EqualValues(t, quota-100, record.AppliedTokenDelta)
			var logs []model.Log
			var receipts []model.BillingLogReceipt
			require.NoError(t, logDB.Find(&logs).Error)
			require.NoError(t, logDB.Find(&receipts).Error)
			require.Len(t, logs, 1)
			require.Len(t, receipts, 1)
			require.EqualValues(t, quota, logs[0].Quota)
			require.Equal(t, record.OperationKey, receipts[0].OperationKey)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			require.EqualValues(t, quota, other["subscription_used"])
			require.EqualValues(t, 1000-quota, other["subscription_remain"])
			snapshot := billingProcessDatabasesSnapshot(t, db, logDB)
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.Equal(t, snapshot, billingProcessDatabasesSnapshot(t, db, logDB))
		})
	}
}

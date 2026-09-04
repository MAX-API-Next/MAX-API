package common

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func replaceToolPricesForTest(t *testing.T, prices map[string]float64) {
	t.Helper()

	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	original := make(map[string]float64, len(setting.Prices))
	for name, price := range setting.Prices {
		original[name] = price
	}

	setting.Prices = make(map[string]float64, len(prices))
	for name, price := range prices {
		setting.Prices[name] = price
	}
	operation_setting.RebuildToolPriceIndex()

	t.Cleanup(func() {
		setting.Prices = original
		operation_setting.RebuildToolPriceIndex()
	})
}

func TestToolUsageLedgerRecordsOnlyPricedCustomCalls(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup": 5,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)

	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "call-1",
		Position: "choice:0:tool:0",
	}))
	require.False(t, ledger.ObserveCustom("unpriced", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "call-2",
		Position: "choice:0:tool:1",
	}))
	require.True(t, ledger.CommitAttempt(0))

	snapshot := ledger.Snapshot()
	require.Equal(t, "gpt-test", snapshot.ModelName)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, ToolUsageItem{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}, snapshot.Items[0])
}

func TestToolUsageLedgerSnapshotExcludesBuiltInRecords(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup":     5,
		"web_search": 10,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)
	require.True(t, ledger.ObserveBuiltIn("web_search", ToolCallIdentity{
		Scope:    "openai-responses",
		CallID:   "web-1",
		Position: "output:0",
	}))
	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-responses",
		CallID:   "custom-1",
		Position: "output:1",
	}))
	require.True(t, ledger.CommitAttempt(0))

	require.Equal(t, []ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, ledger.Snapshot().Items)
}

func TestToolUsageLedgerDeduplicatesAliasesButCountsDistinctCalls(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup": 5,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)

	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-chat",
		Position: "choice:0:tool:0",
	}))
	// Some providers emit the stable call ID in a later chunk. The position
	// alias must join that later observation to the already-counted call.
	require.False(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "call-1",
		Position: "choice:0:tool:0",
	}))
	require.False(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:  "openai-chat",
		CallID: "call-1",
	}))
	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "call-2",
		Position: "choice:0:tool:1",
	}))
	require.True(t, ledger.CommitAttempt(0))

	snapshot := ledger.Snapshot()
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, 2, snapshot.Items[0].CallCount)
}

func TestToolUsageLedgerRejectsObservationsAfterCommit(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup": 5,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)
	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:  "openai-chat",
		CallID: "call-1",
	}))
	require.True(t, ledger.CommitAttempt(0))
	require.False(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:  "openai-chat",
		CallID: "call-2",
	}))

	require.Equal(t, []ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, ledger.Snapshot().Items)
}

func TestToolUsageLedgerAliasConflictFailsClosed(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup": 5,
		"search": 7,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)
	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "gemini",
		CallID:   "call-1",
		Position: "candidate:0:part:0",
	}))

	// The same upstream aliases resolving to different names are ambiguous.
	// Billing must discard the conflicted record rather than retain a charge
	// that may refer to a conversion artifact or a replayed stream position.
	require.False(t, ledger.ObserveCustom("search", ToolCallIdentity{
		Scope:    "gemini",
		CallID:   "call-1",
		Position: "candidate:0:part:0",
	}))
	require.True(t, ledger.CommitAttempt(0))
	require.Empty(t, ledger.Snapshot().Items)
}

func TestToolUsageLedgerRequiresStableIdentity(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup": 5,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)

	require.False(t, ledger.ObserveCustom("lookup", ToolCallIdentity{Scope: "openai-chat"}))
	require.True(t, ledger.CommitAttempt(0))
	require.Empty(t, ledger.Snapshot().Items)
}

func TestToolUsageLedgerRejectsReservedNamesForCustomCalls(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"web_search":         10,
		"web_search_preview": 10,
		"file_search":        2.5,
		"google_search":      14,
		"image_generation":   40,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)

	for i, name := range []string{
		"web_search",
		"web_search_preview",
		"file_search",
		"google_search",
		"image_generation",
	} {
		require.False(t, ledger.ObserveCustom(name, ToolCallIdentity{
			Scope:    "openai-chat",
			Position: "choice:0:tool:" + string(rune('0'+i)),
		}))
	}
	require.True(t, ledger.CommitAttempt(0))
	require.Empty(t, ledger.Snapshot().Items)
}

func TestToolUsageLedgerRetryKeepsOnlyWinningAttempt(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"first_attempt":  5,
		"second_attempt": 7,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)
	require.True(t, ledger.ObserveCustom("first_attempt", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "failed-call",
		Position: "choice:0:tool:0",
	}))

	ledger.BeginAttempt(1)
	require.True(t, ledger.ObserveCustom("second_attempt", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "winning-call",
		Position: "choice:0:tool:0",
	}))
	// Re-entering the same attempt must not discard observations made by an
	// earlier stage of the same response pipeline.
	ledger.BeginAttempt(1)
	require.True(t, ledger.CommitAttempt(1))

	snapshot := ledger.Snapshot()
	require.Equal(t, 1, snapshot.Attempt)
	require.Equal(t, []ToolUsageItem{{
		Name:       "second_attempt",
		CallCount:  1,
		PricePer1K: 7,
	}}, snapshot.Items)
}

func TestToolUsageLedgerFreezesPricesAtRequestStart(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{
		"lookup": 5,
	})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)

	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	setting.Prices = map[string]float64{"lookup": 9}
	operation_setting.RebuildToolPriceIndex()

	require.True(t, ledger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "call-1",
		Position: "choice:0:tool:0",
	}))
	require.True(t, ledger.CommitAttempt(0))

	snapshot := ledger.Snapshot()
	require.NotZero(t, snapshot.PriceVersion)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, float64(5), snapshot.Items[0].PricePer1K)

	newLedger := NewToolUsageLedger("gpt-test")
	newLedger.BeginAttempt(0)
	require.True(t, newLedger.ObserveCustom("lookup", ToolCallIdentity{
		Scope:    "openai-chat",
		CallID:   "call-2",
		Position: "choice:0:tool:0",
	}))
	require.True(t, newLedger.CommitAttempt(0))
	require.Equal(t, float64(9), newLedger.Snapshot().Items[0].PricePer1K)
}

func TestToolUsageLedgerConcurrentDuplicateObservationsCountOnce(t *testing.T) {
	replaceToolPricesForTest(t, map[string]float64{"lookup": 5})

	ledger := NewToolUsageLedger("gpt-test")
	ledger.BeginAttempt(0)
	var inserted atomic.Int32
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if ledger.ObserveCustom("lookup", ToolCallIdentity{
				Scope:    "openai-chat",
				CallID:   "call-1",
				Position: "choice:0:tool:0",
			}) {
				inserted.Add(1)
			}
		}()
	}
	workers.Wait()

	require.EqualValues(t, 1, inserted.Load())
	require.True(t, ledger.CommitAttempt(0))
	require.Equal(t, []ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, ledger.Snapshot().Items)
}

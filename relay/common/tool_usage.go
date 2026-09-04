package common

import (
	"sort"
	"strings"
	"sync"

	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
)

type ToolUsageKind string

const (
	ToolUsageKindCustom  ToolUsageKind = "custom"
	ToolUsageKindBuiltIn ToolUsageKind = "built_in"
)

var reservedBillableToolNames = map[string]struct{}{
	"web_search":         {},
	"web_search_preview": {},
	"file_search":        {},
	"google_search":      {},
	"image_generation":   {},
}

// ToolCallIdentity carries independent aliases for one actual upstream call.
// Scope separates protocols; CallID is preferred when available; Position is
// the stable choice/output/content-block position used by streaming protocols.
type ToolCallIdentity struct {
	Scope    string
	CallID   string
	Position string
}

type ToolUsageItem struct {
	Name       string  `json:"name"`
	CallCount  int     `json:"call_count"`
	PricePer1K float64 `json:"price_per_1k"`
}

type ToolUsageSnapshot struct {
	Attempt      int             `json:"attempt"`
	ModelName    string          `json:"model_name"`
	PriceVersion uint64          `json:"price_version"`
	Items        []ToolUsageItem `json:"items,omitempty"`
}

type toolUsageRecord struct {
	name    string
	kind    ToolUsageKind
	aliases map[string]struct{}
}

// ToolUsageLedger is a request-local, attempt-aware ledger. It records only
// actual, stably identifiable upstream calls and prices them using the frozen
// request-start snapshot. Starting a new retry attempt discards observations
// from the failed attempt without touching the durable billing lifecycle.
type ToolUsageLedger struct {
	mu sync.Mutex

	modelName string
	prices    operation_setting.ToolPriceSnapshot

	attempt        int
	attemptStarted bool
	committed      bool
	nextRecordID   uint64
	records        map[uint64]*toolUsageRecord
	aliases        map[string]uint64
	ambiguous      map[string]struct{}
}

func NewToolUsageLedger(modelName string) *ToolUsageLedger {
	return &ToolUsageLedger{
		modelName: modelName,
		prices:    operation_setting.CaptureToolPriceSnapshot(modelName),
	}
}

// PriceFor returns the request-start price for a tool. The underlying price
// index is immutable, so config reloads cannot change an in-flight request's
// billing decision.
func (l *ToolUsageLedger) PriceFor(toolName string) float64 {
	if l == nil {
		return 0
	}
	return l.prices.PriceFor(toolName)
}

// BeginAttempt selects the active upstream attempt. Re-entering the same
// attempt is a no-op so multiple response stages can share one ledger.
func (l *ToolUsageLedger) BeginAttempt(attempt int) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.beginAttemptLocked(attempt)
}

func (l *ToolUsageLedger) beginAttemptLocked(attempt int) bool {
	if l.attemptStarted && l.attempt == attempt {
		return false
	}
	l.attempt = attempt
	l.attemptStarted = true
	l.committed = false
	l.nextRecordID = 0
	l.records = make(map[uint64]*toolUsageRecord)
	l.aliases = make(map[string]uint64)
	l.ambiguous = make(map[string]struct{})
	return true
}

// CommitAttempt makes the active attempt visible to settlement. Observations
// from an uncommitted or failed attempt never appear in Snapshot.
func (l *ToolUsageLedger) CommitAttempt(attempt int) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.attemptStarted || l.attempt != attempt {
		return false
	}
	l.committed = true
	return true
}

func (l *ToolUsageLedger) ObserveCustom(name string, identity ToolCallIdentity) bool {
	return l.observe(ToolUsageKindCustom, name, identity)
}

func (l *ToolUsageLedger) ObserveBuiltIn(name string, identity ToolCallIdentity) bool {
	return l.observe(ToolUsageKindBuiltIn, name, identity)
}

func (l *ToolUsageLedger) observe(kind ToolUsageKind, name string, identity ToolCallIdentity) bool {
	if l == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if kind == ToolUsageKindCustom {
		if _, reserved := reservedBillableToolNames[name]; reserved {
			return false
		}
	}
	if l.prices.PriceFor(name) <= 0 {
		return false
	}

	identity.Scope = strings.TrimSpace(identity.Scope)
	identity.CallID = strings.TrimSpace(identity.CallID)
	identity.Position = strings.TrimSpace(identity.Position)
	if identity.Scope == "" || (identity.CallID == "" && identity.Position == "") {
		return false
	}

	aliases := make([]string, 0, 2)
	if identity.CallID != "" {
		aliases = append(aliases, identity.Scope+"\x00id\x00"+identity.CallID)
	}
	if identity.Position != "" {
		aliases = append(aliases, identity.Scope+"\x00position\x00"+identity.Position)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.attemptStarted {
		l.beginAttemptLocked(0)
	}
	if l.committed {
		return false
	}

	matched := make(map[uint64]struct{})
	hasAmbiguousAlias := false
	for _, alias := range aliases {
		if _, ambiguous := l.ambiguous[alias]; ambiguous {
			hasAmbiguousAlias = true
		}
		if recordID, ok := l.aliases[alias]; ok {
			matched[recordID] = struct{}{}
		}
	}
	if hasAmbiguousAlias {
		l.invalidateAmbiguousLocked(matched, aliases)
		return false
	}

	if len(matched) == 0 {
		l.nextRecordID++
		recordID := l.nextRecordID
		record := &toolUsageRecord{
			name:    name,
			kind:    kind,
			aliases: make(map[string]struct{}, len(aliases)),
		}
		for _, alias := range aliases {
			record.aliases[alias] = struct{}{}
			l.aliases[alias] = recordID
		}
		l.records[recordID] = record
		return true
	}

	var primaryID uint64
	for recordID := range matched {
		record := l.records[recordID]
		if record == nil || record.name != name || record.kind != kind {
			l.invalidateAmbiguousLocked(matched, aliases)
			return false
		}
		if primaryID == 0 || recordID < primaryID {
			primaryID = recordID
		}
	}
	primary := l.records[primaryID]
	for recordID := range matched {
		if recordID == primaryID {
			continue
		}
		duplicate := l.records[recordID]
		for alias := range duplicate.aliases {
			primary.aliases[alias] = struct{}{}
			l.aliases[alias] = primaryID
		}
		delete(l.records, recordID)
	}
	for _, alias := range aliases {
		primary.aliases[alias] = struct{}{}
		l.aliases[alias] = primaryID
	}
	return false
}

// invalidateAmbiguousLocked removes every record connected to a conflicting
// identity and tombstones all known aliases. A later chunk therefore cannot
// recreate a charge from only one side of an identity conflict.
func (l *ToolUsageLedger) invalidateAmbiguousLocked(recordIDs map[uint64]struct{}, aliases []string) {
	if l.ambiguous == nil {
		l.ambiguous = make(map[string]struct{})
	}
	for recordID := range recordIDs {
		record := l.records[recordID]
		if record == nil {
			continue
		}
		for alias := range record.aliases {
			delete(l.aliases, alias)
			l.ambiguous[alias] = struct{}{}
		}
		delete(l.records, recordID)
	}
	for _, alias := range aliases {
		delete(l.aliases, alias)
		l.ambiguous[alias] = struct{}{}
	}
}

func (l *ToolUsageLedger) Snapshot() ToolUsageSnapshot {
	if l == nil {
		return ToolUsageSnapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	counts := make(map[string]int)
	if l.committed {
		for _, record := range l.records {
			if record.kind != ToolUsageKindCustom {
				continue
			}
			counts[record.name]++
		}
	}
	items := make([]ToolUsageItem, 0, len(counts))
	for name, count := range counts {
		items = append(items, ToolUsageItem{
			Name:       name,
			CallCount:  count,
			PricePer1K: l.prices.PriceFor(name),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].PricePer1K < items[j].PricePer1K
		}
		return items[i].Name < items[j].Name
	})

	return ToolUsageSnapshot{
		Attempt:      l.attempt,
		ModelName:    l.modelName,
		PriceVersion: l.prices.Version(),
		Items:        items,
	}
}

func (l *ToolUsageLedger) currentBuiltInCallCounts() map[string]int {
	counts := make(map[string]int)
	if l == nil {
		return counts
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.records {
		if record != nil && record.kind == ToolUsageKindBuiltIn {
			counts[record.name]++
		}
	}
	return counts
}

func (info *RelayInfo) beginToolUsageAttempt() bool {
	if info == nil {
		return false
	}
	if info.ToolUsage == nil {
		info.ToolUsage = NewToolUsageLedger(info.OriginModelName)
	}
	return info.ToolUsage.BeginAttempt(info.RetryIndex)
}

func (info *RelayInfo) ObserveCustomToolCall(name string, identity ToolCallIdentity) bool {
	if info == nil {
		return false
	}
	if info.ToolUsage == nil {
		info.ToolUsage = NewToolUsageLedger(info.OriginModelName)
		info.ToolUsage.BeginAttempt(info.RetryIndex)
	}
	return info.ToolUsage.ObserveCustom(name, identity)
}

func (info *RelayInfo) ObserveBuiltInToolCall(name string, identity ToolCallIdentity) bool {
	if info == nil {
		return false
	}
	if info.ToolUsage == nil {
		info.ToolUsage = NewToolUsageLedger(info.OriginModelName)
		info.ToolUsage.BeginAttempt(info.RetryIndex)
	}
	accepted := info.ToolUsage.ObserveBuiltIn(name, identity)
	if info.ResponsesUsageInfo != nil {
		counts := info.ToolUsage.currentBuiltInCallCounts()
		for _, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if tool != nil {
				tool.CallCount = 0
			}
		}
		for toolName, count := range counts {
			if tool := info.ResponsesUsageInfo.BuiltInTools[toolName]; tool != nil {
				tool.CallCount = count
			}
		}
	}
	return accepted
}

// FrozenToolPrice returns the request-scoped price used by both pre-consume
// and settlement paths. Normal relay requests initialize ToolUsage in
// genBaseRelayInfo before any upstream work. The lazy fallback keeps
// hand-built RelayInfo values used by legacy callers/tests compatible while
// still freezing their first observed price for the remainder of the request.
func (info *RelayInfo) FrozenToolPrice(toolName string) float64 {
	if info == nil {
		return 0
	}
	if info.ToolUsage == nil {
		info.ToolUsage = NewToolUsageLedger(info.OriginModelName)
	}
	return info.ToolUsage.PriceFor(toolName)
}

func (info *RelayInfo) CommitToolUsageAttempt() bool {
	if info == nil || info.ToolUsage == nil {
		return false
	}
	return info.ToolUsage.CommitAttempt(info.RetryIndex)
}

func (info *RelayInfo) ToolUsageSnapshot() ToolUsageSnapshot {
	if info == nil || info.ToolUsage == nil {
		return ToolUsageSnapshot{}
	}
	return info.ToolUsage.Snapshot()
}

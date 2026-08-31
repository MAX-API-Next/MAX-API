package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/billing_reconciliation_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingSettlementSourceWallet       = "wallet"
	BillingSettlementSourceSubscription = "subscription"
	BillingSettlementStatusPending      = "pending"
	BillingSettlementStatusApplied      = "applied"
	BillingSettlementStatusManual       = "manual"
	BillingSettlementEffectPending      = "pending"
	BillingSettlementEffectApplying     = "applying"
	BillingSettlementEffectApplied      = "applied"
)

const billingRequestOperationPrefix = "request:"
const billingRequestFinalizeSuffix = ":finalize"

const billingSettlementBacklogSampleInterval = 30 * time.Second

var (
	ErrBillingSettlementManualReview       = errors.New("billing settlement requires manual review")
	ErrBillingSettlementTaskConflict       = errors.New("billing settlement task quota conflict")
	ErrBillingSettlementOperationConflict  = errors.New("billing settlement operation conflict")
	ErrBillingSettlementRecordNotDurable   = errors.New("billing settlement record was not durably created")
	ErrBillingSettlementReviewConflict     = errors.New("billing settlement is no longer reviewable")
	ErrSubscriptionSettlementUnbound       = errors.New("subscription settlement is not bound to its pre-consume request")
	ErrSubscriptionSettlementPeriodChanged = errors.New("subscription settlement crossed a quota reset period")
)

type permanentBillingSettlementError struct {
	err error
}

func (e *permanentBillingSettlementError) Error() string { return e.err.Error() }
func (e *permanentBillingSettlementError) Unwrap() error { return e.err }

func permanentBillingSettlement(err error) error {
	if err == nil {
		return nil
	}
	var permanentErr *permanentBillingSettlementError
	if errors.As(err, &permanentErr) {
		return err
	}
	return &permanentBillingSettlementError{err: err}
}

func isPermanentBillingSettlementError(err error) bool {
	var permanentErr *permanentBillingSettlementError
	return errors.As(err, &permanentErr)
}

// BillingSettlement is the durable idempotency record for a balance mutation.
// The record and every related DB mutation are committed in one transaction.
type BillingSettlement struct {
	ID                              int64  `gorm:"primaryKey"`
	OperationKey                    string `gorm:"type:varchar(191);uniqueIndex;not null"`
	Source                          string `gorm:"type:varchar(32);not null"`
	UserID                          int    `gorm:"not null;default:0;index:idx_billing_settlement_admission,priority:1"`
	SubscriptionID                  int    `gorm:"not null;default:0"`
	TokenID                         int    `gorm:"not null;default:0"`
	FundingDelta                    int64  `gorm:"not null;default:0;index:idx_billing_settlement_admission,priority:3;index:idx_billing_settlement_status_funding,priority:2;index:idx_billing_settlement_reconciliation,priority:2"`
	AppliedFundingDelta             int64  `gorm:"not null;default:0"`
	TokenDelta                      int64  `gorm:"not null;default:0"`
	AppliedTokenDelta               int64  `gorm:"not null;default:0"`
	TaskID                          int64  `gorm:"not null;default:0"`
	TaskQuota                       int64  `gorm:"not null;default:0"`
	TaskQuotaTarget                 int64  `gorm:"not null;default:0"`
	Status                          string `gorm:"type:varchar(16);index;index:idx_billing_settlement_admission,priority:2;index:idx_billing_settlement_status_funding,priority:1;index:idx_billing_settlement_reconciliation,priority:1;not null;default:'applied'"`
	Attempts                        int    `gorm:"not null;default:0"`
	LastError                       string `gorm:"type:text"`
	NextAttempt                     int64  `gorm:"index;not null;default:0"`
	Revision                        int64  `gorm:"not null;default:1"`
	SubscriptionPreConsumeRequestID string `gorm:"type:varchar(64);index"`
	FinalizeSubscriptionPreConsume  bool   `gorm:"not null;default:false"`
	AllowMissingToken               bool   `gorm:"not null;default:false"`
	ManualOnFailure                 bool   `gorm:"not null;default:false"`
	PreConsumeRequestID             string `gorm:"type:varchar(64);index"`
	PreConsumeModelName             string `gorm:"type:varchar(191);not null;default:''"`
	PreConsumeRequestedQuota        int64  `gorm:"not null;default:0"`
	PreConsumeEffectiveQuota        int64  `gorm:"not null;default:0"`
	EffectPayload                   string `gorm:"type:text"`
	EffectStatus                    string `gorm:"type:varchar(16);index"`
	ReconciliationReviewedAt        int64  `gorm:"not null;default:0;index:idx_billing_settlement_reconciliation,priority:3"`
	ReconciliationReviewedBy        int    `gorm:"not null;default:0"`
	ReconciliationReviewNote        string `gorm:"type:text"`
	UserBlockingOverride            *bool  `gorm:"index:idx_billing_settlement_reconciliation,priority:4"`
	CreatedAt                       int64  `gorm:"not null"`
	UpdatedAt                       int64  `gorm:"index;not null"`
}

// BillingPreConsumeSelection is the immutable funding-source decision for one
// request. Wallet and subscription mutations claim it inside their own balance
// transaction so concurrent attempts cannot commit against different sources.
type BillingPreConsumeSelection struct {
	ID             int64  `gorm:"primaryKey"`
	RequestID      string `gorm:"type:varchar(64);uniqueIndex;not null"`
	Source         string `gorm:"type:varchar(32);not null"`
	UserID         int    `gorm:"not null"`
	TokenID        int    `gorm:"not null;default:0"`
	ModelName      string `gorm:"type:varchar(191);not null;default:''"`
	RequestedQuota int64  `gorm:"not null;default:0"`
	EffectiveQuota int64  `gorm:"not null;default:0"`
	CreatedAt      int64  `gorm:"not null"`
}

type BillingSettlementEffect struct {
	LogType           int                                  `json:"log_type"`
	Content           string                               `json:"content"`
	ChannelID         int                                  `json:"channel_id"`
	ModelName         string                               `json:"model_name"`
	TokenID           int                                  `json:"token_id"`
	TokenName         string                               `json:"token_name,omitempty"`
	Group             string                               `json:"group"`
	Other             map[string]interface{}               `json:"other"`
	NodeName          string                               `json:"node_name"`
	UpdateUsage       bool                                 `json:"update_usage"`
	Quota             int64                                `json:"quota,omitempty"`
	QuotaIsActual     bool                                 `json:"quota_is_actual,omitempty"`
	PromptTokens      int                                  `json:"prompt_tokens,omitempty"`
	CompletionTokens  int                                  `json:"completion_tokens,omitempty"`
	UseTimeSeconds    int                                  `json:"use_time_seconds,omitempty"`
	IsStream          bool                                 `json:"is_stream,omitempty"`
	RequestID         string                               `json:"request_id,omitempty"`
	UpstreamRequestID string                               `json:"upstream_request_id,omitempty"`
	Subscription      *BillingSettlementSubscriptionEffect `json:"subscription,omitempty"`
}

type BillingSettlementSubscriptionEffect struct {
	PreConsumed               int64 `json:"pre_consumed"`
	AmountTotal               int64 `json:"amount_total"`
	AmountUsedAfterPreConsume int64 `json:"amount_used_after_pre_consume"`
}

type BillingSettlementInput struct {
	OperationKey                    string
	Source                          string
	UserID                          int
	SubscriptionID                  int
	TokenID                         int
	TokenKey                        string
	FundingDelta                    int64
	TokenDelta                      int64
	TaskID                          int64
	TaskQuota                       int64
	TaskQuotaTarget                 int64
	SubscriptionPreConsumeRequestID string
	FinalizeSubscriptionPreConsume  bool
	// AllowMissingToken is only for terminal task refunds. A token may be
	// deleted while its asynchronous task is still pending; that must not
	// prevent the user funding balance from being refunded.
	AllowMissingToken        bool
	ManualOnFailure          bool
	Effect                   *BillingSettlementEffect
	PreConsumeRequestID      string
	PreConsumeModelName      string
	PreConsumeRequestedQuota int64
	PreConsumeEffectiveQuota int64
}

// BillingSettlementBacklogStats is a read-only operational projection of open
// positive-final-settlement alerts. User admission is evaluated separately by
// the global and per-record blocking policy.
type BillingSettlementBacklogStats struct {
	Count           int64 `gorm:"column:record_count"`
	OldestCreatedAt int64 `gorm:"column:oldest_created_at"`
}

// BillingSettlementReconciliationItem is a read-only operator projection. It
// deliberately excludes token keys, effect payloads, request bodies, and any
// other secret-bearing fields.
type BillingSettlementReconciliationItem struct {
	ID                       int64  `json:"id"`
	Revision                 int64  `json:"revision"`
	OperationKey             string `json:"operation_key"`
	Status                   string `json:"status"`
	Source                   string `json:"source"`
	UserID                   int    `json:"user_id"`
	SubscriptionID           int    `json:"subscription_id"`
	TokenID                  int    `json:"token_id"`
	TaskID                   int64  `json:"task_id"`
	FundingDelta             int64  `json:"funding_delta"`
	AppliedFundingDelta      int64  `json:"applied_funding_delta"`
	TokenDelta               int64  `json:"token_delta"`
	AppliedTokenDelta        int64  `json:"applied_token_delta"`
	Attempts                 int    `json:"attempts"`
	LastError                string `json:"last_error"`
	NextAttempt              int64  `json:"next_attempt"`
	CreatedAt                int64  `json:"created_at"`
	UpdatedAt                int64  `json:"updated_at"`
	ReconciliationReviewedAt int64  `json:"reconciliation_reviewed_at"`
	ReconciliationReviewedBy int    `json:"reconciliation_reviewed_by"`
	ReconciliationReviewNote string `json:"reconciliation_review_note"`
	UserBlockingOverride     *bool  `json:"user_blocking_override"`
	RecordBlocksUser         bool   `json:"record_blocks_user"`
	BlocksUser               bool   `json:"blocks_user"`
}

type BillingSettlementReconciliationData struct {
	TotalCount int64 `json:"total_count"`
	// PendingCount and ManualCount classify financial states among currently
	// open administrator alerts. Reviewed records are retained for durable
	// financial recovery and leave this projection unless an administrator
	// explicitly chose to keep the affected user blocked.
	PendingCount int64 `json:"pending_count"`
	ManualCount  int64 `json:"manual_count"`
	// OpenAlertCount is retained as an explicit operational label for clients;
	// it equals TotalCount because this projection contains open alerts only.
	OpenAlertCount int64 `json:"open_alert_count"`
	// ReviewedCount is retained for API compatibility. Closed alerts are no
	// longer part of this active projection, so the value is always zero.
	ReviewedCount       int64                                 `json:"reviewed_count"`
	BlockingRecordCount int64                                 `json:"blocking_record_count"`
	BlockedUserCount    int64                                 `json:"blocked_user_count"`
	BlockUserByDefault  bool                                  `json:"block_user_by_default"`
	OldestCreatedAt     int64                                 `json:"oldest_created_at"`
	Truncated           bool                                  `json:"truncated"`
	GeneratedAt         int64                                 `json:"generated_at"`
	Items               []BillingSettlementReconciliationItem `json:"items"`
}

func unresolvedPositiveFinalizeSettlementScope(db *gorm.DB) *gorm.DB {
	return db.Model(&BillingSettlement{}).
		Where("funding_delta > 0").
		Where("status IN ?", []string{BillingSettlementStatusPending, BillingSettlementStatusManual}).
		Where("operation_key LIKE ?", BillingRequestFinalizeOperationKey("%"))
}

func openPositiveFinalizeSettlementAlertScope(db *gorm.DB) *gorm.DB {
	return unresolvedPositiveFinalizeSettlementScope(db).
		Where("(reconciliation_reviewed_at = ? OR user_blocking_override = ?)", 0, true)
}

func blockingPositiveFinalizeSettlementScope(db *gorm.DB, blockUserByDefault bool) *gorm.DB {
	scope := openPositiveFinalizeSettlementAlertScope(db)
	if blockUserByDefault {
		return scope.Where("(user_blocking_override IS NULL OR user_blocking_override = ?)", true)
	}
	return scope.Where("user_blocking_override = ?", true)
}

var billingSettlementRunnerOnce sync.Once
var billingSettlementRunnerState struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

var billingSettlementBacklogObserverState struct {
	sync.RWMutex
	observer func(BillingSettlementBacklogStats, time.Time)
}

// SetBillingSettlementBacklogObserver installs the operational observer used
// by the background settlement runner. The observer must remain read-only: it
// may alert operators, but it must not mutate or retry settlement records.
func SetBillingSettlementBacklogObserver(observer func(BillingSettlementBacklogStats, time.Time)) {
	billingSettlementBacklogObserverState.Lock()
	billingSettlementBacklogObserverState.observer = observer
	billingSettlementBacklogObserverState.Unlock()
}

// StartBillingSettlementTaskRunner retries durable settlement intents after a
// process restart or a transient database failure. All balance mutations are
// still protected by ApplyBillingSettlementOnce's operation key.
func StartBillingSettlementTaskRunner() {
	if DB == nil {
		return
	}
	billingSettlementRunnerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		billingSettlementRunnerState.Lock()
		billingSettlementRunnerState.cancel = cancel
		billingSettlementRunnerState.done = done
		billingSettlementRunnerState.Unlock()
		gopool.Go(func() {
			defer close(done)
			ProcessPendingBillingSettlementsOnce()
			observeBillingSettlementBacklog(time.Now())
			settlementTicker := time.NewTicker(time.Second)
			backlogTicker := time.NewTicker(billingSettlementBacklogSampleInterval)
			defer settlementTicker.Stop()
			defer backlogTicker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-settlementTicker.C:
					ProcessPendingBillingSettlementsOnce()
				case observedAt := <-backlogTicker.C:
					observeBillingSettlementBacklog(observedAt)
				}
			}
		})
	})
}

func StopBillingSettlementTaskRunner(ctx context.Context) error {
	billingSettlementRunnerState.Lock()
	cancel := billingSettlementRunnerState.cancel
	done := billingSettlementRunnerState.done
	billingSettlementRunnerState.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ProcessPendingBillingSettlementsOnce() {
	processPendingBillingSettlements()
}

func observeBillingSettlementBacklog(observedAt time.Time) {
	stats, err := GetUnresolvedPositiveFinalizeSettlementStats()
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to query unresolved positive final billing settlements: %s", err.Error()))
		return
	}
	billingSettlementBacklogObserverState.RLock()
	observer := billingSettlementBacklogObserverState.observer
	billingSettlementBacklogObserverState.RUnlock()
	if observer != nil {
		observer(stats, observedAt)
	}
}

// GetUnresolvedPositiveFinalizeSettlementStats returns the count and oldest
// creation time for open positive request-finalize alerts. Reviewed rows
// remain durable financial records and leave this operational projection
// unless an administrator explicitly keeps the affected user blocked or new
// failure evidence reopens them.
func GetUnresolvedPositiveFinalizeSettlementStats() (BillingSettlementBacklogStats, error) {
	if DB == nil {
		return BillingSettlementBacklogStats{}, errors.New("database is not initialized")
	}
	return getOpenPositiveFinalizeSettlementAlertStatsDB(DB)
}

// GetUnresolvedPositiveFinalizeSettlements returns bounded, read-only evidence
// for open operator alerts. Reviewed rows remain durable financial records and
// are excluded after closure unless an administrator explicitly keeps the
// affected user blocked. This function never retries, updates, deletes, or
// otherwise mutates a settlement record or any financial balance.
func GetUnresolvedPositiveFinalizeSettlements(limit int) (BillingSettlementReconciliationData, error) {
	if DB == nil {
		return BillingSettlementReconciliationData{}, errors.New("database is not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}

	data := BillingSettlementReconciliationData{
		BlockUserByDefault: billing_reconciliation_setting.BlockUserByDefault(),
		GeneratedAt:        time.Now().Unix(),
		Items:              make([]BillingSettlementReconciliationItem, 0),
	}
	transactionOptions := make([]*sql.TxOptions, 0, 1)
	if common.UsingMySQL || common.UsingPostgreSQL {
		transactionOptions = append(transactionOptions, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  true,
		})
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		stats, err := getOpenPositiveFinalizeSettlementAlertStatsDB(tx)
		if err != nil {
			return err
		}
		data.TotalCount = stats.Count
		data.OldestCreatedAt = stats.OldestCreatedAt

		if err := openPositiveFinalizeSettlementAlertScope(tx).
			Where("status = ?", BillingSettlementStatusPending).
			Count(&data.PendingCount).Error; err != nil {
			return err
		}
		if err := openPositiveFinalizeSettlementAlertScope(tx).
			Where("status = ?", BillingSettlementStatusManual).
			Count(&data.ManualCount).Error; err != nil {
			return err
		}
		data.OpenAlertCount = data.TotalCount
		data.ReviewedCount = 0
		blockingScope := blockingPositiveFinalizeSettlementScope(tx, data.BlockUserByDefault)
		if err := blockingScope.Count(&data.BlockingRecordCount).Error; err != nil {
			return err
		}
		if err := blockingPositiveFinalizeSettlementScope(tx, data.BlockUserByDefault).
			Distinct("user_id").
			Count(&data.BlockedUserCount).Error; err != nil {
			return err
		}

		items := make([]BillingSettlementReconciliationItem, 0, limit+1)
		if err := openPositiveFinalizeSettlementAlertScope(tx).
			Select(
				"id", "revision", "operation_key", "status", "source", "user_id", "subscription_id", "token_id", "task_id",
				"funding_delta", "applied_funding_delta", "token_delta", "applied_token_delta", "attempts", "last_error",
				"next_attempt", "created_at", "updated_at", "reconciliation_reviewed_at", "reconciliation_reviewed_by",
				"reconciliation_review_note", "user_blocking_override",
			).
			Order("created_at ASC").
			Order("id ASC").
			Limit(limit + 1).
			Scan(&items).Error; err != nil {
			return err
		}
		if len(items) > limit {
			data.Truncated = true
			items = items[:limit]
		}
		displayedUserIDs := make([]int, 0, len(items))
		for index := range items {
			items[index].LastError = common.SanitizePersistedLogContent(
				common.MaskSensitiveInfo(items[index].LastError),
			)
			items[index].ReconciliationReviewNote = common.SanitizePersistedLogContent(
				common.MaskSensitiveInfo(items[index].ReconciliationReviewNote),
			)
			items[index].RecordBlocksUser = billingSettlementBlocksUser(
				items[index].UserBlockingOverride,
				data.BlockUserByDefault,
			)
			if items[index].UserID > 0 {
				displayedUserIDs = append(displayedUserIDs, items[index].UserID)
			}
		}
		blockedUsers := make(map[int]struct{})
		if len(displayedUserIDs) > 0 {
			blockedUserIDs := make([]int, 0, len(displayedUserIDs))
			if err := blockingPositiveFinalizeSettlementScope(tx, data.BlockUserByDefault).
				Where("user_id IN ?", displayedUserIDs).
				Distinct("user_id").
				Pluck("user_id", &blockedUserIDs).Error; err != nil {
				return err
			}
			for _, userID := range blockedUserIDs {
				blockedUsers[userID] = struct{}{}
			}
		}
		for index := range items {
			_, items[index].BlocksUser = blockedUsers[items[index].UserID]
		}
		data.Items = items
		return nil
	}, transactionOptions...)
	return data, err
}

func getOpenPositiveFinalizeSettlementAlertStatsDB(db *gorm.DB) (BillingSettlementBacklogStats, error) {
	var stats BillingSettlementBacklogStats
	err := openPositiveFinalizeSettlementAlertScope(db).
		Select("COUNT(*) AS record_count, COALESCE(MIN(created_at), 0) AS oldest_created_at").
		Scan(&stats).Error
	return stats, err
}

func billingSettlementBlocksUser(override *bool, blockUserByDefault bool) bool {
	if override != nil {
		return *override
	}
	return blockUserByDefault
}

// HasUnresolvedPositiveFinalizeSettlement reports whether a user has an
// upstream-accepted request whose positive final settlement has not completed.
// New paid requests must stop before they can consume more service against the
// residual balance. Reserve failures are deliberately excluded: no upstream
// request has been released for those operation kinds.
func HasUnresolvedPositiveFinalizeSettlement(userID int) (bool, error) {
	if DB == nil {
		return false, errors.New("database is not initialized")
	}
	if userID <= 0 {
		return false, nil
	}

	blockUserByDefault := billing_reconciliation_setting.BlockUserByDefault()
	var record BillingSettlement
	result := blockingPositiveFinalizeSettlementScope(DB, blockUserByDefault).
		Select("id").
		Where("user_id = ?", userID).
		Limit(1).
		Find(&record)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func billingSettlementReviewUpdates(reviewerID int, blockUser bool, note string, reviewedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"reconciliation_reviewed_at": reviewedAt.Unix(),
		"reconciliation_reviewed_by": reviewerID,
		"reconciliation_review_note": note,
		"user_blocking_override":     blockUser,
	}
}

// BillingSettlementReviewTarget identifies the exact financial snapshot an
// administrator intends to acknowledge and close.
type BillingSettlementReviewTarget struct {
	ID       int64 `json:"id"`
	Revision int64 `json:"revision"`
}

// ReviewBillingSettlements atomically acknowledges a bounded set of current
// alert snapshots. It records administrator review metadata and explicitly
// allows the affected records, but never changes settlement status, balances,
// applied deltas, effect state, updated_at, or financial revision.
func ReviewBillingSettlements(targets []BillingSettlementReviewTarget, reviewerID int) ([]BillingSettlement, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if reviewerID <= 0 || len(targets) == 0 || len(targets) > 200 {
		return nil, ErrBillingSettlementReviewConflict
	}
	seen := make(map[int64]struct{}, len(targets))
	for _, target := range targets {
		if target.ID <= 0 || target.Revision <= 0 {
			return nil, ErrBillingSettlementReviewConflict
		}
		if _, exists := seen[target.ID]; exists {
			return nil, ErrBillingSettlementReviewConflict
		}
		seen[target.ID] = struct{}{}
	}
	sortedTargets := append([]BillingSettlementReviewTarget(nil), targets...)
	sort.Slice(sortedTargets, func(i, j int) bool {
		return sortedTargets[i].ID < sortedTargets[j].ID
	})

	reviewed := make([]BillingSettlement, 0, len(targets))
	err := DB.Transaction(func(tx *gorm.DB) error {
		reviewedAt := time.Now()
		updatedIDs := make([]int64, 0, len(sortedTargets))
		for _, target := range sortedTargets {
			result := openPositiveFinalizeSettlementAlertScope(tx).
				Where("id = ? AND revision = ?", target.ID, target.Revision).
				UpdateColumns(billingSettlementReviewUpdates(reviewerID, false, "", reviewedAt))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingSettlementReviewConflict
			}
			updatedIDs = append(updatedIDs, target.ID)
		}
		if err := tx.Where("id IN ?", updatedIDs).Order("id ASC").Find(&reviewed).Error; err != nil {
			return err
		}
		if len(reviewed) != len(updatedIDs) {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reviewed, nil
}

func billingSettlementReviewMatches(record BillingSettlement, reviewerID int, blockUser bool, note string, reviewedAt int64) bool {
	return record.ReconciliationReviewedAt == reviewedAt &&
		record.ReconciliationReviewedBy == reviewerID &&
		record.ReconciliationReviewNote == note &&
		record.UserBlockingOverride != nil &&
		*record.UserBlockingOverride == blockUser
}

func billingSettlementReviewSnapshotScope(db *gorm.DB, record BillingSettlement) *gorm.DB {
	scope := db.Where(
		"id = ? AND revision = ? AND reconciliation_reviewed_at = ? AND reconciliation_reviewed_by = ?",
		record.ID,
		record.Revision,
		record.ReconciliationReviewedAt,
		record.ReconciliationReviewedBy,
	)
	if record.ReconciliationReviewNote == "" {
		scope = scope.Where("(reconciliation_review_note = ? OR reconciliation_review_note IS NULL)", "")
	} else {
		scope = scope.Where("reconciliation_review_note = ?", record.ReconciliationReviewNote)
	}
	if record.UserBlockingOverride == nil {
		return scope.Where("user_blocking_override IS NULL")
	}
	return scope.Where("user_blocking_override = ?", *record.UserBlockingOverride)
}

// ReviewBillingSettlement records an administrator's alert disposition without
// changing settlement status, balances, applied deltas, or effect state.
func ReviewBillingSettlement(id int64, reviewerID int, blockUser bool, note string) (BillingSettlement, error) {
	if DB == nil {
		return BillingSettlement{}, errors.New("database is not initialized")
	}
	if id <= 0 || reviewerID <= 0 {
		return BillingSettlement{}, ErrBillingSettlementReviewConflict
	}

	var record BillingSettlement
	if err := DB.Select(
		"id",
		"user_id",
		"revision",
		"reconciliation_reviewed_at",
		"reconciliation_reviewed_by",
		"reconciliation_review_note",
		"user_blocking_override",
	).First(&record, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BillingSettlement{}, ErrBillingSettlementReviewConflict
		}
		return BillingSettlement{}, err
	}

	reviewedAt := time.Now()
	result := billingSettlementReviewSnapshotScope(
		unresolvedPositiveFinalizeSettlementScope(DB),
		record,
	).
		UpdateColumns(billingSettlementReviewUpdates(reviewerID, blockUser, note, reviewedAt))
	if result.Error != nil {
		return BillingSettlement{}, result.Error
	}
	if result.RowsAffected != 1 {
		var matching BillingSettlement
		err := unresolvedPositiveFinalizeSettlementScope(DB).
			Where("id = ?", id).
			First(&matching).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return BillingSettlement{}, ErrBillingSettlementReviewConflict
			}
			return BillingSettlement{}, err
		}
		if !billingSettlementReviewMatches(matching, reviewerID, blockUser, note, reviewedAt.Unix()) {
			return BillingSettlement{}, ErrBillingSettlementReviewConflict
		}
		return matching, nil
	}
	if err := DB.First(&record, id).Error; err != nil {
		return BillingSettlement{}, err
	}
	return record, nil
}

func BillingRequestFinalizeOperationKey(requestID string) string {
	return billingRequestOperationPrefix + requestID + billingRequestFinalizeSuffix
}

// GetBillingSettlementStatus returns the durable funding state for one stable
// operation key. Effect replay is intentionally separate: once funding is
// applied, an effect-only pending state must not block async task polling.
func GetBillingSettlementStatus(operationKey string) (status string, found bool, err error) {
	if DB == nil {
		return "", false, errors.New("database is not initialized")
	}
	if operationKey == "" {
		return "", false, nil
	}
	var record BillingSettlement
	result := DB.Select("status").Where("operation_key = ?", operationKey).Limit(1).Find(&record)
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 0 {
		return "", false, nil
	}
	return record.Status, true, nil
}

func ApplyBillingSettlementOnce(input BillingSettlementInput) (appliedFundingDelta int64, alreadyApplied bool, err error) {
	if input.OperationKey == "" {
		return 0, false, errors.New("billing settlement operation key is required")
	}

	record, alreadyApplied, err := ensureBillingSettlementRecord(input)
	if err != nil {
		return 0, false, err
	}
	resolvedTokenKey := input.TokenKey
	effectiveTokenID := input.TokenID
	effectiveTokenDelta := input.TokenDelta
	appliedTokenDelta := int64(0)
	var cacheTasks []CacheInvalidationTask
	if alreadyApplied {
		if input.PreConsumeRequestID != "" {
			if claimErr := DB.Transaction(func(tx *gorm.DB) error {
				return claimBillingPreConsumeSourceTx(tx, billingPreConsumeSelectionFromSettlement(input))
			}); claimErr != nil {
				return 0, false, claimErr
			}
		}
		if resolvedTokenKey == "" {
			resolvedTokenKey = lookupBillingTokenKey(record.TokenID)
		}
		invalidateBillingSettlementCaches(record, resolvedTokenKey)
		return record.AppliedFundingDelta, true, nil
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		var current BillingSettlement
		if err := withRowLock(tx).Where("id = ?", record.ID).First(&current).Error; err != nil {
			return err
		}
		if err := validateBillingSettlement(current, input); err != nil {
			return err
		}
		if current.Status == "" || current.Status == BillingSettlementStatusApplied {
			appliedFundingDelta = current.AppliedFundingDelta
			alreadyApplied = true
			return nil
		}
		if current.Status == BillingSettlementStatusManual {
			return permanentBillingSettlement(fmt.Errorf("%w: %s", ErrBillingSettlementManualReview, current.LastError))
		}
		if current.Status != BillingSettlementStatusPending {
			return permanentBillingSettlement(fmt.Errorf("unknown billing settlement status %q", current.Status))
		}
		if err := validateBillingSettlementInput(input); err != nil {
			return err
		}
		if input.PreConsumeRequestID != "" {
			if err := claimBillingPreConsumeSourceTx(tx, billingPreConsumeSelectionFromSettlement(input)); err != nil {
				return err
			}
		}

		if effectiveTokenID > 0 && effectiveTokenDelta != 0 {
			var token Token
			if lookupErr := tx.Select("key", "user_id").First(&token, effectiveTokenID).Error; lookupErr != nil {
				if input.AllowMissingToken && errors.Is(lookupErr, gorm.ErrRecordNotFound) {
					effectiveTokenID = 0
					effectiveTokenDelta = 0
				} else {
					return lookupErr
				}
			} else {
				if token.UserId != input.UserID {
					return permanentBillingSettlement(fmt.Errorf("billing settlement token user identity mismatch: token_id=%d", effectiveTokenID))
				}
				resolvedTokenKey = token.Key
			}
		}
		if input.TaskID > 0 {
			var task Task
			taskErr := withRowLock(tx).
				Select("quota").
				Where("id = ?", input.TaskID).
				First(&task).Error
			if taskErr != nil {
				if errors.Is(taskErr, gorm.ErrRecordNotFound) {
					return permanentBillingSettlement(fmt.Errorf("%w: task_id=%d expected_quota=%d", ErrBillingSettlementTaskConflict, input.TaskID, input.TaskQuota))
				}
				return taskErr
			}
			if int64(task.Quota) != input.TaskQuota {
				return permanentBillingSettlement(fmt.Errorf("%w: task_id=%d expected_quota=%d", ErrBillingSettlementTaskConflict, input.TaskID, input.TaskQuota))
			}
		}

		var applyErr error
		appliedFundingDelta, applyErr = applyFundingDeltaTx(tx, input)
		if applyErr != nil {
			return applyErr
		}
		if input.Source == BillingSettlementSourceSubscription && input.TokenDelta == input.FundingDelta {
			effectiveTokenDelta = appliedFundingDelta
		}
		if err := applyTokenDeltaTx(tx, input.UserID, effectiveTokenID, effectiveTokenDelta, input.AllowMissingToken); err != nil {
			return err
		}
		if effectiveTokenID > 0 {
			appliedTokenDelta = effectiveTokenDelta
		}

		if input.TaskID > 0 {
			appliedTaskQuotaTarget := input.TaskQuota + appliedFundingDelta
			if appliedTaskQuotaTarget != input.TaskQuota {
				result := tx.Model(&Task{}).
					Where("id = ? AND quota = ?", input.TaskID, input.TaskQuota).
					Update("quota", appliedTaskQuotaTarget)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return permanentBillingSettlement(fmt.Errorf("%w: task_id=%d expected_quota=%d", ErrBillingSettlementTaskConflict, input.TaskID, input.TaskQuota))
				}
			}
		}

		result := tx.Model(&BillingSettlement{}).
			Where("id = ? AND status = ?", record.ID, BillingSettlementStatusPending).
			Updates(map[string]interface{}{
				"applied_funding_delta": appliedFundingDelta,
				"applied_token_delta":   appliedTokenDelta,
				"status":                BillingSettlementStatusApplied,
				"effect_status":         effectStatusAfterSettlement(current.EffectPayload),
				"last_error":            "",
				"next_attempt":          0,
				"updated_at":            time.Now().Unix(),
				"revision":              gorm.Expr("revision + ?", 1),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return permanentBillingSettlement(fmt.Errorf("%w: %s", ErrBillingSettlementOperationConflict, input.OperationKey))
		}
		if input.UserID > 0 {
			userTask, err := stageUserCacheInvalidationTx(tx, input.UserID, false)
			if err != nil {
				return err
			}
			cacheTasks = append(cacheTasks, userTask)
		}
		if effectiveTokenID > 0 && effectiveTokenDelta != 0 && resolvedTokenKey != "" {
			tokenTask, err := stageTokenCacheInvalidationTx(tx, resolvedTokenKey, false)
			if err != nil {
				return err
			}
			cacheTasks = append(cacheTasks, tokenTask)
		}
		return nil
	})
	if err != nil {
		markBillingSettlementFailure(input.OperationKey, err)
		return 0, false, err
	}

	dispatchStagedCacheInvalidations(cacheTasks)
	return appliedFundingDelta, alreadyApplied, nil
}

// ResolveBillingPreConsumeSource returns the funding source already selected by
// a request's durable pre-consume record. A request must never move between a
// wallet settlement and a subscription pre-consume on replay.
func ResolveBillingPreConsumeSource(requestID string) (string, bool, error) {
	if requestID == "" {
		return "", false, nil
	}
	if DB == nil {
		return "", false, errors.New("database is not initialized")
	}

	var selection BillingPreConsumeSelection
	selectionQuery := DB.Select("source").
		Where("request_id = ?", requestID).
		Limit(1).
		Find(&selection)
	if selectionQuery.Error != nil {
		return "", false, selectionQuery.Error
	}
	if selectionQuery.RowsAffected > 0 {
		if selection.Source != BillingSettlementSourceWallet && selection.Source != BillingSettlementSourceSubscription {
			return "", false, fmt.Errorf("%w: request %s has invalid pre-consume source %q", ErrBillingSettlementOperationConflict, requestID, selection.Source)
		}
		return selection.Source, true, nil
	}

	// Backward-compatible discovery for operations created before the selection
	// table existed. The next replay backfills the immutable selection in the
	// same transaction as its idempotent balance operation.
	var walletRecord BillingSettlement
	walletQuery := DB.Select("source").
		Where("operation_key = ?", "request:"+requestID+":pre-consume").
		Limit(1).
		Find(&walletRecord)
	if walletQuery.Error != nil {
		return "", false, walletQuery.Error
	}

	var subscriptionRecord SubscriptionPreConsumeRecord
	subscriptionQuery := DB.Select("id").
		Where("request_id = ?", requestID).
		Limit(1).
		Find(&subscriptionRecord)
	if subscriptionQuery.Error != nil {
		return "", false, subscriptionQuery.Error
	}

	hasWallet := walletQuery.RowsAffected > 0
	hasSubscription := subscriptionQuery.RowsAffected > 0
	if hasWallet && hasSubscription {
		return "", false, fmt.Errorf("%w: request %s has multiple pre-consume funding sources", ErrBillingSettlementOperationConflict, requestID)
	}
	if hasWallet {
		if walletRecord.Source != BillingSettlementSourceWallet {
			return "", false, fmt.Errorf("%w: request %s has invalid wallet pre-consume source %q", ErrBillingSettlementOperationConflict, requestID, walletRecord.Source)
		}
		return BillingSettlementSourceWallet, true, nil
	}
	if hasSubscription {
		return BillingSettlementSourceSubscription, true, nil
	}
	return "", false, nil
}

func billingPreConsumeSelectionFromSettlement(input BillingSettlementInput) BillingPreConsumeSelection {
	return BillingPreConsumeSelection{
		RequestID:      input.PreConsumeRequestID,
		Source:         input.Source,
		UserID:         input.UserID,
		TokenID:        input.TokenID,
		ModelName:      input.PreConsumeModelName,
		RequestedQuota: input.PreConsumeRequestedQuota,
		EffectiveQuota: input.PreConsumeEffectiveQuota,
	}
}

func claimBillingPreConsumeSourceTx(tx *gorm.DB, selection BillingPreConsumeSelection) error {
	if tx == nil {
		return errors.New("billing pre-consume selection transaction is required")
	}
	if selection.RequestID == "" || selection.UserID <= 0 || selection.RequestedQuota < 0 || selection.EffectiveQuota < 0 {
		return fmt.Errorf("%w: invalid billing pre-consume selection", ErrBillingSettlementOperationConflict)
	}
	if selection.Source != BillingSettlementSourceWallet && selection.Source != BillingSettlementSourceSubscription {
		return fmt.Errorf("%w: invalid billing pre-consume source %q", ErrBillingSettlementOperationConflict, selection.Source)
	}
	selection.CreatedAt = time.Now().Unix()
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "request_id"}},
		DoNothing: true,
	}).Create(&selection).Error; err != nil {
		return err
	}

	var existing BillingPreConsumeSelection
	if err := tx.Where("request_id = ?", selection.RequestID).First(&existing).Error; err != nil {
		return err
	}
	if existing.Source != selection.Source ||
		existing.UserID != selection.UserID ||
		existing.TokenID != selection.TokenID ||
		existing.ModelName != selection.ModelName ||
		existing.RequestedQuota != selection.RequestedQuota ||
		existing.EffectiveQuota != selection.EffectiveQuota {
		return fmt.Errorf("%w: request %s reused with different pre-consume source or parameters", ErrBillingSettlementOperationConflict, selection.RequestID)
	}
	return nil
}

func ensureBillingSettlementRecord(input BillingSettlementInput) (BillingSettlement, bool, error) {
	return ensureBillingSettlementRecordDB(DB, input)
}

func ensureBillingSettlementRecordDB(db *gorm.DB, input BillingSettlementInput) (BillingSettlement, bool, error) {
	if db == nil {
		return BillingSettlement{}, false, fmt.Errorf("%w: database is not initialized", ErrBillingSettlementRecordNotDurable)
	}
	effectPayload, err := billingSettlementEffectPayload(input.Effect)
	if err != nil {
		return BillingSettlement{}, false, fmt.Errorf("%w: %v", ErrBillingSettlementRecordNotDurable, err)
	}
	now := time.Now().Unix()
	record := BillingSettlement{
		OperationKey: input.OperationKey, Source: input.Source, UserID: input.UserID,
		SubscriptionID: input.SubscriptionID, TokenID: input.TokenID,
		FundingDelta: input.FundingDelta, TokenDelta: input.TokenDelta,
		TaskID: input.TaskID, TaskQuota: input.TaskQuota, TaskQuotaTarget: input.TaskQuotaTarget,
		SubscriptionPreConsumeRequestID: input.SubscriptionPreConsumeRequestID,
		AllowMissingToken:               input.AllowMissingToken,
		FinalizeSubscriptionPreConsume:  input.FinalizeSubscriptionPreConsume,
		ManualOnFailure:                 input.ManualOnFailure,
		PreConsumeRequestID:             input.PreConsumeRequestID,
		PreConsumeModelName:             input.PreConsumeModelName,
		PreConsumeRequestedQuota:        input.PreConsumeRequestedQuota,
		PreConsumeEffectiveQuota:        input.PreConsumeEffectiveQuota,
		EffectPayload:                   effectPayload,
		Status:                          BillingSettlementStatusPending, NextAttempt: now, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "operation_key"}},
		DoNothing: true,
	}).Create(&record)
	if result.Error != nil {
		return BillingSettlement{}, false, fmt.Errorf("%w: %v", ErrBillingSettlementRecordNotDurable, result.Error)
	}
	if err := db.Where("operation_key = ?", input.OperationKey).First(&record).Error; err != nil {
		return BillingSettlement{}, false, err
	}
	if err := validateBillingSettlement(record, input); err != nil {
		return BillingSettlement{}, false, err
	}
	if record.Status == BillingSettlementStatusManual {
		return BillingSettlement{}, false, fmt.Errorf("%w: %s", ErrBillingSettlementManualReview, record.LastError)
	}
	return record, record.Status == "" || record.Status == BillingSettlementStatusApplied, nil
}

func billingSettlementEffectPayload(effect *BillingSettlementEffect) (string, error) {
	if effect == nil {
		return "", nil
	}
	safeEffect := *effect
	safeEffect.Content = common.SanitizePersistedLogContent(safeEffect.Content)
	data, err := common.Marshal(&safeEffect)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeBillingSettlementEffect(payload string) (*BillingSettlementEffect, error) {
	if payload == "" {
		return nil, nil
	}
	var effect BillingSettlementEffect
	if err := common.UnmarshalJsonStr(payload, &effect); err != nil {
		return nil, err
	}
	return &effect, nil
}

func effectStatusAfterSettlement(payload string) string {
	if payload == "" {
		return ""
	}
	return BillingSettlementEffectPending
}

func lookupBillingTokenKey(tokenID int) string {
	if tokenID <= 0 || DB == nil {
		return ""
	}
	var token Token
	if err := DB.Select("key").First(&token, tokenID).Error; err != nil {
		return ""
	}
	return token.Key
}

func invalidateBillingSettlementCaches(record BillingSettlement, tokenKey string) {
	if record.UserID > 0 {
		if cacheErr := invalidateUserQuotaCache(record.UserID); cacheErr != nil {
			enqueueUserCacheInvalidationRetry(record.UserID, cacheErr)
		}
	}
	if record.TokenID > 0 && record.AppliedTokenDelta != 0 && tokenKey != "" {
		invalidateTokenQuotaCache(tokenKey)
	}
}

func markBillingSettlementFailure(operationKey string, cause error) {
	if DB == nil || operationKey == "" {
		return
	}
	var record BillingSettlement
	if err := DB.Where("operation_key = ?", operationKey).First(&record).Error; err != nil {
		common.SysLog("failed to load billing settlement retry record: " + err.Error())
		return
	}
	attempts := record.Attempts + 1
	delay := 1 << min(attempts, 6)
	status := BillingSettlementStatusPending
	nextAttempt := time.Now().Add(time.Duration(delay) * time.Second).Unix()
	if record.ManualOnFailure || isPermanentBillingSettlementError(cause) {
		status = BillingSettlementStatusManual
		nextAttempt = 0
	}
	lastError := ""
	if cause != nil {
		lastError = cause.Error()
	}
	resetReview := lastError != record.LastError || status != record.Status
	if err := DB.Model(&BillingSettlement{}).
		Where("id = ? AND status = ?", record.ID, BillingSettlementStatusPending).
		Updates(billingSettlementFailureUpdates(attempts, lastError, status, nextAttempt, time.Now().Unix(), resetReview)).Error; err != nil {
		common.SysLog("failed to reschedule billing settlement: " + err.Error())
	}
}

func billingSettlementFailureUpdates(attempts int, lastError string, status string, nextAttempt int64, updatedAt int64, resetReview bool) map[string]interface{} {
	updates := map[string]interface{}{
		"attempts":     attempts,
		"last_error":   lastError,
		"status":       status,
		"next_attempt": nextAttempt,
		"updated_at":   updatedAt,
		"revision":     gorm.Expr("revision + ?", 1),
	}
	if resetReview {
		updates["reconciliation_reviewed_at"] = 0
		updates["reconciliation_reviewed_by"] = 0
		updates["reconciliation_review_note"] = ""
		updates["user_blocking_override"] = nil
	}
	return updates
}

func processPendingBillingSettlements() {
	if DB == nil {
		return
	}
	var records []BillingSettlement
	if err := DB.Where("status = ? AND next_attempt <= ?", BillingSettlementStatusPending, time.Now().Unix()).Order("id").Limit(100).Find(&records).Error; err != nil {
		common.SysLog("failed to load billing settlement retries: " + err.Error())
		return
	}
	for _, record := range records {
		effect, decodeErr := decodeBillingSettlementEffect(record.EffectPayload)
		if decodeErr != nil {
			markBillingSettlementFailure(record.OperationKey, permanentBillingSettlement(decodeErr))
			continue
		}
		_, _, err := ApplyBillingSettlementOnce(BillingSettlementInput{
			OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID,
			SubscriptionID: record.SubscriptionID, TokenID: record.TokenID,
			FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta,
			TaskID: record.TaskID, TaskQuota: record.TaskQuota, TaskQuotaTarget: record.TaskQuotaTarget,
			SubscriptionPreConsumeRequestID: record.SubscriptionPreConsumeRequestID,
			FinalizeSubscriptionPreConsume:  record.FinalizeSubscriptionPreConsume,
			AllowMissingToken:               record.AllowMissingToken,
			ManualOnFailure:                 record.ManualOnFailure,
			PreConsumeRequestID:             record.PreConsumeRequestID,
			PreConsumeModelName:             record.PreConsumeModelName,
			PreConsumeRequestedQuota:        record.PreConsumeRequestedQuota,
			PreConsumeEffectiveQuota:        record.PreConsumeEffectiveQuota,
			Effect:                          effect,
		})
		if err != nil {
			common.SysLog("billing settlement retry remains pending: " + err.Error())
			continue
		}
		if effectErr := ProcessBillingSettlementEffect(record.OperationKey); effectErr != nil {
			common.SysLog("billing settlement effect remains pending: " + effectErr.Error())
		}
	}
	processPendingBillingSettlementEffects()
}

func validateBillingSettlement(existing BillingSettlement, input BillingSettlementInput) error {
	// Effect payload is non-financial metadata. The first persisted payload wins;
	// replays must not overwrite it or turn harmless description drift into a balance conflict.
	if existing.Source != input.Source ||
		existing.UserID != input.UserID ||
		existing.SubscriptionID != input.SubscriptionID ||
		existing.TokenID != input.TokenID ||
		existing.FundingDelta != input.FundingDelta ||
		existing.TokenDelta != input.TokenDelta ||
		existing.TaskID != input.TaskID ||
		existing.TaskQuota != input.TaskQuota ||
		existing.TaskQuotaTarget != input.TaskQuotaTarget ||
		existing.SubscriptionPreConsumeRequestID != input.SubscriptionPreConsumeRequestID ||
		existing.FinalizeSubscriptionPreConsume != input.FinalizeSubscriptionPreConsume ||
		existing.AllowMissingToken != input.AllowMissingToken ||
		existing.ManualOnFailure != input.ManualOnFailure {
		return permanentBillingSettlement(fmt.Errorf("%w: operation key reused with different parameters: %s", ErrBillingSettlementOperationConflict, input.OperationKey))
	}
	if billingSettlementHasPreConsumeFields(existing) &&
		(existing.PreConsumeRequestID != input.PreConsumeRequestID ||
			existing.PreConsumeModelName != input.PreConsumeModelName ||
			existing.PreConsumeRequestedQuota != input.PreConsumeRequestedQuota ||
			existing.PreConsumeEffectiveQuota != input.PreConsumeEffectiveQuota) {
		return permanentBillingSettlement(fmt.Errorf("%w: operation key reused with different pre-consume parameters: %s", ErrBillingSettlementOperationConflict, input.OperationKey))
	}
	return nil
}

func billingSettlementHasPreConsumeFields(record BillingSettlement) bool {
	return record.PreConsumeRequestID != "" ||
		record.PreConsumeModelName != "" ||
		record.PreConsumeRequestedQuota != 0 ||
		record.PreConsumeEffectiveQuota != 0
}

func processPendingBillingSettlementEffects() {
	if DB == nil {
		return
	}
	var records []BillingSettlement
	if err := DB.Where("status = ? AND effect_status = ?", BillingSettlementStatusApplied, BillingSettlementEffectPending).
		Order("id").Limit(100).Find(&records).Error; err != nil {
		common.SysLog("failed to load billing settlement effects: " + err.Error())
		return
	}
	for _, record := range records {
		if err := ProcessBillingSettlementEffect(record.OperationKey); err != nil {
			common.SysLog("billing settlement effect remains pending: " + err.Error())
		}
	}
}

func ProcessBillingSettlementEffect(operationKey string) error {
	if DB == nil || operationKey == "" {
		return errors.New("billing settlement effect operation key is required")
	}
	var record BillingSettlement
	if err := DB.Where("operation_key = ?", operationKey).First(&record).Error; err != nil {
		return err
	}
	if record.EffectPayload == "" || record.EffectStatus == BillingSettlementEffectApplied {
		return nil
	}
	if record.Status != BillingSettlementStatusApplied || record.EffectStatus != BillingSettlementEffectPending {
		return fmt.Errorf("billing settlement effect is not ready: operation=%s status=%s effect_status=%s", operationKey, record.Status, record.EffectStatus)
	}
	effect, err := decodeBillingSettlementEffect(record.EffectPayload)
	if err != nil {
		return err
	}
	if effect == nil {
		return nil
	}

	appliedDelta := record.AppliedFundingDelta
	useActualQuota := effect.QuotaIsActual || effect.Quota > 0
	logQuota := appliedDelta
	if useActualQuota {
		logQuota = effect.Quota
	}
	if effect.UpdateUsage && logQuota < 0 {
		return fmt.Errorf("billing settlement effect usage quota cannot be negative: operation=%s quota=%d", operationKey, logQuota)
	}
	if logQuota != 0 || effect.QuotaIsActual {
		other := effect.Other
		if other == nil {
			other = make(map[string]interface{})
		}
		applySubscriptionSettlementEffectMetadata(record, effect, other)
		if record.TaskID > 0 {
			other["actual_quota"] = record.TaskQuota + appliedDelta
		}
		quota := logQuota
		if quota < 0 {
			quota = -quota
		}
		if err := RecordTaskBillingLogOnce(operationKey, RecordTaskBillingLogParams{
			UserId: record.UserID, LogType: effect.LogType, Content: effect.Content,
			ChannelId: effect.ChannelID, ModelName: effect.ModelName, Quota: int(quota),
			TokenId: effect.TokenID, TokenName: effect.TokenName,
			Group: effect.Group, Other: other, NodeName: effect.NodeName,
			PromptTokens: effect.PromptTokens, CompletionTokens: effect.CompletionTokens,
			UseTimeSeconds: effect.UseTimeSeconds, IsStream: effect.IsStream,
			RequestId: effect.RequestID, UpstreamRequestId: effect.UpstreamRequestID,
		}); err != nil {
			return err
		}
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&BillingSettlement{}).
			Where("id = ? AND status = ? AND effect_status = ?", record.ID, BillingSettlementStatusApplied, BillingSettlementEffectPending).
			Update("effect_status", BillingSettlementEffectApplying)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil
		}
		usageQuota := appliedDelta
		if useActualQuota {
			usageQuota = effect.Quota
		}
		if effect.UpdateUsage && (usageQuota > 0 || effect.QuotaIsActual) {
			userResult := tx.Model(&User{}).Where("id = ?", record.UserID).Updates(map[string]interface{}{
				"used_quota":    gorm.Expr("used_quota + ?", usageQuota),
				"request_count": gorm.Expr("request_count + ?", 1),
			})
			if userResult.Error != nil {
				return userResult.Error
			}
			if userResult.RowsAffected != 1 {
				return fmt.Errorf("billing settlement effect user not found: user_id=%d", record.UserID)
			}
			if effect.ChannelID > 0 {
				if err := tx.Model(&Channel{}).Where("id = ?", effect.ChannelID).
					Update("used_quota", gorm.Expr("used_quota + ?", usageQuota)).Error; err != nil {
					return err
				}
			}
		}
		result := tx.Model(&BillingSettlement{}).
			Where("id = ? AND effect_status = ?", record.ID, BillingSettlementEffectApplying).
			Update("effect_status", BillingSettlementEffectApplied)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("billing settlement effect state conflict: operation=%s", operationKey)
		}
		return nil
	})
}

func BillingSettlementOwnsEffect(operationKey string) (bool, error) {
	if DB == nil || operationKey == "" {
		return false, errors.New("billing settlement effect operation key is required")
	}
	var record BillingSettlement
	if err := DB.Select("effect_payload").Where("operation_key = ?", operationKey).First(&record).Error; err != nil {
		return false, err
	}
	return record.EffectPayload != "", nil
}

func applySubscriptionSettlementEffectMetadata(
	record BillingSettlement,
	effect *BillingSettlementEffect,
	other map[string]interface{},
) {
	if record.Source != BillingSettlementSourceSubscription || effect == nil ||
		effect.Subscription == nil || other == nil {
		return
	}

	info := effect.Subscription
	postDelta := record.AppliedFundingDelta
	if postDelta != 0 {
		other["subscription_post_delta"] = postDelta
	} else {
		delete(other, "subscription_post_delta")
	}

	consumed := info.PreConsumed + postDelta
	if consumed < 0 {
		consumed = 0
	}
	if consumed > 0 {
		other["subscription_consumed"] = consumed
	} else {
		delete(other, "subscription_consumed")
	}

	used := info.AmountUsedAfterPreConsume + postDelta
	if used < 0 {
		used = 0
	}
	if info.AmountTotal > 0 {
		remain := info.AmountTotal - used
		if remain < 0 {
			remain = 0
		}
		other["subscription_total"] = info.AmountTotal
		other["subscription_used"] = used
		other["subscription_remain"] = remain
	}
}

func validateBillingSettlementInput(input BillingSettlementInput) error {
	if input.Source != BillingSettlementSourceWallet && input.Source != BillingSettlementSourceSubscription {
		return permanentBillingSettlement(fmt.Errorf("unsupported billing settlement source: %s", input.Source))
	}
	if input.TaskID > 0 && input.TaskQuotaTarget != input.TaskQuota+input.FundingDelta {
		return permanentBillingSettlement(fmt.Errorf("task quota target mismatch: task_id=%d current=%d target=%d funding_delta=%d", input.TaskID, input.TaskQuota, input.TaskQuotaTarget, input.FundingDelta))
	}
	if input.Source == BillingSettlementSourceSubscription && input.SubscriptionPreConsumeRequestID == "" {
		return permanentBillingSettlement(ErrSubscriptionSettlementUnbound)
	}
	if input.FinalizeSubscriptionPreConsume && (input.Source != BillingSettlementSourceSubscription || input.FundingDelta >= 0) {
		return permanentBillingSettlement(errors.New("subscription pre-consume finalization requires a negative subscription delta"))
	}
	return nil
}

func applyFundingDeltaTx(tx *gorm.DB, input BillingSettlementInput) (int64, error) {
	switch input.Source {
	case BillingSettlementSourceWallet:
		if input.UserID <= 0 {
			return 0, permanentBillingSettlement(errors.New("wallet settlement user id is required"))
		}
		if input.FundingDelta > 0 {
			result := tx.Model(&User{}).
				Where("id = ? AND quota >= ?", input.UserID, input.FundingDelta).
				Update("quota", gorm.Expr("quota - ?", input.FundingDelta))
			if result.Error != nil {
				return 0, result.Error
			}
			if result.RowsAffected != 1 {
				return 0, permanentBillingSettlement(fmt.Errorf("%w: id=%d, need=%d", ErrUserQuotaInsufficient, input.UserID, input.FundingDelta))
			}
		} else if input.FundingDelta < 0 {
			result := tx.Model(&User{}).
				Where("id = ?", input.UserID).
				Update("quota", gorm.Expr("quota + ?", -input.FundingDelta))
			if result.Error != nil {
				return 0, result.Error
			}
			if result.RowsAffected != 1 {
				return 0, permanentBillingSettlement(fmt.Errorf("%w: id=%d", ErrUserNotFound, input.UserID))
			}
		}
		return input.FundingDelta, nil
	case BillingSettlementSourceSubscription:
		return applySubscriptionDeltaTx(tx, input)
	default:
		return 0, permanentBillingSettlement(fmt.Errorf("unsupported billing settlement source: %s", input.Source))
	}
}

func applyTokenDeltaTx(tx *gorm.DB, userID int, tokenID int, delta int64, allowMissing bool) error {
	if tokenID <= 0 || delta == 0 {
		return nil
	}
	if delta > 0 {
		result := tx.Model(&Token{}).
			Where("id = ? AND user_id = ? AND (unlimited_quota = ? OR remain_quota >= ?)", tokenID, userID, true, delta).
			Updates(map[string]interface{}{
				"remain_quota":  gorm.Expr("remain_quota - ?", delta),
				"used_quota":    gorm.Expr("used_quota + ?", delta),
				"accessed_time": common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var token Token
			if err := tx.Select("id").First(&token, tokenID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return permanentBillingSettlement(fmt.Errorf("%w: id=%d, need=%d", ErrTokenQuotaInsufficient, tokenID, delta))
		}
		return nil
	}

	result := tx.Model(&Token{}).
		Where("id = ? AND user_id = ? AND used_quota >= ?", tokenID, userID, -delta).
		Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", -delta),
			"used_quota":    gorm.Expr("used_quota - ?", -delta),
			"accessed_time": common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		if allowMissing && delta < 0 {
			var token Token
			if err := tx.Select("id").First(&token, tokenID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
		}
		return permanentBillingSettlement(fmt.Errorf("%w: id=%d", ErrTokenQuotaInsufficient, tokenID))
	}
	return nil
}

func applySubscriptionDeltaTx(tx *gorm.DB, input BillingSettlementInput) (int64, error) {
	requestID := input.SubscriptionPreConsumeRequestID
	if requestID == "" {
		return 0, permanentBillingSettlement(ErrSubscriptionSettlementUnbound)
	}
	var record SubscriptionPreConsumeRecord
	if err := withRowLock(tx).Where("request_id = ?", requestID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, permanentBillingSettlement(fmt.Errorf("%w: request=%s", ErrSubscriptionSettlementUnbound, requestID))
		}
		return 0, err
	}
	if input.SubscriptionID <= 0 || record.UserSubscriptionId != input.SubscriptionID {
		return 0, permanentBillingSettlement(fmt.Errorf("subscription settlement identity mismatch: request=%s", requestID))
	}
	if record.UserId != input.UserID || (input.TokenDelta != 0 && record.TokenId != input.TokenID) {
		return 0, permanentBillingSettlement(fmt.Errorf("subscription settlement owner identity mismatch: request=%s", requestID))
	}
	if record.Status == "refunded" {
		return 0, permanentBillingSettlement(fmt.Errorf("subscription pre-consume already refunded: request=%s", requestID))
	}
	var sub UserSubscription
	if err := withRowLock(tx).Where("id = ?", record.UserSubscriptionId).First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, permanentBillingSettlement(fmt.Errorf("subscription settlement target no longer exists: request=%s", requestID))
		}
		return 0, err
	}
	if sub.UserId != input.UserID {
		return 0, permanentBillingSettlement(fmt.Errorf("subscription settlement user identity mismatch: request=%s", requestID))
	}
	if err := validateSubscriptionPreConsumePeriod(record, sub); err != nil {
		return 0, permanentBillingSettlement(err)
	}
	if input.FinalizeSubscriptionPreConsume {
		if input.FundingDelta >= 0 || -input.FundingDelta < record.PreConsumed {
			return 0, permanentBillingSettlement(fmt.Errorf("subscription refund does not cover pre-consumed amount: request=%s", requestID))
		}
	}
	applied, err := postConsumeUserSubscriptionDeltaTx(tx, record.UserSubscriptionId, input.FundingDelta)
	if err != nil {
		if errors.Is(err, ErrSubscriptionQuotaInsufficient) {
			return 0, permanentBillingSettlement(err)
		}
		return 0, err
	}
	if input.FinalizeSubscriptionPreConsume {
		if applied != input.FundingDelta {
			return 0, permanentBillingSettlement(fmt.Errorf("subscription refund was clamped: request=%s requested=%d applied=%d", requestID, input.FundingDelta, applied))
		}
		result := tx.Model(&SubscriptionPreConsumeRecord{}).
			Where("id = ? AND status = ?", record.Id, "consumed").
			Updates(map[string]interface{}{"status": "refunded", "updated_at": common.GetTimestamp()})
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected != 1 {
			return 0, permanentBillingSettlement(errors.New("subscription refund claim lost"))
		}
	}
	return applied, nil
}

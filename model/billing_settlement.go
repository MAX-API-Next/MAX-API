package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
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

var (
	ErrBillingSettlementManualReview       = errors.New("billing settlement requires manual review")
	ErrBillingSettlementTaskConflict       = errors.New("billing settlement task quota conflict")
	ErrBillingSettlementOperationConflict  = errors.New("billing settlement operation conflict")
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
	UserID                          int    `gorm:"not null;default:0"`
	SubscriptionID                  int    `gorm:"not null;default:0"`
	TokenID                         int    `gorm:"not null;default:0"`
	FundingDelta                    int64  `gorm:"not null;default:0"`
	AppliedFundingDelta             int64  `gorm:"not null;default:0"`
	TokenDelta                      int64  `gorm:"not null;default:0"`
	AppliedTokenDelta               int64  `gorm:"not null;default:0"`
	TaskID                          int64  `gorm:"not null;default:0"`
	TaskQuota                       int64  `gorm:"not null;default:0"`
	TaskQuotaTarget                 int64  `gorm:"not null;default:0"`
	Status                          string `gorm:"type:varchar(16);index;not null;default:'applied'"`
	Attempts                        int    `gorm:"not null;default:0"`
	LastError                       string `gorm:"type:text"`
	NextAttempt                     int64  `gorm:"index;not null;default:0"`
	Revision                        int64  `gorm:"not null;default:1"`
	SubscriptionPreConsumeRequestID string `gorm:"type:varchar(64);index"`
	FinalizeSubscriptionPreConsume  bool   `gorm:"not null;default:false"`
	AllowMissingToken               bool   `gorm:"not null;default:false"`
	ManualOnFailure                 bool   `gorm:"not null;default:false"`
	EffectPayload                   string `gorm:"type:text"`
	EffectStatus                    string `gorm:"type:varchar(16);index"`
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
	LogType     int                    `json:"log_type"`
	Content     string                 `json:"content"`
	ChannelID   int                    `json:"channel_id"`
	ModelName   string                 `json:"model_name"`
	TokenID     int                    `json:"token_id"`
	Group       string                 `json:"group"`
	Other       map[string]interface{} `json:"other"`
	NodeName    string                 `json:"node_name"`
	UpdateUsage bool                   `json:"update_usage"`
	Quota       int64                  `json:"quota,omitempty"`
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

var billingSettlementRunnerOnce sync.Once

// StartBillingSettlementTaskRunner retries durable settlement intents after a
// process restart or a transient database failure. All balance mutations are
// still protected by ApplyBillingSettlementOnce's operation key.
func StartBillingSettlementTaskRunner() {
	billingSettlementRunnerOnce.Do(func() {
		if DB == nil {
			return
		}
		gopool.Go(func() {
			processPendingBillingSettlements()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				processPendingBillingSettlements()
			}
		})
	})
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
			resolvedTokenKey = lookupBillingTokenKey(input.TokenID)
		}
		invalidateBillingSettlementCaches(input, resolvedTokenKey)
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
		return BillingSettlement{}, false, errors.New("database is not initialized")
	}
	effectPayload, err := billingSettlementEffectPayload(input.Effect)
	if err != nil {
		return BillingSettlement{}, false, err
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
		EffectPayload:                   effectPayload,
		Status:                          BillingSettlementStatusPending, NextAttempt: now, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "operation_key"}},
		DoNothing: true,
	}).Create(&record)
	if result.Error != nil {
		return BillingSettlement{}, false, result.Error
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
	data, err := common.Marshal(effect)
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

func invalidateBillingSettlementCaches(input BillingSettlementInput, tokenKey string) {
	if input.UserID > 0 {
		if cacheErr := invalidateUserQuotaCache(input.UserID); cacheErr != nil {
			enqueueUserCacheInvalidationRetry(input.UserID, cacheErr)
		}
	}
	if input.TokenID > 0 && input.TokenDelta != 0 && tokenKey != "" {
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
	if err := DB.Model(&BillingSettlement{}).Where("id = ? AND status = ?", record.ID, BillingSettlementStatusPending).Updates(map[string]interface{}{
		"attempts": attempts, "last_error": lastError,
		"status": status, "next_attempt": nextAttempt,
		"updated_at": time.Now().Unix(), "revision": gorm.Expr("revision + ?", 1),
	}).Error; err != nil {
		common.SysLog("failed to reschedule billing settlement: " + err.Error())
	}
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
	return nil
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
	logQuota := appliedDelta
	if effect.Quota > 0 {
		logQuota = effect.Quota
	}
	if logQuota != 0 {
		other := effect.Other
		if other == nil {
			other = make(map[string]interface{})
		}
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
			TokenId: effect.TokenID, Group: effect.Group, Other: other, NodeName: effect.NodeName,
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
		if effect.Quota > 0 {
			usageQuota = effect.Quota
		}
		if effect.UpdateUsage && usageQuota > 0 {
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

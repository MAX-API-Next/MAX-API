package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo               *relaycommon.RelayInfo
	funding                 FundingSource
	preConsumedQuota        int   // 实际预扣额度（信任用户可能为 0）
	tokenConsumed           int   // 令牌额度实际扣减量
	extraReserved           int   // 发送前补充预扣的额度（订阅退款时需要单独回滚）
	trusted                 bool  // 是否命中信任额度旁路
	fundingSettled          bool  // funding.Settle 已成功，资金来源已提交
	appliedFundingDelta     int64 // 已提交、尚未完成令牌对账的资金差额
	compensationFailed      bool  // 资金补偿返回错误且结果不确定；后续仅重试令牌
	fundingReconcilePending bool  // 已知部分补偿已生效；重试令牌前先补齐资金差额
	reserveRevision         int64 // 单次请求内累计预留的单调修订号
	settled                 bool  // Settle 全部完成（资金 + 令牌）
	refunded                bool  // Refund 已调用
	refundInFlight          bool
	settleInFlight          bool
	fundingOutcomeUnknown   bool
	mu                      sync.Mutex
}

// ErrBillingFundingOutcomeUnknown means a non-durable funding mutation did
// not return a fully confirmed result. Such a mutation must not be retried
// blindly because the provider may already have committed it.
var ErrBillingFundingOutcomeUnknown = errors.New("billing funding settlement outcome is unknown")

type SettlementPreparer interface {
	PrepareSettlement(actualQuota int) (*model.BillingSettlementInput, error)
}

var _ SettlementPreparer = (*BillingSession)(nil)

type RefundSettlementPreparer interface {
	PrepareRefundSettlement() (*model.BillingSettlementInput, error)
}

var _ RefundSettlementPreparer = (*BillingSession)(nil)

type billingSettleIntent struct {
	input model.BillingSettlementInput
}

// Settle 根据实际消耗额度进行结算。
// 资金来源和令牌额度分两步提交。若令牌调整失败，会优先反向补偿已提交的
// 资金差额。已确认的部分补偿会记录剩余差额并在重试令牌前补齐；补偿
// 返回错误时结果不明确，不会自动重复该非幂等资金操作，后续仅重试令牌。
func (s *BillingSession) Settle(actualQuota int) error {
	return s.settleWithEffect(actualQuota, nil)
}

func (s *BillingSession) SettleWithEffect(actualQuota int, effect *model.BillingSettlementEffect) error {
	return s.settleWithEffect(actualQuota, effect)
}

func (s *BillingSession) settleWithEffect(actualQuota int, effect *model.BillingSettlementEffect) error {
	var lastErr error
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := s.settleAttempt(actualQuota, effect); err == nil {
			return nil
		} else {
			lastErr = err
			if errors.Is(err, ErrBillingFundingOutcomeUnknown) {
				return err
			}
		}
		if attempt < maxAttempts-1 {
			time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
		}
	}
	return fmt.Errorf("billing settlement failed after %d attempts: %w", maxAttempts, lastErr)
}

func (s *BillingSession) settleAttempt(actualQuota int, effect *model.BillingSettlementEffect) error {
	intent, ok, err := s.beginSettleAttempt(actualQuota, effect)
	if err != nil || !ok {
		return err
	}
	return s.applyDurableSettleIntent(intent)
}

func (s *BillingSession) beginSettleAttempt(actualQuota int, effect *model.BillingSettlementEffect) (*billingSettleIntent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded {
		return nil, false, nil
	}
	if s.fundingOutcomeUnknown {
		return nil, false, ErrBillingFundingOutcomeUnknown
	}
	if s.refundInFlight {
		return nil, false, errors.New("billing refund is already in progress")
	}
	if s.settleInFlight {
		return nil, false, errors.New("billing settlement is already in progress")
	}
	delta := actualQuota - s.preConsumedQuota
	if delta == 0 && effect == nil {
		s.settled = true
		return nil, false, nil
	}
	if input, ok := s.prepareDurableSettlementLocked(actualQuota, effect); ok {
		s.settleInFlight = true
		return &billingSettleIntent{input: *input}, true, nil
	}
	if effect != nil {
		return nil, false, errors.New("billing settlement effects require a durable funding source and request id")
	}
	return nil, false, s.settleNonDurableLocked(delta)
}

func (s *BillingSession) applyDurableSettleIntent(intent *billingSettleIntent) error {
	applied, _, err := model.ApplyBillingSettlementOnce(intent.input)
	if err != nil {
		s.finishDurableSettleIntent(false, 0)
		return err
	}

	var effectErr error
	if intent.input.Effect != nil {
		effectErr = model.ProcessBillingSettlementEffect(intent.input.OperationKey)
	}
	s.finishDurableSettleIntent(true, applied)
	return effectErr
}

func (s *BillingSession) finishDurableSettleIntent(applied bool, appliedDelta int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settleInFlight = false
	if !applied {
		return
	}
	s.appliedFundingDelta = appliedDelta
	s.fundingSettled = true
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += appliedDelta
	}
	s.settled = true
}

func (s *BillingSession) settleNonDurableLocked(delta int) error {
	// 1) 调整资金来源（仅在尚未提交时执行，防止重复调用）
	if !s.fundingSettled {
		applied, err := s.funding.Settle(delta)
		if err != nil || applied != int64(delta) {
			// FundingSource has no durable idempotency contract. Treat both an
			// error and a short/over-applied result as outcome unknown: the
			// mutation may have committed and must never be repeated blindly.
			s.fundingSettled = true
			s.appliedFundingDelta = applied
			s.fundingOutcomeUnknown = true
			if err != nil {
				return fmt.Errorf("%w: %v", ErrBillingFundingOutcomeUnknown, err)
			}
			return fmt.Errorf("%w: requested=%d applied=%d", ErrBillingFundingOutcomeUnknown, delta, applied)
		}
		s.appliedFundingDelta = applied
		s.fundingSettled = true
		s.compensationFailed = false
		s.fundingReconcilePending = false
	} else if s.fundingReconcilePending {
		// A partial compensation leaves a residual funding delta committed.
		// Reconcile that residual to the target before retrying the token step.
		needed := int64(delta) - s.appliedFundingDelta
		if needed != 0 {
			applied, err := s.funding.Settle(int(needed))
			s.appliedFundingDelta += applied
			if err != nil || applied != needed {
				// The funding source has no durable idempotency contract. A
				// failed or partial reconciliation may already have committed;
				// do not repeat it or proceed with the token leg.
				s.fundingOutcomeUnknown = true
				s.fundingReconcilePending = false
				if err != nil {
					return fmt.Errorf("%w: %v", ErrBillingFundingOutcomeUnknown, err)
				}
				return fmt.Errorf("%w: reconciliation requested=%d applied=%d", ErrBillingFundingOutcomeUnknown, needed, applied)
			}
		}
		if s.appliedFundingDelta != int64(delta) {
			return fmt.Errorf("funding reconciliation incomplete: target=%d applied=%d", delta, s.appliedFundingDelta)
		}
		s.fundingReconcilePending = false
	}
	// 2) 调整令牌额度
	var tokenErr error
	if !s.relayInfo.IsPlayground {
		if delta > 0 {
			tokenErr = model.DecreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta)
		} else {
			tokenErr = model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, -delta)
		}
		if tokenErr != nil {
			common.SysLog(fmt.Sprintf("error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s",
				s.relayInfo.UserId, s.relayInfo.TokenId, delta, tokenErr.Error()))
			if s.compensationFailed {
				return tokenErr
			}
			if s.appliedFundingDelta == 0 {
				s.fundingSettled = false
			} else {
				committed := s.appliedFundingDelta
				compensated, compensationErr := s.funding.Settle(-int(committed))
				if compensationErr != nil {
					// A funding error may be an ambiguous commit. Retain the target
					// delta and never repeat this non-idempotent compensation or
					// continue with the token leg.
					s.compensationFailed = true
					s.fundingOutcomeUnknown = true
					s.fundingReconcilePending = false
					common.SysLog(fmt.Sprintf("error compensating funding after token settlement failure (userId=%d, tokenId=%d, delta=%d, applied=%d, compensated=%d): %v",
						s.relayInfo.UserId, s.relayInfo.TokenId, delta, committed, compensated, compensationErr))
				} else if compensated != -committed {
					s.appliedFundingDelta = committed + compensated
					s.compensationFailed = false
					s.fundingOutcomeUnknown = true
					s.fundingReconcilePending = false
					common.SysLog(fmt.Sprintf("incomplete funding compensation after token settlement failure (userId=%d, tokenId=%d, delta=%d, applied=%d, compensated=%d)",
						s.relayInfo.UserId, s.relayInfo.TokenId, delta, committed, compensated))
				} else {
					s.fundingSettled = false
					s.appliedFundingDelta = 0
					s.compensationFailed = false
					s.fundingReconcilePending = false
				}
			}
			return tokenErr
		}
	}
	// 3) 更新 relayInfo 上的订阅 PostDelta（用于日志）
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += s.appliedFundingDelta
	}
	s.compensationFailed = false
	s.fundingReconcilePending = false
	s.settled = true
	return nil
}

// PrepareSettlement returns the exact durable request-finalize intent without
// applying it. Task submission uses this to commit upstream identity and the
// billing intent atomically before releasing the success response.
func (s *BillingSession) PrepareSettlement(actualQuota int) (*model.BillingSettlementInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded {
		return nil, nil
	}
	if actualQuota == s.preConsumedQuota {
		input, ok := s.prepareDurableSettlementLocked(actualQuota, nil)
		if !ok {
			return nil, nil
		}
		return input, nil
	}
	input, ok := s.prepareDurableSettlementLocked(actualQuota, nil)
	if !ok {
		return nil, errors.New("billing settlement intent requires a durable funding source and request id")
	}
	return input, nil
}

func (s *BillingSession) prepareDurableSettlementLocked(actualQuota int, effect *model.BillingSettlementEffect) (*model.BillingSettlementInput, bool) {
	if s.relayInfo == nil || s.relayInfo.RequestId == "" {
		return nil, false
	}
	source, userID, subscriptionID, ok := durableFundingIdentity(s.funding)
	if !ok {
		return nil, false
	}
	delta := actualQuota - s.preConsumedQuota
	tokenDelta := int64(delta)
	if s.relayInfo.IsPlayground {
		tokenDelta = 0
	}
	return &model.BillingSettlementInput{
		OperationKey:                    "request:" + s.relayInfo.RequestId + ":finalize",
		Source:                          source,
		UserID:                          userID,
		SubscriptionID:                  subscriptionID,
		TokenID:                         s.relayInfo.TokenId,
		TokenKey:                        s.relayInfo.TokenKey,
		FundingDelta:                    int64(delta),
		TokenDelta:                      tokenDelta,
		SubscriptionPreConsumeRequestID: subscriptionPreConsumeRequestID(s.funding),
		Effect:                          effect,
	}, true
}

func durableFundingIdentity(funding FundingSource) (source string, userID int, subscriptionID int, ok bool) {
	switch value := funding.(type) {
	case *WalletFunding:
		return model.BillingSettlementSourceWallet, value.userId, 0, true
	case *SubscriptionFunding:
		return model.BillingSettlementSourceSubscription, value.userId, value.subscriptionId, true
	default:
		return "", 0, 0, false
	}
}

type billingRefundIntent struct {
	input         model.BillingSettlementInput
	userID        int
	requestID     string
	tokenConsumed int
	fundingSource string
}

func (s *BillingSession) prepareRefundIntent() (*billingRefundIntent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded || s.refundInFlight || s.settleInFlight || !s.needsRefundLocked() {
		return nil, false
	}
	input, err := s.refundSettlementInputLocked()
	if err != nil {
		common.SysLog(err.Error())
		return nil, false
	}

	s.refundInFlight = true
	return &billingRefundIntent{
		input:         *input,
		userID:        s.relayInfo.UserId,
		requestID:     s.relayInfo.RequestId,
		tokenConsumed: s.tokenConsumed,
		fundingSource: s.funding.Source(),
	}, true
}

func (s *BillingSession) refundSettlementInputLocked() (*model.BillingSettlementInput, error) {
	refundFunding := s.refundFundingAmountLocked()
	refundToken := int64(s.tokenConsumed)
	if s.relayInfo.IsPlayground {
		refundToken = 0
	}
	requestID := s.relayInfo.RequestId
	if requestID == "" || refundFunding <= 0 {
		return nil, fmt.Errorf("billing refund requires manual review (userId=%d, requestId=%q, funding=%d)", s.relayInfo.UserId, requestID, refundFunding)
	}
	source, userID, subscriptionID, ok := durableFundingIdentity(s.funding)
	if !ok {
		return nil, fmt.Errorf("billing refund skipped for non-durable funding source (userId=%d, requestId=%s)", s.relayInfo.UserId, requestID)
	}
	return &model.BillingSettlementInput{
		OperationKey:                    "request:" + requestID + ":finalize",
		Source:                          source,
		UserID:                          userID,
		SubscriptionID:                  subscriptionID,
		TokenID:                         s.relayInfo.TokenId,
		TokenKey:                        s.relayInfo.TokenKey,
		FundingDelta:                    -refundFunding,
		TokenDelta:                      -refundToken,
		SubscriptionPreConsumeRequestID: subscriptionPreConsumeRequestID(s.funding),
		FinalizeSubscriptionPreConsume:  source == model.BillingSettlementSourceSubscription,
		AllowMissingToken:               true,
	}, nil
}

// PrepareRefundSettlement returns the exact durable refund intent without
// applying it. Async submission paths use it to commit a failed task state and
// the refund operation atomically before attempting the balance mutation.
func (s *BillingSession) PrepareRefundSettlement() (*model.BillingSettlementInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded || s.refundInFlight || s.settleInFlight || !s.needsRefundLocked() {
		return nil, nil
	}
	return s.refundSettlementInputLocked()
}

func (s *BillingSession) finishRefundIntent(applied bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refundInFlight = false
	if !applied {
		return
	}
	s.refunded = true
	s.tokenConsumed = 0
	s.extraReserved = 0
	s.preConsumedQuota = 0
}

// Refund 退还所有预扣费。普通钱包/订阅会话必须通过持久化结算记录执行；
// 没有稳定 request id 的自定义资金来源不会被自动修改，避免无法证明幂等时重复退款。
func (s *BillingSession) Refund(c *gin.Context) {
	intent, ok := s.prepareRefundIntent()
	if !ok {
		return
	}

	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费（token_quota=%s, funding=%s）",
		intent.userID,
		logger.FormatQuota(intent.tokenConsumed),
		intent.fundingSource,
	))
	_, _, err := model.ApplyBillingSettlementOnce(intent.input)
	if err != nil {
		common.SysLog(fmt.Sprintf("billing refund remains pending/manual (userId=%d, requestId=%s): %s", intent.userID, intent.requestID, err.Error()))
		s.finishRefundIntent(false)
		return
	}
	s.finishRefundIntent(true)
}

func (s *BillingSession) refundFundingAmountLocked() int64 {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		return int64(funding.consumed)
	case *SubscriptionFunding:
		return funding.preConsumed + int64(s.extraReserved)
	default:
		return int64(s.preConsumedQuota)
	}
}

func subscriptionPreConsumeRequestID(funding FundingSource) string {
	if subscription, ok := funding.(*SubscriptionFunding); ok {
		return subscription.requestId
	}
	return ""
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.fundingSettled || s.settleInFlight {
		// fundingSettled 时资金来源已提交结算，不能再退预扣费
		return false
	}
	if s.tokenConsumed > 0 {
		return true
	}
	// 订阅可能在 tokenConsumed=0 时仍预扣了额度
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	return false
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fundingOutcomeUnknown {
		return ErrBillingFundingOutcomeUnknown
	}
	if s.settled || s.refunded || s.trusted || targetQuota <= s.preConsumedQuota {
		return nil
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	atomicReserved := false
	if s.relayInfo.RequestId != "" {
		if source, userID, subscriptionID, ok := durableFundingIdentity(s.funding); ok {
			tokenDelta := int64(delta)
			if s.relayInfo.IsPlayground {
				tokenDelta = 0
			}
			nextRevision := s.reserveRevision + 1
			if _, _, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
				OperationKey:                    fmt.Sprintf("request:%s:reserve:%d", s.relayInfo.RequestId, nextRevision),
				Source:                          source,
				UserID:                          userID,
				SubscriptionID:                  subscriptionID,
				TokenID:                         s.relayInfo.TokenId,
				TokenKey:                        s.relayInfo.TokenKey,
				FundingDelta:                    int64(delta),
				TokenDelta:                      tokenDelta,
				SubscriptionPreConsumeRequestID: subscriptionPreConsumeRequestID(s.funding),
				ManualOnFailure:                 true,
			}); err != nil {
				return err
			}
			if wallet, ok := s.funding.(*WalletFunding); ok {
				wallet.consumed += delta
			}
			s.reserveRevision = nextRevision
			atomicReserved = true
		}
	}
	if !atomicReserved {
		if err := s.reserveFunding(delta); err != nil {
			return err
		}
		if err := s.reserveToken(delta); err != nil {
			s.rollbackFundingReserve(delta)
			return err
		}
	}

	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.extraReserved += delta
	s.syncRelayInfo()
	return nil
}

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣费入口（含信任额度旁路）
// ---------------------------------------------------------------------------

// preConsume 执行预扣费：信任检查 -> 令牌预扣 -> 资金来源预扣。
// 任一步骤失败时原子回滚已完成的步骤。
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.MaxAPIError {
	effectiveQuota := quota

	// ---- 信任额度旁路 ----
	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, fmt.Sprintf("用户 %d 额度充足, 信任且不需要预扣费 (funding=%s)", s.relayInfo.UserId, s.funding.Source()))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 需要预扣费 %s (funding=%s)", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota), s.funding.Source()))
	}

	atomicPreConsumed := false
	if !s.relayInfo.IsPlayground {
		switch funding := s.funding.(type) {
		case *WalletFunding:
			if s.relayInfo.RequestId == "" {
				if effectiveQuota > 0 {
					return mapAtomicPreConsumeError(errors.New("wallet pre-consume requires a stable request id"))
				}
				break
			}
			applied, _, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
				OperationKey:             "request:" + s.relayInfo.RequestId + ":pre-consume",
				Source:                   model.BillingSettlementSourceWallet,
				UserID:                   s.relayInfo.UserId,
				TokenID:                  s.relayInfo.TokenId,
				TokenKey:                 s.relayInfo.TokenKey,
				FundingDelta:             int64(effectiveQuota),
				TokenDelta:               int64(effectiveQuota),
				ManualOnFailure:          true,
				PreConsumeRequestID:      s.relayInfo.RequestId,
				PreConsumeModelName:      s.relayInfo.OriginModelName,
				PreConsumeRequestedQuota: int64(quota),
				PreConsumeEffectiveQuota: int64(effectiveQuota),
			})
			if err != nil {
				return mapAtomicPreConsumeError(err)
			}
			if applied != int64(effectiveQuota) {
				return mapAtomicPreConsumeError(fmt.Errorf("wallet pre-consume applied unexpected amount: requested=%d applied=%d", effectiveQuota, applied))
			}
			funding.consumed = int(applied)
			s.tokenConsumed = int(applied)
			atomicPreConsumed = true
		case *SubscriptionFunding:
			if effectiveQuota <= 0 {
				break
			}
			res, err := model.PreConsumeTokenAndUserSubscription(
				s.relayInfo.RequestId,
				s.relayInfo.UserId,
				s.relayInfo.TokenId,
				s.relayInfo.TokenKey,
				funding.modelName,
				0,
				int64(effectiveQuota),
			)
			if err != nil {
				return mapAtomicPreConsumeError(err)
			}
			funding.applyPreConsumeResult(res)
			s.tokenConsumed = effectiveQuota
			atomicPreConsumed = true
		}
	}

	// Custom funding sources and playground requests retain their existing
	// hooks; normal wallet/subscription requests use the atomic branches above.
	if !atomicPreConsumed && effectiveQuota > 0 {
		if err := PreConsumeTokenQuota(s.relayInfo, effectiveQuota); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if !s.relayInfo.IsPlayground {
			s.tokenConsumed = effectiveQuota
		}
	}

	if !atomicPreConsumed {
		if err := s.funding.PreConsume(effectiveQuota); err != nil {
			// 预扣费失败，回滚令牌额度
			if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
				if rollbackErr := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, s.tokenConsumed); rollbackErr != nil {
					common.SysLog(fmt.Sprintf("error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s",
						s.relayInfo.UserId, s.relayInfo.TokenId, s.tokenConsumed, err.Error(), rollbackErr.Error()))
				}
				s.tokenConsumed = 0
			}
			return mapAtomicPreConsumeError(err)
		}
	}

	s.preConsumedQuota = effectiveQuota

	// ---- 同步 RelayInfo 兼容字段 ----
	s.syncRelayInfo()

	return nil
}

func mapAtomicPreConsumeError(err error) *types.MaxAPIError {
	if errors.Is(err, model.ErrTokenQuotaInsufficient) {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	if errors.Is(err, model.ErrUserQuotaInsufficient) {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	if errors.Is(err, model.ErrSubscriptionQuotaInsufficient) {
		return types.NewErrorWithStatusCode(fmt.Errorf("订阅额度不足或未配置订阅: %s", err.Error()), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") {
		return types.NewErrorWithStatusCode(fmt.Errorf("订阅额度不足或未配置订阅: %s", errMsg), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

func (s *BillingSession) reserveFunding(delta int) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if err := model.DecreaseUserQuota(funding.userId, delta, false); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		funding.consumed += delta
		return nil
	case *SubscriptionFunding:
		if _, err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, int64(delta)); err != nil {
			return mapAtomicPreConsumeError(err)
		}
		return nil
	default:
		return types.NewError(fmt.Errorf("unsupported funding source: %s", s.funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
}

func (s *BillingSession) rollbackFundingReserve(delta int) {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if err := model.IncreaseUserQuota(funding.userId, delta, false); err != nil {
			common.SysLog("error rolling back wallet funding reserve: " + err.Error())
		} else {
			funding.consumed -= delta
		}
	case *SubscriptionFunding:
		if _, err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, -int64(delta)); err != nil {
			common.SysLog("error rolling back subscription funding reserve: " + err.Error())
		}
	}
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// shouldTrust 统一信任额度检查，适用于钱包和订阅。
func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	// 异步任务（ForcePreConsume=true）必须预扣全额，不允许信任旁路
	if s.relayInfo.ForcePreConsume {
		return false
	}

	trustQuota := common.GetTrustQuota()
	if trustQuota <= 0 {
		return false
	}

	// 检查令牌是否充足
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenQuota := c.GetInt64("token_quota")
		tokenTrusted = tokenQuota > int64(trustQuota)
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return s.relayInfo.UserQuota > int64(trustQuota)
	case BillingSourceSubscription:
		// 订阅不能启用信任旁路。原因：
		// 1. PreConsumeUserSubscription 要求 amount>0 来创建预扣记录并锁定订阅
		// 2. 若信任旁路将 effectiveQuota 设为 0，订阅无法创建有效预扣记录
		return false
	default:
		return false
	}
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionPreConsumed = sub.preConsumed + int64(s.extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(s.extraReserved)
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.MaxAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if blocked, err := model.HasUnresolvedPositiveFinalizeSettlement(relayInfo.UserId); err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	} else if blocked {
		return nil, types.NewErrorWithStatusCode(
			errors.New("存在未完成的计费对账，请勿重复提交请求并联系管理员处理"),
			types.ErrorCodeInsufficientUserQuota,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)

	// 新钱包请求先做友好的额度检查；已持久化的钱包重放必须进入
	// operation-key 校验，不能因扣款后的当前余额变化而切换资金源。
	tryWallet := func(replay bool) (*BillingSession, *types.MaxAPIError) {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !replay && userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if !replay && userQuota-int64(preConsumedQuota) < 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo: relayInfo,
			funding:   &WalletFunding{userId: relayInfo.UserId},
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.MaxAPIError) {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			funding: &SubscriptionFunding{
				requestId: relayInfo.RequestId,
				userId:    relayInfo.UserId,
				modelName: relayInfo.OriginModelName,
			},
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证订阅至少创建 1 额度预扣记录。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	persistedSource, hasPersistedSource, sourceErr := model.ResolveBillingPreConsumeSource(relayInfo.RequestId)
	if sourceErr != nil {
		return nil, types.NewError(sourceErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if hasPersistedSource {
		switch persistedSource {
		case model.BillingSettlementSourceWallet:
			return tryWallet(true)
		case model.BillingSettlementSourceSubscription:
			return trySubscription()
		default:
			return nil, types.NewError(
				fmt.Errorf("unsupported persisted billing source: %s", persistedSource),
				types.ErrorCodeUpdateDataError,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet(false)
	case "wallet_first":
		session, err := tryWallet(false)
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet(false)
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return tryWallet(false)
			}
			return nil, apiErr
		}
		return session, nil
	}
}

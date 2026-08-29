package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

func billingLogContext(ctx *gin.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func billingEffectRequestIDs(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) (string, string) {
	requestID, upstreamRequestID := "", ""
	if ctx != nil {
		requestID = ctx.GetString(common.RequestIdKey)
		upstreamRequestID = ctx.GetString(common.UpstreamRequestIdKey)
	}
	if requestID == "" && relayInfo != nil {
		requestID = relayInfo.RequestId
	}
	return requestID, upstreamRequestID
}

func newConsumeBillingSettlementEffect(
	relayInfo *relaycommon.RelayInfo,
	params model.RecordConsumeLogParams,
	requestID string,
	upstreamRequestID string,
	updateUsage bool,
) *model.BillingSettlementEffect {
	effect := &model.BillingSettlementEffect{
		LogType:           model.LogTypeConsume,
		Content:           params.Content,
		ChannelID:         params.ChannelId,
		ModelName:         params.ModelName,
		TokenID:           params.TokenId,
		TokenName:         params.TokenName,
		Group:             params.Group,
		Other:             params.Other,
		NodeName:          common.NodeName,
		UpdateUsage:       updateUsage,
		Quota:             int64(params.Quota),
		QuotaIsActual:     true,
		PromptTokens:      params.PromptTokens,
		CompletionTokens:  params.CompletionTokens,
		UseTimeSeconds:    params.UseTimeSeconds,
		IsStream:          params.IsStream,
		RequestID:         requestID,
		UpstreamRequestID: upstreamRequestID,
	}
	if relayInfo != nil && relayInfo.BillingSource == BillingSourceSubscription {
		effect.Subscription = &model.BillingSettlementSubscriptionEffect{
			PreConsumed:               relayInfo.SubscriptionPreConsumed,
			AmountTotal:               relayInfo.SubscriptionAmountTotal,
			AmountUsedAfterPreConsume: relayInfo.SubscriptionAmountUsedAfterPreConsume,
		}
	}
	return effect
}

func validatePreConsumedQuota(preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.MaxAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return nil
}

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.MaxAPIError {
	if apiErr := validatePreConsumedQuota(preConsumedQuota, relayInfo); apiErr != nil {
		return apiErr
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	_, err := SettleBillingWithEffect(ctx, relayInfo, actualQuota, nil)
	return err
}

// SettleBillingWithEffect settles funding before projecting usage and logs.
// The returned flag is true when the durable settlement owns the projection;
// callers must not write a second usage/log record in that case, including when
// effect processing is still pending after the funding mutation succeeded.
func SettleBillingWithEffect(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, effect *model.BillingSettlementEffect) (bool, error) {
	if relayInfo == nil {
		return false, fmt.Errorf("relayInfo is nil")
	}
	logCtx := billingLogContext(ctx)
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(logCtx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(logCtx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(logCtx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if effect != nil {
			if settler, ok := relayInfo.Billing.(interface {
				SettleWithEffect(int, *model.BillingSettlementEffect) error
			}); ok {
				if err := settler.SettleWithEffect(actualQuota, effect); err != nil {
					if errors.Is(err, ErrBillingSettlementEffectNotDurable) ||
						errors.Is(err, model.ErrBillingSettlementRecordNotDurable) ||
						errors.Is(err, ErrBillingFundingOutcomeUnknown) {
						return false, err
					}
					return true, err
				}
				if actualQuota != 0 {
					if relayInfo.BillingSource == BillingSourceSubscription {
						checkAndSendSubscriptionQuotaNotify(relayInfo)
					} else {
						checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
					}
				}
				return true, nil
			}
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return false, err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return false, nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if effect != nil && relayInfo.RequestId != "" {
		return postConsumeQuotaOnceWithEffect(relayInfo, "finalize", quotaDelta, relayInfo.FinalPreConsumedQuota, true, effect)
	}
	if quotaDelta != 0 {
		return false, PostConsumeQuotaOnce(relayInfo, "finalize", quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return false, nil
}

// settleAndRecordConsume keeps the successful usage projection behind the
// funding settlement. Durable BillingSession effects replay both the log and
// counters after a transient failure; legacy/custom sessions project locally
// only after SettleBilling succeeds.
func settleAndRecordConsume(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, shouldUpdateUsage bool, params model.RecordConsumeLogParams) {
	logCtx := billingLogContext(ctx)
	if relayInfo == nil {
		logger.LogError(logCtx, "error settling billing: relayInfo is nil")
		return
	}
	requestID, upstreamRequestID := billingEffectRequestIDs(ctx, relayInfo)
	effect := newConsumeBillingSettlementEffect(relayInfo, params, requestID, upstreamRequestID, shouldUpdateUsage)

	effectHandled, err := SettleBillingWithEffect(ctx, relayInfo, params.Quota, effect)
	if err != nil {
		logger.LogError(logCtx, "error settling billing: "+err.Error())
		return
	}
	if effectHandled {
		return
	}
	if params.Other == nil {
		params.Other = make(map[string]interface{})
	}
	appendBillingInfo(relayInfo, params.Other)
	if shouldUpdateUsage {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, params.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, params.Quota)
	}
	model.RecordConsumeLog(ctx, relayInfo.UserId, params)
}

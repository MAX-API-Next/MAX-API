package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/shopspring/decimal"

	"github.com/gin-gonic/gin"
)

const (
	ViolationFeeCodePrefix     = "violation_fee."
	CSAMViolationMarker        = "Failed check: SAFETY_CHECK_TYPE"
	ContentViolatesUsageMarker = "Content violates usage guidelines"
)

func IsViolationFeeCode(code types.ErrorCode) bool {
	return strings.HasPrefix(string(code), ViolationFeeCodePrefix)
}

func HasCSAMViolationMarker(err *types.MaxAPIError) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), CSAMViolationMarker) || strings.Contains(err.Error(), ContentViolatesUsageMarker) {
		return true
	}
	msg := err.ToOpenAIError().Message
	return strings.Contains(msg, CSAMViolationMarker) || strings.Contains(err.Error(), ContentViolatesUsageMarker)
}

func WrapAsViolationFeeGrokCSAM(err *types.MaxAPIError) *types.MaxAPIError {
	if err == nil {
		return nil
	}
	oai := err.ToOpenAIError()
	oai.Type = string(types.ErrorCodeViolationFeeGrokCSAM)
	oai.Code = string(types.ErrorCodeViolationFeeGrokCSAM)
	return types.WithOpenAIError(oai, err.StatusCode, types.ErrOptionWithSkipRetry())
}

// NormalizeViolationFeeError ensures:
// - if the CSAM marker is present, error.code is set to a stable violation-fee code and skip-retry is enabled.
// - if error.code already has the violation-fee prefix, skip-retry is enabled.
//
// It must be called before retry decision logic.
func NormalizeViolationFeeError(err *types.MaxAPIError) *types.MaxAPIError {
	if err == nil {
		return nil
	}

	if HasCSAMViolationMarker(err) {
		return WrapAsViolationFeeGrokCSAM(err)
	}

	if IsViolationFeeCode(err.GetErrorCode()) {
		oai := err.ToOpenAIError()
		return types.WithOpenAIError(oai, err.StatusCode, types.ErrOptionWithSkipRetry())
	}

	return err
}

func shouldChargeViolationFee(err *types.MaxAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetErrorCode() == types.ErrorCodeViolationFeeGrokCSAM {
		return true
	}
	// In case some callers didn't normalize, keep a safety net.
	return HasCSAMViolationMarker(err)
}

func calcViolationFeeQuota(amount, groupRatio float64) int {
	if amount <= 0 {
		return 0
	}
	if groupRatio <= 0 {
		return 0
	}
	quota := decimal.NewFromFloat(amount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(groupRatio)).
		Round(0).
		IntPart()
	if quota <= 0 {
		return 0
	}
	return int(quota)
}

type violationFeeChargeResult struct {
	applicable bool
	settled    bool
}

// HandleFailedBilling finalizes a failed request as either a violation fee or
// a normal refund. Once a violation settlement intent exists, it must not be
// replaced by a refund using the same request-finalize operation key.
func HandleFailedBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.MaxAPIError) {
	result := chargeViolationFeeIfNeeded(ctx, relayInfo, apiErr)
	if !result.applicable && relayInfo != nil && relayInfo.Billing != nil {
		relayInfo.Billing.Refund(ctx)
	}
}

func ChargeViolationFeeIfNeeded(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.MaxAPIError) bool {
	return chargeViolationFeeIfNeeded(ctx, relayInfo, apiErr).settled
}

func chargeViolationFeeIfNeeded(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.MaxAPIError) violationFeeChargeResult {
	if ctx == nil || relayInfo == nil || apiErr == nil {
		return violationFeeChargeResult{}
	}
	//if relayInfo.IsPlayground {
	//	return false
	//}
	if !shouldChargeViolationFee(apiErr) {
		return violationFeeChargeResult{}
	}

	settings := model_setting.GetGrokSettings()
	if settings == nil || !settings.ViolationDeductionEnabled {
		return violationFeeChargeResult{}
	}

	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	feeQuota := calcViolationFeeQuota(settings.ViolationDeductionAmount, groupRatio)
	if feeQuota <= 0 {
		return violationFeeChargeResult{}
	}

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	tokenName := ctx.GetString("token_name")
	oai := apiErr.ToOpenAIError()

	other := map[string]any{
		"violation_fee":        true,
		"violation_fee_code":   string(types.ErrorCodeViolationFeeGrokCSAM),
		"fee_quota":            feeQuota,
		"base_amount":          settings.ViolationDeductionAmount,
		"group_ratio":          groupRatio,
		"status_code":          apiErr.StatusCode,
		"upstream_error_type":  oai.Type,
		"upstream_error_code":  fmt.Sprintf("%v", oai.Code),
		"violation_fee_marker": CSAMViolationMarker,
	}
	requestID, upstreamRequestID := billingEffectRequestIDs(ctx, relayInfo)
	logParams := model.RecordConsumeLogParams{
		ChannelId:      relayInfo.ChannelId,
		ModelName:      relayInfo.OriginModelName,
		TokenName:      tokenName,
		Quota:          feeQuota,
		Content:        "Violation fee charged",
		TokenId:        relayInfo.TokenId,
		UseTimeSeconds: int(useTimeSeconds),
		IsStream:       relayInfo.IsStream,
		Group:          relayInfo.UsingGroup,
		Other:          other,
	}
	effect := newConsumeBillingSettlementEffect(relayInfo, logParams, requestID, upstreamRequestID, true)

	if settler, ok := relayInfo.Billing.(interface {
		SettleWithEffect(int, *model.BillingSettlementEffect) error
	}); ok {
		if err := settler.SettleWithEffect(feeQuota, effect); err != nil {
			logger.LogError(ctx, fmt.Sprintf("violation fee settlement remains pending/manual: %s", err.Error()))
			return violationFeeChargeResult{applicable: true}
		}
		return violationFeeChargeResult{applicable: true, settled: true}
	}

	if err := SettleBilling(ctx, relayInfo, feeQuota); err != nil {
		logger.LogError(ctx, fmt.Sprintf("violation fee settlement remains pending/manual: %s", err.Error()))
		return violationFeeChargeResult{applicable: true}
	}
	model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, feeQuota)
	model.UpdateChannelUsedQuota(relayInfo.ChannelId, feeQuota)

	model.RecordConsumeLog(ctx, relayInfo.UserId, logParams)

	return violationFeeChargeResult{applicable: true, settled: true}
}

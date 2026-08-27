package service

import (
	"fmt"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	pricehelper "github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

// TieredResultWrapper wraps billingexpr.TieredResult for use at the service layer.
type TieredResultWrapper = billingexpr.TieredResult

// BuildTieredTokenParams constructs billingexpr.TokenParams from a dto.Usage,
// normalizing P and C so they mean "tokens not separately priced by the
// expression". Sub-categories (cache, image, audio) are only subtracted
// when the expression references them via their own variable.
//
// GPT-format APIs report prompt_tokens / completion_tokens as totals that
// include all sub-categories (cache, image, audio). Claude-format APIs
// report them as text-only. This function normalizes to text-only when
// sub-categories are separately priced.
func BuildTieredTokenParams(usage *dto.Usage, isClaudeUsageSemantic bool, usedVars map[string]bool) billingexpr.TokenParams {
	p := float64(usage.PromptTokens)
	c := float64(usage.CompletionTokens)
	cr := float64(usage.PromptTokensDetails.CachedTokens)
	cc5m := float64(usage.PromptTokensDetails.CacheCreationTokensTotal())
	cc1h := float64(0)

	if usage.UsageSemantic == "anthropic" {
		cc1h = float64(usage.ClaudeCacheCreation1hTokens)
		cc5m = float64(usage.ClaudeCacheCreation5mTokens)
	}

	img := float64(usage.PromptTokensDetails.ImageTokens)
	ai := float64(usage.PromptTokensDetails.AudioTokens)
	imgO := float64(usage.CompletionTokenDetails.ImageTokens)
	ao := float64(usage.CompletionTokenDetails.AudioTokens)

	// len = total input context length for tier condition evaluation.
	// Non-Claude: prompt_tokens already includes everything.
	// Claude: input_tokens is text-only, so add cache read + cache creation.
	inputLen := p
	if isClaudeUsageSemantic {
		inputLen = p + cr + cc5m + cc1h
	}

	if !isClaudeUsageSemantic {
		if usedVars["cr"] {
			p -= cr
		}
		if usedVars["cc"] {
			p -= cc5m
		}
		if usedVars["cc1h"] {
			p -= cc1h
		}
		if usedVars["img"] {
			p -= img
		}
		if usedVars["ai"] {
			p -= ai
		}
		if usedVars["img_o"] {
			c -= imgO
		}
		if usedVars["ao"] {
			c -= ao
		}
	}

	if p < 0 {
		p = 0
	}
	if c < 0 {
		c = 0
	}

	return billingexpr.TokenParams{
		P:    p,
		C:    c,
		Len:  inputLen,
		CR:   cr,
		CC:   cc5m,
		CC1h: cc1h,
		Img:  img,
		ImgO: imgO,
		AI:   ai,
		AO:   ao,
	}
}

func BuildRealtimeTieredTokenParams(usage *dto.RealtimeUsage, usedVars map[string]bool) billingexpr.TokenParams {
	if usage == nil {
		return billingexpr.TokenParams{}
	}
	normalized := &dto.Usage{
		PromptTokens:           usage.InputTokens,
		CompletionTokens:       usage.OutputTokens,
		TotalTokens:            usage.TotalTokens,
		PromptTokensDetails:    usage.InputTokenDetails,
		CompletionTokenDetails: usage.OutputTokenDetails,
	}
	return BuildTieredTokenParams(normalized, false, usedVars)
}

func refreshTieredBillingGroup(relayInfo *relaycommon.RelayInfo) (*billingexpr.BillingSnapshot, error) {
	if relayInfo == nil {
		return nil, nil
	}
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return nil, nil
	}

	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	if snap.GroupRatio == groupRatio {
		return snap, nil
	}

	estimatedQuotaAfterGroup := snap.EstimatedQuotaBeforeGroup * groupRatio
	estimatedQuota, err := billingexpr.QuotaRoundStrict(estimatedQuotaAfterGroup)
	if err != nil {
		return nil, err
	}
	estimatedQuota, err = pricehelper.ApplyPreConsumedQuotaFloor(estimatedQuota, groupRatio > 0)
	if err != nil {
		return nil, err
	}
	snap.GroupRatio = groupRatio
	snap.EstimatedQuotaAfterGroup = estimatedQuota
	return snap, nil
}

// PrepareTieredBillingForSelectedGroup refreshes routing-dependent tiered
// billing after the final group for this upstream attempt has been selected.
func PrepareTieredBillingForSelectedGroup(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.MaxAPIError {
	snap, err := refreshTieredBillingGroup(relayInfo)
	if err != nil {
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if snap == nil {
		return nil
	}
	targetQuota, quotaErr := AlphaSearchPreConsumeQuota(
		snap.EstimatedQuotaAfterGroup,
		relayInfo,
		relayInfo.PriceData.GroupRatioInfo.GroupRatio,
	)
	if quotaErr != nil {
		return types.NewErrorWithStatusCode(
			quotaErr,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if relayInfo.RelayMode == relayconstant.RelayModeAlphaSearch {
		relayInfo.PriceData.QuotaToPreConsume = targetQuota
	}
	if snap.GroupRatio == 0 {
		return nil
	}

	relayInfo.PriceData.FreeModel = false

	if relayInfo.Billing == nil {
		if c == nil {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("tiered billing switched from free to paid group without request context"),
				types.ErrorCodeModelPriceError,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
		return PreConsumeBilling(c, targetQuota, relayInfo)
	}
	if err := relayInfo.Billing.Reserve(targetQuota); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
	return nil
}

// TryTieredSettle checks if the request uses tiered_expr billing and, if so,
// computes the actual quota using the frozen BillingSnapshot. Returns:
//   - ok=true, quota, result  when tiered billing applies
//   - ok=false, 0, nil        when it doesn't (caller should fall through to existing logic)
func TryTieredSettle(relayInfo *relaycommon.RelayInfo, params billingexpr.TokenParams) (ok bool, quota int, result *billingexpr.TieredResult) {
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return false, 0, nil
	}

	requestInput := billingexpr.RequestInput{}
	if relayInfo.BillingRequestInput != nil {
		requestInput = *relayInfo.BillingRequestInput
	}

	tr, err := billingexpr.ComputeTieredQuotaWithRequest(snap, params, requestInput)
	if err != nil {
		quota = relayInfo.FinalPreConsumedQuota
		if snap.EstimatedQuotaAfterGroup > quota {
			quota = snap.EstimatedQuotaAfterGroup
		}
		common.SysError(fmt.Sprintf("tiered billing settlement expression failed; using frozen fallback quota (requestId=%s, model=%s, preConsumed=%d, estimated=%d, fallback=%d): %v",
			relayInfo.RequestId, relayInfo.OriginModelName, relayInfo.FinalPreConsumedQuota, snap.EstimatedQuotaAfterGroup, quota, err))
		return true, quota, nil
	}

	noteQuotaClamp(relayInfo, tr.Clamp)

	return true, tr.ActualQuotaAfterGroup, &tr
}

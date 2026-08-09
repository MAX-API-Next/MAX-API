package service

import (
	"fmt"
	"math"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// AlphaSearchPreConsumeQuota adds the endpoint's deterministic single-search
// surcharge to the normal model estimate. The same surcharge is recorded only
// after a successful upstream response, so failed requests still refund the
// complete reservation through the existing billing failure path.
func AlphaSearchPreConsumeQuota(baseQuota int, info *relaycommon.RelayInfo, groupRatio float64) (int, error) {
	if info == nil || info.RelayMode != relayconstant.RelayModeAlphaSearch {
		return baseQuota, nil
	}
	if baseQuota < 0 {
		return 0, fmt.Errorf("alpha search pre-consume quota cannot be negative: %d", baseQuota)
	}
	if math.IsNaN(groupRatio) || math.IsInf(groupRatio, 0) || groupRatio < 0 {
		return 0, fmt.Errorf("alpha search group ratio is invalid: %v", groupRatio)
	}

	pricePerThousand := operation_setting.GetToolPriceForModel(dto.BuildInToolWebSearchPreview, info.OriginModelName)
	if math.IsNaN(pricePerThousand) || math.IsInf(pricePerThousand, 0) || pricePerThousand < 0 {
		return 0, fmt.Errorf("alpha search tool price is invalid: %v", pricePerThousand)
	}
	if pricePerThousand == 0 || groupRatio == 0 {
		return baseQuota, nil
	}

	surcharge := decimal.NewFromFloat(pricePerThousand).
		Div(decimal.NewFromInt(1000)).
		Mul(decimal.NewFromFloat(groupRatio)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	total := decimal.NewFromInt(int64(baseQuota)).Add(surcharge)
	quota, clamp := common.QuotaFromDecimalChecked(total)
	if clamp != nil {
		return 0, fmt.Errorf("alpha search pre-consume quota is out of range: %w", clamp)
	}
	return quota, nil
}

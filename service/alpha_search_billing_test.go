package service

import (
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchPreConsumeQuotaMatchesSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "o1",
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					ToolName:  dto.BuildInToolWebSearchPreview,
					CallCount: 1,
				},
			},
		},
	}
	info.PriceData.AddOtherRatio("n", 3)

	preConsumedQuota, err := AlphaSearchPreConsumeQuota(0, info, 1)
	require.NoError(t, err)

	settlement := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.Equal(t, settlement.Quota, preConsumedQuota)
	require.Equal(t, common.QuotaFromDecimal(settlement.ToolCallSurchargeQuota), preConsumedQuota)
}

func TestAlphaSearchPreConsumeQuotaLeavesOtherModesUnchanged(t *testing.T) {
	quota, err := AlphaSearchPreConsumeQuota(123, &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "o1",
	}, 1)

	require.NoError(t, err)
	require.Equal(t, 123, quota)
}

func TestAlphaSearchPreConsumeQuotaRejectsOverflow(t *testing.T) {
	_, err := AlphaSearchPreConsumeQuota(common.MaxQuota, &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "o1",
	}, 1)

	require.Error(t, err)
}

func TestPrepareTieredAlphaSearchBillingKeepsSurchargeAfterGroupChange(t *testing.T) {
	const expr = `tier("base", p)`
	billing := &recordingBillingSettler{preConsumed: 6_000}
	info := &relaycommon.RelayInfo{
		RelayMode:             relayconstant.RelayModeAlphaSearch,
		OriginModelName:       "o1",
		Billing:               billing,
		FinalPreConsumedQuota: 6_000,
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                expr,
			ExprHash:                  billingexpr.ExprHashString(expr),
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 1_000,
			EstimatedQuotaAfterGroup:  1_000,
			QuotaPerUnit:              common.QuotaPerUnit,
		},
	}

	expected, err := AlphaSearchPreConsumeQuota(2_000, info, 2)
	require.NoError(t, err)
	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, info))

	require.Equal(t, []int{expected}, billing.reserves)
	require.Equal(t, expected, info.FinalPreConsumedQuota)
	require.Equal(t, expected, info.PriceData.QuotaToPreConsume)
	require.Equal(t, 2_000, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
}

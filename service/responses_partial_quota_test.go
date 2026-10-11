package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesPartialZeroDoesNotBroadenReservationFallbackPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name                                     string
		usage                                    *dto.Usage
		successful, chat, expressionError, audio bool
		quota                                    int
	}{
		{name: "unknown", usage: &dto.Usage{}, quota: 100},
		{name: "unreported_zero", usage: &dto.Usage{BillingUsage: &dto.BillingUsage{Source: dto.BillingUsageSourceOAIChat}}, quota: 100},
		{name: "negative_counts", usage: &dto.Usage{PromptTokens: -1, CompletionTokens: -1, TotalTokens: -2, BillingUsage: dto.NewReportedOpenAIChatBillingUsage(&dto.Usage{PromptTokens: -1, CompletionTokens: -1, TotalTokens: -2})}, quota: 100},
		{name: "negative_canonical_counts", usage: &dto.Usage{BillingUsage: dto.NewReportedOpenAIChatBillingUsage(&dto.Usage{PromptTokens: -1, CompletionTokens: -1, TotalTokens: -2})}, quota: 100},
		{name: "reported_zero", usage: &dto.Usage{BillingUsage: dto.NewReportedOpenAIChatBillingUsage(&dto.Usage{})}, quota: 0},
		{name: "known_free", usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}, quota: 0},
		{name: "success_policy", usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}, successful: true, quota: 100},
		{name: "chat_policy", usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}, chat: true, quota: 100},
		{name: "expression_error", usage: &dto.Usage{BillingUsage: dto.NewReportedOpenAIChatBillingUsage(&dto.Usage{})}, expressionError: true, quota: 100},
		{name: "audio_free", usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, PromptTokensDetails: dto.InputTokenDetails{AudioTokens: 10}, CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 2}}, audio: true, quota: 0},
		{name: "audio_expression_error", usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, PromptTokensDetails: dto.InputTokenDetails{AudioTokens: 10}, CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 2}}, audio: true, expressionError: true, quota: 100},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 171, 1000)
			seedToken(t, 172, 171, "synthetic-responses-policy-key", 1000)
			seedChannel(t, 173)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			requestID := "responses-policy-" + scenario.name
			c.Set(common.RequestIdKey, requestID)
			info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 171, TokenId: 172, TokenKey: "synthetic-responses-policy-key", ForcePreConsume: true,
				IsStream: true, RelayMode: relayconstant.RelayModeResponses, StartTime: time.Now(), OriginModelName: "test-model", UsingGroup: "default",
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 173}, UserSetting: dto.UserSetting{BillingPreference: "wallet_only", QuotaWarningThreshold: 1},
				StreamStatus: relaycommon.NewStreamStatus(), PriceData: types.PriceData{ModelRatio: 0, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, nil)
			if scenario.chat {
				info.RelayMode = relayconstant.RelayModeChatCompletions
			}
			if scenario.expressionError {
				expr := `unknown_function()`
				info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), EstimatedQuotaAfterGroup: 100}
			}
			require.Nil(t, PreConsumeBilling(c, 100, info))
			if scenario.audio {
				postAudioConsumeQuota(c, info, scenario.usage, "partial upstream response", false)
			} else if scenario.successful {
				PostTextConsumeQuota(c, info, scenario.usage, nil)
			} else {
				PostPartialConsumeQuota(c, info, scenario.usage)
			}
			info.Billing.Refund(c)
			var user model.User
			require.NoError(t, model.DB.First(&user, 171).Error)
			var token model.Token
			require.NoError(t, model.DB.First(&token, 172).Error)
			require.EqualValues(t, 1000-scenario.quota, user.Quota)
			require.EqualValues(t, 1000-scenario.quota, token.RemainQuota)
			var settlement model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
			require.EqualValues(t, scenario.quota-100, settlement.FundingDelta)
			require.EqualValues(t, scenario.quota-100, settlement.TokenDelta)
			require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
			require.Equal(t, scenario.quota, getLastLog(t).Quota)
		})
	}
}

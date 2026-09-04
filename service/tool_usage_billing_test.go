package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func setServiceToolPricesForTest(t *testing.T, additions map[string]float64) {
	t.Helper()

	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	original := make(map[string]float64, len(setting.Prices))
	for name, price := range setting.Prices {
		original[name] = price
	}
	setting.Prices = make(map[string]float64, len(original)+len(additions))
	for name, price := range original {
		setting.Prices[name] = price
	}
	for name, price := range additions {
		setting.Prices[name] = price
	}
	operation_setting.RebuildToolPriceIndex()

	t.Cleanup(func() {
		setting.Prices = original
		operation_setting.RebuildToolPriceIndex()
	})
}

func newToolSettlementInfo(t *testing.T, requestID string, userID, tokenID, channelID int, tokenKey string, initialQuota, preConsumedQuota int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(common.RequestIdKey, requestID)
	ctx.Set("token_quota", int64(initialQuota))

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialQuota)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		RequestId:               requestID,
		UserId:                  userID,
		TokenId:                 tokenID,
		TokenKey:                tokenKey,
		OriginModelName:         "gpt-test",
		StartTime:               time.Now(),
		UserSetting:             dto.UserSetting{BillingPreference: "wallet_only"},
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		UsingGroup:              "default",
		ForcePreConsume:         true,
		ChannelMeta:             &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ToolUsage: relaycommon.NewToolUsageLedger("gpt-test"),
	}
	session, apiErr := NewBillingSession(ctx, info, preConsumedQuota)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	info.Billing = session
	return ctx, info
}

func assertSingleToolSettlement(t *testing.T, requestID string, userID, tokenID, channelID, initialQuota, expectedQuota int, expectedTool string) {
	t.Helper()
	require.EqualValues(t, initialQuota-expectedQuota, getUserQuota(t, userID))
	require.Equal(t, initialQuota-expectedQuota, getTokenRemainQuota(t, tokenID))

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	require.EqualValues(t, expectedQuota, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
	require.EqualValues(t, expectedQuota, channel.UsedQuota)
	require.Equal(t, int64(1), countLogs(t))

	log := getLastLog(t)
	require.Equal(t, expectedQuota, log.Quota)
	require.Equal(t, requestID, log.RequestId)
	var other map[string]any
	require.NoError(t, common.Unmarshal([]byte(log.Other), &other))
	require.Equal(t, "gpt-test", other["tool_billing_model"])
	require.NotZero(t, other["tool_price_version"])
	items, ok := other["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)
	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, expectedTool, item["name"])
	require.Equal(t, float64(1), item["call_count"])
}

func TestPostTextConsumeQuotaZeroTokensKeepsOnlyWinningRetryToolAndSettlesOnce(t *testing.T) {
	setServiceToolPricesForTest(t, map[string]float64{"failed_tool": 5, "winning_tool": 7})
	const requestID = "zero-token-tool-retry-request"
	const userID, tokenID, channelID = 701, 702, 703
	const initialQuota, preConsumedQuota = 100_000, 100
	ctx, info := newToolSettlementInfo(t, requestID, userID, tokenID, channelID, "zero-token-tool-retry-key", initialQuota, preConsumedQuota)

	info.RetryIndex = 0
	info.ToolUsage.BeginAttempt(0)
	require.True(t, info.ObserveCustomToolCall("failed_tool", relaycommon.ToolCallIdentity{Scope: "openai-chat", CallID: "failed-call"}))
	info.RetryIndex = 1
	info.ToolUsage.BeginAttempt(1)
	require.True(t, info.ObserveCustomToolCall("winning_tool", relaycommon.ToolCallIdentity{Scope: "openai-chat", CallID: "winning-call"}))

	expectedQuota := common.QuotaFromDecimal(decimal.NewFromFloat(7).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	PostTextConsumeQuota(ctx, info, &dto.Usage{}, nil)
	assertSingleToolSettlement(t, requestID, userID, tokenID, channelID, initialQuota, expectedQuota, "winning_tool")

	PostTextConsumeQuota(ctx, info, &dto.Usage{}, nil)
	assertSingleToolSettlement(t, requestID, userID, tokenID, channelID, initialQuota, expectedQuota, "winning_tool")
}

func TestPostAudioConsumeQuotaZeroTokensBillsCustomToolOnce(t *testing.T) {
	setServiceToolPricesForTest(t, map[string]float64{"transcribe_lookup": 9})
	const requestID = "zero-token-audio-tool-request"
	const userID, tokenID, channelID = 711, 712, 713
	const initialQuota, preConsumedQuota = 100_000, 100
	ctx, info := newToolSettlementInfo(t, requestID, userID, tokenID, channelID, "zero-token-audio-tool-key", initialQuota, preConsumedQuota)

	info.ToolUsage.BeginAttempt(0)
	require.True(t, info.ObserveCustomToolCall("transcribe_lookup", relaycommon.ToolCallIdentity{Scope: "openai-responses", CallID: "call-1"}))

	expectedQuota := common.QuotaFromDecimal(decimal.NewFromFloat(9).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	PostAudioConsumeQuota(ctx, info, &dto.Usage{}, "")
	assertSingleToolSettlement(t, requestID, userID, tokenID, channelID, initialQuota, expectedQuota, "transcribe_lookup")

	PostAudioConsumeQuota(ctx, info, &dto.Usage{}, "")
	assertSingleToolSettlement(t, requestID, userID, tokenID, channelID, initialQuota, expectedQuota, "transcribe_lookup")
}

func TestCalculateTextToolCallSurchargeSkipsPerRequestModels(t *testing.T) {
	setServiceToolPricesForTest(t, map[string]float64{"lookup": 9})
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test",
		PriceData:       types.PriceData{UsePrice: true},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: {ToolName: dto.BuildInToolWebSearchPreview, CallCount: 1},
		}},
		ToolUsage: relaycommon.NewToolUsageLedger("gpt-test"),
	}
	info.ToolUsage.BeginAttempt(0)
	require.True(t, info.ObserveCustomToolCall("lookup", relaycommon.ToolCallIdentity{Scope: "openai-chat", CallID: "call-1"}))
	require.True(t, info.CommitToolUsageAttempt())

	summary := textQuotaSummary{ModelName: "gpt-test", GroupRatio: 1}
	require.True(t, calculateTextToolCallSurcharge(ctx, info, &summary).IsZero())
	require.Empty(t, summary.CustomToolUsage.Items)
}

func TestCalculateTextQuotaSummaryBillsProjectedResponsesBuiltInToolOnce(t *testing.T) {
	setServiceToolPricesForTest(t, map[string]float64{
		dto.BuildInToolWebSearchPreview: 5,
		dto.BuildInToolFileSearch:       7,
	})
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: {ToolName: dto.BuildInToolWebSearchPreview},
			dto.BuildInToolFileSearch:       {ToolName: dto.BuildInToolFileSearch},
		}},
		ToolUsage: relaycommon.NewToolUsageLedger("gpt-test"),
		StartTime: time.Now(),
	}
	info.ToolUsage.BeginAttempt(0)
	require.True(t, info.ObserveBuiltInToolCall(dto.BuildInToolWebSearchPreview, relaycommon.ToolCallIdentity{
		Scope:    "openai-responses",
		CallID:   "web-1",
		Position: "output:0",
	}))
	require.False(t, info.ObserveBuiltInToolCall(dto.BuildInToolWebSearchPreview, relaycommon.ToolCallIdentity{
		Scope:    "openai-responses",
		CallID:   "web-1",
		Position: "output:0",
	}))
	require.True(t, info.ObserveBuiltInToolCall(dto.BuildInToolFileSearch, relaycommon.ToolCallIdentity{
		Scope:    "openai-responses",
		CallID:   "file-1",
		Position: "output:1",
	}))

	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	expected := decimal.NewFromFloat(5 + 7).
		Div(decimal.NewFromInt(1000)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	require.Equal(t, 1, summary.WebSearchCallCount)
	require.Equal(t, 1, summary.FileSearchCallCount)
	require.True(t, expected.Equal(summary.ToolCallSurchargeQuota))
}

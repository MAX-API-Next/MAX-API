package service

import (
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service/openaicompat"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecimalToQuotaSaturation(t *testing.T) {
	overflowing := decimal.NewFromInt(2000).Mul(decimal.NewFromFloat(1.8446744073686647e19))
	require.Equal(t, math.MaxInt32, decimalToQuota(overflowing))
	require.Equal(t, math.MinInt32, decimalToQuota(overflowing.Neg()))
	require.Equal(t, 42, decimalToQuota(decimal.NewFromFloat(41.7)))
}

func TestCalculateTextQuotaSummaryUnifiedForClaudeSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	priceData := types.PriceData{
		ModelRatio:           1,
		CompletionRatio:      2,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}

	chatRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	messageRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}

	chatSummary := calculateTextQuotaSummary(ctx, chatRelayInfo, usage)
	messageSummary := calculateTextQuotaSummary(ctx, messageRelayInfo, usage)

	require.Equal(t, messageSummary.Quota, chatSummary.Quota)
	require.Equal(t, messageSummary.CacheCreationTokens5m, chatSummary.CacheCreationTokens5m)
	require.Equal(t, messageSummary.CacheCreationTokens1h, chatSummary.CacheCreationTokens1h)
	require.True(t, chatSummary.IsClaudeUsageSemantic)
	require.Equal(t, 1488, chatSummary.Quota)
}

func TestStreamFallbackQuotaUsesPreConsumedQuotaOnAbnormalStreamEnd(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		IsStream:              true,
		FinalPreConsumedQuota: 300,
		PriceData: types.PriceData{
			QuotaToPreConsume: 500,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)

	quota, ok := streamFallbackQuota(relayInfo, 0)

	require.True(t, ok)
	require.Equal(t, 300, quota)
}

func TestStreamFallbackQuotaFallsBackToPriceDataPreConsume(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		IsStream: true,
		PriceData: types.PriceData{
			QuotaToPreConsume: 500,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)

	quota, ok := streamFallbackQuota(relayInfo, 0)

	require.True(t, ok)
	require.Equal(t, 500, quota)
}

func TestStreamFallbackQuotaSkipsNormalEndAndExistingQuota(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		IsStream:              true,
		FinalPreConsumedQuota: 300,
		PriceData: types.PriceData{
			QuotaToPreConsume: 500,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)

	quota, ok := streamFallbackQuota(relayInfo, 0)
	require.False(t, ok)
	require.Equal(t, 0, quota)

	relayInfo.StreamStatus = relaycommon.NewStreamStatus()
	relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	quota, ok = streamFallbackQuota(relayInfo, 200)
	require.False(t, ok)
	require.Equal(t, 200, quota)
}

func TestPostTextConsumeQuotaUpdatesUsageStatsForStreamFallback(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	const userID = 101
	const channelID = 201
	const fallbackQuota = 300

	seedUser(t, userID, 10000)
	seedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:                  userID,
		OriginModelName:         "test-model",
		StartTime:               time.Now(),
		IsStream:                true,
		IsPlayground:            true,
		FinalPreConsumedQuota:   fallbackQuota,
		StreamStatus:            relaycommon.NewStreamStatus(),
		UsingGroup:              "default",
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
		PriceData: types.PriceData{
			ModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
	relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)

	PostTextConsumeQuota(ctx, relayInfo, &dto.Usage{}, nil)

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", userID).First(&user).Error)
	require.EqualValues(t, fallbackQuota, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)

	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", channelID).First(&channel).Error)
	require.Equal(t, int64(fallbackQuota), channel.UsedQuota)

	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, model.LogTypeConsume, log.Type)
	require.Equal(t, fallbackQuota, log.Quota)
}

func TestPostTextConsumeQuotaUsesFallbackForNilUsageWithEstimate(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	const userID = 102
	const channelID = 202
	const fallbackQuota = 300

	seedUser(t, userID, 10000)
	seedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:                  userID,
		OriginModelName:         "test-model",
		StartTime:               time.Now(),
		IsStream:                true,
		IsPlayground:            true,
		FinalPreConsumedQuota:   fallbackQuota,
		StreamStatus:            relaycommon.NewStreamStatus(),
		UsingGroup:              "default",
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
		PriceData: types.PriceData{
			ModelRatio: 2,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
	relayInfo.SetEstimatePromptTokens(20)
	relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)

	PostTextConsumeQuota(ctx, relayInfo, nil, nil)

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", userID).First(&user).Error)
	require.EqualValues(t, fallbackQuota, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)

	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", channelID).First(&channel).Error)
	require.Equal(t, int64(fallbackQuota), channel.UsedQuota)

	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, fallbackQuota, log.Quota)
	require.Equal(t, 20, log.PromptTokens)
}

func TestSettleAndRecordConsumeLeavesUsageProjectionPendingWhenFinalChargeExceedsWallet(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 121, 122, 123
	const preConsumedQuota, actualQuota = 100, 200

	seedUser(t, userID, preConsumedQuota)
	seedToken(t, tokenID, userID, "final-charge-insufficient-token", preConsumedQuota)
	seedChannel(t, channelID)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", int64(preConsumedQuota))
	info := &relaycommon.RelayInfo{
		RequestId:       "final-charge-insufficient-request",
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "final-charge-insufficient-token",
		OriginModelName: "final-charge-insufficient-model",
		UsingGroup:      "default",
		ForcePreConsume: true,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
	}

	session, apiErr := NewBillingSession(ctx, info, preConsumedQuota)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	info.Billing = session

	settleAndRecordConsume(ctx, info, true, model.RecordConsumeLogParams{
		ChannelId: channelID,
		ModelName: "final-charge-insufficient-model",
		TokenId:   tokenID,
		Group:     "default",
		Quota:     actualQuota,
		Content:   "final charge exceeds remaining wallet",
	})

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
	assert.Zero(t, channel.UsedQuota)
	assert.Zero(t, countLogs(t))

	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:final-charge-insufficient-request:finalize").First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
	assert.Contains(t, settlement.LastError, "user quota is not enough")
}

func TestSettleAndRecordConsumeProjectsExplicitZeroFinalQuota(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 124, 125, 126
	const initialQuota, preConsumedQuota = 1_000, 100

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "zero-final-quota-token", initialQuota)
	seedChannel(t, channelID)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(common.RequestIdKey, "zero-final-quota-request")
	ctx.Set("token_quota", int64(initialQuota))
	info := &relaycommon.RelayInfo{
		RequestId:       "zero-final-quota-request",
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "zero-final-quota-token",
		OriginModelName: "zero-final-quota-model",
		UsingGroup:      "default",
		ForcePreConsume: true,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
	}

	session, apiErr := NewBillingSession(ctx, info, preConsumedQuota)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	info.Billing = session

	settleAndRecordConsume(ctx, info, true, model.RecordConsumeLogParams{
		ChannelId:    channelID,
		PromptTokens: 1,
		ModelName:    "zero-final-quota-model",
		TokenId:      tokenID,
		Group:        "default",
		Quota:        0,
		Content:      "billable usage rounded to zero quota",
	})

	assert.EqualValues(t, initialQuota, getUserQuota(t, userID))
	assert.EqualValues(t, initialQuota, getTokenRemainQuota(t, tokenID))
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
	assert.Zero(t, channel.UsedQuota)
	assert.Equal(t, int64(1), countLogs(t))
	log := getLastLog(t)
	assert.Zero(t, log.Quota)
	assert.Equal(t, 1, log.PromptTokens)
	assert.Equal(t, "zero-final-quota-request", log.RequestId)
}

func TestPostTextConsumeQuotaDoesNotProjectUsageWhenFinalizeSettlementFails(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set(common.RequestIdKey, "failed-finalize-text-request")

	const userID, tokenID, channelID = 103, 203, 303
	seedUser(t, userID, 15)
	seedToken(t, tokenID, userID, "failed-finalize-text-token", 100)
	seedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:               "failed-finalize-text-request",
		UserId:                  userID,
		TokenId:                 tokenID,
		TokenKey:                "failed-finalize-text-token",
		OriginModelName:         "test-model",
		StartTime:               time.Now(),
		UserSetting:             dto.UserSetting{BillingPreference: "wallet_only"},
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		UsingGroup:              "default",
		ChannelMeta:             &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	session, apiErr := NewBillingSession(ctx, relayInfo, 10)
	require.Nil(t, apiErr)
	relayInfo.Billing = session

	PostTextConsumeQuota(ctx, relayInfo, &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 10,
		TotalTokens:      20,
	}, nil)

	assert.EqualValues(t, 5, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
	assert.Zero(t, channel.UsedQuota)
	assert.Zero(t, countLogs(t))

	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:failed-finalize-text-request:finalize").First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
	assert.NotEmpty(t, settlement.EffectPayload)
	assert.Empty(t, settlement.EffectStatus)

	// Manual review/reconciliation makes the missing funding available and
	// requeues the durable settlement. Its effect must then project exactly once.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", 15).Error)
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("id = ?", settlement.ID).
		Updates(map[string]interface{}{"status": model.BillingSettlementStatusPending, "next_attempt": time.Now().Unix()}).Error)
	model.ProcessPendingBillingSettlementsOnce()

	assert.EqualValues(t, 5, getUserQuota(t, userID))
	var projectedUser model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&projectedUser, userID).Error)
	assert.EqualValues(t, 20, projectedUser.UsedQuota)
	assert.Equal(t, 1, projectedUser.RequestCount)
	var projectedChannel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&projectedChannel, channelID).Error)
	assert.EqualValues(t, 20, projectedChannel.UsedQuota)
	assert.Equal(t, int64(1), countLogs(t))
	log := getLastLog(t)
	assert.Equal(t, 20, log.Quota)
	assert.Equal(t, 10, log.PromptTokens)
	assert.Equal(t, 10, log.CompletionTokens)
	assert.Equal(t, "failed-finalize-text-request", log.RequestId)

	model.ProcessPendingBillingSettlementsOnce()
	var replayedUser model.User
	require.NoError(t, model.DB.Select("used_quota").First(&replayedUser, userID).Error)
	assert.EqualValues(t, 20, replayedUser.UsedQuota)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestCalculateTextQuotaSummaryUsesSplitClaudeCacheCreationRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           0,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 2,
			CacheCreation1hRatio: 3,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 10,
		},
		ClaudeCacheCreation5mTokens: 2,
		ClaudeCacheCreation1hTokens: 3,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 100 + remaining(5)*1 + 2*2 + 3*3 = 118
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesAnthropicUsageSemanticFromUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, "anthropic", summary.UsageSemantic)
	require.Equal(t, 1488, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesClaudeBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
			InputTokens:              70,
			CacheReadInputTokens:     30,
			CacheCreationInputTokens: 20,
			OutputTokens:             7,
			CacheCreation: &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: 12,
				Ephemeral1hInputTokens: 8,
			},
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, dto.BillingUsageSemanticAnthropic, summary.UsageSemantic)
	require.Equal(t, 70, summary.PromptTokens)
	require.Equal(t, 7, summary.CompletionTokens)
	require.Equal(t, 30, summary.CacheTokens)
	require.Equal(t, 20, summary.CacheCreationTokens)
	require.Equal(t, 12, summary.CacheCreationTokens5m)
	require.Equal(t, 8, summary.CacheCreationTokens1h)
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesGeminiBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-2.5-flash",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
			PromptTokenCount:        100,
			ToolUsePromptTokenCount: 5,
			CandidatesTokenCount:    20,
			ThoughtsTokenCount:      3,
			TotalTokenCount:         128,
			CachedContentTokenCount: 7,
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.False(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, dto.BillingUsageSemanticGemini, summary.UsageSemantic)
	require.Equal(t, 105, summary.PromptTokens)
	require.Equal(t, 23, summary.CompletionTokens)
	require.Equal(t, 7, summary.CacheTokens)
	require.Equal(t, 128, summary.TotalTokens)
	require.Equal(t, 145, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesOpenAIBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gpt-4o",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{
			PromptTokens:     80,
			CompletionTokens: 9,
			TotalTokens:      89,
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.False(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, dto.BillingUsageSemanticOpenAI, summary.UsageSemantic)
	require.Equal(t, 80, summary.PromptTokens)
	require.Equal(t, 9, summary.CompletionTokens)
	require.Equal(t, 89, summary.TotalTokens)
	require.Equal(t, 98, summary.Quota)
}

func TestCalculateTextQuotaSummaryPreservesResponsesInputDetailsThroughBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	chatResp, usage, err := openaicompat.ResponsesResponseToChatCompletionsResponse(&dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		Model:     "gpt-test",
		CreatedAt: 456,
		Status:    []byte(`"completed"`),
		Usage: &dto.Usage{
			InputTokens:  100,
			OutputTokens: 10,
			TotalTokens:  110,
			InputTokensDetails: &dto.InputTokenDetails{
				CachedTokens:     5,
				CacheWriteTokens: 11,
				ImageTokens:      7,
				AudioTokens:      13,
			},
		},
		Output: []dto.ResponsesOutput{{
			Type: "message",
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: "done",
			}},
		}},
	}, "chatcmpl_1")
	require.NoError(t, err)
	require.Equal(t, usage.PromptTokensDetails, chatResp.Usage.PromptTokensDetails)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-test",
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    2,
			CacheRatio:         0,
			CacheCreationRatio: 4,
			ImageRatio:         3,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.Equal(t, dto.BillingUsageSemanticOpenAI, summary.UsageSemantic)
	require.Equal(t, 100, summary.PromptTokens)
	require.Equal(t, 10, summary.CompletionTokens)
	require.Equal(t, 5, summary.CacheTokens)
	require.Equal(t, 11, summary.CacheCreationTokens)
	require.Equal(t, 7, summary.ImageTokens)
	require.Equal(t, 13, summary.AudioTokens)
	require.Equal(t, 162, summary.Quota)
}

func TestUsageBillingPathForLog(t *testing.T) {
	require.Equal(t, usageBillingPathLocal, usageBillingPathForLog(true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.Equal(t, usageBillingPathUpstream, usageBillingPathForLog(false, &dto.Usage{}))
	require.Equal(t, usageBillingPathOpenAI, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
	require.Equal(t, usageBillingPathAnthropic, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.Equal(t, usageBillingPathGemini, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1}),
	}))
	require.Equal(t, usageBillingPathGeminiEstimated, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewEstimatedGeminiChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
}

func TestAppendUsageBillingPathForLogWritesAdminInfo(t *testing.T) {
	other := map[string]interface{}{
		"admin_info": map[string]interface{}{},
	}
	appendUsageBillingPathForLog(other, false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	})

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, usageBillingPathAnthropic, adminInfo["usage_billing_path"])

	other = map[string]interface{}{}
	appendUsageBillingPathForLog(other, true, nil)
	adminInfo, ok = other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, usageBillingPathLocal, adminInfo["usage_billing_path"])
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := textQuotaSummary{CacheCreationTokens: 50}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, cacheWriteTokensTotal(summary))
	})
}

func TestCalculateTextQuotaSummaryHandlesLegacyClaudeDerivedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      5,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     62,
		CompletionTokens: 95,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3544,
		},
		ClaudeCacheCreation5mTokens: 586,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 62 + 3544*0.1 + 586*1.25 + 95*5 = 1624.9 => 1624
	require.Equal(t, 1624, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheReadFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenRouter OpenAI-format display keeps prompt_tokens as total input,
	// but billing still separates normal input from cache read tokens.
	// quota = (2604 - 2432) + 2432*0.1 + 383 = 798.2 => 798
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheCreationFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 100,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// prompt_tokens is still logged as total input, but cache creation is billed separately.
	// quota = (2604 - 100) + 100*1.25 + 383 = 3012
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 3012, summary.Quota)
}

func TestCalculateTextQuotaSummaryKeepsPrePRClaudeOpenRouterBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "anthropic/claude-3.7-sonnet",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Pre-PR PostClaudeConsumeQuota behavior for OpenRouter:
	// prompt = 2604 - 2432 = 172
	// quota = 172 + 2432*0.1 + 383 = 798.2 => 798
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 172, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestComposeTieredTextQuotaKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("image_generation_call", true)
	ctx.Set("image_generation_call_quality", "low")
	ctx.Set("image_generation_call_size", "1024x1024")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: &relaycommon.BuildInToolInfo{
					CallCount: 1,
				},
				dto.BuildInToolFileSearch: &relaycommon.BuildInToolInfo{
					CallCount: 2,
				},
			},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1000, &billingexpr.TieredResult{
		ActualQuotaBeforeGroup: 1000,
		ActualQuotaAfterGroup:  1000,
	})

	require.Equal(t, int64(13000), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 14000, quota)
}

func TestCalculateTextQuotaSummaryZeroTokensStillBillsToolSurcharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					CallCount: 1,
				},
			},
		},
		StartTime: time.Now(),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})

	require.Equal(t, 0, summary.TotalTokens)
	require.False(t, summary.ToolCallSurchargeQuota.IsZero())
	require.Greater(t, summary.Quota, 0)
	require.Equal(t, common.QuotaFromDecimal(summary.ToolCallSurchargeQuota), summary.Quota)
}

func TestCalculateTextQuotaSummaryAlphaSearchKeepsFixedPriceWhenToolPriceIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	toolPrices := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	originalPrices := make(map[string]float64, len(toolPrices.Prices))
	for key, value := range toolPrices.Prices {
		originalPrices[key] = value
	}
	toolPrices.Prices = map[string]float64{"web_search_preview": 0}
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() {
		toolPrices.Prices = originalPrices
		operation_setting.RebuildToolPriceIndex()
	})

	relayInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "fixed-price-alpha-search",
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.02,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {CallCount: 1},
			},
		},
		StartTime: time.Now(),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})

	require.True(t, summary.ToolCallSurchargeQuota.IsZero())
	require.Equal(t, common.QuotaFromFloat(0.02*common.QuotaPerUnit), summary.Quota)
}

func TestCalculateTextQuotaSummaryDoesNotApplyRequestMultipliersToToolSurcharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					CallCount: 1,
				},
			},
		},
		StartTime: time.Now(),
	}
	relayInfo.PriceData.AddOtherRatio("n", 3)

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})
	expected := decimal.NewFromFloat(10.0 / 1000).Mul(decimal.NewFromFloat(common.QuotaPerUnit))

	require.True(t, expected.Equal(summary.ToolCallSurchargeQuota))
	require.Equal(t, common.QuotaFromDecimal(expected), summary.Quota)
}

func TestComposeTieredTextQuotaFallbackKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1250, nil)

	require.Equal(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 13750, quota)
}

func TestComposeTieredTextQuotaErrorFallbackUsesPreConsumedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// tieredResult=nil simulates a settlement error where TryTieredSettle
	// falls back to FinalPreConsumedQuota (2000), which differs from
	// EstimatedQuotaBeforeGroup * GroupRatio (1250).
	preConsumedFallback := 2000
	quota := composeTieredTextQuota(relayInfo, summary, preConsumedFallback, nil)

	require.Equal(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 14500, quota)
}

func TestComposeTieredTextQuotaAlphaSearchErrorFallbackDoesNotDuplicateReservedSurcharge(t *testing.T) {
	summary := textQuotaSummary{
		ToolCallSurchargeQuota: decimal.NewFromInt(12_500),
	}
	relayInfo := &relaycommon.RelayInfo{
		RelayMode:             relayconstant.RelayModeAlphaSearch,
		FinalPreConsumedQuota: 14_500,
	}

	quota := composeTieredTextQuota(relayInfo, summary, 14_500, nil)

	require.Equal(t, 14_500, quota)
}

func TestComposeTieredTextQuotaTrustedAlphaSearchFallbackAddsUnreservedSurcharge(t *testing.T) {
	summary := textQuotaSummary{
		ToolCallSurchargeQuota: decimal.NewFromInt(12_500),
	}
	relayInfo := &relaycommon.RelayInfo{
		RelayMode:             relayconstant.RelayModeAlphaSearch,
		FinalPreConsumedQuota: 0,
	}

	quota := composeTieredTextQuota(relayInfo, summary, 2_000, nil)

	require.Equal(t, 14_500, quota)
}

func TestComposeTieredTextQuotaFallbackSaturatesFinalQuota(t *testing.T) {
	summary := textQuotaSummary{
		ToolCallSurchargeQuota: decimal.NewFromInt(100),
	}

	quota := composeTieredTextQuota(&relaycommon.RelayInfo{}, summary, math.MaxInt32-50, nil)

	require.Equal(t, math.MaxInt32, quota)
}

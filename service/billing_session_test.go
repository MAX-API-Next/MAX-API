package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingFundingSource struct {
	deltas []int
}

func (f *recordingFundingSource) Source() string       { return BillingSourceWallet }
func (f *recordingFundingSource) PreConsume(int) error { return nil }
func (f *recordingFundingSource) Refund() error        { return nil }
func (f *recordingFundingSource) Settle(delta int) (int64, error) {
	f.deltas = append(f.deltas, delta)
	return int64(delta), nil
}

type recordingBillingSettler struct {
	preConsumed int
	reserves    []int
}

func (s *recordingBillingSettler) Settle(int) error         { return nil }
func (s *recordingBillingSettler) Refund(*gin.Context)      {}
func (s *recordingBillingSettler) NeedsRefund() bool        { return false }
func (s *recordingBillingSettler) GetPreConsumedQuota() int { return s.preConsumed }
func (s *recordingBillingSettler) Reserve(target int) error {
	s.reserves = append(s.reserves, target)
	s.preConsumed = target
	return nil
}

func TestBillingSessionPrepareSettlementPersistsZeroDeltaTaskEffect(t *testing.T) {
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			RequestId: "task-fixed-price-request",
			UserId:    41,
			TokenId:   42,
			TokenKey:  "fixed-price-token",
		},
		funding:          &WalletFunding{userId: 41},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	input, err := session.PrepareSettlement(10)

	require.NoError(t, err)
	require.NotNil(t, input)
	assert.Equal(t, "request:task-fixed-price-request:finalize", input.OperationKey)
	assert.Zero(t, input.FundingDelta)
	assert.Zero(t, input.TokenDelta)
}

func TestBillingSessionCompensatesFundingWhenTokenSettlementFails(t *testing.T) {
	truncate(t)
	funding := &recordingFundingSource{}
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   41,
			TokenId:  42,
			TokenKey: "settlement-token",
		},
		funding:          funding,
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	err := session.Settle(15)
	require.Error(t, err)
	assert.Equal(t, []int{5, -5, 5, -5, 5, -5}, funding.deltas)
	assert.False(t, session.settled)
	assert.False(t, session.fundingSettled)

	require.NoError(t, model.DB.Create(&model.Token{
		Id: 42, UserId: 41, Key: "settlement-token", Status: common.TokenStatusEnabled, RemainQuota: 20,
	}).Error)
	require.NoError(t, session.Settle(15))
	assert.Equal(t, []int{5, -5, 5, -5, 5, -5, 5}, funding.deltas)
	assert.True(t, session.settled)

	var token model.Token
	require.NoError(t, model.DB.First(&token, 42).Error)
	assert.EqualValues(t, 15, token.RemainQuota)
	assert.EqualValues(t, 5, token.UsedQuota)
}

func TestBillingSessionCumulativeReservationSettlesExactlyOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID = 801, 802
	const initiallyReserved = 5

	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "realtime-once-user", Quota: 995, Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id: tokenID, UserId: userID, Key: "realtime-once-token", Name: "realtime-once-token",
		Status: common.TokenStatusEnabled, RemainQuota: 995, UsedQuota: initiallyReserved,
	}).Error)

	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RequestId:       "realtime-once-request",
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "realtime-once-token",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &WalletFunding{
			userId: userID, consumed: initiallyReserved,
		},
		preConsumedQuota: initiallyReserved,
		tokenConsumed:    initiallyReserved,
	}
	info.Billing = session
	info.FinalPreConsumedQuota = initiallyReserved
	require.NoError(t, session.Reserve(10))
	require.NoError(t, SettleBilling(ctx, info, 10))

	assert.EqualValues(t, 990, getUserQuota(t, userID))
	assert.EqualValues(t, 990, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 10, session.GetPreConsumedQuota())
}

func TestBillingSessionWalletPreConsumeUsesDurableRequestIdentity(t *testing.T) {
	truncate(t)
	const userID, tokenID = 803, 804
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "durable-pre-consume-token", 100)
	ctx, _ := gin.CreateTestContext(nil)
	newSession := func() *BillingSession {
		return &BillingSession{
			relayInfo: &relaycommon.RelayInfo{
				RequestId: "durable-pre-consume-request", UserId: userID,
				TokenId: tokenID, TokenKey: "durable-pre-consume-token",
			},
			funding: &WalletFunding{userId: userID},
		}
	}

	first := newSession()
	require.Nil(t, first.preConsume(ctx, 10))
	second := newSession()
	require.Nil(t, second.preConsume(ctx, 10))

	assert.EqualValues(t, 90, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:durable-pre-consume-request:pre-consume").First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
}

func TestBillingSessionWalletFirstReplayCannotSwitchFromWalletToSubscription(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 805, 806, 807, 808
	seedUser(t, userID, 10)
	seedToken(t, tokenID, userID, "wallet-replay-token", 100)
	seedBillingSubscription(t, planID, subscriptionID, userID, 100)

	ctx, _ := gin.CreateTestContext(nil)
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RequestId: "wallet-replay-request", UserId: userID,
			TokenId: tokenID, TokenKey: "wallet-replay-token",
			OriginModelName: "wallet-replay-model",
			UserSetting:     dto.UserSetting{BillingPreference: "wallet_first"},
		}
	}

	first, apiErr := NewBillingSession(ctx, newInfo(), 10)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceWallet, first.funding.Source())

	second, apiErr := NewBillingSession(ctx, newInfo(), 10)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceWallet, second.funding.Source())
	assert.EqualValues(t, 0, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 0, getSubscriptionAmountUsed(t, subscriptionID))
	assert.EqualValues(t, 0, countSubscriptionPreConsumeRecords(t, "wallet-replay-request"))
}

func TestBillingSessionWalletFirstReplayCannotSwitchFromSubscriptionToWallet(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 809, 810, 813, 814
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "subscription-replay-token", 100)
	seedBillingSubscription(t, planID, subscriptionID, userID, 100)

	ctx, _ := gin.CreateTestContext(nil)
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RequestId: "subscription-replay-request", UserId: userID,
			TokenId: tokenID, TokenKey: "subscription-replay-token",
			OriginModelName: "subscription-replay-model",
			UserSetting:     dto.UserSetting{BillingPreference: "wallet_first"},
		}
	}

	first, apiErr := NewBillingSession(ctx, newInfo(), 10)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, first.funding.Source())
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", 100).Error)

	second, apiErr := NewBillingSession(ctx, newInfo(), 10)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, second.funding.Source())
	assert.EqualValues(t, 100, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 10, getSubscriptionAmountUsed(t, subscriptionID))
	assert.EqualValues(t, 1, countSubscriptionPreConsumeRecords(t, "subscription-replay-request"))
}

func TestBillingSessionTrustedWalletReplayFailsClosedInsteadOfSwitchingFunding(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 815, 816, 817, 818
	trustQuota := common.GetTrustQuota()
	require.Positive(t, trustQuota, "trust quota must be enabled for this scenario")
	seedUser(t, userID, trustQuota+100)
	seedToken(t, tokenID, userID, "trusted-wallet-replay-token", trustQuota+100)
	seedBillingSubscription(t, planID, subscriptionID, userID, 100)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("token_quota", int64(trustQuota+100))
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RequestId: "trusted-wallet-replay-request", UserId: userID,
			TokenId: tokenID, TokenKey: "trusted-wallet-replay-token",
			OriginModelName: "trusted-wallet-replay-model",
			UserSetting:     dto.UserSetting{BillingPreference: "wallet_first"},
		}
	}

	first, apiErr := NewBillingSession(ctx, newInfo(), 10)
	require.Nil(t, apiErr)
	require.True(t, first.trusted)
	var tombstone model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:trusted-wallet-replay-request:pre-consume").First(&tombstone).Error)
	assert.Zero(t, tombstone.FundingDelta)
	assert.Zero(t, tombstone.TokenDelta)

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", 0).Error)
	ctx.Set("token_quota", int64(0))
	second, apiErr := NewBillingSession(ctx, newInfo(), 10)
	require.Nil(t, second)
	require.NotNil(t, apiErr)
	assert.ErrorIs(t, apiErr, model.ErrBillingSettlementOperationConflict)
	assert.EqualValues(t, trustQuota+100, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 0, getSubscriptionAmountUsed(t, subscriptionID))
	assert.EqualValues(t, 0, countSubscriptionPreConsumeRecords(t, "trusted-wallet-replay-request"))
}

func seedBillingSubscription(t *testing.T, planID int, subscriptionID int, userID int, amountTotal int64) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: planID, Title: fmt.Sprintf("billing-plan-%d", planID), Enabled: true,
		TotalAmount: amountTotal, QuotaResetPeriod: model.SubscriptionResetNever,
	}).Error)
	now := time.Now()
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id: subscriptionID, UserId: userID, PlanId: planID,
		AmountTotal: amountTotal, Status: "active",
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(),
	}).Error)
}

func getSubscriptionAmountUsed(t *testing.T, subscriptionID int) int64 {
	t.Helper()
	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	return subscription.AmountUsed
}

func countSubscriptionPreConsumeRecords(t *testing.T, requestID string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).Count(&count).Error)
	return count
}

func TestPreWssConsumeQuotaUsesCumulativeReservationInsteadOfIndependentCharge(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	})
	require.NoError(t, client.HSet(context.Background(), "user:811", map[string]interface{}{
		"Id": 811, "Quota": 100000, "Role": common.RoleCommonUser, "Status": common.UserStatusEnabled,
	}).Err())
	require.NoError(t, client.HSet(context.Background(), fmt.Sprintf("token:%s", common.GenerateHMAC("realtime-reserve-token")), map[string]interface{}{
		"Id": 812, "UserId": 811, "Status": common.TokenStatusEnabled, "RemainQuota": 100000,
	}).Err())

	ctx, _ := gin.CreateTestContext(nil)
	billing := &recordingBillingSettler{}
	info := &relaycommon.RelayInfo{
		UserId: 811, TokenId: 812, TokenKey: "realtime-reserve-token",
		OriginModelName: "gpt-4o-realtime-preview", UsingGroup: "default", Billing: billing,
	}
	usage := &dto.RealtimeUsage{
		TotalTokens: 4, InputTokens: 4,
		InputTokenDetails: dto.InputTokenDetails{TextTokens: 4},
	}
	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
	require.Len(t, billing.reserves, 1)
	assert.Greater(t, billing.reserves[0], 0)

	firstTarget := billing.reserves[0]
	require.NoError(t, client.HSet(context.Background(), "user:811", "Quota", firstTarget).Err())
	require.NoError(t, client.HSet(context.Background(), fmt.Sprintf("token:%s", common.GenerateHMAC("realtime-reserve-token")), "RemainQuota", firstTarget).Err())
	usage.TotalTokens = 8
	usage.InputTokens = 8
	usage.InputTokenDetails.TextTokens = 8
	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
	require.Len(t, billing.reserves, 2)
	assert.Greater(t, billing.reserves[1], firstTarget)
	assert.LessOrEqual(t, billing.reserves[1]-firstTarget, firstTarget)
}

func TestPostConsumeQuotaRejectsAmountChangedReplay(t *testing.T) {
	truncate(t)
	const userID, tokenID = 821, 822
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "stable-post-token", 100)
	info := &relaycommon.RelayInfo{
		RequestId: "stable-post-request", TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "violation-fee"},
		UserId: userID, TokenId: tokenID, TokenKey: "stable-post-token",
	}

	require.NoError(t, PostConsumeQuota(info, 10, 0, false))
	err := PostConsumeQuota(info, 20, 0, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, model.ErrBillingSettlementOperationConflict)
	assert.EqualValues(t, 90, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))
}

func TestViolationFeeSettlesOriginalSubscriptionPreConsume(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 831, 832, 833, 834
	const preConsumed int64 = 10
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "violation-subscription-token", 100)
	seedChannel(t, 1)
	seedBillingSubscription(t, planID, subscriptionID, userID, 100)

	result, err := model.PreConsumeTokenAndUserSubscription(
		"violation-subscription-request", userID, tokenID,
		"violation-subscription-token", "grok-test", 0, preConsumed,
	)
	require.NoError(t, err)
	require.Equal(t, subscriptionID, result.UserSubscriptionId)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RequestId: "violation-subscription-request", UserId: userID,
		TokenId: tokenID, TokenKey: "violation-subscription-token",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
		OriginModelName: "grok-test", UsingGroup: "default",
		BillingSource: BillingSourceSubscription, SubscriptionId: subscriptionID,
		StartTime: time.Now(),
	}
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId: "violation-subscription-request", userId: userID,
			subscriptionId: subscriptionID, preConsumed: preConsumed,
		},
		preConsumedQuota: int(preConsumed), tokenConsumed: int(preConsumed),
	}
	info.Billing = session

	apiErr := types.NewErrorWithStatusCode(
		errors.New(CSAMViolationMarker), types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)
	HandleFailedBilling(ctx, info, NormalizeViolationFeeError(apiErr))

	feeQuota := calcViolationFeeQuota(0.05, 1)
	require.EqualValues(t, feeQuota, getSubscriptionAmountUsed(t, subscriptionID))
	require.EqualValues(t, 100-feeQuota, getTokenRemainQuota(t, tokenID))
	require.Equal(t, int64(1), countLogs(t))
	require.Equal(t, feeQuota, getLastLog(t).Quota)

	var finalize model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:violation-subscription-request:finalize").First(&finalize).Error)
	require.Equal(t, model.BillingSettlementStatusApplied, finalize.Status)
	var legacyCount int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key LIKE ?", "request:violation-subscription-request:legacy-post:%").Count(&legacyCount).Error)
	require.Zero(t, legacyCount)
}

type compensationFailingFundingSource struct {
	deltas []int
}

func (f *compensationFailingFundingSource) Source() string       { return BillingSourceWallet }
func (f *compensationFailingFundingSource) PreConsume(int) error { return nil }
func (f *compensationFailingFundingSource) Refund() error        { return nil }
func (f *compensationFailingFundingSource) Settle(delta int) (int64, error) {
	f.deltas = append(f.deltas, delta)
	if delta < 0 {
		return 0, errors.New("compensation failed")
	}
	return int64(delta), nil
}

func TestBillingSessionKeepsCommittedFundingRetryableWhenCompensationFails(t *testing.T) {
	truncate(t)
	funding := &compensationFailingFundingSource{}
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{UserId: 51, TokenId: 52, TokenKey: "retry-token"},
		funding:   funding, preConsumedQuota: 10,
	}

	require.Error(t, session.Settle(15))
	assert.Equal(t, []int{5, -5}, funding.deltas)
	assert.True(t, session.fundingSettled)
	assert.True(t, session.compensationFailed)
	assert.False(t, session.settled)

	require.NoError(t, model.DB.Create(&model.Token{
		Id: 52, UserId: 51, Key: "retry-token", Status: common.TokenStatusEnabled, RemainQuota: 20,
	}).Error)
	require.NoError(t, session.Settle(15))
	assert.Equal(t, []int{5, -5}, funding.deltas)
	assert.True(t, session.settled)
}

type ambiguousCompensationFundingSource struct {
	deltas []int
}

func (f *ambiguousCompensationFundingSource) Source() string       { return BillingSourceWallet }
func (f *ambiguousCompensationFundingSource) PreConsume(int) error { return nil }
func (f *ambiguousCompensationFundingSource) Refund() error        { return nil }
func (f *ambiguousCompensationFundingSource) Settle(delta int) (int64, error) {
	f.deltas = append(f.deltas, delta)
	if delta < 0 {
		return int64(delta), errors.New("compensation outcome is unknown")
	}
	return int64(delta), nil
}

func TestBillingSessionDoesNotReapplyFundingAfterAmbiguousCompensationError(t *testing.T) {
	truncate(t)
	funding := &ambiguousCompensationFundingSource{}
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{UserId: 53, TokenId: 54, TokenKey: "ambiguous-compensation-token"},
		funding:   funding, preConsumedQuota: 10,
	}

	require.Error(t, session.Settle(15))
	assert.Equal(t, []int{5, -5}, funding.deltas)
	assert.EqualValues(t, 5, session.appliedFundingDelta)
	assert.True(t, session.compensationFailed)
}

type partialCompensationFundingSource struct {
	deltas []int
}

func (f *partialCompensationFundingSource) Source() string       { return BillingSourceWallet }
func (f *partialCompensationFundingSource) PreConsume(int) error { return nil }
func (f *partialCompensationFundingSource) Refund() error        { return nil }
func (f *partialCompensationFundingSource) Settle(delta int) (int64, error) {
	f.deltas = append(f.deltas, delta)
	if delta < 0 {
		return -2, nil
	}
	return int64(delta), nil
}

func TestBillingSessionReconcilesPartialFundingCompensationBeforeRetry(t *testing.T) {
	truncate(t)
	funding := &partialCompensationFundingSource{}
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{UserId: 56, TokenId: 57, TokenKey: "partial-compensation-token"},
		funding:   funding, preConsumedQuota: 10,
	}

	require.Error(t, session.Settle(15))
	require.Equal(t, []int{5, -5, 2, -5, 2, -5}, funding.deltas)
	require.False(t, session.compensationFailed)
	require.True(t, session.fundingReconcilePending)
	require.EqualValues(t, 3, session.appliedFundingDelta)

	require.NoError(t, model.DB.Create(&model.Token{
		Id: 57, UserId: 56, Key: "partial-compensation-token", Status: common.TokenStatusEnabled, RemainQuota: 20,
	}).Error)
	require.NoError(t, session.Settle(15))
	assert.True(t, session.settled)
	assert.EqualValues(t, 5, session.appliedFundingDelta)
	assert.Equal(t, int64(2), int64(funding.deltas[len(funding.deltas)-1]))
}

type tokenRecoveringFundingSource struct {
	t      *testing.T
	deltas []int
}

func (f *tokenRecoveringFundingSource) Source() string       { return BillingSourceWallet }
func (f *tokenRecoveringFundingSource) PreConsume(int) error { return nil }
func (f *tokenRecoveringFundingSource) Refund() error        { return nil }
func (f *tokenRecoveringFundingSource) Settle(delta int) (int64, error) {
	f.deltas = append(f.deltas, delta)
	if delta < 0 {
		require.NoError(f.t, model.DB.Create(&model.Token{
			Id: 62, UserId: 61, Key: "recovering-token", Status: common.TokenStatusEnabled, RemainQuota: 20,
		}).Error)
	}
	return int64(delta), nil
}

func TestBillingSessionRetriesTransientTokenSettlementWithinSingleCall(t *testing.T) {
	truncate(t)
	funding := &tokenRecoveringFundingSource{t: t}
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{UserId: 61, TokenId: 62, TokenKey: "recovering-token"},
		funding:   funding, preConsumedQuota: 10,
	}

	require.NoError(t, session.Settle(15))
	assert.Equal(t, []int{5, -5, 5}, funding.deltas)
	assert.True(t, session.settled)

	var token model.Token
	require.NoError(t, model.DB.First(&token, 62).Error)
	assert.EqualValues(t, 15, token.RemainQuota)
	assert.EqualValues(t, 5, token.UsedQuota)
}

func TestBillingSessionTrustsFiniteInt64TokenQuota(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	trustQuota := common.GetTrustQuota()
	ctx.Set("token_quota", int64(trustQuota+1))
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{UserQuota: int64(trustQuota + 1)},
		funding:   &recordingFundingSource{},
	}

	assert.True(t, session.shouldTrust(ctx))
}

func TestBillingSessionRefundUsesDurableOperation(t *testing.T) {
	truncate(t)
	const userID, tokenID = 81, 82
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "durable-refund-token", 100)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", 10).Error)

	ctx, _ := gin.CreateTestContext(nil)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			RequestId: "durable-refund-request",
			UserId:    userID,
			TokenId:   tokenID,
			TokenKey:  "durable-refund-token",
		},
		funding:          &WalletFunding{userId: userID, consumed: 10},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	session.Refund(ctx)
	require.EqualValues(t, 110, getUserQuota(t, userID))
	require.EqualValues(t, 110, getTokenRemainQuota(t, tokenID))

	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:durable-refund-request:finalize").First(&settlement).Error)
	require.Equal(t, "applied", settlement.Status)
}

func TestBillingSessionDurableSettleEffectConflictDoesNotRemainRefundable(t *testing.T) {
	truncate(t)
	const userID, tokenID = 83, 84
	const requestID = "durable-effect-conflict-request"
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "durable-effect-conflict-token", 100)

	effect := &model.BillingSettlementEffect{
		LogType: model.LogTypeConsume, Content: "settlement effect conflict",
		ModelName: "test-model", TokenID: tokenID, Group: "default",
	}
	operationKey := "request:" + requestID + ":finalize"
	applied, _, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
		OperationKey: operationKey,
		Source:       model.BillingSettlementSourceWallet,
		UserID:       userID,
		TokenID:      tokenID,
		TokenKey:     "durable-effect-conflict-token",
		FundingDelta: 5,
		TokenDelta:   5,
		Effect:       effect,
	})
	require.NoError(t, err)
	require.EqualValues(t, 5, applied)
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("operation_key = ?", operationKey).
		Update("effect_status", model.BillingSettlementEffectApplying).Error)

	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			RequestId: requestID, UserId: userID,
			TokenId: tokenID, TokenKey: "durable-effect-conflict-token",
		},
		funding:          &WalletFunding{userId: userID, consumed: 10},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	require.NoError(t, session.SettleWithEffect(15, effect))
	assert.True(t, session.settled)
	assert.True(t, session.fundingSettled)
	assert.EqualValues(t, 5, session.appliedFundingDelta)
	assert.False(t, session.NeedsRefund())
	assert.EqualValues(t, 95, getUserQuota(t, userID))
	assert.EqualValues(t, 95, getTokenRemainQuota(t, tokenID))
}

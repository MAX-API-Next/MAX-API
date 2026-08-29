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

type uncertainFundingSource struct {
	deltas  []int
	applied int64
	err     error
}

func TestBillingSettlementBacklogAlertReportsCountAgeAndRecovery(t *testing.T) {
	originalSender := billingSettlementBacklogAlertNotificationSender
	billingSettlementBacklogAlertState.Lock()
	originalState := struct {
		active          bool
		lastCount       int64
		oldestCreatedAt int64
		lastNotifiedAt  time.Time
	}{
		active:          billingSettlementBacklogAlertState.active,
		lastCount:       billingSettlementBacklogAlertState.lastCount,
		oldestCreatedAt: billingSettlementBacklogAlertState.oldestCreatedAt,
		lastNotifiedAt:  billingSettlementBacklogAlertState.lastNotifiedAt,
	}
	billingSettlementBacklogAlertState.active = false
	billingSettlementBacklogAlertState.lastCount = 0
	billingSettlementBacklogAlertState.oldestCreatedAt = 0
	billingSettlementBacklogAlertState.lastNotifiedAt = time.Time{}
	billingSettlementBacklogAlertState.Unlock()
	t.Cleanup(func() {
		billingSettlementBacklogAlertNotificationSender = originalSender
		billingSettlementBacklogAlertState.Lock()
		billingSettlementBacklogAlertState.active = originalState.active
		billingSettlementBacklogAlertState.lastCount = originalState.lastCount
		billingSettlementBacklogAlertState.oldestCreatedAt = originalState.oldestCreatedAt
		billingSettlementBacklogAlertState.lastNotifiedAt = originalState.lastNotifiedAt
		billingSettlementBacklogAlertState.Unlock()
	})

	var alerts []SmartOpsAlert
	billingSettlementBacklogAlertNotificationSender = func(alert SmartOpsAlert) {
		alerts = append(alerts, alert)
	}
	now := time.Unix(10_000, 0)
	stats := model.BillingSettlementBacklogStats{Count: 2, OldestCreatedAt: now.Add(-2 * time.Hour).Unix()}

	observeBillingSettlementBacklog(stats, now)
	require.Len(t, alerts, 1)
	assert.Equal(t, smartOpsAlertStatusFiring, alerts[0].Status)
	assert.Equal(t, "billing", alerts[0].Component)
	assert.Equal(t, float64(2), alerts[0].CurrentValue)
	assert.Equal(t, (2 * time.Hour).Seconds(), alerts[0].Threshold)
	assert.Contains(t, alerts[0].Message, "2 条")
	assert.Contains(t, alerts[0].Message, "2h0m0s")

	observeBillingSettlementBacklog(stats, now.Add(time.Minute))
	require.Len(t, alerts, 1, "an unchanged backlog must be deduplicated inside the reminder interval")

	stats.Count = 3
	observeBillingSettlementBacklog(stats, now.Add(2*time.Minute))
	require.Len(t, alerts, 2)
	assert.Equal(t, float64(3), alerts[1].CurrentValue)

	observeBillingSettlementBacklog(stats, now.Add(2*time.Minute+billingSettlementBacklogNotificationInterval))
	require.Len(t, alerts, 3, "a persistent backlog must produce an age reminder")

	observeBillingSettlementBacklog(model.BillingSettlementBacklogStats{}, now.Add(20*time.Minute))
	require.Len(t, alerts, 4)
	assert.Equal(t, smartOpsAlertStatusResolved, alerts[3].Status)
	assert.Contains(t, alerts[3].Message, "此前共 3 条")
}

func (f *uncertainFundingSource) Source() string       { return BillingSourceWallet }
func (f *uncertainFundingSource) PreConsume(int) error { return nil }
func (f *uncertainFundingSource) Refund() error        { return nil }
func (f *uncertainFundingSource) Settle(delta int) (int64, error) {
	f.deltas = append(f.deltas, delta)
	return f.applied, f.err
}

func TestBillingSessionFailsClosedAfterUncertainNonDurableFundingSettlement(t *testing.T) {
	originalSender := billingFundingOutcomeUnknownAlertNotificationSender
	var alerts []SmartOpsAlert
	var activeSession *BillingSession
	var senderObservedUnlocked bool
	billingFundingOutcomeUnknownAlertNotificationSender = func(alert SmartOpsAlert) {
		if activeSession != nil && activeSession.mu.TryLock() {
			senderObservedUnlocked = true
			activeSession.mu.Unlock()
		}
		alerts = append(alerts, alert)
	}
	t.Cleanup(func() {
		billingFundingOutcomeUnknownAlertNotificationSender = originalSender
	})

	cases := []struct {
		name    string
		applied int64
		err     error
	}{
		{name: "error after possible commit", applied: 5, err: errors.New("provider timeout")},
		{name: "partial result", applied: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			alerts = nil
			senderObservedUnlocked = false
			funding := &uncertainFundingSource{applied: tc.applied, err: tc.err}
			session := &BillingSession{
				relayInfo: &relaycommon.RelayInfo{UserId: 101, TokenId: 102, TokenKey: "uncertain-funding-token"},
				funding:   funding, preConsumedQuota: 10,
			}
			activeSession = session

			err := session.Settle(15)
			require.ErrorIs(t, err, ErrBillingFundingOutcomeUnknown)
			assert.Equal(t, []int{5}, funding.deltas)
			assert.True(t, session.fundingOutcomeUnknown)
			require.Len(t, alerts, 1)
			assert.True(t, senderObservedUnlocked, "funding alerts must be enqueued after releasing the billing session mutex")
			assert.Equal(t, "billing_funding_outcome_unknown", alerts[0].Key)
			assert.Equal(t, smartOpsAlertStatusFiring, alerts[0].Status)
			assert.Contains(t, alerts[0].Message, "userId=101")
			assert.Contains(t, alerts[0].Message, "tokenId=102")
			assert.Contains(t, alerts[0].Message, "requested=5")
			assert.Contains(t, alerts[0].Message, fmt.Sprintf("applied=%d", tc.applied))

			// A second attempt must not repeat the non-idempotent funding call.
			require.ErrorIs(t, session.Settle(15), ErrBillingFundingOutcomeUnknown)
			require.ErrorIs(t, session.Reserve(20), ErrBillingFundingOutcomeUnknown)
			assert.Equal(t, []int{5}, funding.deltas)
			require.Len(t, alerts, 1, "fail-closed retries must not emit duplicate outcome-unknown alerts")
		})
	}
}

type recordingBillingSettler struct {
	preConsumed int
	reserves    []int
	settleCalls int
}

type recordingEffectBillingSettler struct {
	recordingBillingSettler
	actualQuota int
	effect      *model.BillingSettlementEffect
	err         error
}

func TestSettleAndRecordConsumeHandlesNilContextWithoutPanic(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = originalLogConsumeEnabled })

	assert.NotPanics(t, func() {
		settleAndRecordConsume(nil, nil, false, model.RecordConsumeLogParams{})
	})
	assert.NotPanics(t, func() {
		settler := &recordingBillingSettler{}
		settleAndRecordConsume(nil, &relaycommon.RelayInfo{Billing: settler}, false, model.RecordConsumeLogParams{})
		assert.Equal(t, 1, settler.settleCalls)
	})
}

func TestSettleAndRecordConsumeCarriesZeroUsageLogInDurableEffect(t *testing.T) {
	truncate(t)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set(common.RequestIdKey, "zero-usage-request")
	ctx.Set(common.UpstreamRequestIdKey, "zero-usage-upstream-request")
	settler := &recordingEffectBillingSettler{}
	info := &relaycommon.RelayInfo{
		RequestId: "relay-fallback-request",
		UserId:    601,
		Billing:   settler,
	}
	params := model.RecordConsumeLogParams{
		ChannelId:        602,
		PromptTokens:     3,
		CompletionTokens: 5,
		ModelName:        "zero-usage-model",
		Quota:            0,
		Content:          "upstream omitted billable usage",
		TokenId:          603,
		TokenName:        "request-time-token-name",
		UseTimeSeconds:   7,
		IsStream:         true,
		Group:            "default",
		Other:            map[string]interface{}{"reason": "missing_usage"},
	}

	settleAndRecordConsume(ctx, info, false, params)

	require.NotNil(t, settler.effect)
	assert.Zero(t, settler.settleCalls)
	assert.Zero(t, settler.actualQuota)
	assert.False(t, settler.effect.UpdateUsage)
	assert.True(t, settler.effect.QuotaIsActual)
	assert.Equal(t, params.Content, settler.effect.Content)
	assert.Equal(t, params.ChannelId, settler.effect.ChannelID)
	assert.Equal(t, params.ModelName, settler.effect.ModelName)
	assert.Equal(t, params.TokenId, settler.effect.TokenID)
	assert.Equal(t, params.TokenName, settler.effect.TokenName)
	assert.Equal(t, params.PromptTokens, settler.effect.PromptTokens)
	assert.Equal(t, params.CompletionTokens, settler.effect.CompletionTokens)
	assert.Equal(t, params.UseTimeSeconds, settler.effect.UseTimeSeconds)
	assert.Equal(t, params.IsStream, settler.effect.IsStream)
	assert.Equal(t, "zero-usage-request", settler.effect.RequestID)
	assert.Equal(t, "zero-usage-upstream-request", settler.effect.UpstreamRequestID)
}

func TestSettleBillingWithEffectDoesNotClaimPrePersistenceFailure(t *testing.T) {
	settler := &recordingEffectBillingSettler{err: ErrBillingSettlementEffectNotDurable}
	info := &relaycommon.RelayInfo{Billing: settler}

	handled, err := SettleBillingWithEffect(nil, info, 10, &model.BillingSettlementEffect{})

	require.ErrorIs(t, err, ErrBillingSettlementEffectNotDurable)
	assert.False(t, handled)
}

func TestSettleBillingWithEffectDoesNotClaimModelRecordPersistenceFailure(t *testing.T) {
	settler := &recordingEffectBillingSettler{err: model.ErrBillingSettlementRecordNotDurable}
	info := &relaycommon.RelayInfo{Billing: settler}

	handled, err := SettleBillingWithEffect(nil, info, 10, &model.BillingSettlementEffect{})

	require.ErrorIs(t, err, model.ErrBillingSettlementRecordNotDurable)
	assert.False(t, handled)
}

func TestSettleBillingWithEffectDoesNotClaimUnknownFundingOutcome(t *testing.T) {
	settler := &recordingEffectBillingSettler{err: ErrBillingFundingOutcomeUnknown}
	info := &relaycommon.RelayInfo{Billing: settler}

	handled, err := SettleBillingWithEffect(nil, info, 10, &model.BillingSettlementEffect{})

	require.ErrorIs(t, err, ErrBillingFundingOutcomeUnknown)
	assert.False(t, handled)
}

func TestSettleBillingWithEffectKeepsDurablyOwnedFailureHandled(t *testing.T) {
	settlementErr := errors.New("durable settlement remains pending")
	settler := &recordingEffectBillingSettler{err: settlementErr}
	info := &relaycommon.RelayInfo{Billing: settler}

	handled, err := SettleBillingWithEffect(nil, info, 10, &model.BillingSettlementEffect{})

	require.ErrorIs(t, err, settlementErr)
	assert.True(t, handled)
}

func TestSettleAndRecordConsumeDoesNotProjectNonDurableSettlementFailure(t *testing.T) {
	truncate(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalBatchUpdateEnabled := common.BatchUpdateEnabled
	common.LogConsumeEnabled = true
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.BatchUpdateEnabled = originalBatchUpdateEnabled
	})

	cases := []struct {
		name string
		err  error
	}{
		{name: "effect not durable", err: ErrBillingSettlementEffectNotDurable},
		{name: "record not durable", err: model.ErrBillingSettlementRecordNotDurable},
		{name: "funding outcome unknown", err: ErrBillingFundingOutcomeUnknown},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := 621 + i
			channelID := 631 + i
			require.NoError(t, model.DB.Create(&model.User{
				Id:       userID,
				Username: fmt.Sprintf("non-durable-settlement-user-%d", i),
				AffCode:  fmt.Sprintf("non-durable-%d", i),
				Quota:    100,
				Status:   common.UserStatusEnabled,
			}).Error)
			seedChannel(t, channelID)
			settler := &recordingEffectBillingSettler{err: tc.err}
			params := model.RecordConsumeLogParams{
				ChannelId: channelID,
				ModelName: "non-durable-settlement-model",
				Quota:     10,
				Content:   tc.name,
			}

			settleAndRecordConsume(nil, &relaycommon.RelayInfo{
				UserId:  userID,
				Billing: settler,
			}, true, params)

			require.NotNil(t, settler.effect)
			var user model.User
			require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.RequestCount)
			var channel model.Channel
			require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
			assert.Zero(t, channel.UsedQuota)
			var logCount int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).
				Where("user_id = ? AND type = ?", userID, model.LogTypeConsume).
				Count(&logCount).Error)
			assert.Zero(t, logCount)
		})
	}
}

func TestSettleAndRecordConsumePersistsLegacyEffectBeforeFailedSettlement(t *testing.T) {
	truncate(t)
	const (
		userID  = 611
		tokenID = 612
	)
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "legacy-effect-token", 0)
	info := &relaycommon.RelayInfo{
		RequestId: "legacy-effect-request",
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "legacy-effect-token",
	}
	params := model.RecordConsumeLogParams{
		ChannelId: 613,
		ModelName: "legacy-effect-model",
		TokenId:   tokenID,
		Group:     "default",
		Quota:     10,
		Content:   "legacy settlement waits for funding",
	}

	settleAndRecordConsume(nil, info, true, params)

	operationKey := "request:legacy-effect-request:legacy-post:finalize"
	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", operationKey).First(&settlement).Error)
	require.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
	require.NotEmpty(t, settlement.EffectPayload)
	require.Empty(t, settlement.EffectStatus)
	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", userID, model.LogTypeConsume).Count(&logCount).Error)
	require.Zero(t, logCount)

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", 100).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]interface{}{
		"remain_quota": 100,
		"used_quota":   0,
	}).Error)
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).
		Where("id = ?", settlement.ID).
		Updates(map[string]interface{}{
			"status":       model.BillingSettlementStatusPending,
			"last_error":   "",
			"next_attempt": time.Now().Unix(),
		}).Error)
	model.ProcessPendingBillingSettlementsOnce()

	settleAndRecordConsume(nil, info, true, params)
	settleAndRecordConsume(nil, info, true, params)

	require.EqualValues(t, 90, getUserQuota(t, userID))
	require.Equal(t, 90, getTokenRemainQuota(t, tokenID))
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", userID, model.LogTypeConsume).Count(&logCount).Error)
	require.EqualValues(t, 1, logCount)
	var storedUser model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&storedUser, userID).Error)
	require.EqualValues(t, 10, storedUser.UsedQuota)
	require.Equal(t, 1, storedUser.RequestCount)
}

func (s *recordingBillingSettler) Settle(int) error         { s.settleCalls++; return nil }
func (s *recordingBillingSettler) Refund(*gin.Context)      {}
func (s *recordingBillingSettler) NeedsRefund() bool        { return false }
func (s *recordingBillingSettler) GetPreConsumedQuota() int { return s.preConsumed }
func (s *recordingBillingSettler) Reserve(target int) error {
	s.reserves = append(s.reserves, target)
	s.preConsumed = target
	return nil
}

func (s *recordingEffectBillingSettler) SettleWithEffect(actualQuota int, effect *model.BillingSettlementEffect) error {
	s.actualQuota = actualQuota
	s.effect = effect
	return s.err
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

func TestBillingSessionPrepareSettlementBindsTaskReservationAndTarget(t *testing.T) {
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			RequestId:     "task-reservation-binding",
			UserId:        43,
			TokenId:       44,
			TokenKey:      "task-reservation-token",
			TaskRelayInfo: &relaycommon.TaskRelayInfo{PersistedTaskID: 45},
		},
		funding:          &WalletFunding{userId: 43},
		preConsumedQuota: 100,
	}

	input, err := session.PrepareSettlement(175)

	require.NoError(t, err)
	require.NotNil(t, input)
	assert.EqualValues(t, 45, input.TaskID)
	assert.EqualValues(t, 100, input.TaskQuota)
	assert.EqualValues(t, 175, input.TaskQuotaTarget)
	assert.EqualValues(t, 75, input.FundingDelta)
}

func TestBillingSessionSettleAppliesPersistedZeroDeltaTaskFinalize(t *testing.T) {
	truncate(t)
	const userID = 46
	task := makeTask(userID, 0, 10, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSubmitted
	persistTask(t, task)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			RequestId:     "task-zero-delta-finalize",
			UserId:        userID,
			TaskRelayInfo: &relaycommon.TaskRelayInfo{PersistedTaskID: task.ID},
		},
		funding:          &WalletFunding{userId: userID},
		preConsumedQuota: 10,
	}
	input, err := session.PrepareSettlement(10)
	require.NoError(t, err)
	require.NotNil(t, input)
	require.NoError(t, task.UpdateWithSettlementIntent(input))

	require.NoError(t, session.Settle(10))

	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", input.OperationKey).First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
	assert.True(t, session.settled)
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

func TestNewBillingSessionRejectsUnresolvedPositiveFinalizeSettlement(t *testing.T) {
	truncate(t)
	const userID, tokenID = 851, 852
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "unresolved-finalize-token", 100)
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.BillingSettlement{
		OperationKey: "request:unresolved-finalize:finalize",
		Source:       model.BillingSettlementSourceWallet,
		UserID:       userID,
		TokenID:      tokenID,
		FundingDelta: 10,
		TokenDelta:   10,
		Status:       model.BillingSettlementStatusManual,
		CreatedAt:    now,
		UpdatedAt:    now,
		Revision:     1,
	}).Error)

	ctx, _ := gin.CreateTestContext(nil)
	session, apiErr := NewBillingSession(ctx, &relaycommon.RelayInfo{
		RequestId:       "blocked-after-finalize",
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "unresolved-finalize-token",
		OriginModelName: "blocked-finalize-model",
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}, 10)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.EqualValues(t, 100, getUserQuota(t, userID))
	assert.EqualValues(t, 100, getTokenRemainQuota(t, tokenID))
}

func TestInsufficientFinalizeBlocksFurtherBillingSessions(t *testing.T) {
	truncate(t)
	const userID, tokenID = 853, 854
	seedUser(t, userID, 15)
	seedToken(t, tokenID, userID, "insufficient-finalize-token", 100)
	ctx, _ := gin.CreateTestContext(nil)
	newInfo := func(requestID string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RequestId:       requestID,
			UserId:          userID,
			TokenId:         tokenID,
			TokenKey:        "insufficient-finalize-token",
			OriginModelName: "insufficient-finalize-model",
			UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
		}
	}

	session, apiErr := NewBillingSession(ctx, newInfo("insufficient-finalize-request"), 10)
	require.Nil(t, apiErr)
	require.Error(t, session.Settle(20))
	assert.EqualValues(t, 5, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))

	var settlement model.BillingSettlement
	require.NoError(t, model.DB.Where("operation_key = ?", "request:insufficient-finalize-request:finalize").First(&settlement).Error)
	assert.Equal(t, model.BillingSettlementStatusManual, settlement.Status)
	assert.EqualValues(t, 10, settlement.FundingDelta)
	assert.Zero(t, settlement.AppliedFundingDelta)

	nextSession, nextErr := NewBillingSession(ctx, newInfo("blocked-after-insufficient-finalize"), 5)
	require.Nil(t, nextSession)
	require.NotNil(t, nextErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, nextErr.GetErrorCode())
	assert.EqualValues(t, 5, getUserQuota(t, userID))
	assert.EqualValues(t, 90, getTokenRemainQuota(t, tokenID))
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
	ctx.Set(common.RequestIdKey, "violation-client-request")
	ctx.Set(common.UpstreamRequestIdKey, "violation-upstream-request")
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
	log := getLastLog(t)
	require.Equal(t, feeQuota, log.Quota)
	require.Equal(t, "violation-client-request", log.RequestId)
	require.Equal(t, "violation-upstream-request", log.UpstreamRequestId)

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

func TestBillingSessionFailsClosedWhenCompensationOutcomeIsUnknown(t *testing.T) {
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
	assert.True(t, session.fundingOutcomeUnknown)
	assert.False(t, session.settled)

	require.NoError(t, model.DB.Create(&model.Token{
		Id: 52, UserId: 51, Key: "retry-token", Status: common.TokenStatusEnabled, RemainQuota: 20,
	}).Error)
	require.ErrorIs(t, session.Settle(15), ErrBillingFundingOutcomeUnknown)
	assert.Equal(t, []int{5, -5}, funding.deltas)
	assert.False(t, session.settled)
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
	assert.True(t, session.fundingOutcomeUnknown)
	require.ErrorIs(t, session.Settle(15), ErrBillingFundingOutcomeUnknown)
	assert.Equal(t, []int{5, -5}, funding.deltas)
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

func TestBillingSessionFailsClosedAfterPartialFundingCompensation(t *testing.T) {
	truncate(t)
	funding := &partialCompensationFundingSource{}
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{UserId: 56, TokenId: 57, TokenKey: "partial-compensation-token"},
		funding:   funding, preConsumedQuota: 10,
	}

	require.ErrorIs(t, session.Settle(15), ErrBillingFundingOutcomeUnknown)
	require.Equal(t, []int{5, -5}, funding.deltas)
	require.False(t, session.compensationFailed)
	require.True(t, session.fundingOutcomeUnknown)
	require.False(t, session.fundingReconcilePending)
	require.EqualValues(t, 3, session.appliedFundingDelta)

	require.NoError(t, model.DB.Create(&model.Token{
		Id: 57, UserId: 56, Key: "partial-compensation-token", Status: common.TokenStatusEnabled, RemainQuota: 20,
	}).Error)
	require.ErrorIs(t, session.Settle(15), ErrBillingFundingOutcomeUnknown)
	assert.False(t, session.settled)
	assert.EqualValues(t, 3, session.appliedFundingDelta)
	assert.Equal(t, []int{5, -5}, funding.deltas)
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

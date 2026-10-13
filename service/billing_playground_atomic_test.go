package service

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func playgroundAtomicContext(requestID string, playground bool) (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 991, OriginModelName: "playground-atomic-model",
		IsPlayground: playground, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	if !playground {
		info.TokenId, info.TokenKey = 992, "synthetic-playground-atomic-token"
	}
	return c, info
}

func seedPlaygroundAtomic(t *testing.T) {
	t.Helper()
	truncate(t)
	seedUser(t, 991, 100)
	seedToken(t, 992, 991, "synthetic-playground-atomic-token", 100)
}

func TestPlaygroundWalletLookupRaceCannotDebitNormalReservation(t *testing.T) {
	for _, quota := range []int{0, 10} {
		t.Run(fmt.Sprintf("quota_%d", quota), func(t *testing.T) {
			seedPlaygroundAtomic(t)
			lookedUp, resume := make(chan struct{}), make(chan struct{})
			var paused atomic.Bool
			const callback = "test:playground-after-empty-source-read"
			require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "subscription_pre_consume_records" && paused.CompareAndSwap(false, true) {
					close(lookedUp)
					<-resume
				}
			}))
			t.Cleanup(func() { _ = model.DB.Callback().Query().Remove(callback) })
			defer close(resume)
			pgContext, pgInfo := playgroundAtomicContext("playground-normal-race", true)
			pg := &BillingSession{relayInfo: pgInfo, funding: &WalletFunding{userId: 991}}
			result := make(chan *types.MaxAPIError, 1)
			go func() { result <- pg.preConsume(pgContext, quota) }()
			select {
			case <-lookedUp:
			case <-time.After(5 * time.Second):
				t.Fatal("Playground did not finish its empty source lookup")
			}
			paidContext, paidInfo := playgroundAtomicContext(pgInfo.RequestId, false)
			paid := &BillingSession{relayInfo: paidInfo, funding: &WalletFunding{userId: 991}}
			require.Nil(t, paid.preConsume(paidContext, quota))
			resume <- struct{}{}
			select {
			case apiErr := <-result:
				t.Logf("wallet=%d token=%d original reservation=%d", getUserQuota(t, 991), getTokenRemainQuota(t, 992), quota)
				require.EqualValues(t, 100-quota, getUserQuota(t, 991), "the Playground loser must not debit the paid reservation twice")
				require.NotNil(t, apiErr)
				require.ErrorIs(t, apiErr, model.ErrBillingSettlementOperationConflict)
				require.True(t, types.IsSkipRetryError(apiErr))
			case <-time.After(5 * time.Second):
				t.Fatal("Playground pre-consume did not return")
			}
			require.EqualValues(t, 100-quota, getUserQuota(t, 991))
			require.EqualValues(t, 100-quota, getTokenRemainQuota(t, 992))
			var record model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", "request:"+pgInfo.RequestId+":pre-consume").First(&record).Error)
			require.EqualValues(t, quota, record.AppliedFundingDelta)
			require.EqualValues(t, quota, record.AppliedTokenDelta)
			model.ProcessPendingBillingSettlementsOnce()
			require.EqualValues(t, 100-quota, getUserQuota(t, 991))
		})
	}
}

func TestPlaygroundWalletConcurrentEmptyLookupsChargeOnce(t *testing.T) {
	seedPlaygroundAtomic(t)
	lookedUp, resume := make(chan struct{}, 2), make(chan struct{})
	var lookups atomic.Int32
	const callback = "test:two-empty-playground-source-reads"
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "subscription_pre_consume_records" && lookups.Add(1) <= 2 {
			lookedUp <- struct{}{}
			<-resume
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove(callback) })
	defer close(resume)
	results := make(chan *types.MaxAPIError, 2)
	for range 2 {
		c, info := playgroundAtomicContext("two-playground-race", true)
		session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 991}}
		go func() { results <- session.preConsume(c, 10) }()
	}
	for range 2 {
		select {
		case <-lookedUp:
		case <-time.After(5 * time.Second):
			t.Fatal("both empty source lookups did not complete")
		}
	}
	resume <- struct{}{}
	resume <- struct{}{}
	for range 2 {
		select {
		case apiErr := <-results:
			require.Nil(t, apiErr)
		case <-time.After(5 * time.Second):
			t.Fatal("Playground debit did not complete")
		}
	}
	require.EqualValues(t, 90, getUserQuota(t, 991))
	require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingSettlement{}).Where("operation_key = ?", "request:two-playground-race:pre-consume").Count(&count).Error)
	require.EqualValues(t, 1, count)
	model.ProcessPendingBillingSettlementsOnce()
	require.EqualValues(t, 90, getUserQuota(t, 991))
}

func TestPlaygroundWalletDurableReservationPreservesLifecycle(t *testing.T) {
	for _, actual := range []int{0, 6, 10, 14} {
		t.Run(fmt.Sprintf("actual_%d", actual), func(t *testing.T) {
			seedPlaygroundAtomic(t)
			c, info := playgroundAtomicContext(fmt.Sprintf("playground-lifecycle-%d", actual), true)
			session, apiErr := NewBillingSession(c, info, 10)
			require.Nil(t, apiErr)
			require.Equal(t, 10, session.GetPreConsumedQuota())
			require.Zero(t, session.tokenConsumed)
			require.EqualValues(t, 90, getUserQuota(t, 991))
			require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
			var initial model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", "request:"+info.RequestId+":pre-consume").First(&initial).Error)
			require.Equal(t, model.BillingSettlementStatusApplied, initial.Status)
			require.EqualValues(t, 10, initial.AppliedFundingDelta)
			require.Zero(t, initial.TokenDelta)
			require.Zero(t, initial.AppliedTokenDelta)
			paidContext, paidInfo := playgroundAtomicContext(info.RequestId, false)
			_, conflict := NewBillingSession(paidContext, paidInfo, 10)
			require.NotNil(t, conflict)
			require.ErrorIs(t, conflict, model.ErrBillingSettlementOperationConflict)
			effect := &model.BillingSettlementEffect{LogType: model.LogTypeConsume, ModelName: info.OriginModelName,
				RequestID: info.RequestId, Quota: int64(actual), QuotaIsActual: true, UpdateUsage: true, PromptTokens: 1, CompletionTokens: 1}
			require.NoError(t, session.SettleWithEffect(actual, effect))
			require.NoError(t, session.SettleWithEffect(actual, effect))
			for range 2 {
				model.ProcessPendingBillingSettlementsOnce()
			}
			require.EqualValues(t, 100-actual, getUserQuota(t, 991))
			require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
			var final model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).First(&final).Error)
			require.EqualValues(t, actual-10, final.AppliedFundingDelta)
			require.Zero(t, final.AppliedTokenDelta)
			var logs int64
			require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
			require.EqualValues(t, 1, logs)
			var user model.User
			require.NoError(t, model.DB.First(&user, 991).Error)
			require.EqualValues(t, actual, user.UsedQuota)
			require.EqualValues(t, 1, user.RequestCount)
		})
	}
}

func TestPlaygroundWalletDurableReservationRefundAndRollback(t *testing.T) {
	t.Run("refund", func(t *testing.T) {
		seedPlaygroundAtomic(t)
		c, info := playgroundAtomicContext("playground-refund", true)
		session, apiErr := NewBillingSession(c, info, 10)
		require.Nil(t, apiErr)
		session.Refund(c)
		session.Refund(c)
		model.ProcessPendingBillingSettlementsOnce()
		require.EqualValues(t, 100, getUserQuota(t, 991))
		require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
	})
	t.Run("funding_transaction_rollback", func(t *testing.T) {
		seedPlaygroundAtomic(t)
		const callback = "test:playground-user-update-failure"
		require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "users" {
				tx.AddError(errors.New("synthetic Playground funding transaction failure"))
			}
		}))
		callbackRemoved := false
		t.Cleanup(func() {
			if !callbackRemoved {
				_ = model.DB.Callback().Update().Remove(callback)
			}
		})
		c, info := playgroundAtomicContext("playground-rollback", true)
		_, apiErr := NewBillingSession(c, info, 10)
		require.NotNil(t, apiErr)
		require.NoError(t, model.DB.Callback().Update().Remove(callback))
		callbackRemoved = true
		require.EqualValues(t, 100, getUserQuota(t, 991))
		require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
		var count int64
		require.NoError(t, model.DB.Model(&model.BillingPreConsumeSelection{}).Count(&count).Error)
		require.Zero(t, count)
		var record model.BillingSettlement
		require.NoError(t, model.DB.Where("operation_key = ?", "request:"+info.RequestId+":pre-consume").First(&record).Error)
		require.NotEqual(t, model.BillingSettlementStatusApplied, record.Status)
		require.Zero(t, record.AppliedFundingDelta)
		// A failed paid admission stays inspectable and manual. Neither a fresh
		// runner nor a repeated operation may charge a request that was rejected.
		require.Equal(t, model.BillingSettlementStatusManual, record.Status)
		applied, _, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
			OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID, TokenID: record.TokenID,
			FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta, ManualOnFailure: record.ManualOnFailure,
			PreConsumeRequestID: record.PreConsumeRequestID, PreConsumeModelName: record.PreConsumeModelName,
			PreConsumeRequestedQuota: record.PreConsumeRequestedQuota, PreConsumeEffectiveQuota: record.PreConsumeEffectiveQuota})
		require.ErrorIs(t, err, model.ErrBillingSettlementManualReview)
		require.Zero(t, applied)
		model.ProcessPendingBillingSettlementsOnce()
		require.EqualValues(t, 100, getUserQuota(t, 991))
		require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
	})
}

func TestPlaygroundWalletReserveKeepsTokenZeroAndExactFunding(t *testing.T) {
	for _, initial := range []int{0, 10} {
		for _, ending := range []int{-1, 0, 6, 14, 18} {
			t.Run(fmt.Sprintf("initial_%d/ending_%d", initial, ending), func(t *testing.T) {
				seedPlaygroundAtomic(t)
				c, info := playgroundAtomicContext("playground-extra-reserve", true)
				session, apiErr := NewBillingSession(c, info, initial)
				require.Nil(t, apiErr)
				require.NoError(t, session.Reserve(14))
				require.NoError(t, session.Reserve(14))
				require.Equal(t, 14, session.GetPreConsumedQuota())
				require.Zero(t, session.tokenConsumed)
				require.EqualValues(t, 86, getUserQuota(t, 991))
				require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
				want := ending
				if ending == -1 {
					require.True(t, session.NeedsRefund())
					session.Refund(c)
					session.Refund(c)
					want = 0
				} else {
					effect := &model.BillingSettlementEffect{LogType: model.LogTypeConsume, ModelName: info.OriginModelName,
						RequestID: info.RequestId, Quota: int64(ending), QuotaIsActual: true, UpdateUsage: true, PromptTokens: 1, CompletionTokens: 1}
					require.NoError(t, session.SettleWithEffect(ending, effect))
					require.NoError(t, session.SettleWithEffect(ending, effect))
				}
				model.ProcessPendingBillingSettlementsOnce()
				model.ProcessPendingBillingSettlementsOnce()
				require.EqualValues(t, 100-want, getUserQuota(t, 991))
				require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
				var final model.BillingSettlement
				require.NoError(t, model.DB.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).First(&final).Error)
				require.EqualValues(t, want-14, final.AppliedFundingDelta)
				require.Zero(t, final.AppliedTokenDelta)
			})
		}
	}
}

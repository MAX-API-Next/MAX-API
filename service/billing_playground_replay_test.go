package service

import (
	"errors"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPlaygroundWalletPaidReplayRejectsBeforeContextUse(t *testing.T) {
	for _, missingContext := range []bool{false, true} {
		name := "real_context"
		if missingContext {
			name = "nil_context"
		}
		t.Run(name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 991, 100)
			seedToken(t, 992, 991, "synthetic-playground-guard", 100)
			ctx, _ := gin.CreateTestContext(nil)
			info := &relaycommon.RelayInfo{RequestId: "paid-playground-guard", UserId: 991, TokenId: 992,
				TokenKey: "synthetic-playground-guard", OriginModelName: "guard-model", ForcePreConsume: true}
			paid := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 991}}
			require.Nil(t, paid.preConsume(ctx, 10))
			replayInfo := &relaycommon.RelayInfo{RequestId: info.RequestId, UserId: info.UserId, TokenId: info.TokenId,
				TokenKey: info.TokenKey, OriginModelName: info.OriginModelName, ForcePreConsume: true, IsPlayground: true}
			replay := &BillingSession{relayInfo: replayInfo, funding: &WalletFunding{userId: 991}}
			if missingContext {
				ctx = nil
			}
			var apiErr *types.MaxAPIError
			require.NotPanics(t, func() { apiErr = replay.preConsume(ctx, 10) })
			require.NotNil(t, apiErr)
			require.ErrorIs(t, apiErr, model.ErrBillingSettlementOperationConflict)
			require.True(t, types.IsSkipRetryError(apiErr))
			require.EqualValues(t, 90, getUserQuota(t, 991))
			require.EqualValues(t, 90, getTokenRemainQuota(t, 992))
		})
	}
}

func TestPlaygroundWalletReplayLookupFailureFailsClosed(t *testing.T) {
	for _, missingDatabase := range []bool{false, true} {
		name := "query_failure"
		if missingDatabase {
			name = "missing_database"
		}
		t.Run(name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 991, 100)
			seedToken(t, 992, 991, "synthetic-playground-lookup", 100)
			db := model.DB
			const callbackName = "test:playground-selection-query-failure"
			if missingDatabase {
				model.DB = nil
				t.Cleanup(func() { model.DB = db })
			} else {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
					if tx.Statement.Table == "billing_pre_consume_selections" {
						tx.AddError(errors.New("synthetic selection query failure"))
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
			}
			session := &BillingSession{funding: &WalletFunding{userId: 991}, relayInfo: &relaycommon.RelayInfo{
				RequestId: "unknown-playground-identity", UserId: 991, IsPlayground: true, ForcePreConsume: true}}
			var apiErr *types.MaxAPIError
			require.NotPanics(t, func() { apiErr = session.preConsume(nil, 10) })
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			model.DB = db
			if !missingDatabase {
				require.NoError(t, db.Callback().Query().Remove(callbackName))
			}
			require.EqualValues(t, 100, getUserQuota(t, 991))
			require.EqualValues(t, 100, getTokenRemainQuota(t, 992))
		})
	}
}

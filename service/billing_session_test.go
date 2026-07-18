package service

import (
	"errors"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
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
	require.NotEmpty(t, funding.deltas)
	require.True(t, session.compensationFailed)
	require.EqualValues(t, 5, session.appliedFundingDelta)

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

func TestPreConsumeQuotaTrustsFiniteInt64TokenQuota(t *testing.T) {
	truncate(t)
	trustQuota := common.GetTrustQuota()
	seedUser(t, 71, trustQuota+100)
	seedToken(t, 72, 71, "finite-int64-token", trustQuota+100)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("token_quota", int64(trustQuota+100))
	relayInfo := &relaycommon.RelayInfo{
		UserId: 71, TokenId: 72, TokenKey: "finite-int64-token",
	}

	require.Nil(t, PreConsumeQuota(ctx, 10, relayInfo))
	assert.Zero(t, relayInfo.FinalPreConsumedQuota)

	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	assert.EqualValues(t, trustQuota+100, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, 72).Error)
	assert.EqualValues(t, trustQuota+100, token.RemainQuota)
}

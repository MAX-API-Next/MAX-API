package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
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

type exhaustUserAfterTokenCacheReadHook struct {
	userID    int
	tokenKey  string
	exhausted bool
	err       error
}

func (h *exhaustUserAfterTokenCacheReadHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (h *exhaustUserAfterTokenCacheReadHook) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	args := cmd.Args()
	if h.exhausted || cmd.Name() != "hgetall" || len(args) < 2 || fmt.Sprint(args[1]) != h.tokenKey {
		return nil
	}
	h.exhausted = true
	h.err = model.DB.Model(&model.User{}).Where("id = ?", h.userID).Update("quota", 0).Error
	return nil
}

func (*exhaustUserAfterTokenCacheReadHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*exhaustUserAfterTokenCacheReadHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestPreConsumeQuotaMapsConcurrentUserExhaustion(t *testing.T) {
	truncate(t)
	seedUser(t, 73, 10)
	seedToken(t, 74, 73, "concurrent-user-exhaustion", 10)

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
	userCacheKey := "user:73"
	tokenCacheKey := fmt.Sprintf("token:%s", common.GenerateHMAC("concurrent-user-exhaustion"))
	require.NoError(t, client.HSet(context.Background(), userCacheKey, map[string]interface{}{
		"Id": 73, "Quota": 10, "Role": common.RoleCommonUser, "Status": common.UserStatusEnabled,
	}).Err())
	require.NoError(t, client.HSet(context.Background(), tokenCacheKey, map[string]interface{}{
		"Id": 74, "UserId": 73, "Status": common.TokenStatusEnabled, "RemainQuota": 10,
	}).Err())
	hook := &exhaustUserAfterTokenCacheReadHook{userID: 73, tokenKey: tokenCacheKey}
	client.AddHook(hook)

	ctx, _ := gin.CreateTestContext(nil)
	apiErr := PreConsumeQuota(ctx, 7, &relaycommon.RelayInfo{
		UserId: 73, TokenId: 74, TokenKey: "concurrent-user-exhaustion",
	})
	require.NotNil(t, apiErr)
	require.NoError(t, hook.err)
	assert.True(t, hook.exhausted)
	assert.ErrorIs(t, apiErr, model.ErrUserQuotaInsufficient)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, types.IsRecordErrorLog(apiErr))
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

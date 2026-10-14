package relay

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var requestReplayRoutes = []string{"responses_native", "chat_native", "chat_to_responses", "responses_to_chat_custom", "responses_to_chat_global", "claude_to_responses", "gemini_to_responses"}

type requestReplayFixture struct {
	db                      *gorm.DB
	funding                 streamFundingCase
	requestID               string
	c                       *gin.Context
	info                    *relaycommon.RelayInfo
	record                  model.BillingSettlement
	effect                  model.BillingSettlementEffect
	quota, prompt, complete int
	rollback                *atomic.Bool
	sessions                []*service.BillingSession
}

func initRequestReplayTests(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmpty, oldRetries := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.EmptyCompletionRetryEnabled, common.RetryTimes = false, 2
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldEmpty, oldRetries })
}

// Each fixture produces its finalize intent through real admission, HTTP and
// an adapter/Helper. The injected error is inside the funding transaction,
// immediately before its applied marker; all balance legs must roll back.
func newRequestReplayFixture(t *testing.T, route string, funding streamFundingCase, usage string, pending bool, sessionCount int) *requestReplayFixture {
	t.Helper()
	f := &requestReplayFixture{db: setupStreamFundingLedger(t, funding), funding: funding, prompt: 4, complete: 2, rollback: &atomic.Bool{}}
	switch usage {
	case "at_reservation":
		f.prompt, f.complete = 80, 20
	case "above_reservation":
		f.prompt, f.complete = 80, 50
	case "zero":
		f.prompt, f.complete = 0, 0
	}
	f.quota = f.prompt + f.complete
	channelType, frames := streamFundingFrames(t, route, usage)
	frames = append(frames, `{broken`)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
	}))
	t.Cleanup(upstream.Close)
	fingerprint := sha256.Sum256([]byte(t.Name()))
	f.requestID = fmt.Sprintf("request-replay-%x", fingerprint[:16])
	f.c, f.info, _ = streamFundingContext(t, route, f.requestID, upstream.URL, channelType, funding)
	require.Nil(t, service.PreConsumeBilling(f.c, 100, f.info))
	require.Equal(t, funding.source, f.info.BillingSource)
	// Sessions are reconstructed before unresolved positive funding blocks new
	// admission. Their pre-consume replay must not reserve a second time.
	for range sessionCount {
		info := &relaycommon.RelayInfo{RequestId: f.requestID, UserId: 951, TokenId: 952, TokenKey: f.info.TokenKey,
			OriginModelName: f.info.OriginModelName, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
		if funding.source == "wallet" {
			info.UserSetting.BillingPreference = "subscription_only"
		}
		session, apiErr := service.NewBillingSession(f.c, info, 100)
		require.Nil(t, apiErr)
		require.Equal(t, funding.source, info.BillingSource)
		f.sessions = append(f.sessions, session)
	}
	f.rollback.Store(pending)
	const callbackName = "test:request-finalize-rollback"
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if f.rollback.Load() && tx.Statement.Table == "billing_settlements" && ok && updates["status"] == model.BillingSettlementStatusApplied {
			tx.AddError(errors.New("synthetic request finalize transaction failure"))
		}
	}))
	t.Cleanup(func() { _ = f.db.Callback().Update().Remove(callbackName) })
	var apiErr *types.MaxAPIError
	if f.info.RelayFormat == types.RelayFormatOpenAI {
		apiErr = TextHelper(f.c, f.info)
	} else {
		apiErr = ResponsesHelper(f.c, f.info)
	}
	require.NotNil(t, apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
	service.HandleFailedBilling(f.c, f.info, apiErr)
	service.HandleFailedBilling(f.c, f.info, apiErr)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, f.db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(f.requestID)).First(&f.record).Error)
	require.EqualValues(t, f.quota-100, f.record.FundingDelta)
	require.EqualValues(t, f.quota-100, f.record.TokenDelta)
	require.NotEmpty(t, f.record.EffectPayload)
	require.NoError(t, common.UnmarshalJsonStr(f.record.EffectPayload, &f.effect))
	require.True(t, f.effect.QuotaIsActual)
	require.EqualValues(t, f.quota, f.effect.Quota)
	if pending {
		require.Equal(t, model.BillingSettlementStatusPending, f.record.Status)
		require.Zero(t, f.record.AppliedFundingDelta)
		require.Zero(t, f.record.AppliedTokenDelta)
		assertRequestReplayReservation(t, f)
	} else {
		require.Equal(t, model.BillingSettlementStatusApplied, f.record.Status)
		assertStreamFundingBalances(t, f.db, funding, f.requestID, f.quota, true, f.prompt, f.complete)
	}
	return f
}

func assertRequestReplayReservation(t *testing.T, f *requestReplayFixture) {
	t.Helper()
	var user model.User
	var token model.Token
	var sub model.UserSubscription
	var channel model.Channel
	require.NoError(t, f.db.First(&user, 951).Error)
	require.NoError(t, f.db.First(&token, 952).Error)
	require.NoError(t, f.db.First(&sub, 955).Error)
	require.NoError(t, f.db.First(&channel, 953).Error)
	wallet, subUsed := f.funding.wallet, f.funding.subscriptionUsed
	if f.funding.source == "wallet" {
		wallet -= 100
	} else {
		subUsed += 100
	}
	require.EqualValues(t, wallet, user.Quota)
	require.EqualValues(t, subUsed, sub.AmountUsed)
	require.EqualValues(t, 900, token.RemainQuota)
	require.EqualValues(t, 100, token.UsedQuota)
	require.Zero(t, user.UsedQuota)
	require.Zero(t, user.RequestCount)
	require.Zero(t, channel.UsedQuota)
	var logs, receipts int64
	require.NoError(t, f.db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
	require.NoError(t, f.db.Model(&model.BillingLogReceipt{}).Count(&receipts).Error)
	require.Zero(t, logs)
	require.Zero(t, receipts)
}

func requestReplayInput(record model.BillingSettlement, effect *model.BillingSettlementEffect) model.BillingSettlementInput {
	return model.BillingSettlementInput{
		OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID,
		SubscriptionID: record.SubscriptionID, TokenID: record.TokenID,
		FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta,
		TaskID: record.TaskID, TaskQuota: record.TaskQuota, TaskQuotaTarget: record.TaskQuotaTarget,
		SubscriptionPreConsumeRequestID: record.SubscriptionPreConsumeRequestID,
		FinalizeSubscriptionPreConsume:  record.FinalizeSubscriptionPreConsume,
		AllowMissingToken:               record.AllowMissingToken, ManualOnFailure: record.ManualOnFailure,
		PreConsumeRequestID: record.PreConsumeRequestID, PreConsumeModelName: record.PreConsumeModelName,
		PreConsumeRequestedQuota: record.PreConsumeRequestedQuota, PreConsumeEffectiveQuota: record.PreConsumeEffectiveQuota,
		Effect: effect,
	}
}

// Include durable records and all projections, not just the primary balance.
func requestReplaySnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var snapshot struct {
		Users       []model.User
		Tokens      []model.Token
		Channels    []model.Channel
		Subs        []model.UserSubscription
		Reserved    []model.SubscriptionPreConsumeRecord
		Selections  []model.BillingPreConsumeSelection
		Settlements []model.BillingSettlement
		Logs        []model.Log
		Receipts    []model.BillingLogReceipt
		Outbox      []model.CacheInvalidationTask
	}
	for _, target := range []any{&snapshot.Users, &snapshot.Tokens, &snapshot.Channels, &snapshot.Subs, &snapshot.Reserved,
		&snapshot.Selections, &snapshot.Settlements, &snapshot.Logs, &snapshot.Receipts, &snapshot.Outbox} {
		require.NoError(t, db.Order("id").Find(target).Error)
	}
	encoded, err := common.Marshal(snapshot)
	require.NoError(t, err)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func requestReplayMutations() []struct {
	name   string
	mutate func(*model.BillingSettlementInput)
} {
	return []struct {
		name   string
		mutate func(*model.BillingSettlementInput)
	}{
		{"user", func(input *model.BillingSettlementInput) { input.UserID = 958 }},
		{"token", func(input *model.BillingSettlementInput) { input.TokenID = 959 }},
		{"source", func(input *model.BillingSettlementInput) {
			input.Source = model.BillingSettlementSourceWallet
			if input.Source == "wallet" && input.SubscriptionID == 0 {
				input.Source = model.BillingSettlementSourceSubscription
			}
		}},
		{"subscription", func(input *model.BillingSettlementInput) { input.SubscriptionID = 956 }},
		{"funding_delta", func(input *model.BillingSettlementInput) { input.FundingDelta++ }},
		{"token_delta", func(input *model.BillingSettlementInput) { input.TokenDelta++ }},
		{"reservation_request", func(input *model.BillingSettlementInput) {
			input.SubscriptionPreConsumeRequestID = "another-reservation"
		}},
		{"finalize_reservation", func(input *model.BillingSettlementInput) {
			input.FinalizeSubscriptionPreConsume = !input.FinalizeSubscriptionPreConsume
		}},
		{"missing_token_policy", func(input *model.BillingSettlementInput) { input.AllowMissingToken = !input.AllowMissingToken }},
		{"manual_policy", func(input *model.BillingSettlementInput) { input.ManualOnFailure = !input.ManualOnFailure }},
		{"task_lifecycle", func(input *model.BillingSettlementInput) { input.TaskID = 987 }},
		{"task_quota", func(input *model.BillingSettlementInput) { input.TaskQuota++ }},
		{"task_target", func(input *model.BillingSettlementInput) { input.TaskQuotaTarget++ }},
	}
}

func TestRequestFinalizeIdentityRejectsConflictingReplay(t *testing.T) {
	initRequestReplayTests(t)
	for _, route := range requestReplayRoutes {
		for _, funding := range streamFundingCases() {
			for _, usage := range []string{"known", "at_reservation", "above_reservation", "zero"} {
				for _, pending := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/pending_%t", route, funding.name, usage, pending), func(t *testing.T) {
						f := newRequestReplayFixture(t, route, funding, usage, pending, 0)
						require.NoError(t, f.db.Create(&model.User{Id: 958, Username: "request-replay-decoy", AffCode: "replay-decoy", Quota: 1000}).Error)
						require.NoError(t, f.db.Create(&model.Token{Id: 959, UserId: 958, Key: "synthetic-replay-decoy", RemainQuota: 1000}).Error)
						require.NoError(t, f.db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
							StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
						before := requestReplaySnapshot(t, f.db)
						for _, mutation := range requestReplayMutations() {
							t.Run(mutation.name, func(t *testing.T) {
								input := requestReplayInput(f.record, &f.effect)
								mutation.mutate(&input)
								applied, already, err := model.ApplyBillingSettlementOnce(input)
								require.ErrorIs(t, err, model.ErrBillingSettlementOperationConflict)
								require.Zero(t, applied)
								require.False(t, already)
								require.Equal(t, before, requestReplaySnapshot(t, f.db), "conflicting replay must change no ledger or projection")
							})
						}
						f.rollback.Store(false)
						for _, control := range []string{"effect_omitted", "description_drift"} {
							t.Run(control, func(t *testing.T) {
								input := requestReplayInput(f.record, nil)
								if control == "description_drift" {
									effect := f.effect
									effect.Content, effect.ModelName = "different harmless description", "description-only-model"
									input.Effect = &effect
								}
								for range 2 {
									applied, _, err := model.ApplyBillingSettlementOnce(input)
									require.NoError(t, err)
									require.EqualValues(t, f.quota-100, applied)
									require.NoError(t, model.ProcessBillingSettlementEffect(input.OperationKey))
								}
								var record model.BillingSettlement
								require.NoError(t, f.db.Where("operation_key = ?", input.OperationKey).First(&record).Error)
								require.Equal(t, f.record.EffectPayload, record.EffectPayload)
								require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
								assertStreamFundingBalances(t, f.db, funding, f.requestID, f.quota, true, f.prompt, f.complete)
							})
						}
					})
				}
			}
		}
	}
}

func TestRequestAdmissionRejectsIdentityAndReservationDrift(t *testing.T) {
	initRequestReplayTests(t)
	for _, funding := range streamFundingCases() {
		t.Run(funding.name, func(t *testing.T) {
			for _, mutation := range []struct {
				name   string
				mutate func(*relaycommon.RelayInfo, *int)
			}{
				{"user", func(info *relaycommon.RelayInfo, _ *int) { info.UserId = 958 }},
				{"token", func(info *relaycommon.RelayInfo, _ *int) {
					info.TokenId, info.TokenKey = 959, "synthetic-admission-decoy"
				}},
				{"model", func(info *relaycommon.RelayInfo, _ *int) { info.OriginModelName = "another-model" }},
				{"playground", func(info *relaycommon.RelayInfo, _ *int) { info.IsPlayground = true }},
				{"reservation_zero", func(_ *relaycommon.RelayInfo, quota *int) { *quota = 0 }},
				{"reservation_lower", func(_ *relaycommon.RelayInfo, quota *int) { *quota = 99 }},
				{"reservation_higher", func(_ *relaycommon.RelayInfo, quota *int) { *quota = 101 }},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					f := newRequestReplayFixture(t, "responses_native", funding, "known", false, 0)
					require.NoError(t, f.db.Create(&model.User{Id: 958, Username: "admission-replay-decoy", AffCode: "admission-decoy", Quota: 1000}).Error)
					require.NoError(t, f.db.Create(&model.Token{Id: 959, UserId: 958, Key: "synthetic-admission-decoy", RemainQuota: 1000}).Error)
					before := requestReplaySnapshot(t, f.db)
					var beforeUser, afterUser model.User
					require.NoError(t, f.db.First(&beforeUser, 951).Error)
					info := &relaycommon.RelayInfo{RequestId: f.requestID, UserId: 951, TokenId: 952, TokenKey: f.info.TokenKey,
						OriginModelName: f.info.OriginModelName, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: funding.preference}}
					quota := 100
					mutation.mutate(info, &quota)
					session, apiErr := service.NewBillingSession(f.c, info, quota)
					require.NoError(t, f.db.First(&afterUser, 951).Error)
					t.Logf("wallet quota before/after admission replay: %d/%d", beforeUser.Quota, afterUser.Quota)
					require.NotNil(t, apiErr)
					require.Nil(t, session)
					require.Equal(t, before, requestReplaySnapshot(t, f.db), "conflicting admission must not reserve or project again")
				})
			}
		})
	}
}

func TestRequestPlaygroundFreshIdentityKeepsFundingRules(t *testing.T) {
	initRequestReplayTests(t)
	for _, funding := range streamFundingCases() {
		t.Run(funding.name, func(t *testing.T) {
			db := setupStreamFundingLedger(t, funding)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RequestId: "fresh-playground", UserId: 951, IsPlayground: true, ForcePreConsume: true,
				OriginModelName: "gpt-test", UserSetting: dto.UserSetting{BillingPreference: funding.preference}}
			session, apiErr := service.NewBillingSession(c, info, 100)
			require.Nil(t, apiErr)
			require.Equal(t, funding.source, info.BillingSource)
			require.NoError(t, session.Settle(6))
			session.Refund(c)
			var user model.User
			var token model.Token
			var sub model.UserSubscription
			require.NoError(t, db.First(&user, 951).Error)
			require.NoError(t, db.First(&token, 952).Error)
			require.NoError(t, db.First(&sub, 955).Error)
			wallet, subUsed := funding.wallet, funding.subscriptionUsed
			if funding.source == "wallet" {
				wallet -= 6
			} else {
				subUsed += 6
			}
			require.EqualValues(t, wallet, user.Quota)
			require.EqualValues(t, subUsed, sub.AmountUsed)
			require.EqualValues(t, 1000, token.RemainQuota)
			require.Zero(t, token.UsedQuota)
			var record model.BillingSettlement
			require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).First(&record).Error)
			require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
			require.EqualValues(t, -94, record.AppliedFundingDelta)
			require.Zero(t, record.AppliedTokenDelta)
		})
	}
}

func TestRequestFinalizeConcurrentSettlementRefundAndRecovery(t *testing.T) {
	initRequestReplayTests(t)
	for _, route := range requestReplayRoutes {
		for _, funding := range streamFundingCases() {
			for _, usage := range []string{"known", "at_reservation", "above_reservation", "zero"} {
				t.Run(strings.Join([]string{route, funding.name, usage}, "/"), func(t *testing.T) {
					f := newRequestReplayFixture(t, route, funding, usage, true, 6)
					f.rollback.Store(false)
					require.NoError(t, f.db.Model(&model.BillingSettlement{}).Where("id = ?", f.record.ID).Update("next_attempt", 0).Error)
					start := make(chan struct{})
					results := make(chan error, 7)
					var workers sync.WaitGroup
					launch := func(work func()) {
						workers.Add(1)
						go func() { defer workers.Done(); <-start; work() }()
					}
					for _, session := range f.sessions[:3] {
						launch(func() { results <- session.SettleWithEffect(f.quota, &f.effect) })
					}
					for _, session := range f.sessions[3:] {
						launch(func() { session.Refund(f.c) })
					}
					for range 4 {
						launch(func() {
							applied, _, err := model.ApplyBillingSettlementOnce(requestReplayInput(f.record, &f.effect))
							if err == nil && applied != int64(f.quota-100) {
								err = fmt.Errorf("recorded delta differs: got %d", applied)
							}
							results <- err
						})
					}
					for range 3 {
						launch(model.ProcessPendingBillingSettlementsOnce)
					}
					close(start)
					workers.Wait()
					close(results)
					for err := range results {
						require.NoError(t, err)
					}
					model.ProcessPendingBillingSettlementsOnce()
					model.ProcessPendingBillingSettlementsOnce()
					var record model.BillingSettlement
					require.NoError(t, f.db.Where("id = ?", f.record.ID).First(&record).Error)
					require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
					require.Equal(t, model.BillingSettlementEffectApplied, record.EffectStatus)
					require.EqualValues(t, f.quota-100, record.AppliedFundingDelta)
					require.EqualValues(t, f.quota-100, record.AppliedTokenDelta)
					require.Equal(t, f.record.EffectPayload, record.EffectPayload)
					assertStreamFundingBalances(t, f.db, funding, f.requestID, f.quota, true, f.prompt, f.complete)
					var receipts int64
					require.NoError(t, f.db.Model(&model.BillingLogReceipt{}).Where("operation_key = ?", record.OperationKey).Count(&receipts).Error)
					require.EqualValues(t, 1, receipts)
				})
			}
		}
	}
}

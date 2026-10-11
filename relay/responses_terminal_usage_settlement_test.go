package relay

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type terminalUsageFailureWriter struct {
	*httptest.ResponseRecorder
	failure   string
	triggered bool
}

func (w *terminalUsageFailureWriter) Write(data []byte) (int, error) {
	if strings.HasPrefix(string(data), "event: response.output_text.done\n") {
		w.triggered = true
		switch w.failure {
		case "write":
			return 0, io.ErrClosedPipe
		case "short":
			return len(data) - 1, nil
		}
	}
	return w.ResponseRecorder.Write(data)
}

func (w *terminalUsageFailureWriter) FlushError() error {
	if w.triggered && w.failure == "flush" {
		return io.ErrClosedPipe
	}
	w.ResponseRecorder.Flush()
	return nil
}

// Provider accounting has already arrived when a metadata-only terminal write
// fails. Run through real HTTP, ResponsesHelper and the durable funding/effect
// path: a disconnect cannot turn final zero into a positive estimated charge.
func TestResponsesTerminalMetadataFailurePreservesFinalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	for _, provider := range []struct{ name, finishReason string }{
		{"chat", "stop"}, {"gemini_stop", "STOP"}, {"gemini_length", "MAX_TOKENS"}, {"gemini_safety", "SAFETY"},
	} {
		for _, funding := range streamFundingCases() {
			for _, usageKind := range []string{"known", "zero", "at_reservation", "above_reservation", "missing", "empty"} {
				for _, previousUsage := range []bool{false, true} {
					for _, failure := range []string{"write", "short", "flush"} {
						t.Run(fmt.Sprintf("%s/%s/%s/previous_%t/%s", provider.name, funding.name, usageKind, previousUsage, failure), func(t *testing.T) {
							db := setupStreamFundingLedger(t, funding)
							prompt, completion := 4, 2
							switch usageKind {
							case "zero":
								prompt, completion = 0, 0
							case "at_reservation":
								prompt, completion = 80, 20
							case "above_reservation":
								prompt, completion = 80, 50
							case "missing", "empty":
								prompt, completion = 10, service.EstimateTokenByModel("gpt-test", "visible")
								if previousUsage {
									completion = 2
								}
							}
							channelType, route := constant.ChannelTypeAdvancedCustom, "chat_to_responses"
							first := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "visible"}}}}
							last := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}}
							usageField := "usage"
							counts := map[string]int{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
							previousCounts := map[string]int{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}
							if strings.HasPrefix(provider.name, "gemini_") {
								channelType, route = constant.ChannelTypeGemini, "gemini_to_responses"
								first = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "visible"}}}}}}
								last = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{}}, "finishReason": provider.finishReason}}}
								usageField = "usageMetadata"
								counts = map[string]int{"promptTokenCount": prompt, "candidatesTokenCount": completion, "totalTokenCount": prompt + completion}
								previousCounts = map[string]int{"promptTokenCount": 10, "candidatesTokenCount": 2, "totalTokenCount": 12}
							}
							if previousUsage {
								first[usageField] = previousCounts
							}
							if usageKind == "empty" {
								last[usageField] = map[string]any{}
							} else if usageKind != "missing" {
								last[usageField] = counts
							}
							frames := []string{}
							for _, frame := range []map[string]any{first, last} {
								encoded, err := common.Marshal(frame)
								require.NoError(t, err)
								frames = append(frames, string(encoded))
							}
							upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
								w.Header().Set("Content-Type", "text/event-stream")
								_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
							}))
							t.Cleanup(upstream.Close)
							fingerprint := sha256.Sum256([]byte(t.Name()))
							requestID := fmt.Sprintf("terminal-usage-%x", fingerprint[:16])
							c, info, recorder := streamFundingContext(t, route, requestID, upstream.URL, channelType, funding)
							writer := &terminalUsageFailureWriter{ResponseRecorder: recorder, failure: failure}
							failureContext, _ := gin.CreateTestContext(writer)
							c.Writer = failureContext.Writer
							require.Nil(t, service.PreConsumeBilling(c, 100, info))
							// A preference change and earlier-expiring plan cannot take
							// ownership of an already reserved generation.
							require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
								StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
							info.UserSetting.BillingPreference = "wallet_only"
							if funding.source == "wallet" {
								info.UserSetting.BillingPreference = "subscription_only"
							}
							apiErr := ResponsesHelper(c, info)
							require.NotNil(t, apiErr)
							require.True(t, types.IsSkipRetryError(apiErr))
							require.True(t, writer.triggered, "failure must follow delivery and receipt of final usage")
							require.Contains(t, recorder.Body.String(), `"delta":"visible"`)
							require.NotContains(t, recorder.Body.String(), "event: response.completed")
							service.HandleFailedBilling(c, info, apiErr)
							service.HandleFailedBilling(c, info, apiErr)
							quota := prompt + completion
							var settlement model.BillingSettlement
							require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
							require.Equal(t, funding.source, settlement.Source)
							require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
							require.EqualValues(t, quota-100, settlement.AppliedFundingDelta, "final usage must survive metadata transport failure")
							require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
							var effect model.BillingSettlementEffect
							require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
							require.Equal(t, prompt, effect.PromptTokens)
							require.Equal(t, completion, effect.CompletionTokens)
							require.EqualValues(t, quota, effect.Quota)
							require.True(t, effect.QuotaIsActual)
							assertStreamFundingBalances(t, db, funding, requestID, quota, true, prompt, completion)
							var beforeOutbox int64
							require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&beforeOutbox).Error)
							sessions := []*service.BillingSession{}
							for range 2 {
								replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: info.UserId, TokenId: info.TokenId, TokenKey: info.TokenKey,
									OriginModelName: info.OriginModelName, ForcePreConsume: true, UserSetting: info.UserSetting}
								session, replayErr := service.NewBillingSession(c, replayInfo, 100)
								require.Nil(t, replayErr)
								require.Equal(t, funding.source, replayInfo.BillingSource)
								sessions = append(sessions, session)
							}
							var workers sync.WaitGroup
							results := make(chan error, len(sessions))
							for _, session := range sessions {
								workers.Add(1)
								go func() {
									defer workers.Done()
									results <- session.SettleWithEffect(quota, &effect)
									session.Refund(c)
								}()
							}
							workers.Wait()
							close(results)
							for err := range results {
								require.NoError(t, err)
							}
							model.ProcessPendingBillingSettlementsOnce()
							model.ProcessPendingBillingSettlementsOnce()
							var afterOutbox int64
							require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&afterOutbox).Error)
							require.Equal(t, beforeOutbox, afterOutbox)
							var decoy model.UserSubscription
							require.NoError(t, db.First(&decoy, 956).Error)
							require.Zero(t, decoy.AmountUsed)
							assertStreamFundingBalances(t, db, funding, requestID, quota, true, prompt, completion)
						})
					}
				}
			}
		}
	}
}

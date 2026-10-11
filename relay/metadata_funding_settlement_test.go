package relay

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func metadataFundingFrames(t *testing.T, route, usage string) (int, []string) {
	t.Helper()
	prompt, completion := 4, 2
	if usage == "zero" {
		prompt, completion = 0, 0
	}
	encode := func(v map[string]any) string {
		data, err := common.Marshal(v)
		require.NoError(t, err)
		return string(data)
	}
	switch route {
	case "chat_native", "chat_to_responses":
		frame := map[string]any{"id": "chat_metadata", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}}}
		if usage != "missing" {
			frame["usage"] = map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
		}
		channel := constant.ChannelTypeOpenAI
		if route == "chat_to_responses" {
			channel = constant.ChannelTypeAdvancedCustom
		}
		return channel, []string{encode(frame)}
	case "claude_to_responses":
		message := map[string]any{"id": "msg_metadata", "model": "gpt-test", "role": "assistant", "content": []any{}}
		if usage != "missing" {
			message["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": completion}
		}
		return constant.ChannelTypeAnthropic, []string{encode(map[string]any{"type": "message_start", "message": message})}
	case "gemini_to_responses":
		frame := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{}}}}}
		if usage != "missing" {
			frame["usageMetadata"] = map[string]any{"promptTokenCount": prompt, "candidatesTokenCount": completion, "totalTokenCount": prompt + completion}
		}
		return constant.ChannelTypeGemini, []string{encode(frame)}
	default:
		response := map[string]any{"id": "resp_metadata", "model": "gpt-test", "status": "in_progress", "output": []any{}}
		if usage != "missing" {
			response["usage"] = map[string]any{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}
		}
		channel := constant.ChannelTypeOpenAI
		if route == "responses_to_chat_custom" {
			channel = constant.ChannelTypeAdvancedCustom
		}
		return channel, []string{encode(map[string]any{"type": "response.created", "response": response})}
	}
}

// Cancel at the real downstream flush, after metadata was handled. A server
// write alone would not prove the adapter had processed the metadata.
type metadataCancelWriter struct {
	gin.ResponseWriter
	cancel    context.CancelFunc
	triggered atomic.Bool
}

func (w *metadataCancelWriter) Flush() {
	w.ResponseWriter.Flush()
	if w.Size() > 0 && w.triggered.CompareAndSwap(false, true) {
		w.cancel()
	}
}

func TestMetadataOnlyFailureRetainsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmptyRetry := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmptyRetry })
	for _, route := range []string{"responses_native", "chat_native", "chat_to_responses", "responses_to_chat_custom", "responses_to_chat_global", "claude_to_responses", "gemini_to_responses"} {
		for _, funding := range streamFundingCases() {
			for _, usage := range []string{"known", "zero", "missing"} {
				for _, ending := range []string{"eof", "transport", "malformed", "cancel", "timeout", "eof_terminal_write_failure"} {
					if ending == "eof_terminal_write_failure" && route != "chat_to_responses" && route != "claude_to_responses" && route != "gemini_to_responses" {
						continue
					}
					// Native Chat's normal EOF keeps its legacy empty-completion
					// pricing policy. Its abnormal endings belong in this matrix.
					if route == "chat_native" && ending == "eof" {
						continue
					}
					bufferedMetadata := strings.HasPrefix(route, "responses_to_chat_")
					// These converters defer Chat's role event until output. There
					// is no downstream metadata flush at which to cancel a client.
					if bufferedMetadata && ending == "cancel" {
						continue
					}
					t.Run(strings.Join([]string{route, funding.name, usage, ending}, "/"), func(t *testing.T) {
						db := setupStreamFundingLedger(t, funding)
						oldMonitor := *operation_setting.GetMonitorSetting()
						*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{StreamingFirstResultTimeoutSeconds: 1}
						t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldMonitor })
						channelType, frames := metadataFundingFrames(t, route, usage)
						if ending == "malformed" {
							frames = append(frames, `{broken`)
						}
						var calls atomic.Int32
						upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							w.Header().Set("Content-Type", "text/event-stream")
							if ending == "transport" {
								w.Header().Set("Content-Length", "100000")
							}
							_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
							w.(http.Flusher).Flush()
							if ending == "timeout" || ending == "cancel" {
								select {
								case <-r.Context().Done():
								case <-time.After(3 * time.Second):
								}
							}
						}))
						t.Cleanup(upstream.Close)
						fingerprint := sha256.Sum256([]byte(t.Name()))
						requestID := fmt.Sprintf("metadata-%x", fingerprint[:16])
						c, info, recorder := streamFundingContext(t, route, requestID, upstream.URL, channelType, funding)
						require.Nil(t, service.PreConsumeBilling(c, 100, info))
						require.Equal(t, funding.source, info.BillingSource)
						var selection model.BillingPreConsumeSelection
						require.NoError(t, db.Where("request_id = ?", requestID).First(&selection).Error)
						require.Equal(t, funding.source, selection.Source)
						require.EqualValues(t, 100, selection.EffectiveQuota)
						// A newly preferred subscription or changed preference must
						// never receive this request's failure refund.
						require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
							StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
						info.UserSetting.BillingPreference = "wallet_only"
						if funding.source == "wallet" {
							info.UserSetting.BillingPreference = "subscription_only"
						}
						var cancelWriter *metadataCancelWriter
						if ending == "eof_terminal_write_failure" {
							failureContext, _ := gin.CreateTestContext(&customToolEOFWriter{ResponseRecorder: recorder, ending: "terminal_write_failure"})
							c.Writer = failureContext.Writer
						}
						if ending == "cancel" {
							ctx, cancel := context.WithCancel(c.Request.Context())
							t.Cleanup(cancel)
							c.Request = c.Request.WithContext(ctx)
							cancelWriter = &metadataCancelWriter{ResponseWriter: c.Writer, cancel: cancel}
							c.Writer = cancelWriter
						}
						var apiErr *types.MaxAPIError
						if info.RelayFormat == types.RelayFormatOpenAI {
							apiErr = TextHelper(c, info)
						} else {
							apiErr = ResponsesHelper(c, info)
						}
						require.NotNil(t, apiErr)
						require.False(t, info.HasRecordedChannelFirstResult())
						require.True(t, info.FirstResultTime.IsZero())
						if bufferedMetadata {
							require.Empty(t, recorder.Body.String(), "created metadata remains buffered for Chat clients")
						} else {
							require.NotEmpty(t, recorder.Body.String(), "metadata was actually delivered")
						}
						require.NotContains(t, recorder.Body.String(), "response.completed")
						if ending == "timeout" {
							require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, apiErr.GetErrorCode())
							require.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
						}
						if ending == "eof_terminal_write_failure" {
							require.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason, "failure occurs after the scanner records EOF")
							require.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
						}
						if cancelWriter != nil {
							require.True(t, cancelWriter.triggered.Load())
							require.ErrorIs(t, c.Request.Context().Err(), context.Canceled)
						}
						if !bufferedMetadata {
							require.True(t, types.IsSkipRetryError(apiErr), "delivered metadata closes transparent replay")
						}
						service.HandleFailedBilling(c, info, apiErr)
						service.HandleFailedBilling(c, info, apiErr)
						require.EqualValues(t, 1, calls.Load())
						var settlement model.BillingSettlement
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
						require.Equal(t, funding.source, settlement.Source)
						require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
						require.EqualValues(t, -100, settlement.AppliedFundingDelta)
						require.EqualValues(t, -100, settlement.AppliedTokenDelta)
						require.Empty(t, settlement.EffectPayload, "metadata-only failure has no consume projection")
						assertStreamFundingBalances(t, db, funding, requestID, 0, false, 0, 0)
						var outbox int64
						require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&outbox).Error)
						for range 2 {
							_, already, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
								OperationKey: settlement.OperationKey, Source: settlement.Source, UserID: settlement.UserID,
								SubscriptionID: settlement.SubscriptionID, TokenID: settlement.TokenID, TokenKey: info.TokenKey,
								FundingDelta: -100, TokenDelta: -100, SubscriptionPreConsumeRequestID: settlement.SubscriptionPreConsumeRequestID,
								FinalizeSubscriptionPreConsume: settlement.FinalizeSubscriptionPreConsume, AllowMissingToken: true,
							})
							require.NoError(t, err)
							require.True(t, already)
							model.ProcessPendingBillingSettlementsOnce()
						}
						var afterOutbox int64
						require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&afterOutbox).Error)
						require.Equal(t, outbox, afterOutbox)
						var other model.UserSubscription
						require.NoError(t, db.First(&other, 956).Error)
						require.Zero(t, other.AmountUsed)
						assertStreamFundingBalances(t, db, funding, requestID, 0, false, 0, 0)
					})
				}
			}
		}
	}
}

func TestMetadataOnlyCompletedResponsesPreservesReportedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmptyRetry := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmptyRetry })
	for _, funding := range streamFundingCases() {
		t.Run(funding.name, func(t *testing.T) {
			db := setupStreamFundingLedger(t, funding)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+`{"type":"response.completed","response":{"id":"resp_empty_done","status":"completed","output":[],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			requestID := "metadata-complete-" + funding.name
			c, info, _ := streamFundingContext(t, "responses_native", requestID, upstream.URL, constant.ChannelTypeOpenAI, funding)
			require.Nil(t, service.PreConsumeBilling(c, 100, info))
			require.Nil(t, ResponsesHelper(c, info), "a real terminal event keeps normal reported-usage billing")
			assertStreamFundingBalances(t, db, funding, requestID, 6, true, 4, 2)
		})
	}
}

// A terminal event name cannot override a failed response status. Exercise the
// native adapter and original funding ledger, including durable failure replay.
func TestNativeResponsesTerminalFailureRetainsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmptyRetry := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmptyRetry })
	const text = "partial terminal output"
	for _, event := range []string{"response.completed", "response.done", "response.incomplete"} {
		for _, status := range []string{"failed", "cancelled", "canceled"} {
			for _, funding := range streamFundingCases() {
				for _, usageKind := range []string{"known", "zero", "missing"} {
					for _, delivered := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/%s/delivered_%t", event, status, funding.name, usageKind, delivered), func(t *testing.T) {
							db := setupStreamFundingLedger(t, funding)
							_, frames := metadataFundingFrames(t, "responses_native", usageKind)
							if delivered {
								data, err := common.Marshal(map[string]any{"type": "response.output_text.delta", "delta": text})
								require.NoError(t, err)
								frames = append(frames, string(data))
							}
							prompt, completion := 4, 2
							if usageKind == "zero" {
								prompt, completion = 0, 0
							} else if usageKind == "missing" {
								prompt, completion = 10, service.CountTextToken(text, "gpt-test")
							}
							response := map[string]any{"id": "resp_terminal_failure", "status": status, "output": []any{},
								"error": map[string]any{"type": "server_error", "message": "synthetic terminal provider failure"}}
							if usageKind != "missing" {
								response["usage"] = map[string]int{"input_tokens": prompt, "output_tokens": completion, "total_tokens": prompt + completion}
							}
							terminal, err := common.Marshal(map[string]any{"type": event, "response": response})
							require.NoError(t, err)
							frames = append(frames, string(terminal))
							var calls atomic.Int32
							upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
								calls.Add(1)
								w.Header().Set("Content-Type", "text/event-stream")
								_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
							}))
							t.Cleanup(upstream.Close)
							fingerprint := sha256.Sum256([]byte(t.Name()))
							requestID := fmt.Sprintf("native-terminal-%x", fingerprint[:16])
							c, info, _ := streamFundingContext(t, "responses_native", requestID, upstream.URL, constant.ChannelTypeOpenAI, funding)
							require.Nil(t, service.PreConsumeBilling(c, 100, info))
							// A new subscription and changed preference must not receive
							// either this failure refund or the delivered partial charge.
							require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
								StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
							info.UserSetting.BillingPreference = "wallet_only"
							if funding.source == "wallet" {
								info.UserSetting.BillingPreference = "subscription_only"
							}
							apiErr := ResponsesHelper(c, info)
							if apiErr != nil {
								service.HandleFailedBilling(c, info, apiErr)
								service.HandleFailedBilling(c, info, apiErr)
							}
							quota := prompt + completion
							if !delivered {
								quota = 0
							}
							var settlement model.BillingSettlement
							require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
							require.Equal(t, funding.source, settlement.Source)
							require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
							require.EqualValues(t, quota-100, settlement.AppliedFundingDelta)
							require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
							if delivered {
								var effect model.BillingSettlementEffect
								require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
								require.EqualValues(t, quota, effect.Quota)
								require.True(t, effect.QuotaIsActual)
								replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: info.UserId, TokenId: info.TokenId, TokenKey: info.TokenKey,
									OriginModelName: info.OriginModelName, ForcePreConsume: true, UserSetting: info.UserSetting}
								replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
								require.Nil(t, replayErr)
								require.Equal(t, funding.source, replayInfo.BillingSource)
								require.NoError(t, replay.SettleWithEffect(quota, &effect))
								require.NoError(t, replay.SettleWithEffect(quota, &effect))
								replay.Refund(c)
							} else {
								require.Empty(t, settlement.EffectPayload, "a failed metadata-only request has no consumption projection")
								for range 2 {
									_, already, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
										OperationKey: settlement.OperationKey, Source: settlement.Source, UserID: settlement.UserID,
										SubscriptionID: settlement.SubscriptionID, TokenID: settlement.TokenID, TokenKey: info.TokenKey,
										FundingDelta: -100, TokenDelta: -100, SubscriptionPreConsumeRequestID: settlement.SubscriptionPreConsumeRequestID,
										FinalizeSubscriptionPreConsume: settlement.FinalizeSubscriptionPreConsume, AllowMissingToken: true,
									})
									require.NoError(t, err)
									require.True(t, already)
								}
							}
							model.ProcessPendingBillingSettlementsOnce()
							assertStreamFundingBalances(t, db, funding, requestID, quota, delivered, prompt, completion)
							var decoy model.UserSubscription
							require.NoError(t, db.First(&decoy, 956).Error)
							require.Zero(t, decoy.AmountUsed)
							require.EqualValues(t, 1, calls.Load())
							require.Equal(t, delivered, info.HasRecordedChannelFirstResult())
							require.NotNil(t, apiErr, "a failed terminal status must never be returned as success")
							require.Equal(t, "synthetic terminal provider failure", apiErr.Error())
							require.True(t, types.IsSkipRetryError(apiErr), "the earlier created metadata already committed the response")
						})
					}
				}
			}
		}
	}
}

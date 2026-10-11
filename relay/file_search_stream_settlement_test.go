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
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Cancellation follows a real tool-result flush, rather than the preceding
// created event or an upstream write that the adapter may not have processed.
type fileSearchCancelWriter struct {
	gin.ResponseWriter
	recorder  *httptest.ResponseRecorder
	cancel    context.CancelFunc
	triggered atomic.Bool
}

func (w *fileSearchCancelWriter) Flush() {
	w.ResponseWriter.Flush()
	if strings.Contains(w.recorder.Body.String(), `"type":"file_search_call"`) && w.triggered.CompareAndSwap(false, true) {
		w.cancel()
	}
}

func setFileSearchLedgerPriceForTest(t *testing.T, price float64) {
	t.Helper()
	prices := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	oldPrices := prices.Prices
	prices.Prices = map[string]float64{dto.BuildInToolFileSearch: price}
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() {
		prices.Prices = oldPrices
		operation_setting.RebuildToolPriceIndex()
	})
}

func TestFileSearchInterruptedStreamRetainsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmpty, oldRetries := common.EmptyCompletionRetryEnabled, common.RetryTimes
	common.RetryTimes = 2
	setFileSearchLedgerPriceForTest(t, 0.02)
	t.Cleanup(func() {
		common.EmptyCompletionRetryEnabled, common.RetryTimes = oldEmpty, oldRetries
	})
	for _, funding := range streamFundingCases() {
		for _, usageKind := range []string{"known", "zero", "missing"} {
			for _, retry := range []bool{false, true} {
				for _, status := range []string{"completed", "legacy_missing"} {
					for _, ending := range []string{"eof", "transport", "malformed", "cancel"} {
						t.Run(fmt.Sprintf("%s/%s/retry_%t/%s/%s", funding.name, usageKind, retry, status, ending), func(t *testing.T) {
							common.EmptyCompletionRetryEnabled = retry
							db := setupStreamFundingLedger(t, funding)
							_, frames := metadataFundingFrames(t, "responses_native", usageKind)
							item := map[string]any{"type": "file_search_call", "id": "fs_completed", "queries": []string{"synthetic query"},
								"results": []any{map[string]any{"file_id": "file_synthetic", "text": "synthetic-search-result"}}}
							if status == "completed" {
								item["status"] = "completed"
							}
							done, err := common.Marshal(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
							require.NoError(t, err)
							// The same completed invocation must not acquire a second fee.
							frames = append(frames, string(done), string(done))
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
								if ending == "cancel" {
									select {
									case <-r.Context().Done():
									case <-time.After(3 * time.Second):
									}
								}
							}))
							t.Cleanup(upstream.Close)
							fingerprint := sha256.Sum256([]byte(t.Name()))
							requestID := fmt.Sprintf("file-search-%x", fingerprint[:16])
							c, info, recorder := streamFundingContext(t, "responses_native", requestID, upstream.URL, constant.ChannelTypeOpenAI, funding)
							info.Request.(*dto.OpenAIResponsesRequest).Tools = []byte(`[{"type":"file_search","vector_store_ids":["vs_synthetic"]}]`)
							info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
								dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
							}}
							require.Nil(t, service.PreConsumeBilling(c, 100, info))
							require.Equal(t, funding.source, info.BillingSource)
							require.NoError(t, db.Create(&model.UserSubscription{Id: 956, UserId: 951, PlanId: 954, Status: "active", AmountTotal: 1000,
								StartTime: time.Now().Unix() - 1800, EndTime: time.Now().Unix() + 1800}).Error)
							info.UserSetting.BillingPreference = "wallet_only"
							if funding.source == "wallet" {
								info.UserSetting.BillingPreference = "subscription_only"
							}
							var cancelWriter *fileSearchCancelWriter
							if ending == "cancel" {
								ctx, cancel := context.WithCancel(c.Request.Context())
								t.Cleanup(cancel)
								c.Request = c.Request.WithContext(ctx)
								cancelWriter = &fileSearchCancelWriter{ResponseWriter: c.Writer, recorder: recorder, cancel: cancel}
								c.Writer = cancelWriter
							}
							apiErr := ResponsesHelper(c, info)
							require.NotNil(t, apiErr)
							service.HandleFailedBilling(c, info, apiErr)
							service.HandleFailedBilling(c, info, apiErr)
							finalQuota, prompt, completion := 10, 0, 0 // 0.02 / 1000 * 500000; one actual call.
							if usageKind == "known" {
								finalQuota, prompt, completion = 16, 4, 2
							}
							var settlement model.BillingSettlement
							require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
							require.EqualValues(t, finalQuota-100, settlement.AppliedFundingDelta, "delivered File Search owes its fee, not a full refund")
							require.EqualValues(t, finalQuota-100, settlement.AppliedTokenDelta)
							require.Equal(t, funding.source, settlement.Source)
							require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
							require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
							require.NotEmpty(t, settlement.EffectPayload)
							require.True(t, types.IsSkipRetryError(apiErr))
							require.True(t, info.HasRecordedChannelFirstResult())
							require.False(t, info.FirstResultTime.IsZero())
							require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
							require.Contains(t, recorder.Body.String(), "synthetic-search-result")
							require.NotContains(t, recorder.Body.String(), "response.completed")
							require.EqualValues(t, 1, calls.Load())
							if cancelWriter != nil {
								require.True(t, cancelWriter.triggered.Load())
								require.ErrorIs(t, c.Request.Context().Err(), context.Canceled)
							}
							assertStreamFundingBalances(t, db, funding, requestID, finalQuota, true, prompt, completion)
							var log model.Log
							require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
							var other map[string]any
							require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
							require.EqualValues(t, 1, other["file_search_call_count"])
							require.EqualValues(t, 0.02, other["file_search_price"])
							var outbox int64
							require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&outbox).Error)
							for range 2 {
								applied, already, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
									OperationKey: settlement.OperationKey, Source: settlement.Source, UserID: settlement.UserID,
									SubscriptionID: settlement.SubscriptionID, TokenID: settlement.TokenID, TokenKey: info.TokenKey,
									FundingDelta: settlement.FundingDelta, TokenDelta: settlement.TokenDelta,
									SubscriptionPreConsumeRequestID: settlement.SubscriptionPreConsumeRequestID,
									FinalizeSubscriptionPreConsume:  settlement.FinalizeSubscriptionPreConsume,
								})
								require.NoError(t, err)
								require.True(t, already)
								require.EqualValues(t, finalQuota-100, applied)
								model.ProcessPendingBillingSettlementsOnce()
							}
							var afterOutbox int64
							require.NoError(t, db.Model(&model.CacheInvalidationTask{}).Count(&afterOutbox).Error)
							require.Equal(t, outbox, afterOutbox)
							var otherSub model.UserSubscription
							require.NoError(t, db.First(&otherSub, 956).Error)
							require.Zero(t, otherSub.AmountUsed)
							assertStreamFundingBalances(t, db, funding, requestID, finalQuota, true, prompt, completion)
						})
					}
				}
			}
		}
	}
}

func TestFileSearchLifecycleItemsRefundOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmpty := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmpty })
	setFileSearchLedgerPriceForTest(t, 0.02)
	for _, funding := range streamFundingCases() {
		for _, tc := range []struct{ name, event, status string }{
			{"added_running", dto.ResponsesOutputTypeItemAdded, "in_progress"},
			{"added_completed", dto.ResponsesOutputTypeItemAdded, "completed"},
			{"done_running", dto.ResponsesOutputTypeItemDone, "in_progress"},
			{"done_failed", dto.ResponsesOutputTypeItemDone, "failed"},
			{"done_cancelled", dto.ResponsesOutputTypeItemDone, "cancelled"},
			{"done_unknown", dto.ResponsesOutputTypeItemDone, "future_status"},
			{"nil_item", dto.ResponsesOutputTypeItemDone, ""},
		} {
			t.Run(funding.name+"/"+tc.name, func(t *testing.T) {
				db := setupStreamFundingLedger(t, funding)
				_, frames := metadataFundingFrames(t, "responses_native", "known")
				event := dto.ResponsesStreamResponse{Type: tc.event, Item: &dto.ResponsesOutput{Type: dto.BuildInCallFileSearchCall, ID: "fs_metadata", Status: tc.status}}
				if tc.name == "nil_item" {
					event.Item = nil
				}
				data, err := common.Marshal(event)
				require.NoError(t, err)
				frames = append(frames, string(data))
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
				}))
				t.Cleanup(upstream.Close)
				requestID := "file-search-lifecycle-" + funding.name + "-" + tc.name
				c, info, _ := streamFundingContext(t, "responses_native", requestID, upstream.URL, constant.ChannelTypeOpenAI, funding)
				info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
					dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
				}}
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				apiErr := ResponsesHelper(c, info)
				require.NotNil(t, apiErr)
				service.HandleFailedBilling(c, info, apiErr)
				service.HandleFailedBilling(c, info, apiErr)
				require.False(t, info.HasRecordedChannelFirstResult())
				require.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
				assertStreamFundingBalances(t, db, funding, requestID, 0, false, 0, 0)
			})
		}
	}
}

func TestFileSearchPartialSettlementAtAndAboveReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	oldEmpty := common.EmptyCompletionRetryEnabled
	common.EmptyCompletionRetryEnabled = false
	t.Cleanup(func() { common.EmptyCompletionRetryEnabled = oldEmpty })
	for _, funding := range streamFundingCases() {
		for _, quota := range []int{100, 110} {
			t.Run(fmt.Sprintf("%s/quota_%d", funding.name, quota), func(t *testing.T) {
				setFileSearchLedgerPriceForTest(t, float64(quota)/500)
				db := setupStreamFundingLedger(t, funding)
				_, frames := metadataFundingFrames(t, "responses_native", "zero")
				frames = append(frames, `{"type":"response.output_item.done","output_index":0,"item":{"type":"file_search_call","id":"fs_boundary","status":"completed"}}`)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
				}))
				t.Cleanup(upstream.Close)
				requestID := fmt.Sprintf("file-search-boundary-%s-%d", funding.name, quota)
				c, info, _ := streamFundingContext(t, "responses_native", requestID, upstream.URL, constant.ChannelTypeOpenAI, funding)
				info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
					dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
				}}
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				apiErr := ResponsesHelper(c, info)
				require.NotNil(t, apiErr)
				service.HandleFailedBilling(c, info, apiErr)
				service.HandleFailedBilling(c, info, apiErr)
				var settlement model.BillingSettlement
				require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
				require.EqualValues(t, quota-100, settlement.AppliedFundingDelta)
				require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
				require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
				require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
				model.ProcessPendingBillingSettlementsOnce()
				model.ProcessPendingBillingSettlementsOnce()
				assertStreamFundingBalances(t, db, funding, requestID, quota, true, 0, 0)
			})
		}
	}
}

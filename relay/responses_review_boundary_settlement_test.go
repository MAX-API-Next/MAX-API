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

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiChatFrameFailureWriter struct {
	*httptest.ResponseRecorder
	kind      string
	triggered bool
}

func (w *geminiChatFrameFailureWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), `"tool_calls"`) {
		switch w.kind {
		case "write":
			w.triggered = true
			return 0, io.ErrClosedPipe
		case "short":
			w.triggered = true
			return w.ResponseRecorder.Write(data[:3])
		}
	}
	return w.ResponseRecorder.Write(data)
}

func (w *geminiChatFrameFailureWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *geminiChatFrameFailureWriter) Flush() {
	if w.kind == "flush" && strings.Contains(w.Body.String(), `"content":"visible"`) {
		w.triggered = true
		panic(io.ErrClosedPipe)
	}
	w.ResponseRecorder.Flush()
}

func TestGeminiChatConversionFailureRetainsOnlyDeliveredFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	pending := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-pending","name":"lookup","args":{"q":"pending"},"willContinue":true}}]}}]}`
	known := `{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}`
	withUsage := func(frame, usage string) string {
		return strings.TrimSuffix(frame, "}") + `,"usageMetadata":` + usage + `}`
	}
	text := withUsage(`{"candidates":[{"content":{"parts":[{"text":"visible"}]}}]}`, known)
	tool := withUsage(`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-delivered","name":"lookup","args":{"q":"visible"}}}]}}]}`, known)
	for _, scenario := range []struct {
		name, frames, failure string
		delivered             bool
	}{
		{"pending_missing", pending, "", false},
		{"pending_known", withUsage(pending, known), "", false},
		{"pending_zero", withUsage(pending, `{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}`), "", false},
		{"pending_conflict", pending + "\n" + `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-pending","name":"changed"}}]}}]}`, "", false},
		{"pending_stop", pending + "\n" + withUsage(`{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}`, known), "", false},
		{"pending_malformed", pending + "\n{broken", "", false},
		{"first_malformed", `{broken`, "", false},
		{"text_delivered", text + "\n" + pending, "", true},
		{"whitespace_delivered", withUsage(`{"candidates":[{"content":{"parts":[{"text":" "}]}}]}`, known) + "\n" + pending, "", true},
		{"reasoning_delivered", withUsage(`{"candidates":[{"content":{"parts":[{"text":"reasoning","thought":true}]}}]}`, known) + "\n" + pending, "", true},
		{"tool_delivered", tool + "\n" + pending, "", true},
		{"text_malformed", text + "\n{broken", "", true},
		{"tool_write", tool + "\n" + pending, "write", false},
		{"tool_short", tool + "\n" + pending, "short", false},
		{"text_flush", text + "\n" + pending, "flush", true},
	} {
		for _, funding := range streamFundingCases() {
			t.Run(scenario.name+"/"+funding.name, func(t *testing.T) {
				db := setupStreamFundingLedger(t, funding)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: "+strings.ReplaceAll(scenario.frames, "\n", "\n\ndata: ")+"\n\n")
				}))
				defer upstream.Close()
				requestID := fmt.Sprintf("gemini-delivery-%x", sha256.Sum256([]byte(t.Name())))
				c, info, recorder := streamFundingContext(t, "chat_native", requestID, upstream.URL, constant.ChannelTypeGemini, funding)
				writer := &geminiChatFrameFailureWriter{ResponseRecorder: recorder, kind: scenario.failure}
				baseContext := c
				c, _ = gin.CreateTestContext(writer)
				c.Request, c.Keys = baseContext.Request, baseContext.Keys
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				session := info.Billing.(*service.BillingSession)
				info.UserSetting.BillingPreference = "wallet_only"
				if funding.source == "wallet" {
					info.UserSetting.BillingPreference = "subscription_only"
				}
				apiErr := TextHelper(c, info)
				if apiErr != nil {
					service.HandleFailedBilling(c, info, apiErr)
					service.HandleFailedBilling(c, info, apiErr)
				}
				quota, prompt, completion := 0, 0, 0
				if scenario.delivered {
					quota, prompt, completion = 6, 4, 2
				}
				var record model.BillingSettlement
				require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&record).Error)
				require.EqualValues(t, quota-100, record.AppliedFundingDelta)
				require.EqualValues(t, quota-100, record.AppliedTokenDelta)
				require.Equal(t, funding.source, record.Source)
				require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
				require.NotNil(t, apiErr)
				require.Equal(t, scenario.name != "first_malformed", types.IsSkipRetryError(apiErr))
				if scenario.failure != "" {
					require.True(t, writer.triggered)
				}
				if scenario.name == "tool_delivered" {
					require.Contains(t, recorder.Body.String(), "call-delivered")
					require.Contains(t, recorder.Body.String(), "lookup")
				}
				session.Refund(c)
				model.ProcessPendingBillingSettlementsOnce()
				assertStreamFundingBalances(t, db, funding, requestID, quota, scenario.delivered, prompt, completion)
				var effect *model.BillingSettlementEffect
				if scenario.delivered {
					effect = &model.BillingSettlementEffect{}
					require.NoError(t, common.UnmarshalJsonStr(record.EffectPayload, effect))
					replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: info.UserId, TokenId: info.TokenId, TokenKey: info.TokenKey, OriginModelName: info.OriginModelName, ForcePreConsume: true, UserSetting: info.UserSetting}
					replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
					require.Nil(t, replayErr)
					require.Equal(t, funding.source, replayInfo.BillingSource)
					require.NoError(t, replay.SettleWithEffect(quota, effect))
					replay.Refund(c)
				}
				before := requestReplaySnapshot(t, db)
				results := make(chan error, 2)
				var workers sync.WaitGroup
				for range 2 {
					workers.Add(1)
					go func() {
						defer workers.Done()
						_, _, err := model.ApplyBillingSettlementOnce(requestReplayInput(record, effect))
						results <- err
					}()
				}
				workers.Wait()
				close(results)
				for err := range results {
					require.NoError(t, err)
				}
				model.ProcessPendingBillingSettlementsOnce()
				require.Equal(t, before, requestReplaySnapshot(t, db))
			})
		}
	}
}

func TestResponsesNonStreamPartialUsageRetainsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	estimatedOutput := service.EstimateTokenByModel("gpt-test", "visible")
	for _, scenario := range []struct {
		name, usage        string
		prompt, completion int
	}{
		{"prompt_only", `,"usage":{"prompt_tokens":4,"total_tokens":999}`, 4, estimatedOutput},
		{"completion_only", `,"usage":{"completion_tokens":2}`, 10, 2},
		{"prompt_zero", `,"usage":{"prompt_tokens":0}`, 0, estimatedOutput},
		{"completion_zero", `,"usage":{"completion_tokens":0}`, 10, 0},
		{"prompt_null", `,"usage":{"prompt_tokens":null,"completion_tokens":2}`, 10, 2},
		{"completion_null", `,"usage":{"prompt_tokens":4,"completion_tokens":null}`, 4, estimatedOutput},
		{"empty", `,"usage":{}`, 10, estimatedOutput},
		{"missing", "", 10, estimatedOutput},
		{"total_only", `,"usage":{"total_tokens":999}`, 10, estimatedOutput},
		{"complete", `,"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}`, 4, 2},
		{"complete_zero", `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`, 0, 0},
	} {
		for _, funding := range streamFundingCases() {
			t.Run(scenario.name+"/"+funding.name, func(t *testing.T) {
				db := setupStreamFundingLedger(t, funding)
				payload := `{"choices":[{"message":{"role":"assistant","content":"visible"},"finish_reason":"stop"}]` + scenario.usage + `}`
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, payload)
				}))
				defer upstream.Close()
				requestID := fmt.Sprintf("partial-counter-%x", sha256.Sum256([]byte(t.Name())))
				c, info, recorder := streamFundingContext(t, "chat_to_responses", requestID, upstream.URL, constant.ChannelTypeAdvancedCustom, funding)
				info.IsStream = false
				info.Request.(*dto.OpenAIResponsesRequest).Stream = common.GetPointer(false)
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				session := info.Billing.(*service.BillingSession)
				if funding.source == "subscription" {
					info.UserSetting.BillingPreference = "wallet_only"
				} else {
					info.UserSetting.BillingPreference = "subscription_only"
				}
				apiErr := ResponsesHelper(c, info)
				if apiErr != nil {
					service.HandleFailedBilling(c, info, apiErr)
				}
				quota := scenario.prompt + scenario.completion
				var settlement model.BillingSettlement
				require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
				require.EqualValues(t, quota-100, settlement.AppliedFundingDelta)
				require.EqualValues(t, quota-100, settlement.AppliedTokenDelta)
				require.Nil(t, apiErr)
				require.Equal(t, funding.source, settlement.Source)
				require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
				var response dto.OpenAIResponsesResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.NotNil(t, response.Usage)
				require.Equal(t, scenario.prompt, response.Usage.InputTokens)
				require.Equal(t, scenario.completion, response.Usage.OutputTokens)
				require.Equal(t, quota, response.Usage.TotalTokens)
				var effect model.BillingSettlementEffect
				require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
				require.NoError(t, session.SettleWithEffect(quota, &effect))
				session.Refund(c)
				model.ProcessPendingBillingSettlementsOnce()
				assertStreamFundingBalances(t, db, funding, requestID, quota, true, scenario.prompt, scenario.completion)
			})
		}
	}
}

func TestGeminiMovedFunctionCallRetainsOriginalFunding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	for _, route := range []string{"chat_native", "gemini_to_responses"} {
		for _, funding := range streamFundingCases() {
			t.Run(route+"/"+funding.name, func(t *testing.T) {
				db := setupStreamFundingLedger(t, funding)
				frames := []string{
					`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":{"id":"call-moved","name":"lookup","args":{"q":"max"},"willContinue":true}}]}}]}`,
					`{"candidates":[{"index":0,"content":{"parts":[{"text":"visible"},{"functionCall":{"id":"call-moved","willContinue":false}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`,
					`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":{"id":"call-moved","name":"lookup","args":{"q":"max"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`,
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
				}))
				defer upstream.Close()
				requestID := fmt.Sprintf("moved-call-%x", sha256.Sum256([]byte(t.Name())))
				c, info, recorder := streamFundingContext(t, route, requestID, upstream.URL, constant.ChannelTypeGemini, funding)
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				session := info.Billing.(*service.BillingSession)
				info.UserSetting.BillingPreference = "wallet_only"
				if funding.source == "wallet" {
					info.UserSetting.BillingPreference = "subscription_only"
				}
				var apiErr *types.MaxAPIError
				if route == "chat_native" {
					apiErr = TextHelper(c, info)
				} else {
					apiErr = ResponsesHelper(c, info)
				}
				if apiErr != nil {
					service.HandleFailedBilling(c, info, apiErr)
				}
				var settlement model.BillingSettlement
				require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
				require.EqualValues(t, -94, settlement.AppliedFundingDelta)
				require.EqualValues(t, -94, settlement.AppliedTokenDelta)
				require.Nil(t, apiErr)
				require.Equal(t, funding.source, settlement.Source)
				require.Contains(t, recorder.Body.String(), "call-moved")
				require.Contains(t, recorder.Body.String(), "lookup")
				var effect model.BillingSettlementEffect
				require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
				require.NoError(t, session.SettleWithEffect(6, &effect))
				session.Refund(c)
				model.ProcessPendingBillingSettlementsOnce()
				assertStreamFundingBalances(t, db, funding, requestID, 6, true, 4, 2)
			})
		}
	}
}

package relay

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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

package relay

import (
	"fmt"
	"io"
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
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestGlobalChatResponsesProtocolRetainsOriginalFunding(t *testing.T) {
	initRequestReplayTests(t)
	providers := []struct {
		name, wire, converter string
		channelType           int
	}{
		{"claude", "claude", "", constant.ChannelTypeAnthropic},
		{"gemini", "gemini", "", constant.ChannelTypeGemini},
		{"openai", "responses", "", constant.ChannelTypeOpenAI},
		{"custom_none", "responses", dto.AdvancedCustomConverterNone, constant.ChannelTypeAdvancedCustom},
		{"custom_claude", "claude", dto.AdvancedCustomConverterOpenAIChatCompletionsToAnthropicMessages, constant.ChannelTypeAdvancedCustom},
		{"custom_gemini", "gemini", dto.AdvancedCustomConverterOpenAIChatCompletionsToGeminiGenerateContent, constant.ChannelTypeAdvancedCustom},
		{"custom_responses", "responses", dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses, constant.ChannelTypeAdvancedCustom},
	}
	for _, provider := range providers {
		modes := []bool{true}
		if provider.name == "claude" || provider.name == "gemini" {
			modes = []bool{false, true}
		}
		for _, enabled := range modes {
			for _, stream := range []bool{false, true} {
				for _, funding := range streamFundingCases() {
					t.Run(fmt.Sprintf("%s/global_%t/stream_%t/%s", provider.name, enabled, stream, funding.name), func(t *testing.T) {
						db := setupStreamFundingLedger(t, funding)
						body := globalBridgeWireBody(t, provider.wire, stream)
						type observedRequest struct {
							path string
							body map[string]any
							err  error
						}
						requests := make(chan observedRequest, 1)
						var calls atomic.Int32
						upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							data, err := io.ReadAll(r.Body)
							var parsed map[string]any
							if err == nil {
								err = common.Unmarshal(data, &parsed)
							}
							requests <- observedRequest{r.URL.Path, parsed, err}
							if stream {
								w.Header().Set("Content-Type", "text/event-stream")
							} else {
								w.Header().Set("Content-Type", "application/json")
							}
							_, _ = io.WriteString(w, body)
						}))
						t.Cleanup(upstream.Close)
						requestID := "global-protocol-" + provider.name
						c, info, recorder := streamFundingContext(t, "chat_native", requestID, upstream.URL, provider.channelType, funding)
						info.IsStream = stream
						info.Request.(*dto.GeneralOpenAIRequest).Stream = common.GetPointer(stream)
						model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: enabled, AllChannels: true, ModelPatterns: []string{"^gpt-test$"}}
						if provider.channelType == constant.ChannelTypeAdvancedCustom {
							path := "/v1/responses"
							if provider.wire == "claude" {
								path = "/v1/messages"
							} else if provider.wire == "gemini" {
								path = "/v1beta/models/{model}:generateContent"
							}
							c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/chat/completions", UpstreamPath: path, Converter: provider.converter}}}})
						}
						require.Nil(t, service.PreConsumeBilling(c, 100, info))
						apiErr := TextHelper(c, info)
						require.Nil(t, apiErr, "global policy must not send native replies to a Responses parser")
						require.Contains(t, recorder.Body.String(), "partial output")
						require.Contains(t, recorder.Body.String(), "chat.completion")
						require.EqualValues(t, 1, calls.Load())
						select {
						case request := <-requests:
							require.NoError(t, request.err)
							switch provider.wire {
							case "claude":
								require.Contains(t, request.path, "/messages")
								require.Contains(t, request.body, "messages")
								require.NotContains(t, request.body, "input")
							case "gemini":
								require.Contains(t, strings.ToLower(request.path), "generatecontent")
								require.Contains(t, request.body, "contents")
								require.NotContains(t, request.body, "input")
							default:
								require.Equal(t, "/v1/responses", request.path)
								require.Contains(t, request.body, "input")
								require.NotContains(t, request.body, "messages")
							}
						case <-time.After(3 * time.Second):
							t.Fatal("no observed synthetic upstream request")
						}
						assertStreamFundingBalances(t, db, funding, requestID, 6, true, 4, 2)
						var record model.BillingSettlement
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&record).Error)
						require.Equal(t, funding.source, record.Source)
						require.EqualValues(t, -94, record.AppliedFundingDelta)
						require.EqualValues(t, -94, record.AppliedTokenDelta)
						var effect model.BillingSettlementEffect
						require.NoError(t, common.UnmarshalJsonStr(record.EffectPayload, &effect))
						before := requestReplaySnapshot(t, db)
						for i := 0; i < 2; i++ {
							delta, already, err := model.ApplyBillingSettlementOnce(requestReplayInput(record, &effect))
							require.NoError(t, err)
							require.True(t, already)
							require.EqualValues(t, -94, delta)
							require.NoError(t, model.ProcessBillingSettlementEffect(record.OperationKey))
							service.HandleFailedBilling(c, info, apiErr)
						}
						model.ProcessPendingBillingSettlementsOnce()
						require.Equal(t, before, requestReplaySnapshot(t, db))
					})
				}
			}
		}
	}
}

// Exercise real adaptor conversion rather than duplicating the capability list
// in a test. A future adaptor change must update its global bridge eligibility.
func TestGlobalResponsesBridgeEligibilityMatchesAdaptorProtocol(t *testing.T) {
	initRequestReplayTests(t)
	for channelType := 1; channelType < constant.ChannelTypeDummy; channelType++ {
		t.Run(fmt.Sprintf("channel_%d", channelType), func(t *testing.T) {
			funding := streamFundingCases()[3]
			c, info, _ := streamFundingContext(t, "chat_native", "capability-probe", "https://synthetic.invalid", channelType, funding)
			if channelType == constant.ChannelTypeAdvancedCustom {
				c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomConverterNone}}}})
			}
			info.InitChannelMeta(c)
			info.RelayMode = relayconstant.RelayModeResponses
			info.RequestURLPath = "/v1/responses"
			adaptor := GetAdaptor(info.ApiType)
			expected := false
			if adaptor != nil {
				adaptor.Init(info)
				converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{Model: "gpt-test", Input: []byte(`"test"`)})
				if err == nil {
					switch value := converted.(type) {
					case dto.OpenAIResponsesRequest:
						expected = true
					case *dto.OpenAIResponsesRequest:
						expected = value != nil
					}
				}
			}
			for _, policy := range []model_setting.ChatCompletionsToResponsesPolicy{
				{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-test$"}},
				{Enabled: true, ChannelIDs: []int{953}, ModelPatterns: []string{"^gpt-test$"}},
				{Enabled: true, ChannelTypes: []int{channelType}, ModelPatterns: []string{"^gpt-test$"}},
			} {
				require.Equal(t, expected, service.ShouldChatCompletionsUseResponsesPolicy(policy, 953, channelType, "gpt-test"))
				require.False(t, service.ShouldChatCompletionsUseResponsesPolicy(policy, 953, channelType, "other-model"))
				policy.Enabled = false
				require.False(t, service.ShouldChatCompletionsUseResponsesPolicy(policy, 953, channelType, "gpt-test"))
			}
		})
	}
}

func TestGlobalResponsesBridgeRejectsNativeBeforeUpstreamAndRefunds(t *testing.T) {
	initRequestReplayTests(t)
	for _, funding := range streamFundingCases() {
		for _, mismatch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mismatched_adaptor_%t", funding.name, mismatch), func(t *testing.T) {
				db := setupStreamFundingLedger(t, funding)
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
				t.Cleanup(upstream.Close)
				channelType := constant.ChannelTypeAnthropic
				if mismatch {
					channelType = constant.ChannelTypeOpenAI
				}
				requestID := "rejected-global-protocol"
				c, info, recorder := streamFundingContext(t, "chat_native", requestID, upstream.URL, channelType, funding)
				info.InitChannelMeta(c)
				info.RequestURLPath = "/v1/chat/completions"
				adaptor := &claude.Adaptor{}
				adaptor.Init(info)
				require.Nil(t, service.PreConsumeBilling(c, 100, info))
				usage, apiErr := chatCompletionsViaResponses(c, info, adaptor, info.Request.(*dto.GeneralOpenAIRequest))
				require.Nil(t, usage)
				require.NotNil(t, apiErr)
				require.Equal(t, types.ErrorCodeConvertRequestFailed, apiErr.GetErrorCode())
				require.True(t, types.IsSkipRetryError(apiErr))
				require.Zero(t, calls.Load())
				require.Empty(t, recorder.Body.String())
				require.Equal(t, relayconstant.RelayModeChatCompletions, info.RelayMode)
				require.Equal(t, "/v1/chat/completions", info.RequestURLPath)
				service.HandleFailedBilling(c, info, apiErr)
				service.HandleFailedBilling(c, info, apiErr)
				assertStreamFundingBalances(t, db, funding, requestID, 0, false, 0, 0)
				before := requestReplaySnapshot(t, db)
				model.ProcessPendingBillingSettlementsOnce()
				require.Equal(t, before, requestReplaySnapshot(t, db))
			})
		}
	}
}

func globalBridgeWireBody(t *testing.T, wire string, stream bool) string {
	t.Helper()
	if !stream {
		switch wire {
		case "claude":
			return `{"id":"msg_funding","type":"message","role":"assistant","model":"gpt-test","content":[{"type":"text","text":"partial output"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`
		case "gemini":
			return `{"candidates":[{"content":{"role":"model","parts":[{"text":"partial output"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`
		default:
			return `{"id":"resp_funding","status":"completed","model":"gpt-test","output":[{"type":"message","id":"msg_output","role":"assistant","content":[{"type":"output_text","text":"partial output"}]}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}`
		}
	}
	var frames []string
	switch wire {
	case "claude":
		_, frames = streamFundingFrames(t, "claude_to_responses", "known")
		frames = append(frames, `{"type":"message_stop"}`)
	case "gemini":
		_, frames = streamFundingFrames(t, "gemini_to_responses", "known")
	default:
		_, frames = streamFundingFrames(t, "responses_native", "known")
		frames = append(frames, `{"type":"response.completed","response":{"id":"resp_funding","status":"completed","model":"gpt-test","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`)
	}
	return "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
}

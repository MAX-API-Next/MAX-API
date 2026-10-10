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
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type customToolEOFWriter struct {
	*httptest.ResponseRecorder
	ending       string
	secondInput  string
	flushFailure bool
}

func (w *customToolEOFWriter) Write(p []byte) (int, error) {
	data := strings.SplitN(string(p), "data: ", 2)
	if len(data) != 2 {
		return w.ResponseRecorder.Write(p)
	}
	eventType := gjson.Get(data[1], "type").String()
	inputDelta := eventType == "response.custom_tool_call_input.delta"
	fail := w.ending == "first_input_write_failure" && inputDelta ||
		(w.ending == "second_input_write_failure" || w.ending == "finish_second_input_write_failure") && inputDelta && gjson.Get(data[1], "delta").String() == w.secondInput ||
		w.ending == "terminal_write_failure" && (eventType == "response.completed" || eventType == "response.incomplete") ||
		w.ending == "no_delivery" && eventType == "response.output_item.added"
	if fail {
		return 0, io.ErrClosedPipe
	}
	if w.ending == "input_flush_failure" && inputDelta {
		w.flushFailure = true
	}
	return w.ResponseRecorder.Write(p)
}

func (w *customToolEOFWriter) FlushError() error {
	if w.flushFailure {
		return io.ErrClosedPipe
	}
	w.ResponseRecorder.Flush()
	return nil
}

// Exercise EOF finalization at the real ResponsesHelper and funding/effect seam.
// A late custom input must be billed once, and a failed input write must not be billed.
func TestResponsesCustomToolEOFFinalizationUsesDurableLedger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	inputs := []string{"first patch line\nsecond line", strings.Repeat("second patch body ", 50)}
	for _, provider := range []string{"chat", "claude", "gemini"} {
		for _, funding := range []string{"wallet_only", "subscription_only"} {
			for _, usageKind := range []string{"missing", "known", "zero", "input_only", "zero_input_only"} {
				if provider == "chat" && strings.HasSuffix(usageKind, "input_only") {
					continue
				}
				for _, ending := range []string{"eof", "first_input_write_failure", "second_input_write_failure", "finish_second_input_write_failure", "input_flush_failure", "terminal_write_failure", "no_delivery"} {
					t.Run(strings.Join([]string{provider, funding, usageKind, ending}, "/"), func(t *testing.T) {
						db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
						require.NoError(t, err)
						sqlDB, err := db.DB()
						require.NoError(t, err)
						sqlDB.SetMaxOpenConns(1)
						require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{},
							&model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{},
							&model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
						oldDB, oldLogDB := model.DB, model.LOG_DB
						oldSQLite, oldRedis, oldBatch, oldConsume := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
						model.DB, model.LOG_DB = db, db
						common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, true
						t.Cleanup(func() {
							model.DB, model.LOG_DB = oldDB, oldLogDB
							common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldConsume
							_ = sqlDB.Close()
						})
						require.NoError(t, db.Create(&model.User{Id: 901, Username: "custom-eof-ledger", Quota: 1000}).Error)
						require.NoError(t, db.Create(&model.Token{Id: 902, UserId: 901, Key: "synthetic-custom-eof", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
						require.NoError(t, db.Create(&model.Channel{Id: 903}).Error)
						if funding == "subscription_only" {
							require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 904, Title: "custom-eof-plan", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
							require.NoError(t, db.Create(&model.UserSubscription{Id: 905, UserId: 901, PlanId: 904, AmountTotal: 1000, Status: "active",
								StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}).Error)
						}
						prompt := 10
						deliveredInput := strings.Join(inputs, "")
						if ending == "first_input_write_failure" || ending == "no_delivery" {
							deliveredInput = ""
						} else if ending == "second_input_write_failure" || ending == "finish_second_input_write_failure" || ending == "input_flush_failure" {
							deliveredInput = inputs[0]
						}
						completion := service.EstimateTokenByModel("gpt-test", deliveredInput)
						if usageKind == "known" {
							prompt, completion = 4, 2
						} else if usageKind == "zero" {
							prompt, completion = 0, 0
						} else if usageKind == "input_only" {
							prompt = 4
						} else if usageKind == "zero_input_only" {
							prompt = 0
						}
						channelType, frames := customToolEOFFrames(t, provider, usageKind, inputs)
						if ending == "finish_second_input_write_failure" && usageKind != "known" && usageKind != "zero" {
							switch provider {
							case "chat":
								frames = append(frames, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
							case "claude":
								frames = append(frames, `{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`)
							case "gemini":
								frames = append(frames, `{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`)
							}
						}
						var calls atomic.Int32
						upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
						}))
						t.Cleanup(upstream.Close)
						writer := &customToolEOFWriter{ResponseRecorder: httptest.NewRecorder(), ending: ending, secondInput: inputs[1]}
						c, _ := gin.CreateTestContext(writer)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
						c.Set(string(constant.ContextKeyChannelType), channelType)
						c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
						c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
						c.Set(string(constant.ContextKeyChannelId), 903)
						c.Set(string(constant.ContextKeyOriginalModel), "gpt-test")
						if provider == "chat" {
							c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
								{IncomingPath: "/v1/responses", UpstreamPath: "/v1/chat/completions", Converter: dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions},
							}}})
						}
						requestID := "custom-eof-" + strings.Join([]string{provider, funding, usageKind, ending}, "-")
						c.Set(common.RequestIdKey, requestID)
						info := &relaycommon.RelayInfo{RequestId: requestID, UserId: 901, TokenId: 902, TokenKey: "synthetic-custom-eof", ForcePreConsume: true,
							RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true,
							OriginModelName: "gpt-test", StartTime: time.Now(), UsingGroup: "default", UserSetting: dto.UserSetting{BillingPreference: funding, QuotaWarningThreshold: 1},
							PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
							Request: &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: []byte(`"test"`), Stream: common.GetPointer(true),
								Tools: []byte(`[{"type":"custom","name":"apply_patch"}]`)}}
						info.SetEstimatePromptTokens(10)
						require.Nil(t, service.PreConsumeBilling(c, 100, info))
						apiErr := ResponsesHelper(c, info)
						if ending == "eof" && (usageKind == "known" || usageKind == "zero") {
							require.Nil(t, apiErr)
						} else {
							require.NotNil(t, apiErr)
							require.True(t, types.IsSkipRetryError(apiErr))
							service.HandleFailedBilling(c, info, apiErr)
							service.HandleFailedBilling(c, info, apiErr)
						}
						require.EqualValues(t, 1, calls.Load())
						quota := prompt + completion
						if ending == "no_delivery" {
							quota = 0
						}
						var settlement model.BillingSettlement
						require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&settlement).Error)
						require.Equal(t, model.BillingSettlementStatusApplied, settlement.Status)
						require.EqualValues(t, quota-100, settlement.FundingDelta, "late delivered custom input must be reflected in actual funding")
						require.EqualValues(t, quota-100, settlement.TokenDelta)
						if ending != "no_delivery" {
							var effect model.BillingSettlementEffect
							require.NoError(t, common.UnmarshalJsonStr(settlement.EffectPayload, &effect))
							require.Equal(t, model.BillingSettlementEffectApplied, settlement.EffectStatus)
							require.Equal(t, prompt, effect.PromptTokens)
							require.Equal(t, completion, effect.CompletionTokens)
							require.EqualValues(t, quota, effect.Quota)
							require.True(t, effect.QuotaIsActual)
							replayInfo := &relaycommon.RelayInfo{RequestId: requestID, UserId: info.UserId, TokenId: info.TokenId, TokenKey: info.TokenKey,
								OriginModelName: info.OriginModelName, UserSetting: info.UserSetting, ForcePreConsume: true}
							replay, replayErr := service.NewBillingSession(c, replayInfo, 100)
							require.Nil(t, replayErr)
							require.NoError(t, replay.SettleWithEffect(quota, &effect))
							require.NoError(t, replay.SettleWithEffect(quota, &effect))
							replay.Refund(c)
						}
						var user model.User
						var token model.Token
						var channel model.Channel
						require.NoError(t, db.First(&user, 901).Error)
						require.NoError(t, db.First(&token, 902).Error)
						require.NoError(t, db.First(&channel, 903).Error)
						walletQuota := 1000 - quota
						if funding == "subscription_only" {
							walletQuota = 1000
							var sub model.UserSubscription
							require.NoError(t, db.First(&sub, 905).Error)
							require.EqualValues(t, quota, sub.AmountUsed)
						}
						require.EqualValues(t, walletQuota, user.Quota)
						require.EqualValues(t, 1000-quota, token.RemainQuota)
						require.EqualValues(t, quota, user.UsedQuota)
						require.EqualValues(t, quota, token.UsedQuota)
						require.EqualValues(t, quota, channel.UsedQuota)
						var count int64
						require.NoError(t, db.Model(&model.Log{}).Count(&count).Error)
						wantLogs := 1
						if ending == "no_delivery" {
							wantLogs = 0
						}
						require.EqualValues(t, wantLogs, count)
						wantRequests := wantLogs
						if quota == 0 {
							wantRequests = 0
						}
						require.EqualValues(t, wantRequests, user.RequestCount)
						terminalCount := 0
						var gotInput strings.Builder
						for _, line := range strings.Split(writer.Body.String(), "\n") {
							if !strings.HasPrefix(line, "data: ") {
								continue
							}
							data := strings.TrimPrefix(line, "data: ")
							if gjson.Get(data, "type").String() == "response.custom_tool_call_input.delta" {
								gotInput.WriteString(gjson.Get(data, "delta").String())
							}
							if gjson.Get(data, "type").String() == "response.completed" || gjson.Get(data, "type").String() == "response.incomplete" {
								terminalCount++
								require.EqualValues(t, completion, gjson.Get(data, "response.usage.output_tokens").Int())
								require.EqualValues(t, prompt, gjson.Get(data, "response.usage.input_tokens").Int())
							}
						}
						require.Equal(t, deliveredInput, gotInput.String(), "only fully written input deltas may participate in settlement")
						if ending == "eof" {
							require.Equal(t, 1, terminalCount)
						} else {
							require.Zero(t, terminalCount, "no terminal may be appended after the downstream fails")
						}
					})
				}
			}
		}
	}
}

func customToolEOFFrames(t *testing.T, provider, usageKind string, inputs []string) (int, []string) {
	t.Helper()
	marshal := func(v any) string { b, err := common.Marshal(v); require.NoError(t, err); return string(b) }
	prompt, completion := 4, 2
	if strings.HasPrefix(usageKind, "zero") {
		prompt, completion = 0, 0
	}
	complete := usageKind == "known" || usageKind == "zero"
	var frames []string
	switch provider {
	case "chat":
		if usageKind != "missing" {
			frames = append(frames, marshal(map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}}))
		}
		for idx, input := range inputs {
			frames = append(frames, marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": idx, "id": fmt.Sprintf("call_%d", idx), "type": "function", "function": map[string]any{"name": "apply_patch", "arguments": marshal(map[string]string{"input": input})}}}}}}}))
		}
		if usageKind != "missing" {
			frames = append(frames, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
		}
		return constant.ChannelTypeAdvancedCustom, frames
	case "claude":
		message := map[string]any{"id": "msg_custom_eof", "model": "gpt-test"}
		if usageKind != "missing" {
			message["usage"] = map[string]int{"input_tokens": prompt}
		}
		frames = append(frames, marshal(map[string]any{"type": "message_start", "message": message}))
		for idx, input := range inputs {
			frames = append(frames, marshal(map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("call_%d", idx), "name": "apply_patch", "input": map[string]any{}}}),
				marshal(map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "input_json_delta", "partial_json": marshal(map[string]string{"input": input})}}))
		}
		if complete {
			frames = append(frames, marshal(map[string]any{"type": "message_delta", "delta": map[string]string{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": completion}}))
		}
		return constant.ChannelTypeAnthropic, frames
	default:
		if usageKind != "missing" {
			metadata := map[string]int{"promptTokenCount": prompt}
			if complete {
				metadata["candidatesTokenCount"], metadata["totalTokenCount"] = completion, prompt+completion
			}
			frames = append(frames, marshal(map[string]any{"usageMetadata": metadata}))
		}
		var parts []any
		for idx, input := range inputs {
			parts = append(parts, map[string]any{"functionCall": map[string]any{"id": fmt.Sprintf("call_%d", idx), "name": "apply_patch", "args": map[string]string{"input": input}}})
		}
		frames = append(frames, marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}}}}))
		if complete {
			frames = append(frames, `{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`)
		}
		return constant.ChannelTypeGemini, frames
	}
}

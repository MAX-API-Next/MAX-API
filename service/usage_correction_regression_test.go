package service_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestUsageCorrectionResponsesExplicitZeroSettlement(t *testing.T) {
	c, info := usageReplayInfo(t, 9530, "")
	raw := `{"id":"resp_synthetic","object":"response","status":"completed","model":"synthetic-model","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Visible response with explicitly reported zero usage"}]}],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`
	usage, apiErr := openai.OaiResponsesToChatHandler(c, info, usageReplayResponse(raw))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	service.PostTextConsumeQuota(c, info, usage, nil)
	service.PostTextConsumeQuota(c, info, usage, nil)
	var user model.User
	var logs []model.Log
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).Find(&logs).Error)
	require.Equal(t, int64(10000), user.Quota, "reported zero must not become an estimated debit")
	require.Len(t, logs, 1, "the same settlement remains idempotent")
	require.Zero(t, logs[0].Quota)
}

func TestUsageCorrectionClaudeCallerPartialSettlement(t *testing.T) {
	for i, zero := range []bool{false, true} {
		t.Run(fmt.Sprint(zero), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9540+i, "")
			service.InitHttpClient()
			input, output, want := 100, 22, 144 // 100*1 + 22*2, independent of the reserved 1000.
			if zero {
				input, output, want = 0, 0, 0
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"test\",\"model\":\"audit-model\",\"usage\":{\"input_tokens\":%d}}}\n\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"delivered\"}}\n\n"+
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":%d}}\n\n", input, output)
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close() // Abnormal HTTP EOF after complete SSE usage frames.
				}
			}))
			t.Cleanup(upstream.Close)
			c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeAnthropic)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.RelayMode, info.RelayFormat, info.IsStream = relayconstant.RelayModeUnknown, types.RelayFormatClaude, true
			info.Request = &dto.ClaudeRequest{Model: "audit-model", Stream: common.GetPointer(true), MaxTokens: common.GetPointer(uint(100))}
			apiErr := relay.ClaudeHelper(c, info)
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertUsageReplayBalance(t, info, want)
			var settlements []model.BillingSettlement
			require.NoError(t, model.DB.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(info.RequestId)).Find(&settlements).Error)
			require.Len(t, settlements, 1, "partial settlement and failure refund must share one finalize operation")
		})
	}
}

func TestUsageCorrectionChatLateUsageSettlement(t *testing.T) {
	for i, zero := range []bool{false, true} {
		t.Run(fmt.Sprint(zero), func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 9550+i, "")
			service.InitHttpClient()
			input, output, want := 100, 22, 144
			if zero {
				input, output, want = 0, 0, 0
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"delivered\"}}]}\n\n"+
					"data: {\"error\":{\"message\":\"synthetic terminal failure\"}}\n\n"+
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", input, output, input+output)
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}))
			t.Cleanup(upstream.Close)
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
			c.Set(string(constant.ContextKeyChannelId), info.ChannelId)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "audit-model")
			info.IsStream = true
			info.Request = &dto.GeneralOpenAIRequest{Model: "audit-model", Stream: common.GetPointer(true)}
			apiErr := relay.TextHelper(c, info)
			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), "synthetic terminal failure")
			require.True(t, types.IsSkipRetryError(apiErr))
			service.HandleFailedBilling(c, info, apiErr)
			service.HandleFailedBilling(c, info, apiErr)
			assertUsageReplayBalance(t, info, want)
		})
	}
}

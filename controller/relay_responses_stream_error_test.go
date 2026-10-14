package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise Relay's real deferred writer after the selected provider adapter,
// rather than reproducing the controller's error branch in a test fixture.
func TestRelayResponsesFailureUsesClientProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{},
		&model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{}))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSQLite, oldRedis, oldBatch, oldLogConsume := common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	oldRatios := ratio_setting.ModelRatio2JSONString()
	oldRetry, oldEmpty := common.RetryTimes, common.EmptyCompletionRetryEnabled
	oldQuota := *operation_setting.GetQuotaSetting()
	oldStreamingTimeout := constant.StreamingTimeout
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false, false, false
	common.RetryTimes, common.EmptyCompletionRetryEnabled = 2, false
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-terminal-test":0}`))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldSQLite, oldRedis, oldBatch, oldLogConsume
		common.RetryTimes, common.EmptyCompletionRetryEnabled = oldRetry, oldEmpty
		*operation_setting.GetQuotaSetting() = oldQuota
		constant.StreamingTimeout = oldStreamingTimeout
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		_ = sqlDB.Close()
	})
	require.NoError(t, db.Create(&model.User{Id: 701, Username: "terminal-test", Quota: 1000}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 703}).Error)
	nativeCreated := `{"type":"response.created","sequence_number":7,"response":{"id":"resp_native_terminal","object":"response","status":"in_progress","model":"gpt-terminal-test","output":[]}}`
	nativeText := `{"type":"response.output_text.delta","sequence_number":8,"delta":"partial output"}`
	for _, tc := range []struct {
		name     string
		channel  int
		frames   []string
		terminal string
		json     bool
		finish   string
	}{
		{"native_malformed", constant.ChannelTypeOpenAI, []string{nativeCreated, nativeText, `{broken`}, "response.failed", false, ""},
		{"native_eof", constant.ChannelTypeOpenAI, []string{nativeCreated, nativeText}, "response.failed", false, ""},
		{"native_disconnect", constant.ChannelTypeOpenAI, []string{nativeCreated, nativeText}, "response.failed", false, ""},
		{"native_idle_timeout", constant.ChannelTypeOpenAI, []string{nativeCreated, nativeText}, "response.failed", false, ""},
		{"native_provider_failed", constant.ChannelTypeOpenAI, []string{nativeCreated, nativeText, `{"type":"response.failed","sequence_number":9,"response":{"id":"resp_native_terminal","status":"failed","error":{"code":"server_error","message":"provider failure"}}}`}, "response.failed", false, ""},
		{"native_unstarted", constant.ChannelTypeOpenAI, []string{`{broken`}, "", true, ""},
		{"chat_malformed", constant.ChannelTypeAdvancedCustom, []string{`{"choices":[{"delta":{"content":"partial output"}}]}`, `{broken`}, "response.failed", false, ""},
		{"chat_eof", constant.ChannelTypeAdvancedCustom, []string{`{"choices":[{"delta":{"content":"partial output"}}]}`}, "response.incomplete", false, ""},
		{"claude_malformed", constant.ChannelTypeAnthropic, []string{`{"type":"message_start","message":{"id":"msg_test","model":"gpt-terminal-test"}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}`, `{broken`}, "response.failed", false, ""},
		{"gemini_eof", constant.ChannelTypeGemini, []string{`{"candidates":[{"content":{"parts":[{"text":"partial output"}]}}]}`}, "response.incomplete", false, ""},
		{"responses_to_chat_malformed", constant.ChannelTypeAdvancedCustom, []string{nativeCreated, nativeText, `{broken`}, "", false, ""},
		{"responses_to_chat_eof", constant.ChannelTypeAdvancedCustom, []string{nativeCreated, nativeText}, "", false, ""},
		{"responses_to_chat_length", constant.ChannelTypeAdvancedCustom, []string{nativeCreated, nativeText, `{"type":"response.incomplete","response":{"id":"resp_native_terminal","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`}, "", false, "length"},
		{"responses_to_chat_filter", constant.ChannelTypeAdvancedCustom, []string{nativeCreated, nativeText, `{"type":"response.incomplete","response":{"id":"resp_native_terminal","status":"incomplete","incomplete_details":{"reason":"content_filter"}}}`}, "", false, "content_filter"},
		{"responses_to_chat_done", constant.ChannelTypeAdvancedCustom, []string{nativeCreated, nativeText, `{"type":"response.done","response":{"id":"resp_native_terminal","status":"completed"}}`}, "", false, "stop"},
		{"responses_to_chat_global_length", constant.ChannelTypeOpenAI, []string{nativeCreated, nativeText, `{"type":"response.incomplete","response":{"id":"resp_native_terminal","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`}, "", false, "length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldGlobal := *model_setting.GetGlobalSettings()
			*model_setting.GetGlobalSettings() = model_setting.GlobalSettings{}
			t.Cleanup(func() { *model_setting.GetGlobalSettings() = oldGlobal })
			if tc.name == "responses_to_chat_global_length" {
				model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-terminal-test$"}}
			}
			constant.StreamingTimeout = oldStreamingTimeout
			if tc.name == "native_idle_timeout" {
				constant.StreamingTimeout = 1
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if tc.name == "native_disconnect" {
					w.Header().Set("Content-Length", "100000")
				}
				_, _ = fmt.Fprint(w, "data: "+strings.Join(tc.frames, "\n\ndata: ")+"\n\n")
				if tc.name == "native_idle_timeout" {
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			}))
			defer upstream.Close()
			router := gin.New()
			var usedChannels []string
			path, relayFormat := "/v1/responses", types.RelayFormat(types.RelayFormatOpenAIResponses)
			requestBody := `{"model":"gpt-terminal-test","input":"test","stream":true}`
			if strings.HasPrefix(tc.name, "responses_to_chat_") {
				path, relayFormat = "/v1/chat/completions", types.RelayFormatOpenAI
				requestBody = `{"model":"gpt-terminal-test","messages":[{"role":"user","content":"test"}],"stream":true}`
			}
			router.POST(path, func(c *gin.Context) {
				c.Set(common.RequestIdKey, "terminal-"+tc.name)
				c.Set(string(constant.ContextKeyUserId), 701)
				c.Set(string(constant.ContextKeyUserQuota), int64(1000))
				c.Set(string(constant.ContextKeyOriginalModel), "gpt-terminal-test")
				c.Set(string(constant.ContextKeyUsingGroup), "default")
				c.Set(string(constant.ContextKeyUserGroup), "default")
				c.Set(string(constant.ContextKeyChannelId), 703)
				c.Set(string(constant.ContextKeyChannelType), tc.channel)
				c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
				c.Set(string(constant.ContextKeyChannelKey), "synthetic-terminal-key")
				if tc.channel == constant.ChannelTypeAdvancedCustom {
					upstreamPath, converter := "/v1/chat/completions", dto.AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions
					if relayFormat == types.RelayFormatOpenAI {
						upstreamPath, converter = "/v1/responses", dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses
					}
					c.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
						{IncomingPath: path, UpstreamPath: upstreamPath, Converter: converter},
					}}})
				}
				Relay(c, relayFormat)
				usedChannels = c.GetStringSlice("use_channel")
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			body := recorder.Body.String()
			if tc.json {
				require.GreaterOrEqual(t, recorder.Code, 400, body)
				require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
				require.True(t, gjson.Valid(body), body)
				require.NotContains(t, body, "data:")
			} else {
				require.Equal(t, http.StatusOK, recorder.Code, body)
				require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
				require.Contains(t, body, "partial output")
				if relayFormat == types.RelayFormatOpenAI {
					require.NotContains(t, body, "event: response.")
					if tc.finish == "" {
						require.Contains(t, body, `data: {"error":`)
					} else {
						require.NotContains(t, body, `"error":`)
						require.Equal(t, 1, strings.Count(body, `"finish_reason":"`+tc.finish+`"`))
					}
					require.Equal(t, 1, strings.Count(body, "data: [DONE]"))
					for _, frame := range strings.Split(strings.TrimSpace(body), "\n\n") {
						require.True(t, strings.HasPrefix(frame, "data: "), frame)
					}
					require.EqualValues(t, 1, calls.Load())
					return
				}
				require.Equal(t, 1, strings.Count(body, "event: "+tc.terminal+"\n"), body)
				require.NotContains(t, body, "event: response.completed")
				require.NotContains(t, body, "[DONE]")
				for _, frame := range strings.Split(strings.TrimSpace(body), "\n\n") {
					require.True(t, strings.HasPrefix(frame, "event: ") || strings.HasPrefix(frame, ":"), frame)
				}
				require.EqualValues(t, 1, calls.Load(), "committed output must forbid retry")
				require.Len(t, usedChannels, 1)
			}
		})
	}
}

func TestStartedResponsesFailureIdentityAndTerminalDedup(t *testing.T) {
	for _, terminal := range []string{"", "response.failed", "response.incomplete", "response.completed", "response.done", "response.cancelled", "response.canceled", "error", "response.error"} {
		t.Run(terminal, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"}, `{"type":"response.created","sequence_number":7,"response":{"id":"resp_identity","model":"gpt-test","output":[],"status":"in_progress"}}`))
			require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","sequence_number":12,"delta":"partial"}`))
			if terminal != "" {
				payload := fmt.Sprintf(`{"type":%q,"sequence_number":13}`, terminal)
				require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: terminal}, payload))
			}
			before := recorder.Body.String()
			apiErr := types.NewOpenAIError(errors.New("synthetic failure"), types.ErrorCodeBadResponse, 502)
			require.True(t, writeStartedStreamError(c, types.RelayFormatOpenAIResponses, apiErr))
			if terminal == "" {
				body := recorder.Body.String()
				require.Equal(t, 1, strings.Count(body, "event: response.failed\n"), body)
				last := strings.Split(strings.TrimSpace(body), "\n\n")[2]
				data := strings.SplitN(last, "data: ", 2)[1]
				require.Equal(t, "resp_identity", gjson.Get(data, "response.id").String())
				require.Equal(t, "failed", gjson.Get(data, "response.status").String())
				require.EqualValues(t, 13, gjson.Get(data, "sequence_number").Int())
				require.Equal(t, "server_error", gjson.Get(data, "response.error.code").String())
			} else {
				require.Equal(t, before, recorder.Body.String())
			}
			after := recorder.Body.String()
			require.True(t, writeStartedStreamError(c, types.RelayFormatOpenAIResponses, apiErr))
			require.Equal(t, after, recorder.Body.String(), "controller replay must not append a second terminal")
		})
	}
}

type responsesControllerFlushWriter struct {
	*httptest.ResponseRecorder
	writes int
	broken bool
}

func (w *responsesControllerFlushWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.ResponseRecorder.Write(p)
}

func (w *responsesControllerFlushWriter) Flush() {
	if w.broken {
		panic("synthetic flush failure")
	}
	w.ResponseRecorder.Flush()
}

func TestStartedResponsesFailureDoesNotWriteAfterDownstreamFailure(t *testing.T) {
	for _, kind := range []string{"cancel", "short", "pipe", "flush"} {
		t.Run(kind, func(t *testing.T) {
			base := httptest.NewRecorder()
			partial := &partialNativeStreamWriter{ResponseRecorder: base, short: kind == "short"}
			flush := &responsesControllerFlushWriter{ResponseRecorder: base}
			var writer http.ResponseWriter = partial
			if kind == "flush" {
				writer = flush
			}
			c, _ := gin.CreateTestContext(writer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","delta":"partial"}`))
			if kind == "cancel" {
				cancel()
			} else {
				if kind == "flush" {
					flush.broken = true
				}
				err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","delta":"broken"}`)
				if kind == "short" {
					require.ErrorIs(t, err, io.ErrShortWrite)
				} else {
					require.Error(t, err)
				}
			}
			before, writes := base.Body.String(), partial.writes+flush.writes
			apiErr := types.NewError(errors.New("upstream error"), types.ErrorCodeBadResponse)
			require.True(t, writeStartedStreamError(c, types.RelayFormatOpenAIResponses, apiErr))
			require.Equal(t, before, base.Body.String())
			_, lateErr := helper.ResponseChunkDataWithDelivery(c, dto.ResponsesStreamResponse{Type: "response.failed"}, `{"type":"response.failed"}`)
			require.Error(t, lateErr)
			require.Equal(t, writes, partial.writes+flush.writes)
		})
	}
}

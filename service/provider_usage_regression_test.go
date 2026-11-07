package service_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/relay"
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These end-to-end regressions use synthetic provider facts and independently
// derived exact quota amounts. Each replay owns an isolated SQLite database,
// independent of global handles left by earlier package tests.
func usageReplayInfo(t *testing.T, id int, expr string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.BillingLogReceipt{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSQLite, oldMySQL, oldPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	oldRedis, oldBatch, oldConsume := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = oldSQLite, oldMySQL, oldPostgres
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldConsume
		_ = sqlDB.Close()
	})
	require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("audit-wire-%d", id), Quota: 10000, AffCode: fmt.Sprintf("audit-%d", id), Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: id, UserId: id, Key: fmt.Sprintf("audit-wire-%d", id), Name: "synthetic", Status: common.TokenStatusEnabled, RemainQuota: 10000}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: id, Name: "synthetic", Key: "synthetic", Status: common.ChannelStatusEnabled}).Error)
	t.Cleanup(func() {
		model.DB.Unscoped().Where("id = ?", id).Delete(&model.User{})
		model.DB.Unscoped().Where("id = ?", id).Delete(&model.Token{})
		model.DB.Unscoped().Where("id = ?", id).Delete(&model.Channel{})
		model.LOG_DB.Where("user_id = ?", id).Delete(&model.Log{})
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("token_quota", int64(10000))
	info := &relaycommon.RelayInfo{RequestId: fmt.Sprintf("audit-wire-%d", id), UserId: id, TokenId: id, TokenKey: fmt.Sprintf("audit-wire-%d", id),
		OriginModelName: "audit-model", StartTime: time.Now(), UsingGroup: "default", ForcePreConsume: true, DisablePing: true,
		RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id, UpstreamModelName: "gpt-4"},
		PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 2, ImageRatio: 1, CacheRatio: 0.1, CacheCreationRatio: 1.25, CacheCreation5mRatio: 1.25, CacheCreation1hRatio: 2, AudioRatio: 4, AudioCompletionRatio: 3,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	info.SetEstimatePromptTokens(38)
	if expr != "" {
		info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), GroupRatio: 1, QuotaPerUnit: 500000, EstimatedQuotaBeforeGroup: 1000, EstimatedQuotaAfterGroup: 1000}
	}
	session, apiErr := service.NewBillingSession(c, info, 1000)
	require.Nil(t, apiErr)
	info.Billing = session
	return c, info
}

func assertUsageReplayBalance(t *testing.T, info *relaycommon.RelayInfo, want int) {
	t.Helper()
	var user model.User
	var token model.Token
	var logs []model.Log
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	require.NoError(t, model.DB.First(&token, info.TokenId).Error)
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).Find(&logs).Error)
	qs := []int{}
	for _, row := range logs {
		qs = append(qs, row.Quota)
		t.Logf("WIRE_LOG prompt=%d completion=%d other=%s", row.PromptTokens, row.CompletionTokens, row.Other)
	}
	t.Logf("WIRE expected=%d wallet=%d token=%d used=%d requests=%d log_quotas=%v", want, user.Quota, token.RemainQuota, user.UsedQuota, user.RequestCount, qs)
	if want > 0 {
		require.Equal(t, 1, user.RequestCount)
	}
	if user.Quota != int64(10000-want) || token.RemainQuota != int64(10000-want) || user.UsedQuota != int64(want) || len(logs) != 1 || logs[0].Quota != want {
		t.Errorf("wire-to-ledger differs from independent oracle %d", want)
	}
}

func usageReplayRatios(t *testing.T) {
	t.Helper()
	oldC, oldA, oldAO := ratio_setting.CompletionRatio2JSONString(), ratio_setting.AudioRatio2JSONString(), ratio_setting.AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldC))
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(oldA))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(oldAO))
	})
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"audit-model":2}`))
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"audit-model":4}`))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(`{"audit-model":3}`))
}

func usageReplayResponse(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
}

func settleReplayedAudio(c *gin.Context, info *relaycommon.RelayInfo, u *dto.Usage) {
	if u.PromptTokensDetails.AudioTokens > 0 || u.CompletionTokenDetails.AudioTokens > 0 {
		service.PostAudioConsumeQuota(c, info, u, "")
	} else {
		service.PostTextConsumeQuota(c, info, u, nil)
	}
}

func TestUsageRegressionMediaFixedPrice(t *testing.T) {
	for i, kind := range []string{"audio", "realtime"} {
		t.Run(kind, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 1100+i, "")
			info.PriceData.UsePrice = true
			info.PriceData.ModelPrice = 0.01
			info.UsePrice = true
			if kind == "audio" {
				service.PostAudioConsumeQuota(c, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, PromptTokensDetails: dto.InputTokenDetails{TextTokens: 20, AudioTokens: 80}, CompletionTokenDetails: dto.OutputTokenDetails{TextTokens: 10, AudioTokens: 10}}, "")
			} else {
				service.PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, InputTokenDetails: dto.InputTokenDetails{TextTokens: 20, AudioTokens: 80}, OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 10, AudioTokens: 10}}, "")
			}
			assertUsageReplayBalance(t, info, 5000) // USD .01 * 500000 quota/USD * group 1, once.
		})
	}
}

func TestUsageRegressionMediaImageModalDetails(t *testing.T) {
	c, info := usageReplayInfo(t, 1110, `tier("media",p*1+img*2+c*3+img_o*4)`)
	raw := `{"data":[{"b64_json":"c3ludGhldGlj"}],"usage":{"input_tokens":100,"output_tokens":100,"total_tokens":200,"input_tokens_details":{"text_tokens":20,"image_tokens":80},"output_tokens_details":{"text_tokens":10,"image_tokens":90}}}`
	u, e := openai.OpenaiImageHandler(c, info, usageReplayResponse(raw))
	require.Nil(t, e)
	require.NotNil(t, u)
	t.Logf("MEDIA image normalized=%+v", u)
	service.PostTextConsumeQuota(c, info, u, nil)
	assertUsageReplayBalance(t, info, 285) // (20+80*2+10*3+90*4)/2
}

func TestUsageRegressionImageActualCount(t *testing.T) {
	for i, mode := range []string{"json", "json_as_stream", "sse", "xai"} {
		t.Run(mode, func(t *testing.T) {
			c, info := usageReplayInfo(t, 1800+i, "")
			info.PriceData.UsePrice = true
			info.PriceData.ModelPrice = 0.001
			info.PriceData.AddOtherRatio("n", 3)
			info.RelayMode = relayconstant.RelayModeImagesGenerations
			raw := `{"data":[{"b64_json":"c3ludGhldGlj"}]}`
			resp := usageReplayResponse(raw)
			var u *dto.Usage
			var apiErr *types.MaxAPIError
			switch mode {
			case "sse":
				info.IsStream = true
				resp = usageReplayResponse("data: {\"type\":\"image_generation.completed\",\"b64_json\":\"c3ludGhldGlj\"}\n\ndata: [DONE]\n\n")
				resp.Header.Set("Content-Type", "text/event-stream")
				u, apiErr = openai.OpenaiImageStreamHandler(c, info, resp)
			case "json_as_stream":
				info.IsStream = true
				u, apiErr = openai.OpenaiImageStreamHandler(c, info, resp)
			case "xai":
				adaptor := relay.GetAdaptor(constant.APITypeXai)
				adaptor.Init(info)
				var got any
				got, apiErr = adaptor.DoResponse(c, resp, info)
				if got != nil {
					u = got.(*dto.Usage)
				}
			default:
				u, apiErr = openai.OpenaiImageHandler(c, info, resp)
			}
			require.Nil(t, apiErr)
			require.NotNil(t, u)
			// Match ImageHelper's legacy non-token image placeholder.
			if u.TotalTokens == 0 {
				u.TotalTokens = 1
			}
			if u.PromptTokens == 0 {
				u.PromptTokens = 1
			}
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 500) // one generated image * USD.001 * 500000, not requested three.
			var row model.Log
			require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).First(&row).Error)
			var facts struct {
				Count int `json:"image_count"`
			}
			require.NoError(t, common.UnmarshalJsonStr(row.Other, &facts))
			require.Equal(t, 1, facts.Count)
		})
	}
}

func TestUsageRegressionIsolatedLedgerAfterClosedGlobal(t *testing.T) {
	closed, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := closed.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = closed, closed
	t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB })
	c, info := usageReplayInfo(t, 1830, "")
	service.PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, nil)
	assertUsageReplayBalance(t, info, 140)
}

func TestUsageRegressionMediaSTTOfficialDetails(t *testing.T) {
	for i, key := range []string{"input_token_details", "input_tokens_details", "official_no_output_details"} {
		t.Run(key, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 1120+i, "")
			info.PriceData.AudioRatio = 4
			info.PriceData.AudioCompletionRatio = 3
			wireKey := key
			if key == "official_no_output_details" {
				wireKey = "input_token_details"
			}
			raw := `{"text":"synthetic","usage":{"type":"tokens","input_tokens":100,"output_tokens":20,"total_tokens":120,"` + wireKey + `":{"text_tokens":20,"audio_tokens":80},"output_tokens_details":{"text_tokens":20}}}`
			if key == "official_no_output_details" {
				raw = strings.Replace(raw, `,"output_tokens_details":{"text_tokens":20}`, "", 1)
			}
			e, u := openai.OpenaiSTTHandler(c, usageReplayResponse(raw), info, "json")
			require.Nil(t, e)
			require.NotNil(t, u)
			t.Logf("MEDIA stt normalized=%+v", u)
			settleReplayedAudio(c, info, u)
			assertUsageReplayBalance(t, info, 380) // 20 text input + 80 audio*4 + 20 text output*2
		})
	}
}

func TestUsageRegressionMediaGeminiZero(t *testing.T) {
	for i, format := range []types.RelayFormat{types.RelayFormatGemini, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			c, info := usageReplayInfo(t, 1140+i, "")
			info.RelayFormat = format
			info.FinalRequestRelayFormat = types.RelayFormatGemini
			raw := `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}}`
			var u *dto.Usage
			switch format {
			case types.RelayFormatGemini:
				got, e := gemini.GeminiTextGenerationHandler(c, info, usageReplayResponse(raw))
				require.Nil(t, e)
				u = got
			case types.RelayFormatOpenAI:
				got, e := gemini.GeminiChatHandler(c, info, usageReplayResponse(raw))
				require.Nil(t, e)
				u = got
			default:
				got, e := gemini.GeminiResponsesHandler(c, info, usageReplayResponse(raw))
				require.Nil(t, e)
				u = got
			}
			require.NotNil(t, u)
			t.Logf("MEDIA zero normalized=%+v", u)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 0)
		})
	}
}

func TestUsageRegressionMediaOtherNativeAdapters(t *testing.T) {
	cases := []struct {
		name      string
		api, mode int
		raw       string
		stream    bool
		want      int
	}{
		{"baidu", constant.APITypeBaidu, relayconstant.RelayModeChatCompletions, `{"result":"hello","usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`, false, 140},
		{"cohere", constant.APITypeCohere, relayconstant.RelayModeChatCompletions, `{"text":"hello","meta":{"billed_units":{"input_tokens":100,"output_tokens":20}}}`, false, 140},
		{"dify", constant.APITypeDify, relayconstant.RelayModeChatCompletions, `{"answer":"hello","metadata":{"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}}`, false, 140},
		{"tencent", constant.APITypeTencent, relayconstant.RelayModeChatCompletions, `{"Response":{"Choices":[{"Message":{"Role":"assistant","Content":"hello"},"FinishReason":"stop"}],"Usage":{"PromptTokens":100,"CompletionTokens":20,"TotalTokens":120}}}`, false, 140},
		{"tencent_stream", constant.APITypeTencent, relayconstant.RelayModeChatCompletions, "data: {\"Choices\":[{\"Delta\":{\"Role\":\"assistant\",\"Content\":\"hello\"},\"FinishReason\":\"stop\"}],\"Usage\":{\"PromptTokens\":100,\"CompletionTokens\":20,\"TotalTokens\":120}}\n\n", true, 140},
		{"ollama", constant.APITypeOllama, relayconstant.RelayModeChatCompletions, `{"message":{"role":"assistant","content":"hello"},"done":true,"prompt_eval_count":100,"eval_count":20}`, false, 140},
		{"moka_embedding", constant.APITypeMokaAI, relayconstant.RelayModeEmbeddings, `{"data":[{"embedding":[0.1],"index":0,"object":"embedding"}],"usage":{"prompt_tokens":100,"total_tokens":100}}`, false, 100},
		{"coze_context", constant.APITypeCoze, relayconstant.RelayModeChatCompletions, `{"code":0,"data":[{"type":"answer","content":"hello"}]}`, false, 140},
		{"zhipu", constant.APITypeZhipu, relayconstant.RelayModeChatCompletions, `{"success":true,"data":{"choices":[{"role":"assistant","content":"hello"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}}`, false, 140},
		{"siliconflow_rerank", constant.APITypeSiliconFlow, relayconstant.RelayModeRerank, `{"results":[],"meta":{"tokens":{"input_tokens":100,"output_tokens":20}}}`, false, 140},
		{"minimax_tts_characters", constant.APITypeMiniMax, relayconstant.RelayModeAudioSpeech, `{"data":{"audio":"00"},"extra_info":{"usage_characters":100},"base_resp":{"status_code":0}}`, false, 100},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, info := usageReplayInfo(t, 1200+i, "")
			info.IsStream = tc.stream
			info.RelayMode = tc.mode
			if strings.HasPrefix(tc.name, "tencent") {
				info.ApiKey = "synthetic|synthetic|synthetic"
			}
			if tc.name == "coze_context" {
				c.Set("coze_input_count", 100)
				c.Set("coze_output_count", 20)
				c.Set("coze_token_count", 120)
			}
			ad := relay.GetAdaptor(tc.api)
			require.NotNil(t, ad)
			ad.Init(info)
			got, e := ad.DoResponse(c, usageReplayResponse(tc.raw), info)
			require.Nil(t, e)
			u, ok := got.(*dto.Usage)
			require.True(t, ok)
			require.NotNil(t, u)
			t.Logf("MEDIA vendor=%s usage=%+v", tc.name, u)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, tc.want)
		})
	}
}

func TestUsageRegressionMediaXAICache(t *testing.T) {
	for i, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			c, info := usageReplayInfo(t, 1230+i, "")
			info.IsStream = stream
			raw := `{"id":"audit","choices":[{"message":{"role":"assistant","content":"hello"},"delta":{"content":"hello"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60}}}`
			if stream {
				raw = "data: " + raw + "\n\ndata: [DONE]\n\n"
			}
			ad := relay.GetAdaptor(constant.APITypeXai)
			ad.Init(info)
			got, e := ad.DoResponse(c, usageReplayResponse(raw), info)
			require.Nil(t, e)
			u := got.(*dto.Usage)
			t.Logf("MEDIA xai stream=%v cache=%d", stream, u.PromptTokensDetails.CachedTokens)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 86)
		})
	}
}

func TestUsageRegressionMediaDifyDebugInvariant(t *testing.T) {
	old := constant.DifyDebug
	t.Cleanup(func() { constant.DifyDebug = old })
	for i, debug := range []bool{false, true} {
		t.Run(fmt.Sprint(debug), func(t *testing.T) {
			constant.DifyDebug = debug
			c, info := usageReplayInfo(t, 1240+i, "")
			info.IsStream = true
			raw := "data: {\"event\":\"node_finished\",\"data\":{\"node_id\":\"synthetic\",\"node_type\":\"llm\"}}\n\ndata: {\"event\":\"message_end\",\"metadata\":{\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}}\n\n"
			ad := relay.GetAdaptor(constant.APITypeDify)
			ad.Init(info)
			got, e := ad.DoResponse(c, usageReplayResponse(raw), info)
			require.Nil(t, e)
			u := got.(*dto.Usage)
			t.Logf("MEDIA dify debug=%v usage=%+v", debug, u)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 140)
		})
	}
}

func TestUsageRegressionMediaImageStreamDetails(t *testing.T) {
	c, info := usageReplayInfo(t, 1251, `tier("media",p*1+img*2+c*3+img_o*4)`)
	info.IsStream = true
	raw := `{"type":"image_generation.completed","b64_json":"c3ludGhldGlj","usage":{"input_tokens":100,"output_tokens":100,"total_tokens":200,"input_tokens_details":{"text_tokens":20,"image_tokens":80},"output_tokens_details":{"text_tokens":10,"image_tokens":90}}}`
	resp := usageReplayResponse("data: " + raw + "\n\ndata: [DONE]\n\n")
	resp.Header.Set("Content-Type", "text/event-stream")
	u, e := openai.OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, e)
	require.NotNil(t, u)
	service.PostTextConsumeQuota(c, info, u, nil)
	assertUsageReplayBalance(t, info, 285)
}

func usageReplayRecorder(c *gin.Context) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	other, _ := gin.CreateTestContext(r)
	c.Writer = other.Writer
	return r
}

func TestUsageRegressionProtocolChat(t *testing.T) {
	for i, kind := range []string{"cache", "zero"} {
		for j, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini, types.RelayFormatOpenAIResponses} {
			t.Run(fmt.Sprintf("%s/%v", kind, format), func(t *testing.T) {
				c, info := usageReplayInfo(t, 1400+i*10+j, "")
				info.RelayFormat = format
				info.FinalRequestRelayFormat = types.RelayFormatOpenAI
				rec := usageReplayRecorder(c)
				raw := `{"id":"synthetic","model":"audit-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60},"completion_tokens_details":{"reasoning_tokens":5}}}`
				want := 86
				if kind == "zero" {
					raw = `{"id":"synthetic","model":"audit-upstream","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
					want = 0
				}
				var u *dto.Usage
				if format == types.RelayFormatOpenAIResponses {
					v, e := openai.OaiChatToResponsesHandler(c, info, usageReplayResponse(raw))
					require.Nil(t, e)
					u = v
				} else {
					v, e := openai.OpenaiHandler(c, info, usageReplayResponse(raw))
					require.Nil(t, e)
					u = v
				}
				require.NotNil(t, u)
				t.Logf("CROSS raw=%s wire=%s", raw, rec.Body.String())
				service.PostTextConsumeQuota(c, info, u, nil)
				assertUsageReplayBalance(t, info, want)
				if kind == "cache" {
					switch format {
					case types.RelayFormatClaude:
						var out dto.ClaudeResponse
						require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
						require.NotNil(t, out.Usage)
						if out.Usage.InputTokens != 40 || out.Usage.CacheReadInputTokens != 60 {
							t.Errorf("Claude wire semantic oracle: input=40 cache=60, got input=%d cache=%d", out.Usage.InputTokens, out.Usage.CacheReadInputTokens)
						}
					case types.RelayFormatGemini:
						var out dto.GeminiChatResponse
						require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
						if out.UsageMetadata.CachedContentTokenCount != 60 || out.UsageMetadata.ThoughtsTokenCount != 5 || out.UsageMetadata.CandidatesTokenCount != 15 {
							t.Errorf("Gemini wire semantic oracle: cache=60 thoughts=5 candidates=15, got %+v", out.UsageMetadata)
						}
					}
				}
			})
		}
	}
}

func TestUsageRegressionProtocolClaudeChain(t *testing.T) {
	c, info := usageReplayInfo(t, 1430, "")
	info.RelayFormat = types.RelayFormatClaude
	info.FinalRequestRelayFormat = types.RelayFormatOpenAI
	rec := usageReplayRecorder(c)
	raw := `{"id":"synthetic","model":"audit-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60}}}`
	u, e := openai.OpenaiHandler(c, info, usageReplayResponse(raw))
	require.Nil(t, e)
	body := append([]byte(nil), rec.Body.Bytes()...)
	service.PostTextConsumeQuota(c, info, u, nil)
	assertUsageReplayBalance(t, info, 86)
	t.Logf("CROSS first_gateway_wire=%s", body)
	next, ni := usageReplayInfo(t, 1431, "")
	ni.RelayFormat = types.RelayFormatClaude
	ni.FinalRequestRelayFormat = types.RelayFormatClaude
	ci := &claude.ClaudeResponseInfo{Usage: &dto.Usage{}}
	require.Nil(t, claude.HandleClaudeResponseData(next, ni, ci, usageReplayResponse(""), body))
	service.PostTextConsumeQuota(next, ni, ci.Usage, nil)
	assertUsageReplayBalance(t, ni, 86)
}

func TestUsageRegressionProtocolGeminiToClaude(t *testing.T) {
	c, info := usageReplayInfo(t, 1440, "")
	info.RelayFormat = types.RelayFormatClaude
	info.FinalRequestRelayFormat = types.RelayFormatGemini
	rec := usageReplayRecorder(c)
	raw := `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"toolUsePromptTokenCount":5,"cachedContentTokenCount":20,"candidatesTokenCount":10,"thoughtsTokenCount":5,"totalTokenCount":120}}`
	u, e := gemini.GeminiChatHandler(c, info, usageReplayResponse(raw))
	require.Nil(t, e)
	t.Logf("CROSS gemini_to_claude_wire=%s", rec.Body.String())
	service.PostTextConsumeQuota(c, info, u, nil)
	assertUsageReplayBalance(t, info, 117)
	var out dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
	require.NotNil(t, out.Usage)
	if out.Usage.InputTokens != 85 || out.Usage.CacheReadInputTokens != 20 || out.Usage.OutputTokens != 15 {
		t.Errorf("Gemini-to-Claude wire oracle: input=85 cache=20 output=15, got %+v", out.Usage)
	}
}

func TestUsageRegressionProtocolStream(t *testing.T) {
	for i, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			c, info := usageReplayInfo(t, 1460+i, "")
			info.RelayFormat = format
			info.FinalRequestRelayFormat = types.RelayFormatOpenAI
			info.IsStream = true
			info.ShouldIncludeUsage = true
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
			rec := usageReplayRecorder(c)
			frames := []string{`{"id":"synthetic","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`, `{"id":"synthetic","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, `{"id":"synthetic","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60},"completion_tokens_details":{"reasoning_tokens":5}}}`, `[DONE]`}
			raw := "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
			u, e := openai.OaiStreamHandler(c, info, usageReplayResponse(raw))
			require.Nil(t, e)
			require.NotNil(t, u)
			t.Logf("CROSS stream=%v wire=%s", format, rec.Body.String())
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 86)
			if format == types.RelayFormatClaude {
				var last *dto.ClaudeUsage
				for _, line := range strings.Split(rec.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					var ev dto.ClaudeResponse
					if common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(line, "data:")), &ev) == nil && ev.Usage != nil {
						last = ev.Usage
					}
				}
				require.NotNil(t, last)
				t.Logf("CROSS last_claude_usage=%+v", last)
				if last.InputTokens != 40 || last.CacheReadInputTokens != 60 || last.OutputTokens != 20 {
					t.Errorf("stream Claude wire oracle: input=40 cache=60 output=20, got %+v", last)
				}
			} else {
				var last *dto.GeminiUsageMetadata
				for _, line := range strings.Split(rec.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					var ev dto.GeminiChatResponse
					if common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(line, "data:")), &ev) == nil && ev.HasUsageMetadata {
						m := ev.UsageMetadata
						last = &m
					}
				}
				require.NotNil(t, last)
				t.Logf("CROSS last_gemini_metadata=%+v", last)
				if last.PromptTokenCount != 100 || last.CachedContentTokenCount != 60 || last.CandidatesTokenCount != 15 || last.ThoughtsTokenCount != 5 {
					t.Errorf("stream Gemini wire oracle: prompt=100 cache=60 candidates=15 thoughts=5, got %+v", last)
				}
			}
		})
	}
}

func TestUsageRegressionClaude(t *testing.T) {
	for index, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
		for j, tc := range []struct {
			name, raw, expr string
			want            int
		}{
			{"aggregate", `{"input_tokens":100,"output_tokens":10,"cache_creation_input_tokens":100}`, `tier("audit", p*2+c*4+cc*2.5+cc1h*4)`, 245},
			{"partial_ttl", `{"input_tokens":100,"output_tokens":10,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":40,"ephemeral_1h_input_tokens":40}}`, `tier("audit", p*2+c*4+cc*2.5+cc1h*4)`, 275},
			{"nested_zero_ttl", `{"input_tokens":100,"output_tokens":10,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_1h_input_tokens":0},"claude_cache_creation_1_h_tokens":100}`, `tier("audit", p*2+c*4+cc*2.5+cc1h*4)`, 245},
			{"cache_only_expr", `{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":100}`, `tier("audit", p*2+c*4+cr*0.2)`, 10},
			{"cache_only", `{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":100}`, "", 10},
		} {
			t.Run(fmt.Sprintf("%v/%s", format, tc.name), func(t *testing.T) {
				c, info := usageReplayInfo(t, 850+index*10+j, tc.expr)
				info.RelayFormat = format
				info.FinalRequestRelayFormat = types.RelayFormatClaude
				content := `[{"type":"text","text":"hello"}]`
				if strings.HasPrefix(tc.name, "cache_only") {
					content = `[]`
				}
				raw := `{"id":"audit","type":"message","model":"claude-test","role":"assistant","content":` + content + `,"stop_reason":"end_turn","usage":` + tc.raw + `}`
				ci := &claude.ClaudeResponseInfo{Usage: &dto.Usage{}}
				resp := &http.Response{StatusCode: 200, Header: make(http.Header)}
				require.Nil(t, claude.HandleClaudeResponseData(c, info, ci, resp, []byte(raw)))
				service.PostTextConsumeQuota(c, info, ci.Usage, nil)
				assertUsageReplayBalance(t, info, tc.want)
			})
		}
	}
}

func TestUsageRegressionStreamUsageUpdates(t *testing.T) {
	for fi, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for ci, tc := range []struct {
			name, update string
			want         int
		}{
			{"empty", `{}`, 86},
			{"partial", `{"completion_tokens":25}`, 96},
			{"partial_terminal", `{"completion_tokens":25}`, 96},
			{"partial_first_finished", `{"completion_tokens":25}`, 96},
			{"zero", `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`, 0},
		} {
			t.Run(fmt.Sprintf("%v/%s", format, tc.name), func(t *testing.T) {
				c, info := usageReplayInfo(t, 1600+fi*10+ci, "")
				info.RelayFormat = format
				info.FinalRequestRelayFormat = types.RelayFormatOpenAI
				info.IsStream = true
				info.ShouldIncludeUsage = true
				info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
				frames := []string{
					`{"id":"synthetic","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
					`{"id":"synthetic","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60}}}`,
					`{"id":"synthetic","choices":[],"usage":` + tc.update + `}`,
					`{"id":"synthetic","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
					`[DONE]`,
				}
				if tc.name == "partial_terminal" {
					frames = append(frames[:3], `[DONE]`)
				}
				if tc.name == "partial_first_finished" {
					frames[0] = `{"id":"synthetic","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`
				}
				recorder := usageReplayRecorder(c)
				u, e := openai.OaiStreamHandler(c, info, usageReplayResponse("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"))
				require.Nil(t, e)
				service.PostTextConsumeQuota(c, info, u, nil)
				assertUsageReplayBalance(t, info, tc.want) // (100-60)+60*.1+completion*2, or explicit zero.
				if format == types.RelayFormatClaude {
					var last *dto.ClaudeUsage
					stops := 0
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data:") {
							continue
						}
						var event dto.ClaudeResponse
						require.NoError(t, common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(line, "data:")), &event))
						if event.Type == "message_delta" {
							last = event.Usage
						}
						if event.Type == "message_stop" {
							stops++
						}
					}
					require.Equal(t, 1, stops)
					require.NotNil(t, last)
					if tc.want == 0 {
						require.Zero(t, last.InputTokens)
						require.Zero(t, last.CacheReadInputTokens)
						require.Zero(t, last.OutputTokens)
					} else {
						require.Equal(t, 40, last.InputTokens)
						require.Equal(t, 60, last.CacheReadInputTokens)
						output := 20
						if strings.HasPrefix(tc.name, "partial") {
							output = 25
						}
						require.Equal(t, output, last.OutputTokens)
					}
					next, nextInfo := usageReplayInfo(t, 1900+ci, "")
					nextInfo.RelayFormat = types.RelayFormatClaude
					nextInfo.IsStream = true
					nextUsage, nextErr := claude.ClaudeStreamHandler(next, usageReplayResponse(recorder.Body.String()), nextInfo)
					require.Nil(t, nextErr)
					service.PostTextConsumeQuota(next, nextInfo, nextUsage, nil)
					assertUsageReplayBalance(t, nextInfo, tc.want)
				}
			})
		}
	}
}

func TestUsageRegressionAudioSnapshot(t *testing.T) {
	for i, kind := range []string{"audio", "realtime"} {
		t.Run(kind, func(t *testing.T) {
			usageReplayRatios(t)
			c, info := usageReplayInfo(t, 1130+i, "")
			require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"audit-model":8}`))
			u := &dto.Usage{PromptTokens: 100, CompletionTokens: 15, TotalTokens: 115, PromptTokensDetails: dto.InputTokenDetails{TextTokens: 20, AudioTokens: 80}, CompletionTokenDetails: dto.OutputTokenDetails{TextTokens: 10, AudioTokens: 5}}
			if kind == "audio" {
				service.PostAudioConsumeQuota(c, info, u, "")
			} else {
				service.PostWssConsumeQuota(c, info, info.UpstreamModelName, &dto.RealtimeUsage{InputTokens: 100, OutputTokens: 15, TotalTokens: 115, InputTokenDetails: u.PromptTokensDetails, OutputTokenDetails: u.CompletionTokenDetails}, "")
			}
			assertUsageReplayBalance(t, info, 420) // Frozen:20+80*4+10*2+5*4*3; new global would yield 800.
		})
	}
}
func usageReplayWSPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := up.Upgrade(w, r, nil)
		if e == nil {
			accepted <- c
		}
	}))
	t.Cleanup(server.Close)
	peer, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, e)
	gateway := <-accepted
	t.Cleanup(func() { peer.Close(); gateway.Close() })
	return gateway, peer
}
func TestUsageRegressionRealtimeWire(t *testing.T) {
	for i, tc := range []struct {
		name  string
		ids   []string
		cache int
		want  int
	}{{"one", []string{"a"}, 0, 140}, {"two_distinct", []string{"a", "b"}, 0, 280}, {"duplicate_id", []string{"a", "a"}, 0, 140}} {
		t.Run(tc.name, func(t *testing.T) {
			usageReplayRatios(t)
			old := ratio_setting.ModelRatio2JSONString()
			t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(old)) })
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"audit-model":1}`))
			c, info := usageReplayInfo(t, 1160+i, "")
			info.IsFirstRequest = true
			info.UpstreamModelName = "audit-model"
			info.ClientWs = nil
			downstream, client := usageReplayWSPair(t)
			upstream, provider := usageReplayWSPair(t)
			info.ClientWs = downstream
			info.TargetWs = upstream
			done := make(chan *dto.RealtimeUsage, 1)
			go func() {
				e, u := openai.OpenaiRealtimeHandler(c, info)
				if e != nil {
					t.Logf("MEDIA realtime error=%v", e)
				}
				done <- u
			}()
			for index, id := range tc.ids {
				// A proxy may assign a fresh event_id when replaying the same response.
				raw := fmt.Sprintf(`{"type":"response.done","event_id":"event-%d","response":{"id":"resp-%s","status":"completed","usage":{"total_tokens":120,"input_tokens":100,"output_tokens":20,"input_token_details":{"text_tokens":100,"audio_tokens":0,"cached_tokens":%d,"cached_tokens_details":{"text_tokens":%d}},"output_token_details":{"text_tokens":20,"audio_tokens":0}}}}`, index, id, tc.cache, tc.cache)
				require.NoError(t, provider.WriteMessage(websocket.TextMessage, []byte(raw)))
			}
			require.NoError(t, provider.WriteMessage(websocket.TextMessage, []byte(`{"type":"audit.barrier"}`)))
			require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
			for {
				_, b, e := client.ReadMessage()
				require.NoError(t, e)
				if strings.Contains(string(b), "audit.barrier") {
					break
				}
			}
			require.NoError(t, provider.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second)))
			provider.Close()
			var u *dto.RealtimeUsage
			select {
			case u = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("fixture: realtime handler did not close")
			}
			require.NotNil(t, u)
			t.Logf("MEDIA realtime aggregate=%+v", u)
			service.PostWssConsumeQuota(c, info, info.OriginModelName, u, "")
			assertUsageReplayBalance(t, info, tc.want)
		})
	}
}
func TestUsageRegressionRealtimeMappedPrice(t *testing.T) {
	usageReplayRatios(t)
	c, info := usageReplayInfo(t, 1250, "")
	info.UpstreamModelName = "audit-upstream"
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"audit-model":2,"audit-upstream":10}`))
	u := &dto.RealtimeUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, InputTokenDetails: dto.InputTokenDetails{TextTokens: 100}, OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 20}}
	service.PostWssConsumeQuota(c, info, info.UpstreamModelName, u, "")
	assertUsageReplayBalance(t, info, 140)
}

func TestUsageRegressionRealtimeCachePolicy(t *testing.T) {
	for i, tc := range []struct {
		expr string
		want int
	}{
		{"", 140},
		{`tier("cache", p*2+cr*0.2+c*4)`, 86},
	} {
		c, info := usageReplayInfo(t, 1820+i, tc.expr)
		u := &dto.RealtimeUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, InputTokenDetails: dto.InputTokenDetails{TextTokens: 100, CachedTokens: 60}, OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 20}}
		service.PostWssConsumeQuota(c, info, info.UpstreamModelName, u, "")
		assertUsageReplayBalance(t, info, tc.want)
		var row model.Log
		require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", info.UserId, model.LogTypeConsume).First(&row).Error)
		var facts struct {
			Usage           dto.RealtimeUsage `json:"realtime_usage"`
			DiscountApplied *bool             `json:"realtime_cache_discount_applied"`
		}
		require.NoError(t, common.UnmarshalJsonStr(row.Other, &facts))
		require.Equal(t, 60, facts.Usage.InputTokenDetails.CachedTokens)
		if tc.expr == "" {
			require.NotNil(t, facts.DiscountApplied)
			require.False(t, *facts.DiscountApplied)
		} else {
			require.Nil(t, facts.DiscountApplied)
		}
	}
}

func TestUsageRegressionClaudeCacheCreationCleared(t *testing.T) {
	c, info := usageReplayInfo(t, 1920, "")
	info.RelayFormat, info.IsStream = types.RelayFormatClaude, true
	frames := []string{
		`{"type":"message_start","message":{"id":"synthetic","model":"audit-model","role":"assistant","content":[],"usage":{"input_tokens":100,"output_tokens":0,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_1h_input_tokens":100}}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10,"cache_creation_input_tokens":0}}`,
		`{"type":"message_stop"}`,
	}
	usage, apiErr := claude.ClaudeStreamHandler(c, usageReplayResponse("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"), info)
	require.Nil(t, apiErr)
	service.PostTextConsumeQuota(c, info, usage, nil)
	assertUsageReplayBalance(t, info, 120) // latest facts: 100 input + 10 output*2; obsolete cache creation is zero.
}

func TestUsageRegressionInputOutputAliases(t *testing.T) {
	for i, tc := range []struct {
		name, first, final        string
		format                    types.RelayFormat
		quota, prompt, completion int
	}{
		{"native", "", `{"input_tokens":100,"output_tokens":20}`, types.RelayFormatOpenAI, 140, 100, 20},
		{"claude_cache", "", `{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":60},"output_tokens_details":{"reasoning_tokens":5}}`, types.RelayFormatClaude, 86, 100, 20},
		{"explicit_zero", `,"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60}}`, `{"input_tokens":0,"output_tokens":0}`, types.RelayFormatOpenAI, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := usageReplayInfo(t, 1950+i, "")
			info.RelayFormat, info.IsStream = tc.format, true
			info.FinalRequestRelayFormat = types.RelayFormatOpenAI
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{}
			frames := []string{`{"choices":[{"delta":{"content":"hello"}}]` + tc.first + `}`, `{"choices":[],"usage":` + tc.final + `}`, `[DONE]`}
			u, apiErr := openai.OaiStreamHandler(c, info, usageReplayResponse("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"))
			require.Nil(t, apiErr)
			require.NotNil(t, u)
			service.PostTextConsumeQuota(c, info, u, nil)
			// Independent prices: p + 2*c; cached60 replaces input60 at0.1.
			assertUsageReplayBalance(t, info, tc.quota)
			require.Equal(t, tc.prompt, u.PromptTokens)
			require.Equal(t, tc.completion, u.CompletionTokens)
			require.Equal(t, tc.prompt+tc.completion, u.TotalTokens)
		})
	}
}

func TestUsageRegressionClaudeFinalFrameLifecycle(t *testing.T) {
	for i, tc := range []struct {
		name   string
		frames []string
		text   string
	}{
		{"tail_choices", []string{
			`{"choices":[{"delta":{"content":"begin"}}]}`,
			`{"choices":[{"delta":{"content":"end"},"finish_reason":"stop"}]}`,
			`{"choices":[{"delta":{"content":"tail"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`,
		}, "beginendtail"},
		{"first_finish_then_usage", []string{
			`{"choices":[{"delta":{"content":"only"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`,
		}, "only"},
		{"one_frame", []string{
			`{"choices":[{"delta":{"content":"only"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`,
		}, "only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := usageReplayInfo(t, 1960+i, "")
			info.RelayFormat, info.IsStream = types.RelayFormatClaude, true
			info.FinalRequestRelayFormat = types.RelayFormatOpenAI
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{}
			recorder := usageReplayRecorder(c)
			frames := append(append([]string{}, tc.frames...), "[DONE]")
			u, apiErr := openai.OaiStreamHandler(c, info, usageReplayResponse("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"))
			require.Nil(t, apiErr)
			require.NotNil(t, u)
			service.PostTextConsumeQuota(c, info, u, nil)
			assertUsageReplayBalance(t, info, 140)
			counts := map[string]int{}
			var text strings.Builder
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				var event dto.ClaudeResponse
				require.NoError(t, common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(line, "data:")), &event))
				counts[event.Type]++
				if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Text != nil {
					text.WriteString(*event.Delta.Text)
				}
				if event.Type == "message_delta" {
					require.Equal(t, 100, event.Usage.InputTokens)
					require.Equal(t, 20, event.Usage.OutputTokens)
				}
			}
			require.Equal(t, tc.text, text.String())
			require.Equal(t, 1, counts["message_start"])
			require.Equal(t, 1, counts["message_delta"])
			require.Equal(t, 1, counts["message_stop"])
		})
	}
}

func TestUsageRegressionProviderCannotSupplyBillingEvidence(t *testing.T) {
	for i, tc := range []struct {
		name        string
		xai, stream bool
	}{
		{"openai_stream", false, true}, {"xai_stream", true, true},
		{"openai_json", false, false}, {"xai_json", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info := usageReplayInfo(t, 1970+i, "")
			info.IsStream = tc.stream
			raw := `{"choices":[{"delta":{"content":"hello"},"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60},"cost":0.25,"usage_semantic":"anthropic","usage_source":"claude_messages","billing_usage":{"semantic":"openai","openai_usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}}}`
			if tc.stream {
				raw = "data: " + raw + "\n\ndata: [DONE]\n\n"
			}
			var usage *dto.Usage
			if tc.xai {
				ad := relay.GetAdaptor(constant.APITypeXai)
				ad.Init(info)
				got, apiErr := ad.DoResponse(c, usageReplayResponse(raw), info)
				require.Nil(t, apiErr)
				usage = got.(*dto.Usage)
			} else {
				var apiErr *types.MaxAPIError
				if tc.stream {
					usage, apiErr = openai.OaiStreamHandler(c, info, usageReplayResponse(raw))
				} else {
					usage, apiErr = openai.OpenaiHandler(c, info, usageReplayResponse(raw))
				}
				require.Nil(t, apiErr)
			}
			service.PostTextConsumeQuota(c, info, usage, nil)
			assertUsageReplayBalance(t, info, 86) // (100-60)*1 + 60*0.1 + 20*2.
			require.Nil(t, usage.BillingUsage)
			require.Empty(t, usage.UsageSemantic)
			require.Empty(t, usage.UsageSource)
			require.Equal(t, 0.25, usage.Cost)
		})
	}
}

func TestUsageRegressionPartialOpenAIStreamCounters(t *testing.T) {
	for i, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		for j, tc := range []struct {
			name, wire         string
			p, c, total, quota int
		}{
			{"completion_only", `{"completion_tokens":20}`, 100, 20, 120, 140},
			{"completion_zero", `{"output_tokens":0}`, 100, 0, 100, 100},
			{"details_only", `{"prompt_tokens_details":{"cached_tokens":60},"completion_tokens_details":{"reasoning_tokens":5}}`, 100, 5, 105, 56},
			{"reported_total", `{"completion_tokens":20,"total_tokens":120}`, 100, 20, 120, 140},
			{"infer_output", `{"prompt_tokens":0,"total_tokens":120}`, 0, 120, 120, 240},
			{"infer_output_with_input", `{"prompt_tokens":100,"total_tokens":120}`, 100, 20, 120, 140},
			{"stale_total", `{"completion_tokens":25}`, 100, 25, 125, 150},
			{"total_zero", `{"total_tokens":0}`, 0, 0, 0, 0},
		} {
			t.Run(fmt.Sprintf("%s/%s", format, tc.name), func(t *testing.T) {
				c, info := usageReplayInfo(t, 1980+i*10+j, "")
				info.RelayFormat, info.FinalRequestRelayFormat, info.IsStream = format, types.RelayFormatOpenAI, true
				info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{}
				info.SetEstimatePromptTokens(100)
				info.ShouldIncludeUsage = true
				recorder := usageReplayRecorder(c)
				raw := `data: {"choices":[{"delta":{}}],"usage":` + tc.wire + "}\n\ndata: [DONE]\n\n"
				if tc.name == "stale_total" {
					raw = "data: {\"choices\":[],\"usage\":{\"completion_tokens\":20,\"total_tokens\":120}}\n\n" + raw
				}
				u, apiErr := openai.OaiStreamHandler(c, info, usageReplayResponse(raw))
				require.Nil(t, apiErr)
				service.PostTextConsumeQuota(c, info, u, nil)
				assertUsageReplayBalance(t, info, tc.quota)
				require.Equal(t, tc.p, u.PromptTokens)
				require.Equal(t, tc.c, u.CompletionTokens)
				require.Equal(t, tc.total, u.TotalTokens)
				if tc.name != "total_zero" {
					require.NotNil(t, u.BillingUsage)
					require.True(t, u.BillingUsage.Estimated)
				}
				if format == types.RelayFormatOpenAI {
					var last *dto.Usage
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data:") || strings.TrimSpace(strings.TrimPrefix(line, "data:")) == "[DONE]" {
							continue
						}
						var event dto.ChatCompletionsStreamResponse
						require.NoError(t, common.UnmarshalJsonStr(strings.TrimSpace(strings.TrimPrefix(line, "data:")), &event))
						if event.Usage != nil {
							last = event.Usage
						}
					}
					require.NotNil(t, last)
					require.Equal(t, tc.p, last.PromptTokens)
					require.Equal(t, tc.c, last.CompletionTokens)
				}
			})
		}
	}
}

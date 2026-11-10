package router

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/middleware"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These contracts enter the real router over loopback. They do not call an
// adapter, price helper or settlement function to produce the observed result.
// All quota expectations below are independent constants in quota units.
func TestGatewayUsageContractSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), gatewayUsageGORMConfig())
	require.NoError(t, err)
	runGatewayUsageContract(t, db)
}

// External runs explicitly opt in, require an empty authorized disposable DB,
// validate TLS and retain all synthetic rows for independent inspection.
func TestGatewayUsageContractMySQL(t *testing.T) {
	if os.Getenv("MAXAPI_GATEWAY_MYSQL") != "1" {
		t.Skip("requires MAXAPI_GATEWAY_MYSQL=1 and dedicated test credentials")
	}
	cfg, err := mysqldriver.ParseDSN(os.Getenv("TEST_GATEWAY_USAGE_MYSQL_DSN"))
	if err != nil || cfg.DBName != "maxapi_gateway_usage_test_1" || cfg.AllowFallbackToPlaintext {
		t.Fatal("invalid dedicated MySQL configuration (details withheld)")
	}
	pin, err := hex.DecodeString(os.Getenv("TEST_MYSQL_TLS_CERT_SHA256"))
	if err != nil || len(pin) != sha256.Size {
		t.Fatal("requires a verified MySQL certificate pin")
	}
	cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12,
		//nolint:gosec // The exact configured certificate is verified below.
		InsecureSkipVerify: true,
		VerifyConnection: func(s tls.ConnectionState) error {
			if len(s.PeerCertificates) == 0 {
				return fmt.Errorf("missing MySQL certificate")
			}
			actual := sha256.Sum256(s.PeerCertificates[0].Raw)
			if !hmac.Equal(actual[:], pin) {
				return fmt.Errorf("MySQL certificate mismatch")
			}
			return nil
		},
	}
	cfg.ParseTime = true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		t.Fatal("MySQL connector unavailable (details withheld)")
	}
	conn := sql.OpenDB(connector)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var name string
	var relations int
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil || name != cfg.DBName {
		t.Fatal("MySQL identity check failed (details withheld)")
	}
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&relations); err != nil || relations != 0 {
		t.Fatal("MySQL must be a fresh empty dedicated database")
	}
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: conn}), gatewayUsageGORMConfig())
	if err != nil {
		t.Fatal("MySQL GORM initialization failed (details withheld)")
	}
	// The unique claim also rejects two harnesses which both observed an
	// empty catalog before either created the test schema.
	if err := db.AutoMigrate(&gatewayUsageTestOwnership{}); err != nil {
		t.Fatal("MySQL fixture ownership migration failed")
	}
	if db.Create(&gatewayUsageTestOwnership{ID: 1}).Error != nil {
		t.Fatal("MySQL fixture ownership could not be claimed")
	}
	runGatewayUsageContract(t, db)
}

type gatewayUsageTestOwnership struct {
	ID int `gorm:"primaryKey;autoIncrement:false"`
}

func (gatewayUsageTestOwnership) TableName() string { return "gateway_usage_test_ownership" }

func gatewayUsageGORMConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true}
}

func runGatewayUsageContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	if db.Name() == "mysql" {
		// The remote database has real network latency. Polling must not hold
		// the only connection while the asynchronous projections need it.
		sqlDB.SetMaxOpenConns(4)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.Log{}, &model.BillingLogReceipt{}, &model.BillingSettlement{}, &model.BillingPreConsumeSelection{},
		&model.CacheInvalidationTask{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
	oldDB, oldLog := model.DB, model.LOG_DB
	oldSQLite, oldMySQL, oldPG := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	oldRedis, oldMemory, oldBatch, oldConsume := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	oldFloor, oldRetry := common.PreConsumedQuota, common.RetryTimes
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = db.Name() == "sqlite", db.Name() == "mysql", false
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, false, true
	common.PreConsumedQuota, common.RetryTimes = 500, 0
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = oldSQLite, oldMySQL, oldPG
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldMemory, oldBatch, oldConsume
		common.PreConsumedQuota, common.RetryTimes = oldFloor, oldRetry
	})
	service.InitHttpClient()
	service.InitTokenEncoders()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	completed := make(chan gatewayCompletion, 32)
	realtimeDone := make(chan struct{}, 8)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		engine.ServeHTTP(w, r)
		completed <- gatewayCompletion{auth: r.Header.Get("Authorization"), requestID: w.Header().Get(common.RequestIdKey)}
		if r.URL.Path == "/v1/realtime" {
			realtimeDone <- struct{}{}
		}
	}))
	t.Cleanup(gateway.Close)
	var realtimeConnections []*websocket.Conn
	t.Cleanup(func() {
		for _, ws := range realtimeConnections {
			_ = ws.Close()
		}
		for range realtimeConnections {
			select {
			case <-realtimeDone:
			case <-time.After(5 * time.Second):
				t.Errorf("realtime handler did not finish before cleanup")
				return
			}
		}
	})
	registerRealtime := func(ws *websocket.Conn) {
		realtimeConnections = append(realtimeConnections, ws)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	const (
		// 40 uncached input + 60 cached input * 0.1 + 20 output * 2.
		gatewayTextQuota = 86
		// Gemini reports 7 prompt tokens and zero completion tokens.
		gatewayGeminiQuota = 7
		// Two successfully returned images, at 500 quota units each.
		gatewayImageQuota = 1000
		// 20 text tokens * 1 + 80 audio tokens * audio ratio 4.
		gatewayAudioQuota = 340
		// 100 input tokens + 20 output tokens * 2.
		gatewayRealtimeQuota = 140
		gatewayZeroQuota     = 0
	)

	for index, tc := range []struct {
		name, path, body, response                        string
		channel, quota, prompt, completion                int
		stream, unauthorized, failure, changePrice, audio bool
		upstreamPath                                      string
	}{
		{"native_json", "/v1/chat/completions", "", gatewayChatJSON, constant.ChannelTypeOpenAI, gatewayTextQuota, 100, 20, false, false, false, false, false, "/v1/chat/completions"},
		{"claude_json", "/v1/messages", "", gatewayChatJSON, constant.ChannelTypeOpenAI, gatewayTextQuota, 100, 20, false, false, false, false, false, "/v1/chat/completions"},
		{"claude_stream_late_usage", "/v1/messages", "", gatewayChatStream, constant.ChannelTypeOpenAI, gatewayTextQuota, 100, 20, true, false, false, false, false, "/v1/chat/completions"},
		{"stream_final_zero", "/v1/chat/completions", "", gatewayZeroStream, constant.ChannelTypeOpenAI, gatewayZeroQuota, 0, 0, true, false, false, false, false, "/v1/chat/completions"},
		{"private_evidence_rejected", "/v1/chat/completions", "", strings.Replace(gatewayChatJSON, `"usage":{`, `"usage":{"billing_usage":{"input_tokens":0,"output_tokens":0},"usage_semantic":"claude","usage_source":"injected",`, 1), constant.ChannelTypeOpenAI, gatewayTextQuota, 100, 20, false, false, false, false, false, "/v1/chat/completions"},
		{"frozen_price", "/v1/chat/completions", "", gatewayChatJSON, constant.ChannelTypeOpenAI, gatewayTextQuota, 100, 20, false, false, false, true, false, "/v1/chat/completions"},
		{"gemini_reported_zero", "/v1/chat/completions", "", `{"candidates":[{"content":{"role":"model","parts":[{"text":"visible"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":0,"totalTokenCount":7}}`, constant.ChannelTypeGemini, gatewayGeminiQuota, 7, 0, false, false, false, false, false, "/v1beta/models/gpt-4:generateContent"},
		{"responses_native", "/v1/responses", `{"model":"gateway-audit","input":"hi","max_output_tokens":20}`, `{"id":"resp_gateway","object":"response","status":"completed","model":"gpt-4","output":[{"type":"message","id":"msg_gateway","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":60}}}`, constant.ChannelTypeOpenAI, gatewayTextQuota, 100, 20, false, false, false, false, false, "/v1/responses"},
		{"image_partial_success", "/v1/images/generations", `{"model":"gateway-audit","prompt":"synthetic","n":3}`, `{"data":[{"b64_json":"YQ=="},{"b64_json":"Yg=="}]}`, constant.ChannelTypeOpenAI, gatewayImageQuota, 1, 0, false, false, false, false, false, "/v1/images/generations"},
		{"stt_audio_usage", "/v1/audio/transcriptions", "", `{"text":"synthetic","usage":{"input_tokens":100,"output_tokens":0,"total_tokens":100,"input_token_details":{"text_tokens":20,"audio_tokens":80}}}`, constant.ChannelTypeOpenAI, gatewayAudioQuota, 100, 0, false, false, false, false, true, "/v1/audio/transcriptions"},
		{"unauthorized", "/v1/chat/completions", "", gatewayChatJSON, constant.ChannelTypeOpenAI, gatewayZeroQuota, 0, 0, false, true, false, false, false, "/v1/chat/completions"},
		{"upstream_failure_refund", "/v1/chat/completions", "", `{"error":{"message":"synthetic unavailable","type":"server_error"}}`, constant.ChannelTypeOpenAI, gatewayZeroQuota, 0, 0, false, false, true, false, false, "/v1/chat/completions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gatewayUsagePrices(t, tc.path == "/v1/images/generations")
			id := 8200 + index
			var hits atomic.Int32
			var failureFixture gatewayUsageFixture
			streamAnswerSeen := make(chan struct{})
			upstreamModels := make(chan string, 1)
			upstreamObserved := make(chan gatewayUpstreamObservation, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				select {
				case upstreamObserved <- gatewayUpstreamObservation{path: r.URL.Path, authorization: r.Header.Get("Authorization")}:
				default:
				}
				if tc.audio {
					r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
					if r.ParseMultipartForm(1<<20) == nil {
						gatewayUsageCaptureModel(upstreamModels, r.FormValue("model"))
					}
				} else if tc.channel == constant.ChannelTypeGemini {
					gatewayUsageCaptureModel(upstreamModels, r.URL.Path)
				} else {
					body, _ := io.ReadAll(r.Body)
					gatewayUsageCaptureModel(upstreamModels, gjson.GetBytes(body, "model").String())
				}
				if tc.changePrice {
					if err := ratio_setting.UpdateModelRatioByJSONString(`{"gateway-audit":9}`); err != nil {
						t.Errorf("update model ratio: %v", err)
						http.Error(w, "synthetic price update failed", http.StatusInternalServerError)
						return
					}
				}
				if tc.failure {
					reservationVisible := assert.Eventually(t, func() bool {
						var user model.User
						var token model.Token
						if db.First(&user, failureFixture.UserID).Error != nil || db.First(&token, failureFixture.TokenID).Error != nil {
							return false
						}
						return user.Quota < 10000 && token.RemainQuota < 10000
					}, 5*time.Second, 20*time.Millisecond)
					if !reservationVisible {
						http.Error(w, "reservation was not visible", http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					flusher, ok := w.(http.Flusher)
					if !ok {
						return
					}
					for frameIndex, frame := range strings.Split(tc.response, "\n\n") {
						if frame == "" {
							continue
						}
						_, _ = io.WriteString(w, frame+"\n\n")
						flusher.Flush()
						if frameIndex == 1 {
							select {
							case <-streamAnswerSeen:
							case <-time.After(2 * time.Second):
								t.Errorf("stream answer was not delivered before upstream released remaining frames")
								return
							}
						}
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			t.Cleanup(upstream.Close)
			fixture := gatewayUsageSeed(t, db, id, upstream.URL, tc.channel)
			failureFixture = fixture
			body := tc.body
			if body == "" {
				body = fmt.Sprintf(`{"model":"gateway-audit","messages":[{"role":"user","content":"hi"}],"max_tokens":20,"stream":%t,"stream_options":{"include_usage":true}}`, tc.stream)
			}
			contentType := "application/json"
			if tc.audio {
				var b bytes.Buffer
				m := multipart.NewWriter(&b)
				require.NoError(t, m.WriteField("model", "gateway-audit"))
				require.NoError(t, m.WriteField("response_format", "json"))
				part, err := m.CreateFormFile("file", "synthetic.wav")
				require.NoError(t, err)
				// One second of 16 kHz mono PCM silence; real request validation
				// must parse the duration before reaching the simulated provider.
				wav := make([]byte, 44+32000)
				copy(wav, "RIFF")
				binary.LittleEndian.PutUint32(wav[4:], 36+32000)
				copy(wav[8:], "WAVEfmt ")
				binary.LittleEndian.PutUint32(wav[16:], 16)
				binary.LittleEndian.PutUint16(wav[20:], 1)
				binary.LittleEndian.PutUint16(wav[22:], 1)
				binary.LittleEndian.PutUint32(wav[24:], 16000)
				binary.LittleEndian.PutUint32(wav[28:], 32000)
				binary.LittleEndian.PutUint16(wav[32:], 2)
				binary.LittleEndian.PutUint16(wav[34:], 16)
				copy(wav[36:], "data")
				binary.LittleEndian.PutUint32(wav[40:], 32000)
				_, err = part.Write(wav)
				require.NoError(t, err)
				require.NoError(t, m.Close())
				body, contentType = b.String(), m.FormDataContentType()
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, gateway.URL+tc.path, strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", contentType)
			if !tc.unauthorized {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer gatewayfixture%d", id))
			}
			resp, err := client.Do(req)
			require.NoError(t, err)
			var wire []byte
			if tc.stream {
				readDone := make(chan struct{})
				var answerSeen atomic.Bool
				answerMarker := []byte("ok")
				if tc.name == "stream_final_zero" {
					answerMarker = []byte("visible")
				}
				go func() {
					defer close(readDone)
					var buffer bytes.Buffer
					chunk := make([]byte, 1024)
					for {
						n, readErr := resp.Body.Read(chunk)
						if n > 0 {
							_, _ = buffer.Write(chunk[:n])
							if !answerSeen.Load() && gatewayUsageStreamAnswerDelivered(buffer.Bytes(), tc.path, answerMarker) {
								answerSeen.Store(true)
								close(streamAnswerSeen)
							}
						}
						if readErr != nil {
							if readErr != io.EOF {
								err = readErr
							}
							break
						}
					}
					wire = buffer.Bytes()
				}()
				select {
				case <-readDone:
				case <-time.After(5 * time.Second):
					t.Fatal("stream response did not finish")
				}
			} else {
				wire, err = io.ReadAll(resp.Body)
			}
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			requestID := gatewayUsageWaitHandler(t, completed, req.Header.Get("Authorization"), db.Name() == "mysql")
			if !tc.unauthorized {
				select {
				case observed := <-upstreamObserved:
					require.Equal(t, tc.upstreamPath, observed.path)
					if tc.channel == constant.ChannelTypeOpenAI {
						require.Equal(t, "Bearer synthetic", observed.authorization)
					}
				case <-time.After(time.Second):
					t.Fatal("simulated upstream request was not observed")
				}
			}
			t.Logf("gateway status=%d request=%s wire=%s", resp.StatusCode, resp.Header.Get(common.RequestIdKey), wire)
			if tc.unauthorized {
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
				require.Zero(t, hits.Load())
			} else if tc.failure {
				require.GreaterOrEqual(t, resp.StatusCode, 400)
				require.EqualValues(t, 1, hits.Load())
			} else {
				require.Equal(t, http.StatusOK, resp.StatusCode)
				require.EqualValues(t, 1, hits.Load())
			}
			if !tc.unauthorized {
				select {
				case upstreamModel := <-upstreamModels:
					if tc.channel == constant.ChannelTypeGemini {
						require.Contains(t, upstreamModel, "/models/gpt-4:")
					} else {
						require.Equal(t, "gpt-4", upstreamModel, "upstream receives mapped model; ledger retains origin")
					}
				case <-time.After(time.Second):
					t.Fatal("simulated upstream did not capture a model")
				}
			}
			wantLogs := 1
			if tc.unauthorized || tc.failure {
				wantLogs = 0
			}
			if tc.name == "upstream_failure_refund" {
				gatewayUsageAssertRefundEffectComplete(t, db, requestID)
			}
			gatewayUsageAssertLedger(t, db, fixture, tc.quota, wantLogs, tc.prompt, tc.completion)
			if wantLogs == 1 {
				var log model.Log
				require.NoError(t, db.Where("user_id = ? AND type = ?", fixture.UserID, model.LogTypeConsume).First(&log).Error)
				require.Equal(t, resp.Header.Get(common.RequestIdKey), log.RequestId)
				require.NotEmpty(t, log.RequestId)
				require.Equal(t, fixture.TokenID, log.TokenId)
				require.True(t, gjson.Get(log.Other, "is_model_mapped").Bool())
				require.Equal(t, "gpt-4", gjson.Get(log.Other, "upstream_model_name").String())
				if tc.name == "native_json" {
					require.EqualValues(t, 60, gjson.GetBytes(wire, "usage.prompt_tokens_details.cached_tokens").Int())
					finalizeKey := model.BillingRequestFinalizeOperationKey(resp.Header.Get(common.RequestIdKey))
					var settlement model.BillingSettlement
					require.NoError(t, db.Where("operation_key = ? AND user_id = ? AND token_id = ? AND status = ?", finalizeKey, fixture.UserID, fixture.TokenID, model.BillingSettlementStatusApplied).First(&settlement).Error)
					var receipt model.BillingLogReceipt
					require.NoError(t, db.Where("operation_key = ?", finalizeKey).First(&receipt).Error)
				}
				if tc.path == "/v1/images/generations" {
					require.EqualValues(t, 2, gjson.Get(log.Other, "image_count").Int())
				}
			}
			if tc.name == "upstream_failure_refund" {
				finalizeKey := model.BillingRequestFinalizeOperationKey(resp.Header.Get(common.RequestIdKey))
				var settlement model.BillingSettlement
				require.NoError(t, db.Where("operation_key = ? AND user_id = ? AND token_id = ? AND status = ?", finalizeKey, fixture.UserID, fixture.TokenID, model.BillingSettlementStatusApplied).First(&settlement).Error)
			}
			if wantLogs == 1 && !tc.audio && tc.path != "/v1/images/generations" {
				answer := "ok"
				if tc.channel == constant.ChannelTypeGemini || tc.name == "stream_final_zero" {
					answer = "visible"
				}
				if tc.name == "claude_stream_late_usage" || tc.name == "stream_final_zero" {
					answer += "!"
				}
				gatewayUsageAssertWire(t, wire, tc.path, tc.stream, tc.prompt, tc.completion, answer)
			}
			if wantLogs == 1 && tc.audio {
				require.True(t, gjson.ValidBytes(wire), "audio response must be valid JSON")
				require.JSONEq(t, gjson.Get(tc.response, "usage").Raw, gjson.GetBytes(wire, "usage").Raw)
				require.Equal(t, gjson.Get(tc.response, "text").String(), gjson.GetBytes(wire, "text").String())
			}
			if wantLogs == 1 && tc.path == "/v1/images/generations" {
				require.True(t, gjson.ValidBytes(wire), "image response must be valid JSON")
				require.JSONEq(t, gjson.Get(tc.response, "data").Raw, gjson.GetBytes(wire, "data").Raw)
			}
		})
	}
	t.Run("realtime_advanced_custom_mapping", func(t *testing.T) {
		gatewayUsagePrices(t, false)
		var hits atomic.Int32
		upstreamModels := make(chan string, 1)
		upstreamObserved := make(chan gatewayRealtimeObservation, 1)
		upgrader := websocket.Upgrader{}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			models := append([]string(nil), r.URL.Query()["model"]...)
			if len(models) > 0 {
				gatewayUsageCaptureModel(upstreamModels, models[0])
			}
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = ws.Close() }()
			_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			messageType, message, err := ws.ReadMessage()
			if err != nil {
				return
			}
			select {
			case upstreamObserved <- gatewayRealtimeObservation{path: r.URL.Path, route: r.URL.Query().Get("route"), models: models, authorization: r.Header.Get("Authorization"), messageType: messageType, message: append([]byte(nil), message...)}:
			default:
			}
			frame := `{"type":"response.done","event_id":"custom-one","response":{"id":"custom-response","status":"completed","usage":{"total_tokens":120,"input_tokens":100,"output_tokens":20,"input_token_details":{"text_tokens":100},"output_token_details":{"text_tokens":20}}}}`
			if ws.WriteMessage(websocket.TextMessage, []byte(frame)) != nil {
				return
			}
			if ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"audit.barrier"}`)) != nil {
				return
			}
			_, _, _ = ws.ReadMessage()
		}))
		t.Cleanup(upstream.Close)
		fixture := gatewayUsageSeed(t, db, 8400, upstream.URL, constant.ChannelTypeAdvancedCustom)
		settings := dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/realtime",
			UpstreamPath: upstream.URL + "/v1/realtime?model=fixed-model&route=custom",
			Converter:    dto.AdvancedCustomConverterNone,
		}}}}
		settingsBytes, err := common.Marshal(settings)
		require.NoError(t, err)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", fixture.ChannelID).Update("settings", string(settingsBytes)).Error)
		headers := http.Header{"Authorization": []string{"Bearer gatewayfixture8400"}}
		ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/realtime?model=gateway-audit", headers)
		if err != nil && resp != nil {
			_ = resp.Body.Close()
		}
		require.NoError(t, err)
		defer func() { _ = ws.Close() }()
		registerRealtime(ws)
		require.NoError(t, ws.SetReadDeadline(time.Now().Add(5*time.Second)))
		select {
		case upstreamModel := <-upstreamModels:
			require.Equal(t, "gpt-4", upstreamModel)
		case <-time.After(time.Second):
			t.Fatal("custom realtime upstream did not receive a model")
		}
		require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update","session":{}}`)))
		select {
		case observed := <-upstreamObserved:
			require.Equal(t, "/v1/realtime", observed.path)
			require.Equal(t, "custom", observed.route)
			require.Equal(t, []string{"gpt-4"}, observed.models)
			require.Equal(t, "Bearer synthetic", observed.authorization)
			require.Equal(t, websocket.TextMessage, observed.messageType)
			require.JSONEq(t, `{"type":"session.update","session":{}}`, string(observed.message))
		case <-time.After(time.Second):
			t.Fatal("custom realtime upstream did not observe the session update")
		}
		sawCompletion := false
		for {
			_, message, readErr := ws.ReadMessage()
			require.NoError(t, readErr)
			eventType := gjson.GetBytes(message, "type").String()
			if eventType == "response.done" {
				require.Equal(t, "custom-one", gjson.GetBytes(message, "event_id").String())
				require.Equal(t, "custom-response", gjson.GetBytes(message, "response.id").String())
				require.EqualValues(t, 100, gjson.GetBytes(message, "response.usage.input_tokens").Int())
				require.EqualValues(t, 20, gjson.GetBytes(message, "response.usage.output_tokens").Int())
				require.EqualValues(t, 120, gjson.GetBytes(message, "response.usage.total_tokens").Int())
				sawCompletion = true
			}
			if eventType == "audit.barrier" {
				require.True(t, sawCompletion, "Realtime completion event must be delivered before the barrier")
				break
			}
		}
		require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"audit.received"}`)))
		requestID := gatewayUsageWaitHandler(t, completed, "Bearer gatewayfixture8400", db.Name() == "mysql")
		require.EqualValues(t, 1, hits.Load())
		gatewayUsageAssertLedger(t, db, fixture, gatewayRealtimeQuota, 1, 100, 20)
		var log model.Log
		require.NoError(t, db.Where("user_id = ? AND type = ?", fixture.UserID, model.LogTypeConsume).First(&log).Error)
		require.Equal(t, fixture.TokenID, log.TokenId)
		require.Equal(t, fixture.ChannelID, log.ChannelId)
		require.Equal(t, requestID, log.RequestId)
		require.True(t, gjson.Get(log.Other, "is_model_mapped").Bool())
		require.Equal(t, "gpt-4", gjson.Get(log.Other, "upstream_model_name").String())
	})

	t.Run("realtime_duplicate_response", func(t *testing.T) {
		gatewayUsagePrices(t, false)
		var hits atomic.Int32
		upstreamObserved := make(chan gatewayRealtimeObservation, 1)
		upgrader := websocket.Upgrader{}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			models := append([]string(nil), r.URL.Query()["model"]...)
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = ws.Close() }()
			_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			messageType, message, err := ws.ReadMessage() // client ready; no timing sleep
			if err != nil {
				return
			}
			select {
			case upstreamObserved <- gatewayRealtimeObservation{path: r.URL.Path, models: models, authorization: r.Header.Get("Authorization"), messageType: messageType, message: append([]byte(nil), message...)}:
			default:
			}
			for _, event := range []struct {
				eventID, responseID       string
				inputTokens, outputTokens int
			}{
				{"one", "same-response", 100, 20},
				{"replayed", "same-response", 100, 20},
				{"two", "second-response", 50, 10},
				{"replayed-again", "same-response", 100, 20},
			} {
				frame := fmt.Sprintf(`{"type":"response.done","event_id":"%s","response":{"id":"%s","status":"completed","usage":{"total_tokens":%d,"input_tokens":%d,"output_tokens":%d,"input_token_details":{"text_tokens":%d},"output_token_details":{"text_tokens":%d}}}}`, event.eventID, event.responseID, event.inputTokens+event.outputTokens, event.inputTokens, event.outputTokens, event.inputTokens, event.outputTokens)
				if ws.WriteMessage(websocket.TextMessage, []byte(frame)) != nil {
					return
				}
			}
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"audit.barrier"}`))
			// Wait for receipt before normal upstream close, allowing the real
			// gateway to collect all completed events and settle the session.
			_, _, _ = ws.ReadMessage()
			_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
		}))
		t.Cleanup(upstream.Close)
		fixture := gatewayUsageSeed(t, db, 8300, upstream.URL, constant.ChannelTypeOpenAI)
		var seededChannel model.Channel
		require.NoError(t, db.First(&seededChannel, fixture.ChannelID).Error)
		require.Equal(t, `{"gateway-audit":"gpt-4"}`, seededChannel.GetModelMapping())
		headers := http.Header{"Authorization": []string{"Bearer gatewayfixture8300"}}
		ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/realtime?model=gateway-audit", headers)
		if err != nil && resp != nil {
			wire, _ := io.ReadAll(resp.Body)
			t.Logf("upgrade status=%d body=%s", resp.StatusCode, wire)
			_ = resp.Body.Close()
		}
		require.NoError(t, err)
		t.Cleanup(func() { _ = ws.Close() })
		registerRealtime(ws)
		require.NoError(t, ws.SetReadDeadline(time.Now().Add(10*time.Second)))
		require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update","session":{}}`)))
		select {
		case observed := <-upstreamObserved:
			require.Equal(t, "/v1/realtime", observed.path)
			require.Equal(t, []string{"gpt-4"}, observed.models, "Realtime upstream must receive mapped model")
			require.Equal(t, "Bearer synthetic", observed.authorization)
			require.Equal(t, websocket.TextMessage, observed.messageType)
			require.JSONEq(t, `{"type":"session.update","session":{}}`, string(observed.message))
		case <-time.After(time.Second):
			t.Fatal("realtime upstream did not observe the session update")
		}
		var doneEvents int
		var identities [][2]string
		for {
			_, wire, err := ws.ReadMessage()
			require.NoError(t, err)
			if gjson.GetBytes(wire, "type").String() == "response.done" {
				doneEvents++
				identities = append(identities, [2]string{gjson.GetBytes(wire, "event_id").String(), gjson.GetBytes(wire, "response.id").String()})
				wantInput, wantOutput := int64(100), int64(20)
				if gjson.GetBytes(wire, "response.id").String() == "second-response" {
					wantInput, wantOutput = 50, 10
				}
				require.EqualValues(t, wantInput, gjson.GetBytes(wire, "response.usage.input_tokens").Int())
				require.EqualValues(t, wantOutput, gjson.GetBytes(wire, "response.usage.output_tokens").Int())
				require.EqualValues(t, wantInput+wantOutput, gjson.GetBytes(wire, "response.usage.total_tokens").Int())
				require.EqualValues(t, wantInput, gjson.GetBytes(wire, "response.usage.input_token_details.text_tokens").Int())
				require.EqualValues(t, wantOutput, gjson.GetBytes(wire, "response.usage.output_token_details.text_tokens").Int())
			}
			if gjson.GetBytes(wire, "type").String() == "audit.barrier" {
				break
			}
		}
		require.Equal(t, 4, doneEvents, "all provider frames are forwarded; billing deduplicates only the repeated response identity")
		require.Equal(t, [][2]string{
			{"one", "same-response"},
			{"replayed", "same-response"},
			{"two", "second-response"},
			{"replayed-again", "same-response"},
		}, identities)
		require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"audit.received"}`)))
		requestID := gatewayUsageWaitHandler(t, completed, "Bearer gatewayfixture8300", db.Name() == "mysql")
		gatewayUsageAssertLedger(t, db, fixture, 210, 1, 150, 30) // Two distinct responses: 150 + 30*2.
		var log model.Log
		require.NoError(t, db.Where("user_id = ? AND type = ?", fixture.UserID, model.LogTypeConsume).First(&log).Error)
		require.Equal(t, fixture.TokenID, log.TokenId)
		require.Equal(t, requestID, log.RequestId)
		require.True(t, gjson.Get(log.Other, "is_model_mapped").Bool())
		require.Equal(t, "gpt-4", gjson.Get(log.Other, "upstream_model_name").String())
		require.EqualValues(t, 1, hits.Load())
	})
}

// gatewayUsageCaptureModel records the first model without delaying forwarding.
func gatewayUsageCaptureModel(models chan<- string, model string) {
	select {
	case models <- model:
	default:
	}
}

// An unexpected second forward must reach the hit-count assertion, not leave
// httptest.Server.Close waiting on an abandoned model-capture send.
func TestGatewayUsageModelCaptureDoesNotBlock(t *testing.T) {
	models := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		gatewayUsageCaptureModel(models, "first")
		gatewayUsageCaptureModel(models, "duplicate")
		close(done)
	}()
	select {
	case <-done:
		select {
		case model := <-models:
			require.Equal(t, "first", model)
		default:
			t.Fatal("model capture did not record a model")
		}
	case <-time.After(time.Second):
		// Release an old blocking implementation even on the red run so the
		// counterexample cannot strand a goroutine during test cleanup.
		<-models
		<-done
		t.Fatal("model capture blocked on an unexpected duplicate")
	}
}

func gatewayUsageWaitHandler(t *testing.T, completed <-chan gatewayCompletion, auth string, external bool) string {
	t.Helper()
	wait := 10 * time.Second
	if external {
		wait = 45 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case actual := <-completed:
			if actual.auth != auth {
				// A failed earlier subtest may leave its handler completion queued;
				// discard it instead of attributing it to this request.
				continue
			}
			require.NotEmpty(t, actual.requestID)
			return actual.requestID
		case <-timer.C:
			t.Fatal("gateway handler has not completed settlement")
			return ""
		}
	}
}

func gatewayUsageAssertRefundEffectComplete(t *testing.T, db *gorm.DB, requestID string) {
	t.Helper()
	finalizeKey := model.BillingRequestFinalizeOperationKey(requestID)
	require.Eventually(t, func() bool {
		var settlement model.BillingSettlement
		if db.Where("operation_key = ?", finalizeKey).First(&settlement).Error != nil {
			return false
		}
		return settlement.Status == model.BillingSettlementStatusApplied &&
			(settlement.EffectPayload == "" || settlement.EffectStatus == model.BillingSettlementEffectApplied)
	}, 10*time.Second, 20*time.Millisecond, "refund settlement effect must be complete before asserting zero consume logs")
}

const gatewayChatJSON = `{"id":"chatcmpl-gateway","model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":60}}}`
const gatewayChatStream = "data: {\"id\":\"chatcmpl-gateway\",\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chatcmpl-gateway\",\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":60}}}\n\ndata: [DONE]\n\n"
const gatewayZeroStream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"visible\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0,\"total_tokens\":0}}\n\ndata: [DONE]\n\n"

func gatewayUsagePrices(t *testing.T, image bool) {
	t.Helper()
	for _, pair := range []struct {
		get   func() string
		set   func(string) error
		value string
	}{
		{ratio_setting.ModelRatio2JSONString, ratio_setting.UpdateModelRatioByJSONString, `{"gateway-audit":1}`},
		{ratio_setting.CompletionRatio2JSONString, ratio_setting.UpdateCompletionRatioByJSONString, `{"gateway-audit":2}`},
		{ratio_setting.CacheRatio2JSONString, ratio_setting.UpdateCacheRatioByJSONString, `{"gateway-audit":0.1}`},
		{ratio_setting.AudioRatio2JSONString, ratio_setting.UpdateAudioRatioByJSONString, `{"gateway-audit":4}`},
		{ratio_setting.AudioCompletionRatio2JSONString, ratio_setting.UpdateAudioCompletionRatioByJSONString, `{"gateway-audit":3}`},
		{ratio_setting.ModelPrice2JSONString, ratio_setting.UpdateModelPriceByJSONString, `{}`},
		{ratio_setting.GroupRatio2JSONString, ratio_setting.UpdateGroupRatioByJSONString, `{"default":1}`},
	} {
		old, set := pair.get(), pair.set
		t.Cleanup(func() { require.NoError(t, set(old)) })
		require.NoError(t, set(pair.value))
	}
	if image {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gateway-audit":0.001}`))
	}
}

type gatewayUsageFixture struct {
	UserID    int
	TokenID   int
	ChannelID int
}

type gatewayCompletion struct {
	auth      string
	requestID string
}

type gatewayUpstreamObservation struct {
	path          string
	authorization string
}

type gatewayRealtimeObservation struct {
	path          string
	route         string
	models        []string
	authorization string
	messageType   int
	message       []byte
}

func gatewayUsageSeed(t *testing.T, db *gorm.DB, id int, base string, channelType int) gatewayUsageFixture {
	t.Helper()
	fixture := gatewayUsageFixture{UserID: id, TokenID: id + 100000, ChannelID: id + 200000}
	require.NoError(t, db.Create(&model.User{Id: fixture.UserID, Username: fmt.Sprintf("gateway%d", id), Quota: 10000, Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: fmt.Sprintf("gw%d", id)}).Error)
	require.NoError(t, db.Create(&model.Token{Id: fixture.TokenID, UserId: fixture.UserID, Key: fmt.Sprintf("gatewayfixture%d", id), Status: common.TokenStatusEnabled, RemainQuota: 10000, ExpiredTime: -1}).Error)
	mapping := `{"gateway-audit":"gpt-4"}`
	require.NoError(t, db.Create(&model.Channel{Id: fixture.ChannelID, Type: channelType, Key: "synthetic", Name: fmt.Sprintf("gateway%d", id), Status: common.ChannelStatusEnabled, Models: "gateway-audit", Group: "default", BaseURL: &base, ModelMapping: &mapping, AutoBan: common.GetPointer(0)}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gateway-audit", ChannelId: fixture.ChannelID, Enabled: true}).Error)
	t.Cleanup(func() {
		// Retain audit rows but remove each finished fixture from routing before
		// the next case, whose upstream server and channel type are different.
		require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", fixture.ChannelID).Update("enabled", false).Error)
	})
	return fixture
}

func gatewayUsageAssertLedger(t *testing.T, db *gorm.DB, fixture gatewayUsageFixture, quota, logCount, prompt, completion int) {
	t.Helper()
	var user model.User
	var token model.Token
	var channel model.Channel
	var logs []model.Log
	wait, poll := 10*time.Second, 20*time.Millisecond
	if db.Name() == "mysql" {
		wait, poll = 45*time.Second, 200*time.Millisecond
	}
	defer func() {
		if t.Failed() {
			t.Logf("last projection user=%d token=%d channel=%d wallet=%d used=%d tokenRemain=%d channelUsed=%d requests=%d logs=%d", fixture.UserID, fixture.TokenID, fixture.ChannelID, user.Quota, user.UsedQuota, token.RemainQuota, channel.UsedQuota, user.RequestCount, len(logs))
		}
	}()
	require.Eventually(t, func() bool {
		if db.First(&user, fixture.UserID).Error != nil || db.First(&token, fixture.TokenID).Error != nil || db.First(&channel, fixture.ChannelID).Error != nil || db.Where("user_id = ? AND type = ?", fixture.UserID, model.LogTypeConsume).Find(&logs).Error != nil {
			return false
		}
		return user.Quota == int64(10000-quota) && token.RemainQuota == int64(10000-quota) && token.UsedQuota == int64(quota) && user.UsedQuota == int64(quota) && channel.UsedQuota == int64(quota) && len(logs) == logCount
	}, wait, poll, "wallet/token/channel/logs must match independent quota %d", quota)
	wantRequests := logCount
	if quota == 0 {
		// Existing text settlement records a zero consume log but does not
		// increment the billable request counter. This is not a paid request.
		wantRequests = 0
	}
	require.Equal(t, wantRequests, user.RequestCount)
	if logCount == 1 {
		require.Equal(t, quota, logs[0].Quota)
		require.Equal(t, prompt, logs[0].PromptTokens)
		require.Equal(t, completion, logs[0].CompletionTokens)
		require.Equal(t, "gateway-audit", logs[0].ModelName)
		require.Equal(t, fixture.ChannelID, logs[0].ChannelId)
	}
	t.Logf("independent ledger user=%d token=%d channel=%d quota=%d wallet=%d tokenRemain=%d channelUsed=%d requests=%d logs=%d", fixture.UserID, fixture.TokenID, fixture.ChannelID, quota, user.Quota, token.RemainQuota, channel.UsedQuota, user.RequestCount, len(logs))
}

func gatewayUsageStreamAnswerDelivered(wire []byte, path string, marker []byte) bool {
	events := gatewayUsageCompleteSSEPrefixData(wire)
	var text strings.Builder
	for _, data := range events {
		if data == "[DONE]" {
			continue
		}
		candidate := gjson.Parse(data)
		if path == "/v1/messages" {
			text.WriteString(candidate.Get("content_block.text").String())
			text.WriteString(candidate.Get("delta.text").String())
		} else {
			text.WriteString(candidate.Get("choices.0.delta.content").String())
		}
	}
	return strings.Contains(text.String(), string(marker))
}

type gatewaySSEEvent struct {
	event string
	data  string
}

func gatewayUsageCompleteSSEEvents(wire []byte) []gatewaySSEEvent {
	if !strings.HasSuffix(strings.ReplaceAll(string(wire), "\r\n", "\n"), "\n\n") {
		return nil
	}
	return gatewayUsageParseSSEEvents(wire)
}

func gatewayUsageCompleteSSEPrefixData(wire []byte) []string {
	events := gatewayUsageParseSSEEvents(wire)
	data := make([]string, 0, len(events))
	for _, event := range events {
		data = append(data, event.data)
	}
	return data
}

func gatewayUsageParseSSEEvents(wire []byte) []gatewaySSEEvent {
	normalized := strings.ReplaceAll(string(wire), "\r\n", "\n")
	blocks := strings.Split(normalized, "\n\n")
	limit := len(blocks)
	if !strings.HasSuffix(normalized, "\n\n") {
		limit--
	}
	if limit <= 0 {
		return nil
	}
	events := make([]gatewaySSEEvent, 0, len(blocks))
	for _, block := range blocks[:limit] {
		var event gatewaySSEEvent
		var dataLines []string
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				event.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			}
		}
		if len(dataLines) > 0 {
			event.data = strings.Join(dataLines, "\n")
			events = append(events, event)
		}
	}
	return events
}

func gatewayUsageCompleteSSEData(wire []byte) []string {
	events := gatewayUsageCompleteSSEEvents(wire)
	data := make([]string, 0, len(events))
	for _, event := range events {
		data = append(data, event.data)
	}
	return data
}

func TestGatewayUsageSSEParserJoinsDataLines(t *testing.T) {
	events := gatewayUsageCompleteSSEEvents([]byte("data: {broken\ndata: }\n\n"))
	require.Len(t, events, 1)
	require.Equal(t, "{broken\n}", events[0].data)
	require.False(t, gjson.Valid(events[0].data))
}

// gatewayUsageAssertWire checks the answer independently of usage, including
// payloads emitted before a final zero-usage event.
func gatewayUsageAssertWire(t *testing.T, wire []byte, path string, stream bool, prompt, completion int, answer string) {
	t.Helper()
	var usage gjson.Result
	var text strings.Builder
	streamContentAt, streamUsageAt, streamStopAt := -1, -1, -1
	streamMessageStartAt, streamBlockStartAt, streamBlockStopAt := -1, -1, -1
	streamBlockStartIndex, streamBlockStopIndex := int64(-1), int64(-1)
	if stream {
		events := gatewayUsageCompleteSSEEvents(wire)
		for eventIndex, event := range events {
			if event.data == "[DONE]" {
				if path != "/v1/messages" {
					require.Equal(t, len(events)-1, eventIndex, "[DONE] must be the final event")
				}
				continue
			}
			require.True(t, gjson.Valid(event.data), "client SSE data must be valid JSON")
			candidate := gjson.Parse(event.data)
			if path == "/v1/messages" && event.event != "" {
				require.Equal(t, candidate.Get("type").String(), event.event, "Claude SSE event name must match payload type")
			}
			if path == "/v1/messages" {
				switch candidate.Get("type").String() {
				case "message_start":
					streamMessageStartAt = eventIndex
				case "content_block_start":
					streamBlockStartAt = eventIndex
					streamBlockStartIndex = candidate.Get("index").Int()
				case "content_block_stop":
					streamBlockStopAt = eventIndex
					streamBlockStopIndex = candidate.Get("index").Int()
				}
				text.WriteString(candidate.Get("content_block.text").String())
				text.WriteString(candidate.Get("delta.text").String())
				if candidate.Get("content_block.text").String() != "" || candidate.Get("delta.text").String() != "" {
					streamContentAt = eventIndex
				}
			} else {
				text.WriteString(candidate.Get("choices.0.delta.content").String())
			}
			if candidate.Get("usage").Exists() {
				usage = candidate.Get("usage")
				if path == "/v1/messages" {
					streamUsageAt = eventIndex
				}
			}
			if path == "/v1/messages" && candidate.Get("type").String() == "message_stop" {
				streamStopAt = eventIndex
			}
		}
	} else {
		require.True(t, gjson.ValidBytes(wire), "client response must be valid JSON")
		usage = gjson.GetBytes(wire, "usage")
		switch path {
		case "/v1/messages":
			for _, block := range gjson.GetBytes(wire, "content").Array() {
				text.WriteString(block.Get("text").String())
			}
		case "/v1/responses":
			text.WriteString(gjson.GetBytes(wire, "output.0.content.0.text").String())
		default:
			text.WriteString(gjson.GetBytes(wire, "choices.0.message.content").String())
		}
	}
	require.Equal(t, answer, text.String(), "client answer must survive independently of final usage")
	if stream && path != "/v1/messages" {
		events := gatewayUsageCompleteSSEData(wire)
		require.NotEmpty(t, events)
		require.Equal(t, "[DONE]", events[len(events)-1])
	}
	require.True(t, usage.Exists(), "client must receive final usage")
	switch path {
	case "/v1/messages":
		// This fixture has 60 cached tokens, included by OpenAI and excluded
		// from Claude input_tokens. Its billed aggregate remains 100.
		require.EqualValues(t, prompt-60, usage.Get("input_tokens").Int())
		require.EqualValues(t, 60, usage.Get("cache_read_input_tokens").Int())
		require.EqualValues(t, completion, usage.Get("output_tokens").Int())
		require.Equal(t, gjson.Number, usage.Get("input_tokens").Type)
		require.Equal(t, gjson.Number, usage.Get("output_tokens").Type)
		if stream {
			require.Equal(t, 1, strings.Count(string(wire), "event: message_stop"))
			require.GreaterOrEqual(t, streamContentAt, 0)
			require.GreaterOrEqual(t, streamUsageAt, 0)
			require.GreaterOrEqual(t, streamStopAt, 0)
			require.GreaterOrEqual(t, streamMessageStartAt, 0)
			require.GreaterOrEqual(t, streamBlockStartAt, 0)
			require.GreaterOrEqual(t, streamBlockStopAt, 0)
			require.Less(t, streamMessageStartAt, streamBlockStartAt)
			require.Less(t, streamBlockStartAt, streamContentAt)
			require.Less(t, streamContentAt, streamBlockStopAt)
			require.Less(t, streamContentAt, streamStopAt)
			require.Less(t, streamUsageAt, streamStopAt)
			require.Equal(t, streamBlockStartIndex, streamBlockStopIndex)
			require.Equal(t, len(gatewayUsageCompleteSSEEvents(wire))-1, streamStopAt)
		}
	case "/v1/responses":
		require.Equal(t, gjson.Number, usage.Get("input_tokens").Type)
		require.Equal(t, gjson.Number, usage.Get("output_tokens").Type)
		require.EqualValues(t, prompt, usage.Get("input_tokens").Int())
		require.EqualValues(t, completion, usage.Get("output_tokens").Int())
		require.EqualValues(t, prompt+completion, usage.Get("total_tokens").Int())
	default:
		require.Equal(t, gjson.Number, usage.Get("prompt_tokens").Type)
		require.Equal(t, gjson.Number, usage.Get("completion_tokens").Type)
		require.Equal(t, gjson.Number, usage.Get("total_tokens").Type)
		require.EqualValues(t, prompt, usage.Get("prompt_tokens").Int())
		require.EqualValues(t, completion, usage.Get("completion_tokens").Int())
		require.EqualValues(t, prompt+completion, usage.Get("total_tokens").Int())
	}
}

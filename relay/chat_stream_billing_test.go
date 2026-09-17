package relay

import (
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
	perfmetrics "github.com/MAX-API-Next/MAX-API/pkg/perf_metrics"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/perf_metrics_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type chatStreamBillingRecorder struct {
	quotas  []int
	effects []*model.BillingSettlementEffect
	refunds int
}

var chatStreamMetricSequence atomic.Uint64

func (b *chatStreamBillingRecorder) Settle(int) error { panic("expected settlement with effect") }
func (b *chatStreamBillingRecorder) SettleWithEffect(quota int, effect *model.BillingSettlementEffect) error {
	b.quotas = append(b.quotas, quota)
	b.effects = append(b.effects, effect)
	return nil
}
func (b *chatStreamBillingRecorder) Refund(*gin.Context) {
	if b.NeedsRefund() {
		b.refunds++
	}
}
func (b *chatStreamBillingRecorder) NeedsRefund() bool        { return len(b.quotas) == 0 && b.refunds == 0 }
func (b *chatStreamBillingRecorder) GetPreConsumedQuota() int { return 100 }
func (b *chatStreamBillingRecorder) Reserve(int) error        { return nil }

func TestNativeChatRelayBillingAcrossAttempts(t *testing.T) {
	for _, failure := range []string{"overload", "empty", "partial", "all_failed"} {
		t.Run(failure, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			service.InitHttpClient()
			oldFlag, oldTimes := common.EmptyCompletionRetryEnabled, common.RetryTimes
			common.EmptyCompletionRetryEnabled, common.RetryTimes = true, 2
			t.Cleanup(func() { common.EmptyCompletionRetryEnabled, common.RetryTimes = oldFlag, oldTimes })
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				frames := []string{`{"choices":[{"delta":{"role":"assistant"}}]}`}
				if (attempt > 1 && failure != "all_failed") || failure == "partial" {
					frames = append(frames, `{"choices":[{"delta":{"content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
				}
				if (attempt == 1 && failure != "empty") || failure == "all_failed" {
					frames = append(frames, `{"error":{"message":"overloaded","type":"upstream_error"}}`)
				} else if attempt == 1 {
					frames = append(frames, `{"choices":[],"usage":{"prompt_tokens":38,"completion_tokens":0,"total_tokens":38}}`)
				}
				_, _ = w.Write([]byte("data: " + strings.Join(append(frames, "[DONE]"), "\n\ndata: ") + "\n\n"))
			}))
			t.Cleanup(upstream.Close)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
			c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
			c.Set(string(constant.ContextKeyChannelKey), "synthetic-test-key")
			c.Set(string(constant.ContextKeyOriginalModel), "gpt-test")
			billing := &chatStreamBillingRecorder{}
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI,
				OriginModelName: "gpt-test", StartTime: time.Now(), IsStream: true, DisablePing: true,
				RequestId: "chat-test-" + failure, Billing: billing,
				UserQuota: 1000, UserSetting: dto.UserSetting{QuotaWarningThreshold: 1},
				PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
				Request:   &dto.GeneralOpenAIRequest{Model: "gpt-test", Stream: common.GetPointer(true)},
			}
			apiErr := TextHelper(c, info)
			require.NotNil(t, apiErr)
			if failure == "partial" {
				require.True(t, types.IsSkipRetryError(apiErr))
				require.Equal(t, []int{12}, billing.quotas)
				service.HandleFailedBilling(c, info, apiErr)
				require.Zero(t, billing.refunds)
			} else {
				require.Empty(t, billing.quotas, "failed attempt must not settle or project usage")
				require.False(t, c.Writer.Written())
				info.RetryIndex++
				apiErr = TextHelper(c, info)
				if failure == "all_failed" {
					require.NotNil(t, apiErr)
					service.HandleFailedBilling(c, info, apiErr)
					service.HandleFailedBilling(c, info, apiErr)
					require.Equal(t, 1, billing.refunds)
					require.Empty(t, billing.quotas)
				} else {
					require.Nil(t, apiErr)
					require.Equal(t, []int{12}, billing.quotas, "only winning attempt usage may settle")
					require.Len(t, billing.effects, 1)
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "hello"))
					require.NotContains(t, recorder.Body.String(), "overloaded")
				}
			}
		})
	}
}

func TestNativeChatPartialBillingDoesNotRecordSuccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	setting := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	previousSetting := *setting
	model.DB, common.RedisEnabled, setting.Enabled = db, false, true
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		*setting = previousSetting
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	billing := &chatStreamBillingRecorder{}
	info := &relaycommon.RelayInfo{
		OriginModelName: fmt.Sprintf("partial-metric-%d", chatStreamMetricSequence.Add(1)),
		StartTime:       time.Now(), IsStream: true, Billing: billing,
		UserQuota: 1000, UserSetting: dto.UserSetting{QuotaWarningThreshold: 1},
		ChannelMeta: &relaycommon.ChannelMeta{},
		PriceData:   types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	service.PostPartialConsumeQuota(c, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12})
	require.Equal(t, []int{12}, billing.quotas)
	// The controller records the terminal failure once. The settlement entry
	// point must not enqueue an additional successful sample for this request.
	perfmetrics.RecordRelaySample(info, false, 0)
	// Observe the full window on the test goroutine. Never's worker may outlive
	// its timeout and query the restored/closed DB during cleanup.
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		result, queryErr := perfmetrics.QuerySummaryAll(1, nil)
		require.NoError(t, queryErr)
		found := false
		for _, summary := range result.Models {
			if summary.ModelName == info.OriginModelName {
				found = true
				require.EqualValues(t, 1, summary.RequestCount)
				require.Zero(t, summary.SuccessRate)
				break
			}
		}
		require.True(t, found, "missing failure metric")
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

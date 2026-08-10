package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingAlphaSearchResponseWriter struct {
	gin.ResponseWriter
	err error
}

func (w *failingAlphaSearchResponseWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func (w *failingAlphaSearchResponseWriter) WriteString(_ string) (int, error) {
	return 0, w.err
}

type recordingAlphaSearchBilling struct {
	settledQuotas []int
}

func (b *recordingAlphaSearchBilling) Settle(actualQuota int) error {
	b.settledQuotas = append(b.settledQuotas, actualQuota)
	return nil
}

func (b *recordingAlphaSearchBilling) Refund(_ *gin.Context) {}

func (b *recordingAlphaSearchBilling) NeedsRefund() bool {
	return len(b.settledQuotas) == 0
}

func (b *recordingAlphaSearchBilling) GetPreConsumedQuota() int {
	return 0
}

func (b *recordingAlphaSearchBilling) Reserve(_ int) error {
	return nil
}

func TestBuildAlphaSearchRequestBodyPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"id":"req_1",
		"model":"gpt-5.1",
		"input":[{"role":"user","content":"hi"}],
		"commands":{"search_query":[{"q":"weather","recency":1}]},
		"settings":{"locale":"en"},
		"future_field":{"nested":true}
	}`)

	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1-mapped")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(out, &body))
	require.Equal(t, "gpt-5.1-mapped", body["model"])
	require.Equal(t, "req_1", body["id"])
	require.Contains(t, body, "commands")
	require.Contains(t, body, "settings")
	require.Contains(t, body, "future_field")
	require.Contains(t, body, "input")

	commands, ok := body["commands"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, commands, "search_query")

	future, ok := body["future_field"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, future["nested"])
}

func TestBuildAlphaSearchRequestBodyNoMappingKeepsRawBytes(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.1","commands":{"search_query":[{"q":"x"}]},"future_field":1}`)

	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1")

	require.NoError(t, err)
	require.Equal(t, raw, out)
}

func TestAlphaSearchSettlesSuccessfulUpstreamBeforeClientWriteFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("upstream path = %q, want /search", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"computer_initialize_state"}`))
	}))
	t.Cleanup(upstream.Close)

	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = previousLogConsumeEnabled
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", nil)
	writeErr := errors.New("client response write failed")
	ctx.Writer = &failingAlphaSearchResponseWriter{
		ResponseWriter: ctx.Writer,
		err:            writeErr,
	}
	ctx.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeAdvancedCustom)
	ctx.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
	ctx.Set(string(constant.ContextKeyChannelKey), "test-key")
	ctx.Set(string(constant.ContextKeyOriginalModel), "gpt-5.1")
	ctx.Set(string(constant.ContextKeyChannelOtherSetting), dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/alpha/search",
				UpstreamPath: "/search",
				Converter:    dto.AdvancedCustomConverterNone,
			},
		}},
	})

	billing := &recordingAlphaSearchBilling{}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "gpt-5.1",
		StartTime:       time.Now(),
		Billing:         billing,
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0},
		},
		Request: &dto.AlphaSearchRequest{
			Model:   "gpt-5.1",
			RawBody: []byte(`{"model":"gpt-5.1","input":"latest news"}`),
		},
	}

	apiErr := AlphaSearchHelper(ctx, info)

	require.ErrorIs(t, apiErr, writeErr)
	require.Equal(t, []int{0}, billing.settledQuotas)
	require.NotNil(t, info.ResponsesUsageInfo)
	tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]
	require.NotNil(t, tool)
	require.Equal(t, 1, tool.CallCount)
}

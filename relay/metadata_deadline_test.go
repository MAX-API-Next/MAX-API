package relay

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstResultDeadlineBeforeMetadataDoesNotPromiseDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	funding := streamFundingCase{name: "wallet_only", preference: "wallet_only", source: "wallet", wallet: 1000}
	db := setupStreamFundingLedger(t, funding)
	oldMonitor := *operation_setting.GetMonitorSetting()
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{StreamingFirstResultTimeoutSeconds: 1}
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = oldMonitor })
	var calls atomic.Int32
	var upstreamGuardExpired atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Do not publish any frame: this isolates the pre-metadata deadline
		// from EOF and from the existing timeout-after-metadata matrix.
		guard := time.NewTimer(4 * time.Second)
		defer guard.Stop()
		select {
		case <-r.Context().Done():
		case <-guard.C:
			upstreamGuardExpired.Store(true)
		}
	}))
	t.Cleanup(upstream.Close)
	c, info, recorder := streamFundingContext(t, "claude_to_responses", "metadata-before-delivery", upstream.URL, constant.ChannelTypeAnthropic, funding)
	require.Nil(t, service.PreConsumeBilling(c, 100, info))
	apiErr := ResponsesHelper(c, info)
	require.False(t, upstreamGuardExpired.Load(), "the one-second first-result deadline must cancel upstream before the independent four-second guard")
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
	require.False(t, info.HasRecordedChannelFirstResult())
	service.HandleFailedBilling(c, info, apiErr)
	service.HandleFailedBilling(c, info, apiErr)
	require.EqualValues(t, 1, calls.Load())
	assertStreamFundingBalances(t, db, funding, "metadata-before-delivery", 0, false, 0, 0)
}

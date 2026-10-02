package helper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamScannerHandlerEnforcesFirstResultTimeout(t *testing.T) {
	originalSetting := *operation_setting.GetMonitorSetting()
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		StreamingFirstResultTimeoutSeconds: 1,
	}
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = originalSetting })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	maxcommon.SetContextKey(c, constant.ContextKeyChannelId, 936)
	maxcommon.SetContextKey(c, constant.ContextKeyChannelAutoBan, true)
	info := &relaycommon.RelayInfo{IsStream: true}
	info.InitChannelMeta(c)

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	started := time.Now()
	StreamScannerHandler(c, &http.Response{Body: reader}, info, func(_ string, _ *StreamResult) {})

	require.GreaterOrEqual(t, time.Since(started), time.Second)
	require.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.False(t, info.HasRecordedChannelFirstResponse())
}

func TestStreamScannerHandlerDoesNotTreatUsageOnlyFrameAsFirstResult(t *testing.T) {
	originalSetting := *operation_setting.GetMonitorSetting()
	*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{
		StreamingFirstResultTimeoutSeconds: 1,
	}
	t.Cleanup(func() { *operation_setting.GetMonitorSetting() = originalSetting })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{IsStream: true}
	info.InitChannelMeta(c)
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	go func() {
		_, _ = io.WriteString(writer, "data: {\"usage\":{\"total_tokens\":1}}\n\n")
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_ = writer.Close()
	}()

	StreamScannerHandler(c, &http.Response{Body: reader}, info, func(data string, sr *StreamResult) {
		if strings.Contains(data, `"content"`) {
			_, _ = c.Writer.Write([]byte("payload"))
		}
	})

	require.True(t, info.HasRecordedChannelFirstResponse())
	require.NotEqual(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
}

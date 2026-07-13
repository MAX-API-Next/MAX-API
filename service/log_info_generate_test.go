package service

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoMarksRetryLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Set("use_channel", []string{"1", "2"})

	now := time.Now()
	info := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, 0, 1)

	require.Equal(t, true, other["retry_log"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, []string{"1", "2"}, adminInfo["use_channel"])
}

func TestGenerateTextOtherInfoDoesNotMarkSingleChannelAsRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Set("use_channel", []string{"1"})

	now := time.Now()
	info := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, 0, 1)

	require.NotContains(t, other, "retry_log")
}

func TestAttachQuotaSaturationToOtherPreservesMalformedAdminInfo(t *testing.T) {
	clamp := &common.QuotaClamp{
		Op:       "test",
		Kind:     common.QuotaClampOverflow,
		Original: float64(common.MaxQuota) + 1,
		Clamped:  common.MaxQuota,
	}
	other := map[string]interface{}{
		"admin_info": "malformed",
	}

	attachQuotaSaturationToOther(other, clamp)

	require.Equal(t, "malformed", other["admin_info"])
	require.NotContains(t, other, "quota_saturation")
}

func TestAttachQuotaSaturationToOtherWritesValidAdminInfo(t *testing.T) {
	clamp := &common.QuotaClamp{
		Op:       "test",
		Kind:     common.QuotaClampOverflow,
		Original: float64(common.MaxQuota) + 1,
		Clamped:  common.MaxQuota,
	}

	other := map[string]interface{}{}
	attachQuotaSaturationToOther(other, clamp)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, clamp.AuditMap(), adminInfo["quota_saturation"])

	adminInfo = map[string]interface{}{
		"existing": "value",
	}
	other = map[string]interface{}{
		"admin_info": adminInfo,
	}
	attachQuotaSaturationToOther(other, clamp)
	require.Equal(t, "value", adminInfo["existing"])
	require.Equal(t, clamp.AuditMap(), adminInfo["quota_saturation"])
}

func TestAttachQuotaSaturationSkipsNilOtherWithoutWarning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	relayInfo := &relaycommon.RelayInfo{
		QuotaClamp: &common.QuotaClamp{
			Op:       "test",
			Kind:     common.QuotaClampOverflow,
			Original: float64(common.MaxQuota) + 1,
			Clamped:  common.MaxQuota,
		},
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	attachQuotaSaturation(ctx, relayInfo, nil)

	require.NotContains(t, logBuffer.String(), "quota saturation on consume log")
}

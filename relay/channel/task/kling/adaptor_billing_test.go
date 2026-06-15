package kling

import (
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

func TestEstimateTaskBillingUsesVideoListAsVideoInput(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "kling/kling-v3-omni-video-generation",
		Prompt: "edit video",
		Metadata: map[string]interface{}{
			"model_name": "kling/kling-v3-omni-video-generation",
			"mode":       "pro",
			"duration":   "5",
			"sound":      "off",
			"video_list": []interface{}{
				map[string]interface{}{"video_url": "https://example.com/video.mp4"},
			},
		},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "kling/kling-v3-omni-video-generation",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "kling/kling-v3-omni-video-generation",
		},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}

	got, err := (&TaskAdaptor{}).EstimateTaskBilling(c, info)
	if err != nil {
		t.Fatalf("EstimateTaskBilling returned error: %v", err)
	}
	if got == nil {
		t.Fatal("EstimateTaskBilling returned nil")
	}
	if got.RowID != "pro_video_no_audio" || got.UnitPrice != 1.2 || got.Quota != 6000 {
		t.Fatalf("unexpected billing result: %+v", got)
	}
}

package ali

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappedAliImageModelsUseUpstreamProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name          string
		mode          int
		upstreamModel string
		wantPath      string
		wantAsync     bool
	}{
		{name: "sync qwen image", mode: constant.RelayModeImagesGenerations, upstreamModel: "qwen-image", wantPath: "/api/v1/services/aigc/multimodal-generation/generation"},
		{name: "wan edit", mode: constant.RelayModeImagesEdits, upstreamModel: "wan2.6-image", wantPath: "/api/v1/services/aigc/image-generation/generation", wantAsync: true},
		{name: "async image", mode: constant.RelayModeImagesGenerations, upstreamModel: "wanx-v1", wantPath: "/api/v1/services/aigc/text2image/image-synthesis", wantAsync: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images", nil)
			info := &relaycommon.RelayInfo{
				RelayMode:       tc.mode,
				OriginModelName: "customer-alias",
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://dashscope.example", UpstreamModelName: tc.upstreamModel},
			}
			adaptor := &Adaptor{}
			url, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://dashscope.example"+tc.wantPath, url)
			header := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
			assert.Equal(t, tc.wantAsync, header.Get("X-DashScope-Async") == "enable")
			if tc.mode == constant.RelayModeImagesGenerations {
				_, err = adaptor.ConvertImageRequest(c, info, dto.ImageRequest{Model: tc.upstreamModel, Prompt: "poster"})
				require.NoError(t, err)
				assert.Equal(t, !tc.wantAsync, adaptor.IsSyncImageModel)
			}
		})
	}
}

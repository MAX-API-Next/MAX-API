package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeBetaHeaderOnlyForPreviewModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		model     string
		wantBeta  bool
		websocket bool
	}{
		{name: "ga http", model: "gpt-realtime", wantBeta: false},
		{name: "preview http", model: "gpt-4o-realtime-preview", wantBeta: true},
		{name: "ga websocket", model: "gpt-realtime", wantBeta: false, websocket: true},
		{name: "preview websocket", model: "gpt-4o-realtime-preview", wantBeta: true, websocket: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/realtime", nil)
			if tt.websocket {
				c.Request.Header.Set("Sec-WebSocket-Protocol", "realtime")
			}
			header := http.Header{}
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeRealtime,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					ApiKey:            "test-key",
					UpstreamModelName: tt.model,
				},
			}
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
			if tt.websocket {
				hasBeta := strings.Contains(header.Get("Sec-WebSocket-Protocol"), "openai-beta.realtime-v1")
				assert.Equal(t, tt.wantBeta, hasBeta)
			} else {
				assert.Equal(t, tt.wantBeta, header.Get("openai-beta") != "")
			}
		})
	}
}

func TestRealtimeRequestURLAlwaysUsesMappedModel(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "supplied client model", path: "/v1/realtime?model=client-alias&foo=bar", want: "ws://upstream.example/v1/realtime?foo=bar&model=gpt-4"},
		{name: "model omitted", path: "/v1/realtime", want: "ws://upstream.example/v1/realtime?model=gpt-4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayMode:      relayconstant.RelayModeRealtime,
				RequestURLPath: tt.path,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					ChannelBaseUrl:    "http://upstream.example",
					UpstreamModelName: "gpt-4",
				},
			}
			got, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

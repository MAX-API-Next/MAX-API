package ali

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAliImageResponseFormatUsesOriginalRequest(t *testing.T) {
	var downloads atomic.Int32
	imageBytes := []byte("synthetic image fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer server.Close()
	fetch := system_setting.GetFetchSetting()
	previous := *fetch
	t.Cleanup(func() { *fetch = previous })
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	fetch.EnableSSRFProtection = true
	fetch.AllowPrivateIp = true
	fetch.AllowedPorts = []string{port}
	fetch.DomainList = nil
	fetch.IpList = nil
	fetch.DomainFilterMode = false
	fetch.IpFilterMode = false
	for _, shape := range []string{"results", "choices"} {
		for _, format := range []string{"", "url", "b64_json"} {
			t.Run(shape+format, func(t *testing.T) {
				before := downloads.Load()
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
				c.Set("response_format", "url") // stale context must not override the original DTO
				output := map[string]any{"results": []any{map[string]any{"url": server.URL + "/image.png"}}}
				if shape == "choices" {
					output = map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": []any{map[string]any{"image": server.URL + "/image.png"}}}}}}
				}
				body, err := common.Marshal(map[string]any{"output": output, "usage": map[string]any{"image_count": 1}})
				require.NoError(t, err)
				resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
				info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{ResponseFormat: format}, StartTime: time.Unix(1700000000, 0)}
				apiErr, usage := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, resp, info)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				var result dto.ImageResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
				require.Len(t, result.Data, 1)
				require.Equal(t, 1.0, info.PriceData.OtherRatios["n"])
				if format == "b64_json" {
					require.Equal(t, base64.StdEncoding.EncodeToString(imageBytes), result.Data[0].B64Json)
					require.Equal(t, before+1, downloads.Load())
				} else {
					require.Empty(t, result.Data[0].B64Json)
					require.Equal(t, before, downloads.Load())
				}
			})
		}
	}
}

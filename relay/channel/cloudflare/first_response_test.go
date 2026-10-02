package cloudflare

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCloudflareResponseContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, recorder
}

func newCloudflareResponseInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "cloudflare-test",
		},
	}
}

func TestCloudflareStreamHandlerRecordsAfterValidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder := newCloudflareResponseContext()
	info := newCloudflareResponseInfo()
	body := "data: not-json\n" + `data: {"choices":[]}` + "\n" + `data: {"choices":[{"delta":{"content":"hello"}}]}` + "\n" + "data: [DONE]\n"

	apiErr, usage := cfStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.True(t, info.HasRecordedChannelFirstResponse())
	require.Contains(t, recorder.Body.String(), `"content":"hello"`)
}

func TestCloudflareNonStreamingHandlersRecordAfterSuccessfulWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("chat", func(t *testing.T) {
		c, _ := newCloudflareResponseContext()
		info := newCloudflareResponseInfo()
		body := `{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`

		apiErr, usage := cfHandler(c, info, &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		})

		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		require.True(t, info.HasRecordedChannelFirstResponse())
	})

	t.Run("chat-empty-choices", func(t *testing.T) {
		c, _ := newCloudflareResponseContext()
		info := newCloudflareResponseInfo()
		body := `{"choices":[]}`

		apiErr, usage := cfHandler(c, info, &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		})

		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		require.False(t, info.HasRecordedChannelFirstResponse())
	})

	t.Run("stt", func(t *testing.T) {
		c, _ := newCloudflareResponseContext()
		info := newCloudflareResponseInfo()
		body := `{"result":{"text":"hello"}}`

		apiErr, usage := cfSTTHandler(c, info, &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		})

		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		require.True(t, info.HasRecordedChannelFirstResponse())
	})
}

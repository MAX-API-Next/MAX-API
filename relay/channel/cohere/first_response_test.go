package cohere

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

func newCohereResponseContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(&closeNotifyRecorder{
		ResponseRecorder: recorder,
		closeNotify:      make(chan bool),
	})
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, recorder
}

func newCohereResponseInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "cohere-test",
		},
	}
}

func TestCohereStreamHandlerRecordsFirstResponseAfterValidConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder := newCohereResponseContext()
	info := newCohereResponseInfo()
	body := "not-json\n" + `{"text":"hello","is_finished":false}` + "\n"

	usage, apiErr := cohereStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.True(t, info.HasRecordedChannelFirstResponse())
	require.Contains(t, recorder.Body.String(), `"content":"hello"`)
}

func TestCohereNonStreamingHandlersRecordAfterSuccessfulWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("chat", func(t *testing.T) {
		c, _ := newCohereResponseContext()
		info := newCohereResponseInfo()
		body := `{"response_id":"resp-1","text":"hello","finish_reason":"COMPLETE","meta":{"billed_units":{"input_tokens":1,"output_tokens":2}}}`

		usage, apiErr := cohereHandler(c, info, &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		})

		require.Nil(t, apiErr)
		require.Equal(t, 3, usage.TotalTokens)
		require.True(t, info.HasRecordedChannelFirstResponse())
	})

	t.Run("rerank", func(t *testing.T) {
		c, _ := newCohereResponseContext()
		info := newCohereResponseInfo()
		body := `{"results":[],"meta":{"billed_units":{"input_tokens":1,"output_tokens":0}}}`

		usage, apiErr := cohereRerankHandler(c, &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, info)

		require.Nil(t, apiErr)
		require.Equal(t, 1, usage.TotalTokens)
		require.True(t, info.HasRecordedChannelFirstResponse())
	})
}

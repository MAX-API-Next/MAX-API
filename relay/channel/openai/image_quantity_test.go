package openai

import (
	"net/http"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageWriteFailureCannotReduceQuantity(t *testing.T) {
	c, _, resp, info := newImageTestContext(t,
		"data: {\"type\":\"image_generation.completed\",\"b64_json\":\"synthetic\"}\n\ndata: [DONE]\n\n", "text/event-stream", true)
	failed, _ := gin.CreateTestContext(&rejectedChatResponseWriter{header: make(http.Header)})
	c.Writer = failed.Writer
	info.PriceData = types.PriceData{UsePrice: true, OtherRatios: map[string]float64{"n": 3}}
	_, apiErr := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "a committed image stream must not trigger an external retry")
	require.Equal(t, float64(3), info.PriceData.OtherRatios["n"])
	require.True(t, info.StreamStatus.HasErrors(), "a concurrently scanned DONE cannot hide the write error")
}

func TestOpenAIImageQuantityContract(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{`{"data":{"url":"https://synthetic.invalid/image"}}`, 1},
		{`{"data":[{"url":"a"},{"url":"b"},{"b64_json":"YQ=="},{"error":"failed"}]}`, 2},
		{`{"data":[{"url":"a","b64_json":"YQ=="}]}`, 1},
		{`{"data":[{}, {"url":""},{"b64_json":null}]}`, 0},
	} {
		require.Equal(t, tc.want, openAIImageResultCount([]byte(tc.raw)))
	}
	for _, tc := range []struct {
		count              int
		complete, perImage bool
		want               float64
	}{
		{1, true, true, 1}, {1, false, true, 3}, {4, false, true, 4},
		{0, true, true, 3}, {dto.MaxImageN + 1, true, true, 3}, {1, true, false, 3},
	} {
		info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: tc.perImage, OtherRatios: map[string]float64{"n": 3}}}
		applyOpenAIImageCount(info, tc.count, tc.complete)
		require.Equal(t, tc.want, info.PriceData.OtherRatios["n"])
	}
}

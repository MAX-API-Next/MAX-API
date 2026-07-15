package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenaiTTSStreamPreservesAudioUsageDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`data: {"usage":{"input_tokens":100,"output_tokens":25,"total_tokens":125,"input_tokens_details":{"text_tokens":80,"audio_tokens":20},"output_tokens_details":{"audio_tokens":25}}}` + "\n" +
				"data: [DONE]\n",
		)),
	}

	usage := OpenaiTTSHandler(c, resp, info)

	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 25, usage.CompletionTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.AudioTokens)
	require.Equal(t, 80, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 25, usage.CompletionTokenDetails.AudioTokens)
}

func TestOpenaiSTTPreservesInputTokenDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"text":"hello","usage":{"input_tokens":100,"output_tokens":5,"total_tokens":105,"input_tokens_details":{"text_tokens":10,"audio_tokens":90}}}`,
		)),
	}

	maxAPIError, usage := OpenaiSTTHandler(c, resp, info, "json")

	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 5, usage.CompletionTokens)
	require.Equal(t, dto.InputTokenDetails{TextTokens: 10, AudioTokens: 90}, usage.PromptTokensDetails)
}

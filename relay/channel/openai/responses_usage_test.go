package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func detailedResponsesUsage() *dto.Usage {
	return &dto.Usage{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         1,
			CachedCreationTokens: 2,
			CacheWriteTokens:     3,
			TextTokens:           4,
			ImageTokens:          5,
			AudioTokens:          6,
		},
		OutputTokensDetails: &dto.OutputTokenDetails{
			ReasoningTokens: 7,
			TextTokens:      8,
			ImageTokens:     9,
			AudioTokens:     10,
		},
	}
}

func assertDetailedResponsesUsage(t *testing.T, usage *dto.Usage) {
	t.Helper()
	require.NotNil(t, usage)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 50, usage.CompletionTokens)
	require.Equal(t, 150, usage.TotalTokens)
	require.Equal(t, dto.InputTokenDetails{
		CachedTokens:         1,
		CachedCreationTokens: 2,
		CacheWriteTokens:     3,
		TextTokens:           4,
		ImageTokens:          5,
		AudioTokens:          6,
	}, usage.PromptTokensDetails)
	require.NotNil(t, usage.InputTokensDetails)
	require.Equal(t, usage.PromptTokensDetails, *usage.InputTokensDetails)
	require.Equal(t, dto.OutputTokenDetails{
		ReasoningTokens: 7,
		TextTokens:      8,
		ImageTokens:     9,
		AudioTokens:     10,
	}, usage.CompletionTokenDetails)
}

func newResponsesUsageTestContext() (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	return c, info
}

func TestOaiResponsesHandlerPreservesDetailedUsage(t *testing.T) {
	c, info := newResponsesUsageTestContext()
	payload := dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{{
			Type: "message",
			Role: "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type: "output_text",
				Text: "ok",
			}},
		}},
		Usage: detailedResponsesUsage(),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, maxAPIError := OaiResponsesHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, maxAPIError)
	assertDetailedResponsesUsage(t, usage)
}

func TestOaiResponsesStreamHandlerPreservesDetailedUsage(t *testing.T) {
	c, info := newResponsesUsageTestContext()
	completed := dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			Output: []dto.ResponsesOutput{{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{{
					Type: "output_text",
					Text: "ok",
				}},
			}},
			Usage: detailedResponsesUsage(),
		},
	}
	completedData, err := common.Marshal(completed)
	require.NoError(t, err)
	body := append([]byte("data: "), completedData...)
	body = append(body, []byte("\ndata: [DONE]\n")...)

	usage, maxAPIError := OaiResponsesStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, maxAPIError)
	assertDetailedResponsesUsage(t, usage)
}

func TestOaiResponsesCompactionHandlerPreservesDetailedUsage(t *testing.T) {
	c, info := newResponsesUsageTestContext()
	payload := dto.OpenAIResponsesCompactionResponse{
		Usage: detailedResponsesUsage(),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, maxAPIError := OaiResponsesCompactionHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	})

	require.Nil(t, maxAPIError)
	assertDetailedResponsesUsage(t, usage)
}

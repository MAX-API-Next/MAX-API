package aws

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewAwsInvokeContextFollowsRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancelRequest := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(requestContext)

	ctx, cancel := newAwsInvokeContext(c)
	defer cancel()
	cancelRequest()

	select {
	case <-ctx.Done():
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("AWS invoke context did not follow client cancellation")
	}
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestParseNovaResponseRejectsEmptyContent(t *testing.T) {
	content, usage, err := parseNovaResponse([]byte(
		`{"output":{"message":{"content":[]}},"usage":{"inputTokens":1,"outputTokens":0,"totalTokens":1}}`,
	))

	require.ErrorContains(t, err, "nova response content is empty")
	require.Empty(t, content)
	require.Zero(t, usage)
}

func TestConvertToNovaRequestPreservesExplicitZeroValues(t *testing.T) {
	zeroFloat := 0.0
	zeroInt := 0
	zeroUint := uint(0)
	novaRequest := convertToNovaRequest(&dto.GeneralOpenAIRequest{
		MaxTokens:   &zeroUint,
		Temperature: &zeroFloat,
		TopP:        &zeroFloat,
		TopK:        &zeroInt,
	})

	payload, err := common.Marshal(novaRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{"schemaVersion":"messages-v1","messages":[],"inferenceConfig":{"maxTokens":0,"temperature":0,"topP":0,"topK":0}}`, string(payload))
}

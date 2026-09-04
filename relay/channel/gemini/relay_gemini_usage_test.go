package gemini

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setGeminiToolPricesForTest(t *testing.T, additions map[string]float64) {
	t.Helper()

	setting := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	original := make(map[string]float64, len(setting.Prices))
	for name, price := range setting.Prices {
		original[name] = price
	}
	setting.Prices = make(map[string]float64, len(original)+len(additions))
	for name, price := range original {
		setting.Prices[name] = price
	}
	for name, price := range additions {
		setting.Prices[name] = price
	}
	operation_setting.RebuildToolPriceIndex()

	t.Cleanup(func() {
		setting.Prices = original
		operation_setting.RebuildToolPriceIndex()
	})
}

func newGeminiToolBillingContext(path string) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil)
	ledger := relaycommon.NewToolUsageLedger("gemini-test")
	ledger.BeginAttempt(0)
	return c, &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-test",
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
		ToolUsage: ledger,
	}
}

func TestGeminiChatHandlerRecordsCompletedCustomFunctionCalls(t *testing.T) {
	setGeminiToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newGeminiToolBillingContext("/v1/chat/completions")

	payload := `{
		"candidates":[{"index":0,"content":{"role":"model","parts":[
			{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"}}},
			{"functionCall":{"id":"call-2","name":"unpriced","args":{}}}
		]}}],
		"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}
	}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

	_, maxErr := GeminiChatHandler(c, info, resp)
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, info.ToolUsageSnapshot().Items)
}

func TestGeminiStreamHandlerWaitsForCompletedFunctionCallAndDeduplicatesID(t *testing.T) {
	setGeminiToolPricesForTest(t, map[string]float64{"lookup": 5})
	c, info := newGeminiToolBillingContext("/v1/chat/completions")

	body := strings.Join([]string{
		`data: {"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"},"willContinue":true}}]}}]}`,
		`data: {"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"},"willContinue":false}}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`,
		`data: {"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"},"willContinue":false}}]}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}

	_, maxErr := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, maxErr)
	require.True(t, info.CommitToolUsageAttempt())
	require.Equal(t, []relaycommon.ToolUsageItem{{
		Name:       "lookup",
		CallCount:  1,
		PricePer1K: 5,
	}}, info.ToolUsageSnapshot().Items)
}

func TestGeminiChatHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, maxAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiImageHandlerRejectsTooManyPredictions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	payload := dto.GeminiImageResponse{
		Predictions: make([]dto.GeminiImagePrediction, dto.MaxImageN+1),
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	usage, maxAPIError := GeminiImageHandler(c, &relaycommon.RelayInfo{}, resp)
	require.Nil(t, usage)
	require.NotNil(t, maxAPIError)
	require.Contains(t, maxAPIError.Error(), "too many image predictions")
}

func TestGeminiStreamHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, maxAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiTextGenerationHandlerPromptTokensIncludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, maxAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiChatHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, maxAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiStreamHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, maxAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiResponsesStreamHandlerEstimatesUsageAndAuditWhenUpstreamUsageMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	originalLogResponseContentEnabled := common.LogResponseContentEnabled
	common.LogResponseContentEnabled = true
	t.Cleanup(func() {
		common.LogResponseContentEnabled = originalLogResponseContentEnabled
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
	}
	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, maxAPIError := GeminiResponsesStreamHandler(c, info, resp)
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Positive(t, usage.CompletionTokens)
	require.Positive(t, usage.TotalTokens)
	require.Equal(t, "partial", info.AuditResponseContent)

	var completed dto.ResponsesStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(sseDataForEvent(t, recorder.Body.String(), "response.completed"), &completed))
	require.NotNil(t, completed.Response)
	require.NotNil(t, completed.Response.Usage)
	require.Equal(t, 20, completed.Response.Usage.InputTokens)
	require.Positive(t, completed.Response.Usage.OutputTokens)
	require.Positive(t, completed.Response.Usage.TotalTokens)
}

func TestGeminiTextGenerationHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, maxAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func newGeminiUsagePatchTestContext(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:generateContent", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-test",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"},
	}
	return c, info
}

func TestPatchGeminiZeroCompletionUsagePreservesBillingDetails(t *testing.T) {
	c, info := newGeminiUsagePatchTestContext(t)
	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:        100,
		ToolUsePromptTokenCount: 5,
		ThoughtsTokenCount:      10,
		TotalTokenCount:         115,
		CachedContentTokenCount: 20,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 70},
			{Modality: "IMAGE", TokenCount: 10},
			{Modality: "AUDIO", TokenCount: 20},
		},
		ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 5},
		},
	}
	usage := buildUsageFromGeminiMetadata(metadata, 0)
	require.Equal(t, 10, usage.CompletionTokens)

	patchGeminiZeroCompletionUsage(c, info, &usage, "", 2)

	require.Equal(t, 2810, usage.CompletionTokens)
	require.Equal(t, 10, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 2800, usage.CompletionTokenDetails.ImageTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
	require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
	patched := usage.BillingUsage.GeminiUsageMetadata
	require.Equal(t, 20, patched.CachedContentTokenCount)
	require.Equal(t, metadata.PromptTokensDetails, patched.PromptTokensDetails)
	require.Equal(t, metadata.ToolUsePromptTokensDetails, patched.ToolUsePromptTokensDetails)
	require.Equal(t, 10, patched.ThoughtsTokenCount)
	require.Equal(t, 2800, patched.CandidatesTokenCount)
	require.Contains(t, patched.CandidatesTokensDetails, dto.GeminiPromptTokensDetails{Modality: "IMAGE", TokenCount: 2800})
}

func TestPatchGeminiZeroCompletionUsageEstimatesTextAlongsideReasoning(t *testing.T) {
	c, info := newGeminiUsagePatchTestContext(t)
	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:        100,
		ThoughtsTokenCount:      10,
		TotalTokenCount:         110,
		CachedContentTokenCount: 20,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 100},
		},
	}
	usage := buildUsageFromGeminiMetadata(metadata, 0)

	patchGeminiZeroCompletionUsage(c, info, &usage, "visible response text", 0)

	estimatedCandidates := usage.CompletionTokens - 10
	require.Positive(t, estimatedCandidates)
	require.Equal(t, estimatedCandidates, usage.CompletionTokenDetails.TextTokens)
	require.Equal(t, 10, usage.CompletionTokenDetails.ReasoningTokens)
	require.NotNil(t, usage.BillingUsage)
	require.Equal(t, estimatedCandidates, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	require.Equal(t, 10, usage.BillingUsage.GeminiUsageMetadata.ThoughtsTokenCount)
	require.Equal(t, 20, usage.BillingUsage.GeminiUsageMetadata.CachedContentTokenCount)
	require.Contains(t, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokensDetails, dto.GeminiPromptTokensDetails{
		Modality:   "TEXT",
		TokenCount: estimatedCandidates,
	})
}

func TestPatchGeminiZeroCompletionUsageCombinesTextAndImageEstimates(t *testing.T) {
	c, info := newGeminiUsagePatchTestContext(t)
	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:        100,
		ThoughtsTokenCount:      10,
		TotalTokenCount:         110,
		CachedContentTokenCount: 20,
	}
	usage := buildUsageFromGeminiMetadata(metadata, 0)

	patchGeminiZeroCompletionUsage(c, info, &usage, "visible response text", 2)

	textTokens := usage.CompletionTokenDetails.TextTokens
	require.Positive(t, textTokens)
	require.Equal(t, 2800, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 10+textTokens+2800, usage.CompletionTokens)
	require.NotNil(t, usage.BillingUsage)
	require.Equal(t, textTokens+2800, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	require.Contains(t, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokensDetails, dto.GeminiPromptTokensDetails{
		Modality:   "TEXT",
		TokenCount: textTokens,
	})
	require.Contains(t, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokensDetails, dto.GeminiPromptTokensDetails{
		Modality:   "IMAGE",
		TokenCount: 2800,
	})
}

func TestPatchGeminiZeroCompletionUsagePreservesExistingImageDetails(t *testing.T) {
	c, info := newGeminiUsagePatchTestContext(t)
	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:   100,
		ThoughtsTokenCount: 10,
		TotalTokenCount:    110,
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "IMAGE", TokenCount: 321},
		},
	}
	usage := buildUsageFromGeminiMetadata(metadata, 0)

	patchGeminiZeroCompletionUsage(c, info, &usage, "", 2)

	require.Equal(t, 321, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 331, usage.CompletionTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
	require.Equal(t, 321, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
}

func TestPatchGeminiZeroCompletionUsageUsesExistingAudioDetails(t *testing.T) {
	c, info := newGeminiUsagePatchTestContext(t)
	metadata := dto.GeminiUsageMetadata{
		PromptTokenCount:   100,
		ThoughtsTokenCount: 10,
		TotalTokenCount:    110,
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "AUDIO", TokenCount: 55},
		},
	}
	usage := buildUsageFromGeminiMetadata(metadata, 0)

	patchGeminiZeroCompletionUsage(c, info, &usage, "", 0)

	require.Equal(t, 55, usage.CompletionTokenDetails.AudioTokens)
	require.Equal(t, 65, usage.CompletionTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
	require.Equal(t, 55, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
}

func TestGeminiStreamHandlerDoesNotCountAudioInlineDataAsImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-test",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"},
	}
	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role: "model",
				Parts: []dto.GeminiPart{{
					InlineData: &dto.GeminiInlineData{MimeType: "audio/wav", Data: "AA=="},
				}},
			},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 100,
			TotalTokenCount:  100,
		},
	}
	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader([]byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n"))),
	}

	usage, maxAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})

	require.Nil(t, maxAPIError)
	require.NotNil(t, usage)
	require.Zero(t, usage.CompletionTokenDetails.ImageTokens)
	require.NotEqual(t, 1400, usage.CompletionTokens)
}

func sseDataForEvent(t *testing.T, body string, eventType string) string {
	t.Helper()
	for _, event := range strings.Split(body, "\n\n") {
		if !strings.Contains(event, "event: "+eventType) {
			continue
		}
		for _, line := range strings.Split(event, "\n") {
			if strings.HasPrefix(line, "data: ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
	}
	t.Fatalf("event %q not found in body:\n%s", eventType, body)
	return ""
}

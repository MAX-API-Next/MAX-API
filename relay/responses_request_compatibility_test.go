package relay

import (
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel/advancedcustom"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Stop at the transport boundary: no external request or billing is performed.
type responsesCaptureAdaptor struct {
	openai.Adaptor
	endpoint string
	body     []byte
}

func (a *responsesCaptureAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.endpoint, nil
}

func (a *responsesCaptureAdaptor) DoRequest(_ *gin.Context, _ *relaycommon.RelayInfo, body io.Reader) (any, error) {
	var err error
	a.body, err = io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return nil, errors.New("test transport boundary")
}

func TestChatResponsesEndpointCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		channel        int
		extensions     bool
	}{
		{"official", "https://api.openai.com/v1/responses", constant.ChannelTypeOpenAI, false},
		{"official-case-port", "https://API.OPENAI.COM:443/v1/responses", constant.ChannelTypeOpenAI, false},
		{"official-absolute-host", "https://api.openai.com./v1/responses", constant.ChannelTypeOpenAI, false},
		{"third-party", "https://inference.example/v1/responses", constant.ChannelTypeOpenAI, true},
		{"lookalike", "https://api.openai.com.example/v1/responses", constant.ChannelTypeOpenAI, true},
		{"userinfo", "https://api.openai.com@inference.example/v1/responses", constant.ChannelTypeOpenAI, true},
		{"codex-proxy", "https://codex.example/backend-api/codex/responses", constant.ChannelTypeCodex, false},
		{"custom-official", "https://api.openai.com/v1/responses", constant.ChannelTypeAdvancedCustom, false},
		{"custom-codex", "https://chatgpt.com/backend-api/codex/responses", constant.ChannelTypeAdvancedCustom, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tc.channel, ChannelBaseUrl: "https://configured.example"}}
			var req dto.GeneralOpenAIRequest
			// Qwen retains thinking_budget in the existing DTO marshal contract.
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"qwen-plus","messages":[{"role":"user","content":"hello"}],"stop":["END"],"top_k":0,"min_p":0,"repetition_penalty":0,"cache_salt":"","chat_template_kwargs":{"enable_thinking":false},"frequency_penalty":0,"presence_penalty":0,"enable_thinking":false,"thinking_budget":0,"prompt_cache_key":"cache-key","prompt_cache_options":{"ttl":"30m"},"temperature":0,"top_p":0,"parallel_tool_calls":false}`, &req))
			adaptor := &responsesCaptureAdaptor{endpoint: tc.endpoint}
			_, err := chatCompletionsViaResponses(c, info, adaptor, &req)
			require.NotNil(t, err)
			require.NotEmpty(t, adaptor.body)
			for _, field := range []string{"stop", "top_k", "min_p", "repetition_penalty", "cache_salt", "chat_template_kwargs", "frequency_penalty", "presence_penalty", "enable_thinking", "thinking_budget"} {
				require.Equal(t, tc.extensions, gjson.GetBytes(adaptor.body, field).Exists(), field)
			}
			for _, field := range []string{"temperature", "top_p", "parallel_tool_calls", "prompt_cache_key", "prompt_cache_options"} {
				require.True(t, gjson.GetBytes(adaptor.body, field).Exists(), field)
			}
			require.Equal(t, "qwen-plus", gjson.GetBytes(adaptor.body, "model").String())
			require.Equal(t, "cache-key", gjson.GetBytes(adaptor.body, "prompt_cache_key").String())
			require.Equal(t, "30m", gjson.GetBytes(adaptor.body, "prompt_cache_options.ttl").String())
			require.False(t, gjson.GetBytes(adaptor.body, "parallel_tool_calls").Bool())
			require.NotNil(t, req.TopK, "conversion must not mutate the caller's request")
		})
	}
}

func TestResponsesFieldFilterBoundaries(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
	adaptor := &responsesCaptureAdaptor{endpoint: "https://api.openai.com/v1/responses"}
	request := dto.OpenAIResponsesRequest{Model: "gpt-test", TopK: common.GetPointer(0)}
	for _, input := range []any{request, &request} {
		out, err := filterResponsesRequestFields(input, info, adaptor)
		require.NoError(t, err)
		body, err := common.Marshal(out)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(body, "top_k").Exists())
		require.NotNil(t, request.TopK, "filter must copy rather than mutate the input")
	}
	for _, input := range []any{nil, (*dto.OpenAIResponsesRequest)(nil), &dto.GeneralOpenAIRequest{TopK: common.GetPointer(0)}} {
		out, err := filterResponsesRequestFields(input, info, nil)
		require.NoError(t, err)
		require.Equal(t, input, out)
	}
	out, err := filterResponsesRequestFields(&request, nil, nil)
	require.NoError(t, err)
	require.Same(t, &request, out)
	adaptor.endpoint = "https://invalid.example/%wrong?secret=redacted-fixture"
	_, err = filterResponsesRequestFields(request, info, adaptor)
	require.EqualError(t, err, "invalid Responses upstream URL")
}

func TestResponsesFieldFilterUsesAdvancedCustomDestination(t *testing.T) {
	for _, tc := range []struct {
		base, target string
		keep         bool
	}{
		{"https://api.openai.com", "https://inference.example/v1/responses", true},
		{"https://inference.example", "https://api.openai.com/v1/responses", false},
	} {
		t.Run(tc.target, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat:    types.RelayFormatOpenAI,
				RelayMode:      relayconstant.RelayModeChatCompletions,
				RequestURLPath: "/v1/chat/completions",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    constant.ChannelTypeAdvancedCustom,
					ChannelBaseUrl: tc.base,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						AdvancedCustom: &dto.AdvancedCustomConfig{
							Routes: []dto.AdvancedCustomRoute{{
								IncomingPath: "/v1/chat/completions",
								UpstreamPath: tc.target,
								Converter:    dto.AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses,
							}},
						},
					},
				},
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			adaptor := &advancedcustom.Adaptor{}
			adaptor.Init(info)
			converted, err := adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: "model", TopK: common.GetPointer(0)})
			require.NoError(t, err)
			filtered, err := filterResponsesRequestFields(converted, info, adaptor)
			require.NoError(t, err)
			body, err := common.Marshal(filtered)
			require.NoError(t, err)
			require.Equal(t, tc.keep, gjson.GetBytes(body, "top_k").Exists())
			require.NotNil(t, converted.(*dto.OpenAIResponsesRequest).TopK)
		})
	}
}

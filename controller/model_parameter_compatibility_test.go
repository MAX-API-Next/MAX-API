package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	"github.com/MAX-API-Next/MAX-API/relay/channel/deepseek"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	"github.com/MAX-API-Next/MAX-API/relay/channel/xai"
	"github.com/MAX-API-Next/MAX-API/relay/channel/zhipu_4v"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/relay/reasoningcompat"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/service/openaicompat"
	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/MAX-API-Next/MAX-API/setting/reasoning"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParameterCompatibilityGPT5Sampling(t *testing.T) {
	for _, tc := range []struct {
		model, effort string
		sampling      bool
	}{
		{"gpt-5.1", "", true}, {"gpt-5.2", "none", true}, {"gpt-5.4-2026-03-05", "none", true},
		{"gpt-5.4", "high", false}, {"gpt-5.2-high", "none", false}, {"gpt-5.4-none", "high", true},
		{"gpt-5.2-pro", "none", false}, {"gpt-5.1-codex-max", "none", false}, {"gpt-5", "none", false},
		{"gpt-50", "", true}, {"gpt-5.2-2026-02-30", "none", false},
	} {
		t.Run(tc.model+tc.effort, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: tc.model, ReasoningEffort: tc.effort}
			require.NoError(t, common.UnmarshalJsonStr(`{"temperature":0,"top_p":0,"logprobs":false,"top_logprobs":0}`, req))
			body, _ := astraOutboundChat(t, req, req.Model, constant.ChannelTypeOpenAI)
			for _, key := range []string{"temperature", "top_p", "logprobs", "top_logprobs"} {
				require.Equal(t, tc.sampling, gjson.GetBytes(body, key).Exists(), key)
			}
		})
	}
}

func TestParameterCompatibilityZhipu(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := &dto.GeneralOpenAIRequest{Model: "glm-5", ReasoningEffort: "high"}
	converted, err := (&zhipu_4v.Adaptor{}).ConvertOpenAIRequest(c, &relaycommon.RelayInfo{}, req)
	require.NoError(t, err)
	b, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Equal(t, "high", gjson.GetBytes(b, "reasoning_effort").String())
}

func TestParameterCompatibilityDTOExtensions(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		target      any
	}{
		{"chat", `{"model":"local-model","thinking_token_budget":0,"include_reasoning":false,"min_p":0,"repetition_penalty":0,"structured_outputs":{"regex":"a"},"return_token_ids":false,"min_tokens":0,"separate_reasoning":false,"stream_reasoning":false,"regex":"a","ebnf":"root ::= 'a'","stop_token_ids":[0],"stop_regex":"end","no_stop_trim":false,"ignore_eos":false,"skip_special_tokens":false,"continue_final_message":false,"cache_salt":"test"}`, &dto.GeneralOpenAIRequest{}},
		{"responses", `{"model":"local-model","prompt_cache_options":{"ttl":"30m"},"chat_template_kwargs":{"enable_thinking":false},"top_k":0,"min_p":0,"repetition_penalty":0,"stop":["end"],"cache_salt":"test"}`, &dto.OpenAIResponsesRequest{}},
		{"compact", `{"model":"gpt-test","input":"hi","tools":[],"parallel_tool_calls":false,"reasoning":{"effort":"high"},"service_tier":"default","prompt_cache_key":"key","prompt_cache_options":{"ttl":"30m"},"prompt_cache_retention":"24h","text":{"verbosity":"low"}}`, &dto.OpenAIResponsesCompactionRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, common.UnmarshalJsonStr(tc.input, tc.target))
			b, err := common.Marshal(tc.target)
			require.NoError(t, err)
			require.JSONEq(t, tc.input, string(b))
			if compact, ok := tc.target.(*dto.OpenAIResponsesCompactionRequest); ok {
				converted, err := common.Marshal(compact.ToResponsesRequest())
				require.NoError(t, err)
				require.JSONEq(t, tc.input, string(converted))
			}
		})
	}
}

func parameterContext() (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
}

func TestParameterCompatibilityClaudeReasoning(t *testing.T) {
	for _, model := range []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-mythos-5", "claude-mythos-preview"} {
		t.Run(model, func(t *testing.T) {
			c, info := parameterContext()
			info.UpstreamModelName = model
			converted, err := (&claude.Adaptor{}).ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: model, ReasoningEffort: "high", MaxTokens: common.GetPointer(uint(8192)), Temperature: common.GetPointer(.2), TopP: common.GetPointer(.5), TopK: common.GetPointer(20)})
			require.NoError(t, err)
			req := converted.(*dto.ClaudeRequest)
			require.Equal(t, "adaptive", req.Thinking.Type)
			require.Nil(t, req.Thinking.BudgetTokens)
			require.Equal(t, "high", info.ReasoningEffort)
			require.Equal(t, uint(8192), *req.MaxTokens)
			if model == "claude-opus-4-6" || model == "claude-sonnet-4-6" {
				require.Equal(t, 1.0, *req.Temperature)
			} else {
				require.Nil(t, req.Temperature)
			}
			require.Nil(t, req.TopP)
			require.Nil(t, req.TopK)
		})
	}
	c, _ := parameterContext()
	for _, limit := range []uint{0, 1024} {
		_, err := claude.RequestOpenAI2ClaudeMessage(c, dto.GeneralOpenAIRequest{Model: "claude-sonnet-4-5", ReasoningEffort: "high", MaxTokens: &limit})
		require.ErrorContains(t, err, "manual thinking requires")
	}
	out, err := claude.RequestOpenAI2ClaudeMessage(c, dto.GeneralOpenAIRequest{Model: "claude-sonnet-4-5", ReasoningEffort: "high", MaxTokens: common.GetPointer(uint(2000))})
	require.NoError(t, err)
	require.Equal(t, "enabled", out.Thinking.Type)
	require.Equal(t, 1600, *out.Thinking.BudgetTokens)
	require.Equal(t, uint(2000), *out.MaxTokens)
	out, err = claude.RequestOpenAI2ClaudeMessage(c, dto.GeneralOpenAIRequest{Model: "claude-opus-4-7", ReasoningEffort: "none"})
	require.NoError(t, err)
	require.Equal(t, "disabled", out.Thinking.Type)
	_, err = claude.RequestOpenAI2ClaudeMessage(c, dto.GeneralOpenAIRequest{Model: "claude-opus-4-7-high", ReasoningEffort: "low"})
	require.ErrorContains(t, err, "conflicting")

	var native dto.ClaudeRequest
	raw := `{"model":"claude-opus-4-7","max_tokens":2000,"thinking":{"type":"adaptive","display":"omitted"},"output_config":{"effort":"low","format":{"type":"json_schema"}},"temperature":0}`
	require.NoError(t, common.UnmarshalJsonStr(raw, &native))
	before, err := common.Marshal(native)
	require.NoError(t, err)
	_, _, err = reasoningcompat.ApplyClaudeSuffix(&native, native.Model)
	require.NoError(t, err)
	after, err := common.Marshal(native)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

func TestParameterCompatibilityClaudeToOpenRouter(t *testing.T) {
	_, info := parameterContext()
	info.ChannelType = constant.ChannelTypeOpenRouter
	var native dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-opus-4-7","thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`, &native))
	out, err := service.ClaudeToOpenAIRequest(native, info)
	require.NoError(t, err)
	b, err := common.Marshal(out)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(b, "verbosity").Exists())
	require.Equal(t, "high", gjson.GetBytes(b, "reasoning.effort").String())
	native.Thinking = &dto.Thinking{Type: "disabled"}
	out, err = service.ClaudeToOpenAIRequest(native, info)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":false}`, string(out.Reasoning))
}

func TestParameterCompatibilityReasoningBoundaries(t *testing.T) {
	c, info := parameterContext()
	for _, model := range []string{"claude-opus-4-7", "claude-sonnet-4-5", "claude-sonnet-5", "claude-mythos-preview"} {
		out, err := claude.RequestOpenAI2ClaudeMessage(c, dto.GeneralOpenAIRequest{Model: model, IncludeReasoning: common.GetPointer(false)})
		require.NoError(t, err)
		if model == "claude-sonnet-5" || model == "claude-mythos-preview" {
			require.Equal(t, &dto.Thinking{Type: "adaptive", Display: "omitted"}, out.Thinking)
		} else {
			require.Nil(t, out.Thinking, "visibility alone must not enable thinking")
		}
	}
	out, err := claude.RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{Model: "claude-sonnet-4-5", ReasoningEffort: "max", MaxTokens: common.GetPointer(uint(10000))})
	require.NoError(t, err)
	require.Equal(t, 9500, *out.Thinking.BudgetTokens)
	require.Equal(t, uint(10000), *out.MaxTokens)
	_, err = claude.RequestOpenAI2ClaudeMessage(c, dto.GeneralOpenAIRequest{Model: "claude-sonnet-4-5", ReasoningEffort: "high", MaxTokens: common.GetPointer(^uint(0))})
	require.ErrorContains(t, err, "platform integer limit")
	for _, raw := range []string{
		`{"reasoning":{"enabled":true,"max_tokens":0}}`,
		`{"reasoning_effort":"none","reasoning":{"max_tokens":4096}}`,
		`{"reasoning_effort":"high","reasoning":{"enabled":false}}`,
		`{"include_reasoning":false,"reasoning":{"exclude":false}}`,
	} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(raw, &req))
		_, err := reasoningcompat.FromChat(req)
		require.ErrorContains(t, err, "conflicting")
	}
	var native dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-opus-4-7-high","max_tokens":2000,"thinking":{"type":"adaptive","display":"omitted"},"output_config":{"effort":"high","format":{"type":"json_schema"}}}`, &native))
	_, _, err = reasoningcompat.ApplyClaudeSuffix(&native, native.Model)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-7", native.Model)
	require.Equal(t, "omitted", native.Thinking.Display)
	require.Equal(t, "json_schema", gjson.GetBytes(native.OutputConfig, "format.type").String())
	native.Model = "claude-opus-4-7-high"
	native.Thinking = &dto.Thinking{Type: "enabled", BudgetTokens: common.GetPointer(-2)}
	_, _, err = reasoningcompat.ApplyClaudeSuffix(&native, native.Model)
	require.ErrorContains(t, err, "budget must be >= -1")

	info.UpstreamModelName = "gemini-2.5-flash"
	var chat dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-flash","max_tokens":99,"max_completion_tokens":0,"top_p":0,"seed":0,"extra_body":{"google":{"thinking_config":{"thinking_budget":-1,"include_thoughts":false}}}}`, &chat))
	gem, err := gemini.CovertOpenAI2Gemini(c, chat, info)
	require.NoError(t, err)
	require.Equal(t, uint(0), *gem.GenerationConfig.MaxOutputTokens)
	require.Equal(t, 0.0, *gem.GenerationConfig.TopP)
	require.Equal(t, int64(0), *gem.GenerationConfig.Seed)
	require.Equal(t, -1, *gem.GenerationConfig.ThinkingConfig.ThinkingBudget)
	require.False(t, *gem.GenerationConfig.ThinkingConfig.IncludeThoughts)
	chat.IncludeReasoning = common.GetPointer(true)
	_, err = gemini.CovertOpenAI2Gemini(c, chat, info)
	require.ErrorContains(t, err, "conflicting reasoning visibility")
	info.UpstreamModelName = "gemini-2.5-flash-image"
	gem, err = gemini.CovertOpenAI2Gemini(c, dto.GeneralOpenAIRequest{Model: info.UpstreamModelName, IncludeReasoning: common.GetPointer(false)}, info)
	require.NoError(t, err)
	require.Nil(t, gem.GenerationConfig.ThinkingConfig)
	info.UpstreamModelName = "gemini-unknown"
	_, err = gemini.CovertOpenAI2Gemini(c, dto.GeneralOpenAIRequest{Model: info.UpstreamModelName, ReasoningEffort: "high"}, info)
	require.ErrorContains(t, err, "unknown Gemini model")

	var nativeGemini dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":0,"includeThoughts":false}}}`, &nativeGemini))
	before, err := common.Marshal(nativeGemini)
	require.NoError(t, err)
	require.NoError(t, gemini.ThinkingAdaptor(&nativeGemini, info))
	after, err := common.Marshal(nativeGemini)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	settings := model_setting.GetGeminiSettings()
	previous := settings.ThinkingAdapterEnabled
	settings.ThinkingAdapterEnabled = true
	t.Cleanup(func() { settings.ThinkingAdapterEnabled = previous })
	info.UpstreamModelName = "gemini-3-pro-high"
	nativeGemini.GenerationConfig.ThinkingConfig.ThinkingBudget = common.GetPointer(-2)
	require.ErrorContains(t, gemini.ThinkingAdaptor(&nativeGemini, info), "budget must be >= -1")
}

func TestParameterCompatibilityGeminiReasoning(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	before := settings.ThinkingAdapterEnabled
	settings.ThinkingAdapterEnabled = true
	t.Cleanup(func() { settings.ThinkingAdapterEnabled = before })
	for _, tc := range []struct {
		model, effort, level string
		budget               *int
	}{
		{"gemini-2.5-pro-high", "", "", common.GetPointer(24576)},
		{"gemini-2.5-flash", "low", "", common.GetPointer(1024)},
		{"gemini-2.5-flash", "none", "", common.GetPointer(0)},
		{"gemini-2.5-pro", "none", "", common.GetPointer(128)},
		{"gemini-3-pro-max", "", "high", nil},
		{"gemini-3-pro", "medium", "high", nil},
		{"gemini-3.1-pro", "minimal", "low", nil},
		{"gemini-3-flash", "medium", "medium", nil},
		{"gemini-3-pro-thinking-8192", "", "high", nil},
	} {
		t.Run(tc.model+tc.effort, func(t *testing.T) {
			c, info := parameterContext()
			info.UpstreamModelName = tc.model
			out, err := gemini.CovertOpenAI2Gemini(c, dto.GeneralOpenAIRequest{Model: tc.model, ReasoningEffort: tc.effort}, info)
			require.NoError(t, err)
			require.NotNil(t, out.GenerationConfig.ThinkingConfig)
			require.Equal(t, tc.level, out.GenerationConfig.ThinkingConfig.ThinkingLevel)
			require.Equal(t, tc.budget, out.GenerationConfig.ThinkingConfig.ThinkingBudget)
		})
	}
	c, info := parameterContext()
	info.UpstreamModelName = "gemini-2.5-flash"
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-flash","extra_body":{"google":{"thinking_config":{"thinking_budget":0,"include_thoughts":false}}}}`, &req))
	out, err := gemini.CovertOpenAI2Gemini(c, req, info)
	require.NoError(t, err)
	b, err := common.Marshal(out.GenerationConfig.ThinkingConfig)
	require.NoError(t, err)
	require.JSONEq(t, `{"thinkingBudget":0,"includeThoughts":false}`, string(b))
	require.NoError(t, common.UnmarshalJsonStr(`{"extra_body":{"google":{"thinking_config":{"thinking_budget":1.5}}}}`, &req))
	_, err = gemini.CovertOpenAI2Gemini(c, req, info)
	require.Error(t, err)
}

func TestParameterCompatibilitySuffixIdentity(t *testing.T) {
	for _, tc := range []struct{ model, base, effort string }{
		{"vendor-model-high", "vendor-model-high", ""}, {"gemini-3-pro-high", "gemini-3-pro-high", ""},
		{"gpt-5.1-codex-max", "gpt-5.1-codex-max", ""}, {"gpt-5.1-codex-max-high", "gpt-5.1-codex-max", "high"},
		{"openai/gpt-5.4-max", "openai/gpt-5.4", "max"}, {"o3-high", "o3", "high"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			effort, base := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(tc.model)
			require.Equal(t, tc.base, base)
			require.Equal(t, tc.effort, effort)
			converted, err := (&openai.Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, dto.OpenAIResponsesRequest{Model: tc.model})
			require.NoError(t, err)
			require.Equal(t, tc.base, converted.(dto.OpenAIResponsesRequest).Model)
		})
	}
	settings := model_setting.GetGlobalSettings()
	before := settings.ThinkingModelBlacklist
	settings.ThinkingModelBlacklist = []string{"alias", "deepseek-v4-pro-max", "grok-3-mini-high"}
	t.Cleanup(func() { settings.ThinkingModelBlacklist = before })
	c, info := parameterContext()
	info.OriginModelName = "alias"
	info.UpstreamModelName = "gpt-5.4-high"
	out, err := (&openai.Adaptor{}).ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{Model: info.UpstreamModelName})
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4-high", out.(dto.OpenAIResponsesRequest).Model)
	info.OriginModelName = ""
	info.UpstreamModelName = "deepseek-v4-pro-max"
	deep, err := (&deepseek.Adaptor{}).ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: info.UpstreamModelName})
	require.NoError(t, err)
	require.Equal(t, "deepseek-v4-pro-max", deep.(*dto.GeneralOpenAIRequest).Model)
	info.UpstreamModelName = "grok-3-mini-high"
	grok, err := (&xai.Adaptor{}).ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: info.UpstreamModelName})
	require.NoError(t, err)
	require.Equal(t, "grok-3-mini-high", grok.(*dto.GeneralOpenAIRequest).Model)
}

func TestParameterCompatibilityKimiConversionBoundary(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"kimi-k3","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup"}}]}]}`, &req))
	declarations, err := relaycommon.CaptureMessageTools(&req)
	require.NoError(t, err)
	require.NoError(t, relaycommon.ValidateMessageToolsConversion(declarations, &req))
	require.Error(t, relaycommon.ValidateMessageToolsConversion(declarations, &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "system"}}}))
	_, err = openaicompat.ChatCompletionsRequestToResponsesRequest(&req)
	require.ErrorContains(t, err, "message-scoped tools")
	c, info := parameterContext()
	_, err = claude.RequestOpenAI2ClaudeMessage(c, req)
	require.ErrorContains(t, err, "message-scoped tools")
	_, err = gemini.CovertOpenAI2Gemini(c, req, info)
	require.ErrorContains(t, err, "message-scoped tools")
}

func TestParameterCompatibilityZhipuResponses(t *testing.T) {
	_, info := parameterContext()
	info.RelayMode = relayconstant.RelayModeResponses
	info.ChannelBaseUrl = "https://example.test"
	adaptor := &zhipu_4v.Adaptor{}
	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://example.test/api/v1/responses", url)
	for _, stream := range []bool{false, true} {
		req := dto.OpenAIResponsesRequest{Model: "glm-5", Stream: &stream, Reasoning: &dto.Reasoning{Effort: "high"}}
		out, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, req)
		require.NoError(t, err)
		require.Equal(t, req, out)
	}
}

func TestParameterCompatibilityKimiTools(t *testing.T) {
	const input = `{"model":"kimi-k3","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup","description":"lookup data","parameters":{"type":"object"}}}]},{"role":"assistant","content":null}]}`
	req := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.UnmarshalJsonStr(input, req))
	b, err := common.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, input, string(b))
	meta := req.GetTokenCountMeta()
	require.Equal(t, 1, meta.ToolsCount)
	require.Contains(t, meta.CombineText, "lookup")
	before := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = before })
	c, info := parameterContext()
	info.RelayFormat = types.RelayFormatOpenAI
	meta.TokenType = types.TokenTypeTextNumber
	tokens, err := service.EstimateRequestToken(c, meta, info)
	require.NoError(t, err)
	withoutTools := *meta
	withoutTools.ToolsCount = 0
	base, err := service.EstimateRequestToken(c, &withoutTools, info)
	require.NoError(t, err)
	require.Equal(t, 8, tokens-base)                        // each dynamic declaration adds one standard tool overhead
	require.Equal(t, 1, req.GetTokenCountMeta().ToolsCount) // repeated estimation cannot accumulate charges
}

func TestParameterCompatibilityResponsesExtensionRoundTrip(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"local-model","messages":[{"role":"user","content":"hi"}],"prompt_cache_options":{"ttl":"30m"},"prompt_cache_retention":"24h","chat_template_kwargs":{"enable_thinking":false},"top_k":0,"min_p":0,"repetition_penalty":0,"cache_salt":"salt","stop":["end"]}`, &req))
	response, err := openaicompat.ChatCompletionsRequestToResponsesRequest(&req)
	require.NoError(t, err)
	back, err := openaicompat.ResponsesRequestToChatCompletionsRequest(response)
	require.NoError(t, err)
	original, err := common.Marshal(req)
	require.NoError(t, err)
	encoded, err := common.Marshal(back)
	require.NoError(t, err)
	for _, key := range []string{"prompt_cache_options", "prompt_cache_retention", "chat_template_kwargs", "top_k", "min_p", "repetition_penalty", "cache_salt", "stop"} {
		require.JSONEq(t, gjson.GetBytes(original, key).Raw, gjson.GetBytes(encoded, key).Raw, key)
	}
}

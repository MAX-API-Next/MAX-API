package dto

import (
	"fmt"
	"net/url"
	"strings"
)

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk"
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string                `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType         `json:"vertex_key_type,omitempty"`
	OpenRouterEnterprise                  *bool                 `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                  `json:"claude_beta_query,omitempty"`
	AllowServiceTier                      bool                  `json:"allow_service_tier,omitempty"`
	AllowInferenceGeo                     bool                  `json:"allow_inference_geo,omitempty"`
	AllowSpeed                            bool                  `json:"allow_speed,omitempty"`
	AllowSafetyIdentifier                 bool                  `json:"allow_safety_identifier,omitempty"`
	DisableStore                          bool                  `json:"disable_store,omitempty"`
	AllowIncludeObfuscation               bool                  `json:"allow_include_obfuscation,omitempty"`
	AwsKeyType                            AwsKeyType            `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                  `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled    bool                  `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateLastCheckTime      int64                 `json:"upstream_model_update_last_check_time,omitempty"`
	UpstreamModelUpdateLastDetectedModels []string              `json:"upstream_model_update_last_detected_models,omitempty"`
	UpstreamModelUpdateLastRemovedModels  []string              `json:"upstream_model_update_last_removed_models,omitempty"`
	UpstreamModelUpdateIgnoredModels      []string              `json:"upstream_model_update_ignored_models,omitempty"`
	TaskProtocol                          string                `json:"task_protocol,omitempty"`
	TaskProtocolConfig                    *TaskProtocolConfig   `json:"task_protocol_config,omitempty"`
	DisableTaskDeltaSettlement            bool                  `json:"disable_task_delta_settlement,omitempty"`
	AdvancedCustom                        *AdvancedCustomConfig `json:"advanced_custom,omitempty"`
}

type TaskProtocolConfig struct {
	SubmitPath       string            `json:"submit_path,omitempty"`
	QueryPath        string            `json:"query_path,omitempty"`
	TaskIDPath       string            `json:"task_id_path,omitempty"`
	StatusPath       string            `json:"status_path,omitempty"`
	ProgressPath     string            `json:"progress_path,omitempty"`
	ResultURLPaths   []string          `json:"result_url_paths,omitempty"`
	ErrorMessagePath string            `json:"error_message_path,omitempty"`
	CreatedAtPath    string            `json:"created_at_path,omitempty"`
	UpdatedAtPath    string            `json:"updated_at_path,omitempty"`
	StatusMap        map[string]string `json:"status_map,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

const (
	AdvancedCustomConverterNone                                         = "none"
	AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions     = "anthropic_messages_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIChatCompletionsToAnthropicMessages     = "openai_chat_completions_to_anthropic_messages"
	AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses       = "openai_chat_completions_to_openai_responses"
	AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions       = "openai_responses_to_openai_chat_completions"
	AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions = "gemini_generate_content_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIChatCompletionsToGeminiGenerateContent = "openai_chat_completions_to_gemini_generate_content"
)

const (
	AdvancedCustomAuthTypeNone   = "none"
	AdvancedCustomAuthTypeHeader = "header"
	AdvancedCustomAuthTypeQuery  = "query"
)

type AdvancedCustomConfig struct {
	Routes []AdvancedCustomRoute `json:"advanced_routes,omitempty"`
}

type AdvancedCustomRoute struct {
	IncomingPath string                   `json:"incoming_path,omitempty"`
	UpstreamPath string                   `json:"upstream_path,omitempty"`
	Converter    string                   `json:"converter,omitempty"`
	Auth         *AdvancedCustomRouteAuth `json:"auth,omitempty"`
}

type AdvancedCustomRouteAuth struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

const advancedCustomModelPlaceholder = "{model}"

func (c *AdvancedCustomConfig) MatchPath(requestPath string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

func (c *AdvancedCustomConfig) SupportsPath(requestPath string) bool {
	_, ok := c.MatchPath(requestPath)
	return ok
}

func matchAdvancedCustomIncomingPath(configuredPath string, requestPath string) bool {
	if matchAdvancedCustomIncomingPathTemplate(configuredPath, requestPath) {
		return true
	}
	if strings.Contains(configuredPath, ":generateContent") {
		streamPath := strings.Replace(configuredPath, ":generateContent", ":streamGenerateContent", 1)
		return matchAdvancedCustomIncomingPathTemplate(streamPath, requestPath)
	}
	if strings.Contains(configuredPath, ":streamGenerateContent") {
		generatePath := strings.Replace(configuredPath, ":streamGenerateContent", ":generateContent", 1)
		return matchAdvancedCustomIncomingPathTemplate(generatePath, requestPath)
	}
	return false
}

func matchAdvancedCustomIncomingPathTemplate(configuredPath string, requestPath string) bool {
	if !strings.Contains(configuredPath, advancedCustomModelPlaceholder) {
		return configuredPath == requestPath
	}

	parts := strings.Split(configuredPath, advancedCustomModelPlaceholder)
	if len(parts) != 2 {
		return false
	}
	if !strings.HasPrefix(requestPath, parts[0]) || !strings.HasSuffix(requestPath, parts[1]) {
		return false
	}

	model := strings.TrimSuffix(strings.TrimPrefix(requestPath, parts[0]), parts[1])
	return model != "" && !strings.Contains(model, "/")
}

func IsAdvancedCustomConverterAllowed(converter string) bool {
	switch converter {
	case AdvancedCustomConverterNone,
		AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions,
		AdvancedCustomConverterOpenAIChatCompletionsToAnthropicMessages,
		AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses,
		AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions,
		AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions,
		AdvancedCustomConverterOpenAIChatCompletionsToGeminiGenerateContent:
		return true
	default:
		return false
	}
}

func (c *AdvancedCustomConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("advanced_custom is required")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("advanced_custom requires at least one route")
	}

	seenPaths := make(map[string]struct{}, len(c.Routes))
	for i := range c.Routes {
		route := c.Routes[i]
		route.IncomingPath = strings.TrimSpace(route.IncomingPath)
		upstreamPath := strings.TrimSpace(route.UpstreamPath)
		route.Converter = strings.TrimSpace(route.Converter)
		if route.Converter == "" {
			route.Converter = AdvancedCustomConverterNone
		}

		if route.IncomingPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path is required", i)
		}
		if !strings.HasPrefix(route.IncomingPath, "/") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must start with /", i)
		}
		if strings.Contains(route.IncomingPath, "?") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must not include query", i)
		}
		if _, exists := seenPaths[route.IncomingPath]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must be unique: %s", i, route.IncomingPath)
		}
		seenPaths[route.IncomingPath] = struct{}{}

		if upstreamPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path is required", i)
		}
		if err := validateAdvancedCustomUpstreamTarget(i, upstreamPath); err != nil {
			return err
		}

		if !IsAdvancedCustomConverterAllowed(route.Converter) {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].converter is not registered: %s", i, route.Converter)
		}
		if err := validateAdvancedCustomConverterPath(i, route.IncomingPath, route.Converter); err != nil {
			return err
		}
		if err := validateAdvancedCustomRouteAuth(i, route.Auth); err != nil {
			return err
		}
	}

	return nil
}

func validateAdvancedCustomUpstreamTarget(index int, upstreamPath string) error {
	if strings.HasPrefix(upstreamPath, "/") {
		if strings.HasPrefix(upstreamPath, "//") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
		}
		return nil
	}

	parsedURL, err := url.Parse(upstreamPath)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must use http or https", index)
	}
	return nil
}

func validateAdvancedCustomConverterPath(index int, incomingPath string, converter string) error {
	switch converter {
	case AdvancedCustomConverterNone:
		return nil
	case AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions:
		if incomingPath == "/v1/messages" {
			return nil
		}
	case AdvancedCustomConverterOpenAIChatCompletionsToAnthropicMessages,
		AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses,
		AdvancedCustomConverterOpenAIChatCompletionsToGeminiGenerateContent:
		if incomingPath == "/v1/chat/completions" {
			return nil
		}
	case AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions:
		if incomingPath == "/v1/responses" {
			return nil
		}
	case AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions:
		if strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent") {
			return nil
		}
	}
	return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
}

func validateAdvancedCustomRouteAuth(index int, auth *AdvancedCustomRouteAuth) error {
	if auth == nil {
		return nil
	}
	authType := strings.TrimSpace(auth.Type)
	switch authType {
	case AdvancedCustomAuthTypeNone:
		return nil
	case AdvancedCustomAuthTypeHeader, AdvancedCustomAuthTypeQuery:
		if strings.TrimSpace(auth.Name) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.name is required", index)
		}
		if strings.TrimSpace(auth.Value) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.value is required", index)
		}
		return nil
	default:
		return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.type is invalid: %s", index, auth.Type)
	}
}

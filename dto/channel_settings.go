package dto

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
	AzureResponsesVersion                 string              `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType       `json:"vertex_key_type,omitempty"`
	OpenRouterEnterprise                  *bool               `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                `json:"claude_beta_query,omitempty"`
	AllowServiceTier                      bool                `json:"allow_service_tier,omitempty"`
	AllowInferenceGeo                     bool                `json:"allow_inference_geo,omitempty"`
	AllowSpeed                            bool                `json:"allow_speed,omitempty"`
	AllowSafetyIdentifier                 bool                `json:"allow_safety_identifier,omitempty"`
	DisableStore                          bool                `json:"disable_store,omitempty"`
	AllowIncludeObfuscation               bool                `json:"allow_include_obfuscation,omitempty"`
	AwsKeyType                            AwsKeyType          `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled    bool                `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateLastCheckTime      int64               `json:"upstream_model_update_last_check_time,omitempty"`
	UpstreamModelUpdateLastDetectedModels []string            `json:"upstream_model_update_last_detected_models,omitempty"`
	UpstreamModelUpdateLastRemovedModels  []string            `json:"upstream_model_update_last_removed_models,omitempty"`
	UpstreamModelUpdateIgnoredModels      []string            `json:"upstream_model_update_ignored_models,omitempty"`
	TaskProtocol                          string              `json:"task_protocol,omitempty"`
	TaskProtocolConfig                    *TaskProtocolConfig `json:"task_protocol_config,omitempty"`
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

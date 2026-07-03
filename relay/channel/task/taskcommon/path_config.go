package taskcommon

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
)

const taskProtocolConfigBodyKey = "_task_protocol_config"

var taskQueryURLPlaceholders = []string{"{task_id}", "{operation_name}", "{upstream_task_id}"}

// ValidateTaskProtocolSettings 校验渠道 settings JSON 中的任务协议配置。
// 仅在配置存在时校验，不解析失败的 JSON 交由渠道其他校验处理。
func ValidateTaskProtocolSettings(otherSettings string) error {
	if strings.TrimSpace(otherSettings) == "" {
		return nil
	}
	settings := dto.ChannelOtherSettings{}
	if err := common.UnmarshalJsonStr(otherSettings, &settings); err != nil {
		return nil
	}
	if strings.TrimSpace(settings.TaskProtocol) != "" && !UseConfiguredTaskProtocol(settings) {
		return fmt.Errorf("task_protocol must be %s", TaskProtocolGenericVideo)
	}
	cfg := settings.TaskProtocolConfig
	if cfg == nil {
		return nil
	}
	queryPath := strings.TrimSpace(cfg.QueryPath)
	if queryPath == "" {
		return nil
	}
	for _, placeholder := range taskQueryURLPlaceholders {
		if strings.Contains(queryPath, placeholder) {
			return nil
		}
	}
	return fmt.Errorf("task_protocol_config.query_path must include one of %s", strings.Join(taskQueryURLPlaceholders, ", "))
}

func WithTaskProtocolConfig(body map[string]any, settings dto.ChannelOtherSettings) map[string]any {
	if body == nil {
		body = map[string]any{}
	}
	if UseConfiguredTaskProtocol(settings) {
		body[taskProtocolConfigBodyKey] = EffectiveTaskProtocolConfig(settings)
	} else if settings.TaskProtocolConfig != nil {
		body[taskProtocolConfigBodyKey] = settings.TaskProtocolConfig
	}
	return body
}

func StripTaskProtocolConfig(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}
	clean := make(map[string]any, len(body))
	for key, value := range body {
		if key == taskProtocolConfigBodyKey {
			continue
		}
		clean[key] = value
	}
	return clean
}

func BuildTaskSubmitURL(info *relaycommon.RelayInfo, fallback string) string {
	if info == nil || info.ChannelMeta == nil || !HasTaskProtocolConfig(info.ChannelOtherSettings) {
		return fallback
	}
	path := ""
	if UseConfiguredTaskProtocol(info.ChannelOtherSettings) {
		cfg := EffectiveTaskProtocolConfig(info.ChannelOtherSettings)
		path = cfg.SubmitPath
	} else if info.ChannelOtherSettings.TaskProtocolConfig != nil {
		path = info.ChannelOtherSettings.TaskProtocolConfig.SubmitPath
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fallback
	}
	return BuildConfiguredTaskURL(info.ChannelBaseUrl, path, submitURLValues(info))
}

func BuildTaskQueryURL(baseURL string, body map[string]any, fallback string) string {
	cfg := taskProtocolConfigFromBody(body)
	if cfg == nil {
		return fallback
	}
	path := strings.TrimSpace(cfg.QueryPath)
	if path == "" {
		return fallback
	}
	return BuildConfiguredTaskURL(baseURL, path, queryURLValues(body))
}

func BuildConfiguredTaskURL(baseURL string, path string, values map[string]string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return strings.TrimRight(baseURL, "/")
	}
	path = replaceTaskURLValues(path, values)
	if isAbsoluteURL(path) {
		return path
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasPrefix(path, "?") {
		return baseURL + path
	}
	path = trimDuplicatedBasePath(baseURL, path)
	if path == "" {
		return baseURL
	}
	return baseURL + "/" + strings.TrimLeft(path, "/")
}

func trimDuplicatedBasePath(baseURL string, path string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimLeft(path, "/")
	}
	basePath := strings.Trim(parsed.EscapedPath(), "/")
	if basePath == "" {
		return strings.TrimLeft(path, "/")
	}
	path = strings.TrimLeft(path, "/")
	if path == basePath {
		return ""
	}
	if strings.HasPrefix(path, basePath+"/") {
		return strings.TrimPrefix(path, basePath+"/")
	}
	return path
}

func taskProtocolConfigFromBody(body map[string]any) *dto.TaskProtocolConfig {
	if body == nil {
		return nil
	}
	switch v := body[taskProtocolConfigBodyKey].(type) {
	case *dto.TaskProtocolConfig:
		return v
	case dto.TaskProtocolConfig:
		return &v
	default:
		return nil
	}
}

func submitURLValues(info *relaycommon.RelayInfo) map[string]string {
	values := map[string]string{}
	if info == nil || info.ChannelMeta == nil {
		return values
	}
	values["model"] = info.UpstreamModelName
	values["upstream_model"] = info.UpstreamModelName
	values["origin_model"] = info.OriginModelName
	if info.TaskRelayInfo != nil {
		values["action"] = string(info.Action)
		values["origin_task_id"] = info.OriginTaskID
		values["task_id"] = info.OriginTaskID
		values["public_task_id"] = info.PublicTaskID
	}
	return values
}

func queryURLValues(body map[string]any) map[string]string {
	values := map[string]string{}
	if body == nil {
		return values
	}
	if taskID, ok := body["task_id"].(string); ok {
		values["task_id"] = taskID
		values["upstream_task_id"] = taskID
		if decoded, err := DecodeLocalTaskID(taskID); err == nil && decoded != "" {
			values["operation_name"] = decoded
			values["upstream_task_id"] = decoded
		}
	}
	if action, ok := body["action"].(string); ok {
		values["action"] = action
	}
	if model, ok := body["model"].(string); ok {
		values["model"] = model
		values["upstream_model"] = model
	}
	return values
}

func replaceTaskURLValues(path string, values map[string]string) string {
	main, fragment, hasFragment := strings.Cut(path, "#")
	pathPart, queryPart, hasQuery := strings.Cut(main, "?")
	pathPart = replaceTaskURLValuesInPart(pathPart, values, false)
	if hasQuery {
		queryPart = replaceTaskURLValuesInPart(queryPart, values, true)
		main = pathPart + "?" + queryPart
	} else {
		main = pathPart
	}
	if hasFragment {
		return main + "#" + replaceTaskURLValuesInPart(fragment, values, true)
	}
	return main
}

func replaceTaskURLValuesInPart(path string, values map[string]string, query bool) string {
	for key, value := range values {
		path = strings.ReplaceAll(path, "{"+key+"}", escapeTaskURLValue(key, value, query))
	}
	return path
}

func escapeTaskURLValue(key string, value string, query bool) string {
	if query {
		return url.QueryEscape(value)
	}
	if key == "operation_name" {
		return pathEscapePreservingSlash(value)
	}
	return url.PathEscape(value)
}

func pathEscapePreservingSlash(value string) string {
	segments := strings.Split(value, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return strings.Join(segments, "/")
}

func isAbsoluteURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme != "" && u.Host != ""
}

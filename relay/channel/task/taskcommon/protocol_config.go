package taskcommon

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	TaskProtocolGenericVideo = "generic_video_task"
)

var videoTaskChannelTypes = map[int]struct{}{
	constant.ChannelTypeOpenAI:      {},
	constant.ChannelTypeAli:         {},
	constant.ChannelTypeGemini:      {},
	constant.ChannelTypeMiniMax:     {},
	constant.ChannelTypeVertexAi:    {},
	constant.ChannelTypeVolcEngine:  {},
	constant.ChannelTypeKling:       {},
	constant.ChannelTypeJimeng:      {},
	constant.ChannelTypeVidu:        {},
	constant.ChannelTypeDoubaoVideo: {},
	constant.ChannelTypeSora:        {},
}

func HasTaskProtocolConfig(settings dto.ChannelOtherSettings) bool {
	return settings.TaskProtocolConfig != nil || UseConfiguredTaskProtocol(settings)
}

func UseConfiguredTaskProtocol(settings dto.ChannelOtherSettings) bool {
	protocol := strings.ToLower(strings.TrimSpace(settings.TaskProtocol))
	return protocol == TaskProtocolGenericVideo
}

func IsVideoTaskChannelType(channelType int) bool {
	_, ok := videoTaskChannelTypes[channelType]
	return ok
}

func NormalizeTaskProtocolConfig(input *dto.TaskProtocolConfig) dto.TaskProtocolConfig {
	cfg := dto.TaskProtocolConfig{
		SubmitPath:     "/v1/videos/create",
		QueryPath:      "/v1/videos/{task_id}",
		TaskIDPath:     "task_id",
		StatusPath:     "status",
		ProgressPath:   "progress",
		ResultURLPaths: []string{"result.primary_url", "result.urls.0", "result.url", "result.video_url", "result.output_url", "metadata.url", "metadata.video_url", "metadata.output_url", "data.result.primary_url", "data.result.urls.0", "data.result.url", "data.result.video_url", "data.result.output_url", "data.metadata.url", "data.metadata.video_url", "data.metadata.output_url", "url", "video_url", "output_url", "file_url", "download_url", "result"},
		CreatedAtPath:  "created_at",
		UpdatedAtPath:  "updated_at",
		StatusMap: map[string]string{
			"queued":      string(model.TaskStatusQueued),
			"pending":     string(model.TaskStatusQueued),
			"submitted":   string(model.TaskStatusSubmitted),
			"created":     string(model.TaskStatusSubmitted),
			"running":     string(model.TaskStatusInProgress),
			"processing":  string(model.TaskStatusInProgress),
			"in_progress": string(model.TaskStatusInProgress),
			"succeeded":   string(model.TaskStatusSuccess),
			"success":     string(model.TaskStatusSuccess),
			"completed":   string(model.TaskStatusSuccess),
			"complete":    string(model.TaskStatusSuccess),
			"failed":      string(model.TaskStatusFailure),
			"failure":     string(model.TaskStatusFailure),
			"error":       string(model.TaskStatusFailure),
		},
		ErrorMessagePath: "error_message",
	}
	if input == nil {
		return cfg
	}
	if input.SubmitPath != "" {
		cfg.SubmitPath = input.SubmitPath
	}
	if input.QueryPath != "" {
		cfg.QueryPath = input.QueryPath
	}
	if input.TaskIDPath != "" {
		cfg.TaskIDPath = input.TaskIDPath
	}
	if input.StatusPath != "" {
		cfg.StatusPath = input.StatusPath
	}
	if input.ProgressPath != "" {
		cfg.ProgressPath = input.ProgressPath
	}
	if len(input.ResultURLPaths) > 0 {
		cfg.ResultURLPaths = input.ResultURLPaths
	}
	if input.ErrorMessagePath != "" {
		cfg.ErrorMessagePath = input.ErrorMessagePath
	}
	if input.CreatedAtPath != "" {
		cfg.CreatedAtPath = input.CreatedAtPath
	}
	if input.UpdatedAtPath != "" {
		cfg.UpdatedAtPath = input.UpdatedAtPath
	}
	for k, v := range input.StatusMap {
		cfg.StatusMap[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return cfg
}

func EffectiveTaskProtocolConfig(settings dto.ChannelOtherSettings) dto.TaskProtocolConfig {
	return NormalizeTaskProtocolConfig(settings.TaskProtocolConfig)
}

func TryHandleConfiguredSubmitResponse(c *gin.Context, responseBody []byte, info *relaycommon.RelayInfo) (string, bool, *dto.TaskError) {
	if info == nil || info.ChannelMeta == nil || !UseConfiguredTaskProtocol(info.ChannelOtherSettings) {
		return "", false, nil
	}
	cfg := EffectiveTaskProtocolConfig(info.ChannelOtherSettings)
	taskID := StringFromGJSONPath(responseBody, cfg.TaskIDPath)
	if taskID == "" {
		taskID = StringFromGJSONPath(responseBody, "id")
	}
	if taskID == "" {
		return "", false, nil
	}

	// kling 官方路由的客户端使用 kling 官方响应格式，上游解析仍走配置协议
	if c != nil && c.GetBool("kling_official_route") {
		now := time.Now().Unix()
		c.JSON(http.StatusOK, gin.H{
			"code":       0,
			"message":    "",
			"task_id":    info.PublicTaskID,
			"request_id": info.PublicTaskID,
			"data": gin.H{
				"task_id":     info.PublicTaskID,
				"task_status": "submitted",
				"created_at":  now,
				"updated_at":  now,
			},
		})
		return taskID, true, nil
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	ov.Status = dto.VideoStatusQueued
	c.JSON(http.StatusOK, ov)
	return taskID, true, nil
}

func ParseConfiguredTaskResult(respBody []byte, settings dto.ChannelOtherSettings) (*relaycommon.TaskInfo, bool, error) {
	if !UseConfiguredTaskProtocol(settings) {
		return nil, false, nil
	}
	cfg := EffectiveTaskProtocolConfig(settings)
	taskID := StringFromGJSONPath(respBody, cfg.TaskIDPath)
	statusRaw := StringFromGJSONPath(respBody, cfg.StatusPath)
	progressRaw := StringFromGJSONPath(respBody, cfg.ProgressPath)
	resultURL := ExtractConfiguredResultURL(respBody, cfg.ResultURLPaths)
	reason := StringFromGJSONPath(respBody, cfg.ErrorMessagePath)

	if taskID == "" && statusRaw == "" && progressRaw == "" && resultURL == "" && reason == "" {
		return nil, false, nil
	}

	status := MapConfiguredTaskStatus(statusRaw, cfg)
	if statusRaw == "" {
		status = inferConfiguredTaskStatus(resultURL, reason)
	}
	if resultURL != "" && status != string(model.TaskStatusFailure) {
		status = string(model.TaskStatusSuccess)
	}
	progress := NormalizeConfiguredProgress(progressRaw, status)

	return &relaycommon.TaskInfo{
		Code:     0,
		TaskID:   taskID,
		Status:   status,
		Progress: progress,
		Url:      resultURL,
		Reason:   reason,
	}, true, nil
}

func ConvertConfiguredTaskToOpenAIVideo(originTask *model.Task) ([]byte, bool, error) {
	cfg, ok := TaskProtocolConfigFromTask(originTask)
	if !ok {
		return nil, false, nil
	}
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	urlValue := originTask.GetResultURL()
	if urlValue == "" {
		urlValue = ExtractConfiguredResultURL(originTask.Data, cfg.ResultURLPaths)
	}
	openAIVideo.SetMetadata("url", urlValue)
	openAIVideo.CreatedAt = firstTimestamp(originTask.CreatedAt, TimestampFromGJSONPath(originTask.Data, cfg.CreatedAtPath))
	openAIVideo.CompletedAt = firstTimestamp(originTask.UpdatedAt, TimestampFromGJSONPath(originTask.Data, cfg.UpdatedAtPath))
	openAIVideo.Model = originTask.Properties.OriginModelName

	if originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, StringFromGJSONPath(originTask.Data, cfg.ErrorMessagePath)),
			Code:    "upstream_error",
		}
	}
	body, err := common.Marshal(openAIVideo)
	return body, true, err
}

func TaskProtocolConfigFromTask(task *model.Task) (dto.TaskProtocolConfig, bool) {
	if task == nil || task.ChannelId == 0 {
		return dto.TaskProtocolConfig{}, false
	}
	ch, err := model.GetChannelById(task.ChannelId, true)
	if err != nil || ch == nil {
		return dto.TaskProtocolConfig{}, false
	}
	settings := ch.GetOtherSettings()
	if !UseConfiguredTaskProtocol(settings) {
		return dto.TaskProtocolConfig{}, false
	}
	return EffectiveTaskProtocolConfig(settings), true
}

func StringFromGJSONPath(data []byte, path string) string {
	if path == "" {
		return ""
	}
	result := gjson.GetBytes(data, path)
	if !result.Exists() {
		return ""
	}
	if result.IsObject() || result.IsArray() {
		return result.Raw
	}
	return strings.TrimSpace(result.String())
}

func ExtractConfiguredResultURL(data []byte, paths []string) string {
	for _, path := range paths {
		result := gjson.GetBytes(data, path)
		if !result.Exists() {
			continue
		}
		if result.IsObject() {
			if urlValue := firstGJSONString(result.Raw, "primary_url", "url", "video_url", "output_url", "file_url", "download_url", "signed_url"); urlValue != "" {
				return urlValue
			}
			continue
		}
		if result.IsArray() {
			for _, item := range result.Array() {
				if urlValue := resultURLFromGJSON(item); urlValue != "" {
					return urlValue
				}
			}
			continue
		}
		if urlValue := resultURLFromString(result.String()); urlValue != "" {
			return urlValue
		}
	}
	return ""
}

func MapConfiguredTaskStatus(status string, cfg dto.TaskProtocolConfig) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if mapped, ok := cfg.StatusMap[normalized]; ok {
		return NormalizeInternalTaskStatus(mapped)
	}
	return NormalizeInternalTaskStatus(normalized)
}

func NormalizeInternalTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted", "created":
		return string(model.TaskStatusSubmitted)
	case "queued", "pending":
		return string(model.TaskStatusQueued)
	case "in_progress", "running", "processing":
		return string(model.TaskStatusInProgress)
	case "success", "succeeded", "completed", "complete":
		return string(model.TaskStatusSuccess)
	case "failure", "failed", "error":
		return string(model.TaskStatusFailure)
	default:
		return string(model.TaskStatusInProgress)
	}
}

func NormalizeConfiguredProgress(progress string, status string) string {
	progress = strings.TrimSpace(progress)
	if progress == "" {
		switch status {
		case string(model.TaskStatusSuccess), string(model.TaskStatusFailure):
			return "100%"
		case string(model.TaskStatusQueued), string(model.TaskStatusSubmitted):
			return "10%"
		default:
			return "50%"
		}
	}
	if strings.HasSuffix(progress, "%") {
		return progress
	}
	return progress + "%"
}

func TimestampFromGJSONPath(data []byte, path string) int64 {
	value := StringFromGJSONPath(data, path)
	if value == "" {
		return 0
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	return 0
}

func ConfiguredTaskErrorWrapper(err error, code string, statusCode int) *dto.TaskError {
	return &dto.TaskError{
		Code:       code,
		Message:    err.Error(),
		StatusCode: statusCode,
		Error:      err,
	}
}

func inferConfiguredTaskStatus(resultURL string, reason string) string {
	if strings.TrimSpace(reason) != "" {
		return string(model.TaskStatusFailure)
	}
	if strings.TrimSpace(resultURL) != "" {
		return string(model.TaskStatusSuccess)
	}
	return string(model.TaskStatusInProgress)
}

func resultURLFromGJSON(result gjson.Result) string {
	if result.IsObject() {
		return firstGJSONString(result.Raw, "primary_url", "url", "video_url", "output_url", "file_url", "download_url", "signed_url")
	}
	return resultURLFromString(result.String())
}

func resultURLFromString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if common.IsJsonObject(value) {
		return firstGJSONString(value, "primary_url", "url", "video_url", "output_url", "file_url", "download_url", "signed_url")
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "data:") {
		return value
	}
	return ""
}

func firstGJSONString(jsonText string, paths ...string) string {
	for _, path := range paths {
		value := strings.TrimSpace(gjson.Get(jsonText, path).String())
		if value != "" {
			return value
		}
	}
	return ""
}

func firstTimestamp(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func MissingConfiguredTaskIDError() *dto.TaskError {
	return ConfiguredTaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
}

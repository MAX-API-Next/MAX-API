package doubao

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	taskProtocolSeedanceOfficialMedia = "seedance_official_media"
	seedanceMediaRawRequestKey        = "doubao_seedance_media_raw_request"
)

type seedanceMediaRequest struct {
	Model           string
	Prompt          string
	AspectRatio     string
	Capability      string
	ControlMode     string
	DurationSeconds *int
	EndImage        string
	Image           string
	InputMode       string
	ReferenceImages []string
	Resolution      string
	WithAudio       *bool
	Extra           map[string]interface{}
}

func (r *seedanceMediaRequest) ToMap() map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range r.Extra {
		out[k] = v
	}
	setString(out, "model", r.Model)
	setString(out, "prompt", r.Prompt)
	setString(out, "aspect_ratio", r.AspectRatio)
	setString(out, "capability", r.Capability)
	setString(out, "control_mode", r.ControlMode)
	setString(out, "end_image", r.EndImage)
	setString(out, "image", r.Image)
	setString(out, "input_mode", r.InputMode)
	setString(out, "resolution", r.Resolution)
	if r.DurationSeconds != nil {
		out["duration_seconds"] = *r.DurationSeconds
	}
	if len(r.ReferenceImages) > 0 {
		out["reference_images"] = r.ReferenceImages
	}
	if r.WithAudio != nil {
		out["with_audio"] = *r.WithAudio
	}
	return out
}

func normalizeSeedanceMediaProtocolConfig(input *dto.TaskProtocolConfig) dto.TaskProtocolConfig {
	return taskcommon.NormalizeTaskProtocolConfig(input)
}

func (a *TaskAdaptor) useSeedanceMediaProtocol() bool {
	return strings.EqualFold(strings.TrimSpace(a.taskProtocol), taskProtocolSeedanceOfficialMedia)
}

func (a *TaskAdaptor) seedanceMediaConfig() dto.TaskProtocolConfig {
	return normalizeSeedanceMediaProtocolConfig(&a.taskProtocolConfig)
}

func (a *TaskAdaptor) captureSeedanceMediaRequest(c *gin.Context) error {
	var raw map[string]interface{}
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return err
	}
	c.Set(seedanceMediaRawRequestKey, raw)
	return nil
}

func (a *TaskAdaptor) validateSeedanceMediaTaskRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		req.Images = []string{req.Image}
	}
	if len(req.Images) == 0 && len(req.ReferenceImages) > 0 {
		req.Images = append([]string{}, req.ReferenceImages...)
	}
	if err := a.captureSeedanceMediaRequest(c); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, req)
	return nil
}

func (a *TaskAdaptor) convertToSeedanceMediaRequest(c *gin.Context, req *relaycommon.TaskSubmitReq) (*seedanceMediaRequest, error) {
	payload, err := a.convertToGenericMediaRequest(req)
	if err != nil {
		return nil, err
	}
	raw := getSeedanceMediaRawRequest(c)
	applySeedanceMediaRawFields(payload, raw)
	inferSeedanceMediaModes(payload)
	return payload, nil
}

func (a *TaskAdaptor) convertToGenericMediaRequest(req *relaycommon.TaskSubmitReq) (*seedanceMediaRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	payload := &seedanceMediaRequest{
		Model:           req.Model,
		Prompt:          req.Prompt,
		AspectRatio:     req.AspectRatio,
		Capability:      firstNonEmpty(req.Capability, "video_generation"),
		ControlMode:     req.ControlMode,
		DurationSeconds: req.DurationSeconds,
		EndImage:        req.EndImage,
		Image:           firstNonEmpty(req.Image, firstString(req.Images)),
		InputMode:       req.InputMode,
		ReferenceImages: append([]string{}, req.ReferenceImages...),
		Resolution:      req.Resolution,
		WithAudio:       req.WithAudio,
		Extra:           map[string]interface{}{},
	}
	if payload.DurationSeconds == nil {
		if sec, ok := parsePositiveOrAutoSeconds(req.Seconds); ok {
			payload.DurationSeconds = &sec
		} else if req.Duration != 0 {
			sec := req.Duration
			payload.DurationSeconds = &sec
		}
	}
	applySeedanceMediaMetadata(payload, req.Metadata)
	inferSeedanceMediaModes(payload)
	return payload, nil
}

func applySeedanceMediaRawFields(payload *seedanceMediaRequest, raw map[string]interface{}) {
	if payload == nil || raw == nil {
		return
	}
	setPayloadString(raw, "model", &payload.Model)
	setPayloadString(raw, "prompt", &payload.Prompt)
	setPayloadString(raw, "aspect_ratio", &payload.AspectRatio)
	setPayloadString(raw, "capability", &payload.Capability)
	setPayloadString(raw, "control_mode", &payload.ControlMode)
	setPayloadString(raw, "end_image", &payload.EndImage)
	setPayloadString(raw, "image", &payload.Image)
	setPayloadString(raw, "input_mode", &payload.InputMode)
	setPayloadString(raw, "resolution", &payload.Resolution)
	if v, ok := raw["duration_seconds"]; ok {
		if sec, exists := intFromAny(v); exists {
			payload.DurationSeconds = &sec
		}
	}
	if v, ok := raw["with_audio"]; ok {
		if b, exists := boolFromAny(v); exists {
			payload.WithAudio = &b
		}
	}
	if refs, exists := stringSliceFromAny(raw["reference_images"]); exists {
		payload.ReferenceImages = refs
	}
}

func applySeedanceMediaMetadata(payload *seedanceMediaRequest, metadata map[string]interface{}) {
	if payload == nil || metadata == nil {
		return
	}
	for k, v := range metadata {
		switch k {
		case "model":
			setStringValue(v, &payload.Model)
		case "prompt":
			setStringValue(v, &payload.Prompt)
		case "aspect_ratio", "ratio":
			setStringValue(v, &payload.AspectRatio)
		case "capability":
			setStringValue(v, &payload.Capability)
		case "control_mode":
			setStringValue(v, &payload.ControlMode)
		case "duration_seconds":
			if sec, exists := intFromAny(v); exists {
				payload.DurationSeconds = &sec
			}
		case "end_image", "image_tail", "tail_image":
			setStringValue(v, &payload.EndImage)
		case "image":
			setStringValue(v, &payload.Image)
		case "input_mode":
			setStringValue(v, &payload.InputMode)
		case "reference_images":
			if refs, exists := stringSliceFromAny(v); exists {
				payload.ReferenceImages = refs
			}
		case "resolution":
			setStringValue(v, &payload.Resolution)
		case "with_audio", "generate_audio":
			if b, exists := boolFromAny(v); exists {
				payload.WithAudio = &b
			}
		default:
			payload.Extra[k] = v
		}
	}
}

func inferSeedanceMediaModes(payload *seedanceMediaRequest) {
	if payload == nil {
		return
	}
	if payload.Capability == "" {
		payload.Capability = "video_generation"
	}
	if payload.InputMode == "" {
		switch {
		case len(payload.ReferenceImages) > 0:
			payload.InputMode = "multi_image"
		case payload.Image != "":
			payload.InputMode = "single_image"
		default:
			payload.InputMode = "text"
		}
	}
	if payload.ControlMode == "" {
		switch {
		case payload.InputMode == "multi_image":
			payload.ControlMode = "reference"
		case payload.EndImage != "":
			payload.ControlMode = "end_frame"
		default:
			payload.ControlMode = "none"
		}
	}
}

func (a *TaskAdaptor) doSeedanceMediaResponse(c *gin.Context, responseBody []byte, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	if taskID, handled, taskErr := taskcommon.TryHandleConfiguredSubmitResponse(c, responseBody, info); handled || taskErr != nil {
		return taskID, responseBody, taskErr
	}
	return "", nil, taskcommon.MissingConfiguredTaskIDError()
}

func (a *TaskAdaptor) seedanceMediaQueryURL(baseURL, taskID string) string {
	cfg := a.seedanceMediaConfig()
	path := strings.ReplaceAll(cfg.QueryPath, "{task_id}", url.PathEscape(taskID))
	return buildSeedanceMediaURL(baseURL, path)
}

func (a *TaskAdaptor) parseSeedanceMediaTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	cfg := a.seedanceMediaConfig()
	result, ok, err := taskcommon.ParseConfiguredTaskResult(respBody, dto.ChannelOtherSettings{
		TaskProtocol:       taskcommon.TaskProtocolLegacySeedanceMedia,
		TaskProtocolConfig: &cfg,
	})
	if err != nil {
		return nil, err
	}
	if ok {
		return result, nil
	}
	return &relaycommon.TaskInfo{
		Code:     0,
		Status:   string(model.TaskStatusInProgress),
		Progress: taskcommon.ProgressInProgress,
	}, nil
}

func (a *TaskAdaptor) convertSeedanceMediaToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	if body, ok, err := taskcommon.ConvertConfiguredTaskToOpenAIVideo(originTask); ok || err != nil {
		return body, err
	}
	cfg := a.seedanceMediaConfig()
	if !a.useSeedanceMediaProtocol() {
		cfg = seedanceMediaConfigFromTask(originTask)
	}
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	urlValue := originTask.GetResultURL()
	if urlValue == "" {
		urlValue = extractSeedanceMediaResultURL(originTask.Data, cfg.ResultURLPaths)
	}
	openAIVideo.SetMetadata("url", urlValue)
	openAIVideo.CreatedAt = firstTimestamp(originTask.CreatedAt, timestampFromGJSONPath(originTask.Data, cfg.CreatedAtPath))
	openAIVideo.CompletedAt = firstTimestamp(originTask.UpdatedAt, timestampFromGJSONPath(originTask.Data, cfg.UpdatedAtPath))
	openAIVideo.Model = originTask.Properties.OriginModelName

	if originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, stringFromGJSONPath(originTask.Data, cfg.ErrorMessagePath)),
			Code:    "upstream_error",
		}
	}
	return common.Marshal(openAIVideo)
}

func seedanceMediaConfigFromTask(task *model.Task) dto.TaskProtocolConfig {
	if cfg, ok := taskcommon.TaskProtocolConfigFromTask(task); ok {
		return cfg
	}
	cfg := normalizeSeedanceMediaProtocolConfig(nil)
	if task == nil || task.ChannelId == 0 {
		return cfg
	}
	ch, err := model.GetChannelById(task.ChannelId, true)
	if err != nil || ch == nil {
		return cfg
	}
	settings := ch.GetOtherSettings()
	if strings.EqualFold(settings.TaskProtocol, taskProtocolSeedanceOfficialMedia) {
		return normalizeSeedanceMediaProtocolConfig(settings.TaskProtocolConfig)
	}
	return cfg
}

func mapSeedanceMediaStatus(status string, cfg dto.TaskProtocolConfig) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if mapped, ok := cfg.StatusMap[normalized]; ok {
		return normalizeInternalTaskStatus(mapped)
	}
	return normalizeInternalTaskStatus(normalized)
}

func normalizeInternalTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted":
		return string(model.TaskStatusSubmitted)
	case "queued", "pending":
		return string(model.TaskStatusQueued)
	case "in_progress", "running", "processing":
		return string(model.TaskStatusInProgress)
	case "success", "succeeded", "completed":
		return string(model.TaskStatusSuccess)
	case "failure", "failed":
		return string(model.TaskStatusFailure)
	default:
		return string(model.TaskStatusInProgress)
	}
}

func looksLikeSeedanceMediaTask(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return gjson.GetBytes(data, "task_id").Exists() &&
		(gjson.GetBytes(data, "status").Exists() || gjson.GetBytes(data, "object").String() == "media.task")
}

func buildSeedanceMediaURL(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	baseURL = strings.TrimRight(baseURL, "/")
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return baseURL
	}
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Path != "" {
		basePath := strings.Trim(parsed.Path, "/")
		if path == basePath {
			path = ""
		} else if strings.HasPrefix(path, basePath+"/") {
			path = strings.TrimPrefix(path, basePath+"/")
		}
	}
	if path == "" {
		return baseURL
	}
	return baseURL + "/" + path
}

func getSeedanceMediaRawRequest(c *gin.Context) map[string]interface{} {
	if c == nil {
		return nil
	}
	v, ok := c.Get(seedanceMediaRawRequestKey)
	if !ok {
		return nil
	}
	raw, _ := v.(map[string]interface{})
	return raw
}

func stringFromGJSONPath(data []byte, path string) string {
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

func extractSeedanceMediaResultURL(data []byte, paths []string) string {
	for _, path := range paths {
		result := gjson.GetBytes(data, path)
		if !result.Exists() {
			continue
		}
		if result.IsObject() {
			if urlValue := firstGJSONString(result.Raw, "url", "video_url", "output_url"); urlValue != "" {
				return urlValue
			}
			continue
		}
		value := strings.TrimSpace(result.String())
		if value == "" {
			continue
		}
		if common.IsJsonObject(value) {
			if urlValue := firstGJSONString(value, "url", "video_url", "output_url"); urlValue != "" {
				return urlValue
			}
			continue
		}
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "data:") {
			return value
		}
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

func normalizeMediaProgress(progress string, status string) string {
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

func timestampFromGJSONPath(data []byte, path string) int64 {
	value := stringFromGJSONPath(data, path)
	if value == "" {
		return 0
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	return 0
}

func firstTimestamp(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parsePositiveOrAutoSeconds(value string) (int, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	sec, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return sec, sec > 0 || sec == -1
}

func intFromAny(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	default:
		return 0, false
	}
}

func boolFromAny(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		return b, err == nil
	default:
		return false, false
	}
}

func stringSliceFromAny(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(common.Interface2String(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func setPayloadString(raw map[string]interface{}, key string, target *string) {
	if raw == nil || target == nil {
		return
	}
	if value, ok := raw[key]; ok {
		setStringValue(value, target)
	}
}

func setStringValue(value interface{}, target *string) {
	if target == nil {
		return
	}
	if s, ok := value.(string); ok {
		*target = strings.TrimSpace(s)
	}
}

func setString(out map[string]interface{}, key string, value string) {
	if strings.TrimSpace(value) != "" {
		out[key] = value
	}
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

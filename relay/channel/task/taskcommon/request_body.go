package taskcommon

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"

	"github.com/gin-gonic/gin"
)

type configuredMediaRequest struct {
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
	Extra           map[string]any
}

func (r *configuredMediaRequest) ToMap() map[string]any {
	out := map[string]any{}
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

func BuildConfiguredTaskRequestBody(c *gin.Context, info *relaycommon.RelayInfo, req *relaycommon.TaskSubmitReq) (io.Reader, bool, error) {
	if info == nil || info.ChannelMeta == nil || !UseConfiguredTaskProtocol(info.ChannelOtherSettings) {
		return nil, false, nil
	}
	cfg := EffectiveTaskProtocolConfig(info.ChannelOtherSettings)
	switch cfg.RequestBodyMode {
	case TaskRequestBodyModeAdapter:
		return nil, false, nil
	case TaskRequestBodyModePassThrough:
		body, err := buildPassThroughTaskBody(c, info)
		return body, true, err
	case TaskRequestBodyModeFieldMapping:
		body, err := buildFieldMappingTaskBody(c, info, req, cfg.RequestBodyMapping, cfg.RequestBodyDefaults)
		return body, true, err
	case TaskRequestBodyModeMediaGeneration:
		taskReq, err := configuredTaskRequest(c, req)
		if err != nil {
			return nil, true, err
		}
		body, err := buildMediaGenerationTaskBody(c, info, taskReq)
		return body, true, err
	default:
		return nil, false, fmt.Errorf("unsupported task request body mode: %s", cfg.RequestBodyMode)
	}
}

func configuredTaskRequest(c *gin.Context, req *relaycommon.TaskSubmitReq) (*relaycommon.TaskSubmitReq, error) {
	if req != nil {
		return req, nil
	}
	var parsed relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func buildPassThroughTaskBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	var raw map[string]any
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return nil, err
	}
	applyConfiguredModel(raw, info)
	data, err := common.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func buildFieldMappingTaskBody(c *gin.Context, info *relaycommon.RelayInfo, req *relaycommon.TaskSubmitReq, mapping map[string]string, defaults map[string]any) (io.Reader, error) {
	source, err := configuredTaskSource(c, req)
	if err != nil {
		return nil, err
	}
	applyConfiguredModel(source, info)

	out := cloneDefaultRequestBody(defaults)
	for targetPath, sourcePaths := range mapping {
		value, exists := firstMappedSourceValue(source, sourcePaths)
		if !exists {
			continue
		}
		setMappedOutputValue(out, targetPath, value)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("configured task request body mapping produced an empty body")
	}

	data, err := common.Marshal(out)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func buildMediaGenerationTaskBody(c *gin.Context, info *relaycommon.RelayInfo, req *relaycommon.TaskSubmitReq) (io.Reader, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	payload := newConfiguredMediaRequest(req)
	var raw map[string]any
	if err := common.UnmarshalBodyReusable(c, &raw); err == nil {
		applyMediaRawFields(payload, raw)
	}
	inferMediaModes(payload)
	applyConfiguredMediaModel(payload, info)
	data, err := common.Marshal(payload.ToMap())
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func configuredTaskSource(c *gin.Context, req *relaycommon.TaskSubmitReq) (map[string]any, error) {
	source := map[string]any{}
	var raw map[string]any
	err := common.UnmarshalBodyReusable(c, &raw)
	if err != nil && req == nil {
		return nil, err
	}
	for k, v := range raw {
		source[k] = v
	}
	if req != nil {
		enrichTaskSource(source, req)
	}
	return source, nil
}

func enrichTaskSource(source map[string]any, req *relaycommon.TaskSubmitReq) {
	if source == nil || req == nil {
		return
	}
	setSourceIfMissing(source, "prompt", req.Prompt)
	setSourceIfMissing(source, "model", req.Model)
	setSourceIfMissing(source, "mode", req.Mode)
	setSourceIfMissing(source, "image", firstNonEmpty(req.Image, firstString(req.Images)))
	setSourceIfMissing(source, "images", req.Images)
	setSourceIfMissing(source, "size", req.Size)
	if req.Duration != 0 {
		setSourceIfMissing(source, "duration", req.Duration)
	}
	setSourceIfMissing(source, "seconds", req.Seconds)
	setSourceIfMissing(source, "input_reference", req.InputReference)
	setSourceIfMissing(source, "aspect_ratio", req.AspectRatio)
	setSourceIfMissing(source, "capability", req.Capability)
	setSourceIfMissing(source, "control_mode", req.ControlMode)
	if req.DurationSeconds != nil {
		setSourceIfMissing(source, "duration_seconds", *req.DurationSeconds)
	} else if sec, ok := parsePositiveOrAutoSeconds(req.Seconds); ok {
		setSourceIfMissing(source, "duration_seconds", sec)
	} else if req.Duration != 0 {
		setSourceIfMissing(source, "duration_seconds", req.Duration)
	}
	setSourceIfMissing(source, "end_image", req.EndImage)
	setSourceIfMissing(source, "input_mode", req.InputMode)
	setSourceIfMissing(source, "reference_images", req.ReferenceImages)
	setSourceIfMissing(source, "resolution", req.Resolution)
	if req.WithAudio != nil {
		setSourceIfMissing(source, "with_audio", *req.WithAudio)
	}
	if len(req.Metadata) > 0 {
		setSourceIfMissing(source, "metadata", req.Metadata)
	}
}

func setSourceIfMissing(source map[string]any, key string, value any) {
	if source == nil || key == "" {
		return
	}
	if _, exists := source[key]; exists || isEmptySourceValue(value) {
		return
	}
	source[key] = value
}

func isEmptySourceValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		return len(v) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

func cloneDefaultRequestBody(defaults map[string]any) map[string]any {
	out := map[string]any{}
	if len(defaults) == 0 {
		return out
	}
	data, err := common.Marshal(defaults)
	if err != nil {
		for k, v := range defaults {
			out[k] = v
		}
		return out
	}
	if err := common.Unmarshal(data, &out); err != nil {
		for k, v := range defaults {
			out[k] = v
		}
	}
	return out
}

func firstMappedSourceValue(source map[string]any, sourcePaths string) (any, bool) {
	for _, path := range strings.Split(sourcePaths, ",") {
		if value, ok := mappedPathValue(source, strings.TrimSpace(path)); ok {
			return value, true
		}
	}
	return nil, false
}

func mappedPathValue(source any, path string) (any, bool) {
	if strings.TrimSpace(path) == "" {
		return nil, false
	}
	current := source
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func setMappedOutputValue(out map[string]any, path string, value any) {
	path = strings.TrimSpace(path)
	if out == nil || path == "" {
		return
	}
	parts := strings.Split(path, ".")
	current := out
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	leaf := strings.TrimSpace(parts[len(parts)-1])
	if leaf == "" {
		return
	}
	current[leaf] = value
}

func newConfiguredMediaRequest(req *relaycommon.TaskSubmitReq) *configuredMediaRequest {
	payload := &configuredMediaRequest{
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
		Extra:           map[string]any{},
	}
	if payload.DurationSeconds == nil {
		if sec, ok := parsePositiveOrAutoSeconds(req.Seconds); ok {
			payload.DurationSeconds = &sec
		} else if req.Duration != 0 {
			sec := req.Duration
			payload.DurationSeconds = &sec
		}
	}
	applyMediaMetadata(payload, req.Metadata)
	inferMediaModes(payload)
	return payload
}

func applyConfiguredModel(raw map[string]any, info *relaycommon.RelayInfo) {
	if raw == nil || info == nil {
		return
	}
	if info.IsModelMapped {
		raw["model"] = info.UpstreamModelName
		return
	}
	if model, ok := raw["model"].(string); ok && strings.TrimSpace(model) != "" {
		info.UpstreamModelName = strings.TrimSpace(model)
	}
}

func applyConfiguredMediaModel(payload *configuredMediaRequest, info *relaycommon.RelayInfo) {
	if payload == nil || info == nil {
		return
	}
	if info.IsModelMapped {
		payload.Model = info.UpstreamModelName
		return
	}
	if model := strings.TrimSpace(payload.Model); model != "" {
		info.UpstreamModelName = model
	}
}

func applyMediaRawFields(payload *configuredMediaRequest, raw map[string]any) {
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
		} else {
			setPayloadExtra(payload, "duration_seconds", v)
		}
	}
	if v, ok := raw["with_audio"]; ok {
		if b, exists := boolFromAny(v); exists {
			payload.WithAudio = &b
		} else {
			setPayloadExtra(payload, "with_audio", v)
		}
	}
	if v, ok := raw["reference_images"]; ok {
		if refs, exists := stringSliceFromAny(v); exists {
			payload.ReferenceImages = refs
		} else {
			setPayloadExtra(payload, "reference_images", v)
		}
	}
}

func applyMediaMetadata(payload *configuredMediaRequest, metadata map[string]any) {
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
			} else {
				setPayloadExtra(payload, k, v)
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
			} else {
				setPayloadExtra(payload, k, v)
			}
		case "resolution":
			setStringValue(v, &payload.Resolution)
		case "with_audio", "generate_audio":
			if b, exists := boolFromAny(v); exists {
				payload.WithAudio = &b
			} else {
				setPayloadExtra(payload, k, v)
			}
		default:
			payload.Extra[k] = v
		}
	}
}

func inferMediaModes(payload *configuredMediaRequest) {
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

func intFromAny(value any) (int, bool) {
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

func boolFromAny(value any) (bool, bool) {
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

func stringSliceFromAny(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), true
	case []any:
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

func setPayloadString(raw map[string]any, key string, target *string) {
	if raw == nil || target == nil {
		return
	}
	if value, ok := raw[key]; ok {
		setStringValue(value, target)
	}
}

func setPayloadExtra(payload *configuredMediaRequest, key string, value any) {
	if payload == nil || strings.TrimSpace(key) == "" {
		return
	}
	if payload.Extra == nil {
		payload.Extra = map[string]any{}
	}
	payload.Extra[key] = value
}

func setStringValue(value any, target *string) {
	if target == nil {
		return
	}
	if s, ok := value.(string); ok {
		*target = strings.TrimSpace(s)
	}
}

func setString(out map[string]any, key string, value string) {
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

package taskcommon

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"

	"github.com/gin-gonic/gin"
)

var ErrTaskParamOverride = errors.New("task param override failed")

func IsTaskParamOverrideError(err error) bool {
	return errors.Is(err, ErrTaskParamOverride)
}

func BuildConfiguredTaskPassThroughBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, bool, error) {
	if info == nil || info.ChannelMeta == nil || !UseConfiguredTaskProtocol(info.ChannelOtherSettings) ||
		!info.ChannelSetting.PassThroughBodyEnabled || !isJSONRequest(c) {
		return nil, false, nil
	}

	var raw map[string]any
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return nil, true, err
	}
	applyConfiguredModel(raw, info)
	data, err := common.Marshal(raw)
	if err != nil {
		return nil, true, err
	}
	return bytes.NewReader(data), true, nil
}

func ApplyTaskParamOverride(requestBody io.Reader, info *relaycommon.RelayInfo) (io.Reader, error) {
	if requestBody == nil || info == nil || len(info.ParamOverride) == 0 {
		return requestBody, nil
	}
	data, err := ApplyTaskParamOverrideBytes(requestBody, info)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return requestBody, nil
	}
	return bytes.NewReader(data), nil
}

func ApplyTaskParamOverrideBytes(requestBody io.Reader, info *relaycommon.RelayInfo) ([]byte, error) {
	if requestBody == nil || info == nil || len(info.ParamOverride) == 0 {
		return nil, nil
	}
	data, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 || !common.IsJsonObject(string(data)) {
		return data, nil
	}
	data, err = applyTaskParamOverride(data, info)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func SyncTaskRequestContext(c *gin.Context, data []byte) error {
	relaycommon.StoreTaskSubmitRequestBody(c, data)
	if c == nil || c.Request == nil || len(bytes.TrimSpace(data)) == 0 || !common.IsJsonObject(string(data)) {
		return nil
	}
	var raw map[string]any
	if err := common.Unmarshal(data, &raw); err != nil {
		return err
	}
	req := relaycommon.TaskSubmitReq{}
	if existing, err := relaycommon.GetTaskRequest(c); err == nil {
		req = existing
	}
	mergeTaskSubmitReq(&req, raw)
	c.Set("task_request", req)
	return nil
}

func mergeTaskSubmitReq(req *relaycommon.TaskSubmitReq, raw map[string]any) {
	if req == nil || raw == nil {
		return
	}
	if req.Metadata == nil {
		req.Metadata = map[string]interface{}{}
	}
	for key, value := range raw {
		req.Metadata[key] = value
	}

	copyString(raw, "model", &req.Model)
	copyString(raw, "model_name", &req.Model)
	copyString(raw, "prompt", &req.Prompt)
	copyString(raw, "mode", &req.Mode)
	copyString(raw, "image", &req.Image)
	copyString(raw, "image_tail", &req.EndImage)
	copyString(raw, "size", &req.Size)
	copyString(raw, "aspect_ratio", &req.Size)
	copyString(raw, "seconds", &req.Seconds)
	copyString(raw, "input_reference", &req.InputReference)
	copyString(raw, "capability", &req.Capability)
	copyString(raw, "control_mode", &req.ControlMode)
	copyString(raw, "input_mode", &req.InputMode)
	copyString(raw, "resolution", &req.Resolution)
	copyInt(raw, "duration", &req.Duration)
	copyIntPtr(raw, "duration_seconds", &req.DurationSeconds)
	copyBoolPtr(raw, "with_audio", &req.WithAudio)
	copyBoolPtr(raw, "generate_audio", &req.WithAudio)
	if images, ok := stringSliceValue(raw["images"]); ok {
		req.Images = images
	}
	if refs, ok := stringSliceValue(raw["reference_images"]); ok {
		req.ReferenceImages = refs
	}

	if input, ok := mapValue(raw["input"]); ok {
		copyString(input, "prompt", &req.Prompt)
		copyString(input, "img_url", &req.InputReference)
		if req.InputReference == "" {
			copyString(input, "first_frame_url", &req.InputReference)
		}
		copyString(input, "last_frame_url", &req.EndImage)
	}
	if params, ok := mapValue(raw["parameters"]); ok {
		for key, value := range params {
			req.Metadata[key] = value
		}
		copyString(params, "mode", &req.Mode)
		if req.Size == "" {
			if !copyString(params, "aspect_ratio", &req.Size) {
				copyString(params, "aspectRatio", &req.Size)
			}
			if req.Size == "" {
				copyString(params, "size", &req.Size)
			}
		}
		copyString(params, "resolution", &req.Resolution)
		copyInt(params, "duration", &req.Duration)
		copyInt(params, "durationSeconds", &req.Duration)
		copyBoolPtr(params, "audio", &req.WithAudio)
		copyBoolPtr(params, "generateAudio", &req.WithAudio)
	}
	if duration, ok := intValue(raw["duration"]); ok && duration > 0 {
		req.Seconds = strconv.Itoa(duration)
	}
}

func applyTaskParamOverride(data []byte, info *relaycommon.RelayInfo) ([]byte, error) {
	out, err := relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTaskParamOverride, err)
	}
	return out, nil
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

func isJSONRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))), "application/json")
}

func copyString(raw map[string]any, key string, target *string) bool {
	value, ok := stringValue(raw[key])
	if !ok {
		return false
	}
	*target = value
	return true
}

func copyInt(raw map[string]any, key string, target *int) bool {
	value, ok := intValue(raw[key])
	if !ok {
		return false
	}
	*target = value
	return true
}

func copyIntPtr(raw map[string]any, key string, target **int) bool {
	value, ok := intValue(raw[key])
	if !ok {
		return false
	}
	*target = &value
	return true
}

func copyBoolPtr(raw map[string]any, key string, target **bool) bool {
	value, ok := boolValue(raw[key])
	if !ok {
		return false
	}
	*target = &value
	return true
}

func stringValue(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	default:
		return "", false
	}
}

func intValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v == float64(int(v)) {
			return int(v), true
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func boolValue(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on", "enable", "enabled":
			return true, true
		case "false", "0", "no", "off", "disable", "disabled":
			return false, true
		}
	}
	return false, false
}

func stringSliceValue(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := stringValue(item); ok {
			result = append(result, s)
		}
	}
	return result, true
}

func mapValue(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	default:
		return nil, false
	}
}

package taskcommon

import (
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

func EstimateGenericTaskBilling(c *gin.Context, info *relaycommon.RelayInfo, platform string) (*types.TaskBillingResult, error) {
	req := relaycommon.TaskSubmitReq{}
	if parsed, err := relaycommon.GetTaskRequest(c); err == nil {
		req = parsed
	}
	raw := genericBillingRawBody(c)
	input := types.TaskBillingInput{
		Model:         genericBillingOriginModel(info, req),
		UpstreamModel: genericBillingUpstreamModel(info, req, raw),
		Action:        genericBillingAction(info),
		Platform:      platform,
	}
	if !task_billing_setting.HasRateCard(input.Model, input.UpstreamModel) {
		return nil, nil
	}

	fields := genericBillingFields(req, raw)
	for key, value := range fields {
		input.SetField(key, value)
	}
	if duration := genericBillingDuration(req, raw, fields); duration > 0 {
		input.SetNumber("duration", duration)
		input.SetField("duration", formatBillingNumber(duration))
	}

	groupRatio := 1.0
	if info != nil {
		groupRatio = info.PriceData.GroupRatioInfo.GroupRatio
	}
	return task_billing_setting.Calculate(input, groupRatio)
}

func genericBillingRawBody(c *gin.Context) map[string]any {
	raw := map[string]any{}
	if finalBody, ok := relaycommon.GetTaskSubmitRequestBody(c); ok && len(strings.TrimSpace(string(finalBody))) > 0 {
		_ = common.Unmarshal(finalBody, &raw)
	}
	return raw
}

func genericBillingOriginModel(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) string {
	if info != nil && strings.TrimSpace(info.OriginModelName) != "" {
		return info.OriginModelName
	}
	return req.Model
}

func genericBillingUpstreamModel(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq, raw map[string]any) string {
	if model := firstBillingString(raw, "model", "model_name"); model != "" {
		return model
	}
	if info != nil && info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return info.UpstreamModelName
	}
	return req.Model
}

func genericBillingAction(info *relaycommon.RelayInfo) string {
	if info != nil && info.TaskRelayInfo != nil {
		return info.Action
	}
	return ""
}

func genericBillingFields(req relaycommon.TaskSubmitReq, raw map[string]any) map[string]string {
	fields := map[string]string{}
	setIfPresent(fields, "quality", firstBillingString(raw, "quality", "mode"), req.Mode)
	if params, ok := billingMap(raw, "parameters"); ok {
		setIfPresent(fields, "quality", firstBillingString(params, "quality", "mode"), "")
	}
	setIfPresent(fields, "mode", firstBillingString(raw, "mode"), req.Mode)
	setIfPresent(fields, "capability", firstBillingString(raw, "capability"), req.Capability)
	setIfPresent(fields, "control_mode", firstBillingString(raw, "control_mode"), req.ControlMode)
	setIfPresent(fields, "input_mode", firstBillingString(raw, "input_mode"), req.InputMode)
	setIfPresent(fields, "resolution", firstBillingString(raw, "resolution"), req.Resolution, firstBillingString(req.Metadata, "resolution"))
	if params, ok := billingMap(raw, "parameters"); ok {
		setIfPresent(fields, "resolution", firstBillingString(params, "resolution"), "")
	}
	setIfPresent(fields, "ratio", firstBillingString(raw, "ratio", "aspect_ratio", "aspectRatio", "size"), stringPtrValue(req.Ratio), req.AspectRatio, req.Size, firstBillingString(req.Metadata, "ratio", "aspect_ratio", "aspectRatio", "size"))
	if params, ok := billingMap(raw, "parameters"); ok {
		setIfPresent(fields, "ratio", firstBillingString(params, "ratio", "aspect_ratio", "aspectRatio", "size"), "")
	}
	if ratio := fields["ratio"]; ratio != "" {
		fields["aspect_ratio"] = ratio
	}
	if audio, ok := genericBillingAudio(req, raw); ok {
		value := strconv.FormatBool(audio)
		fields["generate_audio"] = value
		fields["with_audio"] = value
		fields["has_audio"] = value
	}
	fields["has_video_input"] = strconv.FormatBool(genericBillingHasVideoInput(req, raw))
	setCountField(fields, "image_count", genericBillingMediaCount(req, raw, "image", "image_url", "images", "image_list"))
	setCountField(fields, "video_count", genericBillingMediaCount(req, raw, "video", "video_url", "videos", "video_list"))
	return fields
}

func genericBillingDuration(req relaycommon.TaskSubmitReq, raw map[string]any, fields map[string]string) float64 {
	if duration := positiveBillingNumber(raw, "duration", "duration_seconds", "durationSeconds", "seconds"); duration > 0 {
		return duration
	}
	if params, ok := billingMap(raw, "parameters"); ok {
		if duration := positiveBillingNumber(params, "duration", "duration_seconds", "durationSeconds", "seconds"); duration > 0 {
			return duration
		}
	}
	if req.DurationSeconds != nil && *req.DurationSeconds > 0 {
		return float64(*req.DurationSeconds)
	}
	if req.Duration > 0 {
		return float64(req.Duration)
	}
	if seconds := positiveBillingNumber(map[string]any{"seconds": req.Seconds}, "seconds"); seconds > 0 {
		return seconds
	}
	if duration := positiveBillingNumber(req.Metadata, "duration", "duration_seconds", "durationSeconds", "seconds"); duration > 0 {
		return duration
	}
	if rawDuration := fields["duration"]; rawDuration != "" {
		if duration, err := strconv.ParseFloat(rawDuration, 64); err == nil && duration > 0 {
			return duration
		}
	}
	return 0
}

func genericBillingAudio(req relaycommon.TaskSubmitReq, raw map[string]any) (bool, bool) {
	if value, ok := billingBool(raw, "generate_audio", "with_audio", "audio", "has_audio", "sound"); ok {
		return value, true
	}
	if params, ok := billingMap(raw, "parameters"); ok {
		if value, ok := billingBool(params, "generate_audio", "with_audio", "audio", "has_audio", "sound"); ok {
			return value, true
		}
	}
	if req.GenerateAudio != nil {
		return *req.GenerateAudio, true
	}
	if req.WithAudio != nil {
		return *req.WithAudio, true
	}
	return billingBool(req.Metadata, "generate_audio", "with_audio", "audio", "has_audio", "sound")
}

func genericBillingHasVideoInput(req relaycommon.TaskSubmitReq, raw map[string]any) bool {
	if hasVideoInput(raw) {
		return true
	}
	if params, ok := billingMap(raw, "parameters"); ok && hasVideoInput(params) {
		return true
	}
	if len(raw) > 0 {
		return false
	}
	if hasVideoInput(req.Metadata) {
		return true
	}
	return false
}

func hasVideoInput(raw map[string]any) bool {
	if raw == nil {
		return false
	}
	for _, key := range []string{"video_url", "video", "videos", "video_list", "input_video", "input_videos"} {
		if value, ok := raw[key]; ok && usableBillingMedia(value) {
			return true
		}
	}
	if content, ok := raw["content"]; ok && contentHasVideo(content) {
		return true
	}
	if input, ok := billingMap(raw, "input"); ok {
		if hasVideoInput(input) {
			return true
		}
	}
	return false
}

func contentHasVideo(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(firstBillingString(itemMap, "type"), "video_url") {
			return usableBillingMedia(itemMap["video_url"]) || usableBillingMedia(itemMap["video"]) || usableBillingMedia(itemMap["url"])
		}
		if usableBillingMedia(itemMap["video_url"]) || usableBillingMedia(itemMap["video"]) {
			return true
		}
	}
	return false
}

func genericBillingMediaCount(req relaycommon.TaskSubmitReq, raw map[string]any, singleKey, urlKey, listKey, altListKey string) int {
	count := mediaCount(raw[singleKey]) + mediaCount(raw[urlKey]) + mediaCount(raw[listKey]) + mediaCount(raw[altListKey])
	count += contentMediaCount(raw["content"], urlKey)
	if input, ok := billingMap(raw, "input"); ok {
		count += mediaCount(input[singleKey]) + mediaCount(input[urlKey]) + mediaCount(input[listKey]) + mediaCount(input[altListKey])
		count += contentMediaCount(input["content"], urlKey)
	}
	if params, ok := billingMap(raw, "parameters"); ok {
		count += mediaCount(params[singleKey]) + mediaCount(params[urlKey]) + mediaCount(params[listKey]) + mediaCount(params[altListKey])
		count += contentMediaCount(params["content"], urlKey)
	}
	if len(raw) == 0 && req.Metadata != nil {
		count += mediaCount(req.Metadata[singleKey]) + mediaCount(req.Metadata[urlKey]) + mediaCount(req.Metadata[listKey]) + mediaCount(req.Metadata[altListKey])
		count += contentMediaCount(req.Metadata["content"], urlKey)
	}
	if singleKey == "image" {
		count += len(req.ReferenceImages)
		if len(raw) == 0 || raw[listKey] == nil {
			count += len(req.Images)
		}
		if (len(raw) == 0 || raw[singleKey] == nil) && strings.TrimSpace(req.Image) != "" {
			count++
		}
	}
	return count
}

func contentMediaCount(value any, mediaKey string) int {
	items, ok := value.([]any)
	if !ok {
		return 0
	}
	count := 0
	expectedType := strings.TrimSuffix(mediaKey, "_url") + "_url"
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType := strings.ToLower(strings.TrimSpace(firstBillingString(itemMap, "type")))
		if itemType != "" && itemType != expectedType {
			continue
		}
		if usableBillingMedia(itemMap[mediaKey]) || usableBillingMedia(itemMap[strings.TrimSuffix(mediaKey, "_url")]) || usableBillingMedia(itemMap["url"]) {
			count++
		}
	}
	return count
}

func setIfPresent(fields map[string]string, key string, values ...string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		fields[key] = value
		return
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func setCountField(fields map[string]string, key string, count int) {
	if count > 0 {
		fields[key] = strconv.Itoa(count)
	}
}

func firstBillingString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw == nil {
			return ""
		}
		if value := strings.TrimSpace(common.Interface2String(raw[key])); value != "" {
			return value
		}
	}
	return ""
}

func billingMap(raw map[string]any, key string) (map[string]any, bool) {
	if raw == nil {
		return nil, false
	}
	value, ok := raw[key]
	if !ok {
		return nil, false
	}
	typed, ok := value.(map[string]any)
	return typed, ok
}

func positiveBillingNumber(raw map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if raw == nil {
			return 0
		}
		if value := positiveBillingNumberValue(raw[key]); value > 0 {
			return value
		}
	}
	return 0
}

func positiveBillingNumberValue(value any) float64 {
	switch typed := value.(type) {
	case int:
		return positiveFloat(float64(typed))
	case int64:
		return positiveFloat(float64(typed))
	case float64:
		return positiveFloat(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return positiveFloat(parsed)
		}
	}
	return 0
}

func positiveFloat(value float64) float64 {
	if value > 0 {
		return value
	}
	return 0
}

func billingBool(raw map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if raw == nil {
			return false, false
		}
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err == nil {
				return parsed, true
			}
		case int:
			return typed != 0, true
		case float64:
			return typed != 0, true
		}
	}
	return false, false
}

func usableBillingMedia(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case bool:
		return typed
	case []any:
		for _, item := range typed {
			if usableBillingMedia(item) {
				return true
			}
		}
		return false
	case map[string]any:
		hasURLLikeKey := false
		for _, key := range []string{"url", "uri", "src"} {
			if _, ok := typed[key]; ok {
				hasURLLikeKey = true
			}
			if usableBillingMedia(typed[key]) {
				return true
			}
		}
		if hasURLLikeKey {
			return false
		}
		for _, value := range typed {
			if usableBillingMedia(value) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func mediaCount(value any) int {
	switch typed := value.(type) {
	case nil:
		return 0
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0
		}
		return 1
	case []any:
		count := 0
		for _, item := range typed {
			if usableBillingMedia(item) {
				count++
			}
		}
		return count
	default:
		if usableBillingMedia(value) {
			return 1
		}
		return 0
	}
}

func formatBillingNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

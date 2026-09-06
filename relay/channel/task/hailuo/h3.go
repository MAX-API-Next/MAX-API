package hailuo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/shopspring/decimal"
)

var h3Ratios = map[string]struct{}{
	"adaptive": {},
	"21:9":     {},
	"16:9":     {},
	"4:3":      {},
	"1:1":      {},
	"3:4":      {},
	"9:16":     {},
}

func isH3Model(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), constant.TaskModelMiniMaxH3)
}

func isUnsupportedH3Model(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), H3MaxModel)
}

func h3RequestModel(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if info != nil {
		if info.ChannelMeta != nil {
			if model := strings.TrimSpace(info.ChannelMeta.UpstreamModelName); model != "" {
				return model
			}
		}
		if model := strings.TrimSpace(info.OriginModelName); model != "" {
			return model
		}
	}
	if req != nil {
		return req.Model
	}
	return ""
}

func h3RequestUsesModel(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) bool {
	if info != nil {
		// Once channel model mapping has run, the effective upstream model is
		// authoritative. This also prevents mapping H3 to a legacy model while
		// still selecting the v2 protocol only because the client sent H3.
		if info.ChannelMeta != nil && strings.TrimSpace(info.ChannelMeta.UpstreamModelName) != "" {
			return isH3Model(info.ChannelMeta.UpstreamModelName)
		}
		if isH3Model(info.OriginModelName) {
			return true
		}
	}
	if req != nil && isH3Model(req.Model) {
		return true
	}
	return false
}

func decodeH3ContentItem(raw map[string]any) (H3ContentItem, error) {
	data, err := common.Marshal(raw)
	if err != nil {
		return H3ContentItem{}, fmt.Errorf("marshal H3 content item: %w", err)
	}
	var item H3ContentItem
	if err := common.Unmarshal(data, &item); err != nil {
		return H3ContentItem{}, fmt.Errorf("decode H3 content item: %w", err)
	}
	item.Type = strings.ToLower(strings.TrimSpace(item.Type))
	item.Role = strings.ToLower(strings.TrimSpace(item.Role))
	if item.Type == "image_url" && item.Role == "" {
		item.Role = "first_frame"
	}
	return item, nil
}

func validateH3Content(items []H3ContentItem) error {
	textCount := 0
	firstFrames := 0
	lastFrames := 0
	referenceImages := 0
	referenceVideos := 0
	referenceAudios := 0

	for i := range items {
		item := &items[i]
		switch item.Type {
		case "text":
			if strings.TrimSpace(item.Text) == "" {
				return fmt.Errorf("H3 content item %d has empty text", i)
			}
			textCount++
		case "image_url":
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return fmt.Errorf("H3 content item %d has invalid image_url", i)
			}
			switch item.Role {
			case "first_frame":
				firstFrames++
			case "last_frame":
				lastFrames++
			case "reference_image":
				referenceImages++
			default:
				return fmt.Errorf("H3 content item %d has unsupported image role", i)
			}
		case "video_url":
			if item.VideoURL == nil || strings.TrimSpace(item.VideoURL.URL) == "" || item.Role != "reference_video" {
				return fmt.Errorf("H3 content item %d has invalid video_url", i)
			}
			referenceVideos++
		case "audio_url":
			if item.AudioURL == nil || strings.TrimSpace(item.AudioURL.URL) == "" || item.Role != "reference_audio" {
				return fmt.Errorf("H3 content item %d has invalid audio_url", i)
			}
			referenceAudios++
		default:
			return fmt.Errorf("H3 content item %d has unsupported type", i)
		}
	}

	if textCount == 0 {
		return fmt.Errorf("H3 content must include a non-empty text item")
	}
	if firstFrames > 1 || lastFrames > 1 {
		return fmt.Errorf("H3 accepts at most one first_frame and one last_frame image")
	}
	if lastFrames > 0 && firstFrames == 0 {
		return fmt.Errorf("H3 last_frame requires a first_frame image")
	}
	if int64(referenceImages) > H3MaxImages || int64(referenceVideos) > H3MaxVideos || int64(referenceAudios) > H3MaxAudios {
		return fmt.Errorf("H3 reference media count exceeds the official limit")
	}
	if (firstFrames > 0 || lastFrames > 0) && (referenceImages > 0 || referenceVideos > 0 || referenceAudios > 0) {
		return fmt.Errorf("H3 frame images and reference media cannot be mixed")
	}
	return nil
}

func buildH3Content(req *relaycommon.TaskSubmitReq) ([]H3ContentItem, error) {
	if req == nil {
		return nil, fmt.Errorf("H3 request is required")
	}

	rawContent := req.Content
	if len(rawContent) == 0 && req.Metadata != nil {
		if value, ok := req.Metadata["content"]; ok {
			data, err := common.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("marshal H3 metadata.content: %w", err)
			}
			if err := common.Unmarshal(data, &rawContent); err != nil {
				return nil, fmt.Errorf("decode H3 metadata.content: %w", err)
			}
		}
	}

	if len(rawContent) > 0 {
		items := make([]H3ContentItem, 0, len(rawContent)+1)
		for _, raw := range rawContent {
			item, err := decodeH3ContentItem(raw)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		hasText := false
		for _, item := range items {
			if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
				hasText = true
				break
			}
		}
		if !hasText && strings.TrimSpace(req.Prompt) != "" {
			items = append([]H3ContentItem{{Type: "text", Text: req.Prompt}}, items...)
		}
		if err := validateH3Content(items); err != nil {
			return nil, err
		}
		return items, nil
	}

	items := make([]H3ContentItem, 0, 1+len(req.Images))
	if strings.TrimSpace(req.Prompt) != "" {
		items = append(items, H3ContentItem{Type: "text", Text: req.Prompt})
	}

	firstFrame, err := h3MetadataMediaURLs(req.Metadata, "first_frame_image")
	if err != nil {
		return nil, err
	}
	lastFrame, err := h3MetadataMediaURLs(req.Metadata, "last_frame_image")
	if err != nil {
		return nil, err
	}
	if len(firstFrame) == 0 && len(lastFrame) == 0 {
		images := append([]string{}, req.Images...)
		if len(images) == 0 && strings.TrimSpace(req.InputReference) != "" {
			images = []string{req.InputReference}
		}
		if len(images) > 2 {
			return nil, fmt.Errorf("H3 accepts at most two frame images")
		}
		for i, image := range images {
			role := "first_frame"
			if i == 1 {
				role = "last_frame"
			}
			items = append(items, H3ContentItem{Type: "image_url", Role: role, ImageURL: &H3MediaURL{URL: strings.TrimSpace(image)}})
		}
	} else {
		if len(firstFrame) > 1 || len(lastFrame) > 1 {
			return nil, fmt.Errorf("H3 accepts at most one first_frame and one last_frame image")
		}
		if len(firstFrame) == 1 {
			items = append(items, H3ContentItem{Type: "image_url", Role: "first_frame", ImageURL: &H3MediaURL{URL: firstFrame[0]}})
		}
		if len(lastFrame) == 1 {
			items = append(items, H3ContentItem{Type: "image_url", Role: "last_frame", ImageURL: &H3MediaURL{URL: lastFrame[0]}})
		}
	}

	if len(firstFrame) == 0 && len(lastFrame) == 0 && len(req.ReferenceImages) > 0 {
		for _, image := range req.ReferenceImages {
			items = append(items, H3ContentItem{Type: "image_url", Role: "reference_image", ImageURL: &H3MediaURL{URL: strings.TrimSpace(image)}})
		}
	}
	if len(firstFrame) == 0 && len(lastFrame) == 0 {
		referenceImages, err := h3MetadataMediaURLs(req.Metadata, "reference_images")
		if err != nil {
			return nil, err
		}
		for _, image := range referenceImages {
			items = append(items, H3ContentItem{Type: "image_url", Role: "reference_image", ImageURL: &H3MediaURL{URL: image}})
		}
	}

	for _, key := range []struct {
		metadataKey string
		role        string
		typeName    string
	}{
		{metadataKey: "reference_video", role: "reference_video", typeName: "video_url"},
		{metadataKey: "reference_audio", role: "reference_audio", typeName: "audio_url"},
	} {
		media, err := h3MetadataMediaURLs(req.Metadata, key.metadataKey)
		if err != nil {
			return nil, err
		}
		for _, value := range media {
			item := H3ContentItem{Type: key.typeName, Role: key.role}
			switch key.typeName {
			case "video_url":
				item.VideoURL = &H3MediaURL{URL: value}
			case "audio_url":
				item.AudioURL = &H3MediaURL{URL: value}
			}
			items = append(items, item)
		}
	}

	if err := validateH3Content(items); err != nil {
		return nil, err
	}
	return items, nil
}

func h3MetadataMediaURLs(metadata map[string]interface{}, key string) ([]string, error) {
	if metadata == nil {
		return nil, nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil, nil
	}
	values := []any{value}
	if list, ok := value.([]any); ok {
		values = list
	} else if list, ok := value.([]string); ok {
		values = make([]any, 0, len(list))
		for _, item := range list {
			values = append(values, item)
		}
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		url, err := h3MediaURLValue(item)
		if err != nil {
			return nil, fmt.Errorf("invalid H3 %s: %w", key, err)
		}
		result = append(result, url)
	}
	return result, nil
}

func h3MediaURLValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		if url := strings.TrimSpace(typed); url != "" {
			return url, nil
		}
	case map[string]any:
		data, err := common.Marshal(typed)
		if err != nil {
			return "", err
		}
		var media H3MediaURL
		if err := common.Unmarshal(data, &media); err != nil {
			return "", err
		}
		if url := strings.TrimSpace(media.URL); url != "" {
			return url, nil
		}
	}
	return "", fmt.Errorf("media URL is empty or not a string/object")
}

func h3Resolution(req *relaycommon.TaskSubmitReq) (string, error) {
	value := strings.TrimSpace(req.Resolution)
	if value == "" {
		metadataValue, err := metadataString(req.Metadata, "resolution")
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(metadataValue)
	}
	if value == "" {
		value = strings.TrimSpace(req.Size)
	}
	if value == "" {
		return Resolution768P, nil
	}
	switch strings.ToUpper(value) {
	case Resolution768P, "768":
		return Resolution768P, nil
	case Resolution2K:
		return Resolution2K, nil
	default:
		return "", fmt.Errorf("H3 resolution must be 768P or 2K")
	}
}

func h3Duration(req *relaycommon.TaskSubmitReq) (int, error) {
	seconds, err := req.ResolvedSeconds()
	if err != nil {
		return 0, fmt.Errorf("H3 duration must be an integer: %w", err)
	}
	duration := H3DefaultDuration
	if req.Duration != nil || req.DurationSeconds != nil || strings.TrimSpace(req.Seconds) != "" {
		duration = seconds
	}
	if int64(duration) < H3MinDuration || int64(duration) > H3MaxDuration {
		return 0, fmt.Errorf("H3 duration must be between %d and %d seconds", H3MinDuration, H3MaxDuration)
	}
	return duration, nil
}

func h3Ratio(req *relaycommon.TaskSubmitReq, content []H3ContentItem) (string, error) {
	metadataValue, err := metadataString(req.Metadata, "ratio")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(metadataValue)
	if value == "" && req.Ratio != nil {
		value = strings.TrimSpace(*req.Ratio)
	}
	if value == "" {
		value = strings.TrimSpace(req.AspectRatio)
	}
	value = strings.ToLower(value)
	if value != "" {
		if _, ok := h3Ratios[value]; !ok {
			return "", fmt.Errorf("H3 ratio is invalid")
		}
	}

	hasFrame := false
	hasReference := false
	for _, item := range content {
		switch item.Role {
		case "first_frame", "last_frame":
			hasFrame = true
		case "reference_image", "reference_video", "reference_audio":
			hasReference = true
		}
	}
	if hasFrame {
		return "adaptive", nil
	}
	if value == "" {
		if hasReference {
			return "adaptive", nil
		}
		return "16:9", nil
	}
	if value == "adaptive" && !hasReference {
		return "", fmt.Errorf("H3 adaptive ratio requires visual reference content")
	}
	return value, nil
}

func metadataString(metadata map[string]interface{}, key string) (string, error) {
	if metadata == nil {
		return "", nil
	}
	value, ok := metadata[key]
	if !ok {
		return "", nil
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("H3 %s must be a string", key)
	}
	return stringValue, nil
}

func h3OptionalBool(metadata map[string]interface{}, key string) (*bool, error) {
	if metadata == nil {
		return nil, nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil, nil
	}
	boolValue, ok := value.(bool)
	if !ok {
		return nil, fmt.Errorf("H3 %s must be boolean", key)
	}
	return &boolValue, nil
}

func h3CallbackURL(req *relaycommon.TaskSubmitReq) (*string, error) {
	if req.CallbackURL != nil {
		value := strings.TrimSpace(*req.CallbackURL)
		if value == "" {
			return nil, nil
		}
		return &value, nil
	}
	raw, exists := req.Metadata["callback_url"]
	if !exists || raw == nil {
		return nil, nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("H3 callback_url must be a string")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	return &value, nil
}

func buildH3Request(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*H3VideoRequest, error) {
	if isUnsupportedH3Model(h3RequestModel(req, info)) {
		return nil, fmt.Errorf("%s is not supported by this adaptor", H3MaxModel)
	}
	content, err := buildH3Content(req)
	if err != nil {
		return nil, err
	}
	duration, err := h3Duration(req)
	if err != nil {
		return nil, err
	}
	resolution, err := h3Resolution(req)
	if err != nil {
		return nil, err
	}
	ratio, err := h3Ratio(req, content)
	if err != nil {
		return nil, err
	}

	callbackURL, err := h3CallbackURL(req)
	if err != nil {
		return nil, err
	}
	watermark := req.Watermark
	if watermark == nil {
		var boolErr error
		watermark, boolErr = h3OptionalBool(req.Metadata, "aigc_watermark")
		if boolErr != nil {
			return nil, boolErr
		}
	}

	return &H3VideoRequest{
		Model:         h3RequestModel(req, info),
		Content:       content,
		Resolution:    resolution,
		Duration:      duration,
		Ratio:         ratio,
		CallbackURL:   callbackURL,
		AigcWatermark: watermark,
	}, nil
}

func parseH3Response(body []byte) (*H3QueryResponse, bool, error) {
	var response H3QueryResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, false, err
	}
	return &response, response.Task != nil || response.Error != nil, nil
}

func parseH3TaskResult(body []byte) (*relaycommon.TaskInfo, bool, error) {
	response, handled, err := parseH3Response(body)
	if err != nil || !handled {
		return nil, handled, err
	}
	if response.Error != nil {
		code := h3APIErrorCode(response.Error)
		message := strings.TrimSpace(response.Error.Message)
		if message == "" {
			message = "H3 query failed"
		}
		if code == httpStatusRequestTimeout || code == httpStatusTooManyRequests || code >= 500 {
			return nil, true, fmt.Errorf("H3 temporary query error: %s", message)
		}
		return &relaycommon.TaskInfo{Code: code, Status: model.TaskStatusFailure, Progress: "100%", Reason: message}, true, nil
	}

	task := response.Task
	if strings.TrimSpace(task.ID) == "" {
		return nil, true, fmt.Errorf("H3 query response missing task id")
	}
	usage := normalizeH3Usage(task.Usage)
	result := &relaycommon.TaskInfo{TaskID: task.ID, Code: 0, Usage: usage}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "queued":
		result.Status = model.TaskStatusQueued
		result.Progress = "30%"
	case "running":
		result.Status = model.TaskStatusInProgress
		result.Progress = "50%"
	case "succeeded":
		result.Progress = "100%"
		if task.Content == nil || strings.TrimSpace(task.Content.URL) == "" {
			result.Status = model.TaskStatusInProgress
			result.Progress = "90%"
		} else {
			result.Status = model.TaskStatusSuccess
			result.Url = strings.TrimSpace(task.Content.URL)
		}
	case "failed", "cancelled":
		result.Status = model.TaskStatusFailure
		result.Progress = "100%"
		result.Code = h3CodeNumber(task.ErrorCode())
		result.Reason = strings.TrimSpace(task.ErrorMessage())
		if result.Reason == "" {
			result.Reason = "H3 task failed"
		}
	default:
		return nil, true, fmt.Errorf("H3 query response has unsupported task status")
	}
	return result, true, nil
}

func h3APIErrorCode(apiError *H3APIError) int {
	if apiError == nil {
		return 0
	}
	if code := h3CodeNumber(apiError.HTTPCode); code != 0 {
		return code
	}
	return h3CodeNumber(apiError.Code)
}

func normalizeH3Usage(usage *H3Usage) *types.TaskUsage {
	result := &types.TaskUsage{
		Source:       types.TaskUsageSourceProviderResponse,
		Completeness: types.TaskUsageCompletenessMissing,
	}
	if usage == nil {
		return result
	}

	total, totalPresent, totalErr := h3SecondsField(usage.TotalSeconds)
	input, inputPresent, inputErr := h3SecondsField(usage.InputSeconds)
	output, outputPresent, outputErr := h3SecondsField(usage.OutputSeconds)
	images, imagesPresent, imagesErr := h3IntegerField(usage.InputImageCount)
	audio, audioPresent, audioErr := h3SecondsField(usage.InputAudioSeconds)
	invalid := totalErr != nil || inputErr != nil || outputErr != nil || imagesErr != nil || audioErr != nil

	if totalPresent {
		invalid = invalid || total.IsNegative() || total.GreaterThan(decimal.NewFromInt(H3MaxDuration+H3MaxInputVideoSeconds))
	}
	if inputPresent {
		invalid = invalid || input.IsNegative() || input.GreaterThan(decimal.NewFromInt(H3MaxInputVideoSeconds))
		if inputErr == nil {
			result.InputVideoDurationMs = h3SecondsToMilliseconds(input)
		}
	}
	if outputPresent {
		invalid = invalid || output.LessThan(decimal.NewFromInt(H3MinDuration)) || output.GreaterThan(decimal.NewFromInt(H3MaxDuration))
		if outputErr == nil {
			result.OutputDurationMs = h3SecondsToMilliseconds(output)
		}
	}
	if imagesPresent {
		invalid = invalid || images < 0 || images > H3MaxImages
		if imagesErr == nil {
			result.InputImageCount = int64Pointer(images)
		}
	}
	if audioPresent {
		invalid = invalid || audio.IsNegative() || audio.GreaterThan(decimal.NewFromInt(H3MaxInputVideoSeconds))
		if audioErr == nil {
			result.InputAudioDurationMs = h3SecondsToMilliseconds(audio)
		}
	}
	if totalPresent && inputPresent && outputPresent {
		invalid = invalid || !total.Equal(input.Add(output))
	}

	presentCount := 0
	for _, present := range []bool{totalPresent, inputPresent, outputPresent, imagesPresent, audioPresent} {
		if present {
			presentCount++
		}
	}
	if invalid {
		result.Completeness = types.TaskUsageCompletenessInvalid
	} else if presentCount == 0 {
		result.Completeness = types.TaskUsageCompletenessMissing
	} else if outputPresent && imagesPresent {
		result.Completeness = types.TaskUsageCompletenessComplete
	} else {
		result.Completeness = types.TaskUsageCompletenessPartial
	}
	return result
}

func h3IntegerField(raw json.RawMessage) (int64, bool, error) {
	value, present, err := h3DecimalField(raw)
	if err != nil || !present {
		return 0, present, err
	}
	if !value.Equal(value.Truncate(0)) || !value.IsInteger() {
		return 0, true, fmt.Errorf("value must be an integer")
	}
	if value.LessThan(decimal.NewFromInt(math.MinInt64)) || value.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, true, fmt.Errorf("value is out of range")
	}
	return value.IntPart(), true, nil
}

func h3SecondsField(raw json.RawMessage) (decimal.Decimal, bool, error) {
	value, present, err := h3DecimalField(raw)
	if err != nil || !present {
		return decimal.Zero, present, err
	}
	if value.IsNegative() {
		return decimal.Zero, true, fmt.Errorf("value cannot be negative")
	}
	milliseconds := value.Mul(decimal.NewFromInt(1000))
	if !milliseconds.Equal(milliseconds.Truncate(0)) {
		return decimal.Zero, true, fmt.Errorf("seconds must be precise to milliseconds")
	}
	if milliseconds.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return decimal.Zero, true, fmt.Errorf("seconds are out of range")
	}
	return value, true, nil
}

func h3DecimalField(raw json.RawMessage) (decimal.Decimal, bool, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return decimal.Zero, false, nil
	}
	if common.GetJsonType(value) == "string" {
		var stringValue string
		if err := common.Unmarshal(value, &stringValue); err != nil {
			return decimal.Zero, true, err
		}
		value = bytes.TrimSpace([]byte(stringValue))
	} else if common.GetJsonType(value) != "number" {
		return decimal.Zero, true, fmt.Errorf("value must be a number")
	}
	result, err := decimal.NewFromString(string(value))
	if err != nil {
		return decimal.Zero, true, err
	}
	return result, true, nil
}

func h3SecondsToMilliseconds(seconds decimal.Decimal) *int64 {
	milliseconds := seconds.Mul(decimal.NewFromInt(1000))
	if seconds.IsNegative() || !milliseconds.Equal(milliseconds.Truncate(0)) || milliseconds.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil
	}
	value := milliseconds.IntPart()
	return int64Pointer(value)
}

func int64Pointer(value int64) *int64 {
	return &value
}

func h3CodeNumber(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func (t *H3Task) ErrorCode() any {
	if t == nil || t.Error == nil {
		return nil
	}
	return t.Error.Code
}

func (t *H3Task) ErrorMessage() string {
	if t == nil || t.Error == nil {
		return ""
	}
	return t.Error.Message
}

const (
	httpStatusRequestTimeout  = 408
	httpStatusTooManyRequests = 429
)

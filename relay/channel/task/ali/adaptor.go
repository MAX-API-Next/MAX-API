package ali

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// AliVideoRequest 阿里通义万相视频生成请求
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

// AliVideoInput 视频输入参数
type AliVideoInput struct {
	Prompt         string                   `json:"prompt,omitempty"`          // 文本提示词
	ImgURL         string                   `json:"img_url,omitempty"`         // 首帧图像URL或Base64（图生视频）
	FirstFrameURL  string                   `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string                   `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	AudioURL       string                   `json:"audio_url,omitempty"`       // 音频URL（wan2.5支持）
	NegativePrompt string                   `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string                   `json:"template,omitempty"`        // 视频特效模板
	Media          []map[string]interface{} `json:"media,omitempty"`
	MultiShot      *bool                    `json:"multi_shot,omitempty"`
	ShotType       string                   `json:"shot_type,omitempty"`
	MultiPrompt    []map[string]interface{} `json:"multi_prompt,omitempty"`
	ElementList    []map[string]interface{} `json:"element_list,omitempty"`
}

// AliVideoParameters 视频参数
type AliVideoParameters struct {
	Mode         string `json:"mode,omitempty"`
	AspectRatio  string `json:"aspect_ratio,omitempty"`
	Resolution   string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P（图生视频、首尾帧生视频）
	Size         string `json:"size,omitempty"`          // 尺寸: 如 "832*480"（文生视频）
	Duration     int    `json:"duration,omitempty"`      // 时长: 3-10秒
	PromptExtend bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool  `json:"audio,omitempty"`         // 是否添加音频（wan2.5）
	Seed         int    `json:"seed,omitempty"`          // 随机数种子
}

// AliVideoResponse 阿里通义万相响应
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

// AliVideoOutput 输出信息
type AliVideoOutput struct {
	TaskID            string `json:"task_id"`
	TaskStatus        string `json:"task_status"`
	SubmitTime        string `json:"submit_time,omitempty"`
	ScheduledTime     string `json:"scheduled_time,omitempty"`
	EndTime           string `json:"end_time,omitempty"`
	OrigPrompt        string `json:"orig_prompt,omitempty"`
	ActualPrompt      string `json:"actual_prompt,omitempty"`
	VideoURL          string `json:"video_url,omitempty"`
	WatermarkVideoURL string `json:"watermark_video_url,omitempty"`
	Code              string `json:"code,omitempty"`
	Message           string `json:"message,omitempty"`
}

type aliKlingOfficialVideoResponse struct {
	Code      int                       `json:"code"`
	Message   string                    `json:"message"`
	TaskID    string                    `json:"task_id,omitempty"`
	RequestID string                    `json:"request_id,omitempty"`
	Data      aliKlingOfficialVideoData `json:"data"`
}

type aliKlingOfficialVideoData struct {
	TaskID        string                       `json:"task_id"`
	TaskStatus    string                       `json:"task_status"`
	TaskStatusMsg string                       `json:"task_status_msg,omitempty"`
	CreatedAt     int64                        `json:"created_at,omitempty"`
	UpdatedAt     int64                        `json:"updated_at,omitempty"`
	TaskResult    *aliKlingOfficialVideoResult `json:"task_result,omitempty"`
}

type aliKlingOfficialVideoResult struct {
	Videos []aliKlingOfficialVideo `json:"videos,omitempty"`
}

type aliKlingOfficialVideo struct {
	URL string `json:"url,omitempty"`
}

// AliUsage 使用统计
type AliUsage struct {
	Duration   dto.IntValue `json:"duration,omitempty"`
	VideoCount dto.IntValue `json:"video_count,omitempty"`
	SR         dto.IntValue `json:"SR,omitempty"`
}

type AliMetadata struct {
	// Input 相关
	AudioURL       string `json:"audio_url,omitempty"`       // 音频URL
	ImgURL         string `json:"img_url,omitempty"`         // 图片URL（图生视频）
	FirstFrameURL  string `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	NegativePrompt string `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string `json:"template,omitempty"`        // 视频特效模板

	// Parameters 相关
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Duration     *int    `json:"duration,omitempty"`      // 时长
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// ValidateMultipartDirect 负责解析并将原始 TaskSubmitReq 存入 context
	taskErr = relaycommon.ValidateMultipartDirect(c, info)
	if taskErr == nil {
		return nil
	}
	if ok := a.tryValidateAliOfficialRequest(c, info); ok {
		return nil
	}
	return taskErr
}

func (a *TaskAdaptor) tryValidateAliOfficialRequest(c *gin.Context, info *relaycommon.RelayInfo) bool {
	var aliReq AliVideoRequest
	if err := common.UnmarshalBodyReusable(c, &aliReq); err != nil {
		return false
	}
	if strings.TrimSpace(aliReq.Model) == "" || strings.TrimSpace(aliReq.Input.Prompt) == "" {
		return false
	}
	var metadata map[string]interface{}
	if err := common.UnmarshalBodyReusable(c, &metadata); err != nil {
		return false
	}

	req := relaycommon.TaskSubmitReq{
		Model:          aliReq.Model,
		Prompt:         aliReq.Input.Prompt,
		InputReference: firstNonEmpty(aliReq.Input.ImgURL, aliReq.Input.FirstFrameURL),
		Metadata:       metadata,
	}
	action := constant.TaskActionTextGenerate
	if req.InputReference != "" || aliReq.Input.LastFrameURL != "" {
		action = constant.TaskActionGenerate
	}
	if aliReq.Parameters != nil {
		req.Duration = aliReq.Parameters.Duration
		req.Mode = aliReq.Parameters.Mode
		if aliReq.Parameters.AspectRatio != "" {
			req.Size = aliReq.Parameters.AspectRatio
		} else {
			req.Size = aliReq.Parameters.Size
		}
	}
	info.Action = action
	c.Set("task_request", req)
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	fallback := fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL)
	return taskcommon.BuildTaskSubmitURL(info, fallback), nil
}

// BuildRequestHeader sets required headers for Ali API
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_ali_request_failed")
	}
	logger.LogJson(c, "ali video request body", aliReq)

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
)

func sizeToResolution(size string) (string, error) {
	if lo.Contains(size480p, size) {
		return "480P", nil
	} else if lo.Contains(size720p, size) {
		return "720P", nil
	} else if lo.Contains(size1080p, size) {
		return "1080P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

func ProcessAliOtherRatios(aliReq *AliVideoRequest) (map[string]float64, error) {
	otherRatios := make(map[string]float64)
	aliRatios := map[string]map[string]float64{
		"wan2.6-i2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.5-t2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-t2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.5-i2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-i2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.2-kf2v-flash": {
			"480P":  1,
			"720P":  2,
			"1080P": 4.8,
		},
		"wan2.2-i2v-flash": {
			"480P": 1,
			"720P": 2,
		},
		"wan2.2-s2v": {
			"480P": 1,
			"720P": 0.9 / 0.5,
		},
	}
	otherRatio, ok := aliRatios[aliReq.Model]
	if !ok {
		return otherRatios, nil
	}
	var resolution string

	// size match
	if aliReq.Parameters.Size != "" {
		toResolution, err := sizeToResolution(aliReq.Parameters.Size)
		if err != nil {
			return nil, err
		}
		resolution = toResolution
	} else {
		resolution = strings.ToUpper(aliReq.Parameters.Resolution)
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
	}
	if ratio, ok := otherRatio[resolution]; ok {
		otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
	}
	return otherRatios, nil
}

func (a *TaskAdaptor) convertToAliRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	upstreamModel := req.Model
	if info.IsModelMapped {
		upstreamModel = info.UpstreamModelName
	}
	if isDashScopeKlingModel(upstreamModel) {
		return a.convertToAliKlingRequest(upstreamModel, req)
	}
	aliReq := &AliVideoRequest{
		Model: upstreamModel,
		Input: AliVideoInput{
			Prompt: req.Prompt,
			ImgURL: req.InputReference,
		},
		Parameters: &AliVideoParameters{
			PromptExtend: true, // 默认开启智能改写
			Watermark:    false,
		},
	}

	// 处理分辨率映射
	if req.Size != "" {
		// text to video size must be contained *
		if strings.Contains(req.Model, "t2v") && !strings.Contains(req.Size, "*") {
			return nil, fmt.Errorf("invalid size: %s, example: %s", req.Size, "1920*1080")
		}
		if strings.Contains(req.Size, "*") {
			aliReq.Parameters.Size = req.Size
		} else {
			resolution := strings.ToUpper(req.Size)
			// 支持 480p, 720p, 1080p 或 480P, 720P, 1080P
			if !strings.HasSuffix(resolution, "P") {
				resolution = resolution + "P"
			}
			aliReq.Parameters.Resolution = resolution
		}
	} else {
		// 根据模型设置默认分辨率
		if strings.Contains(req.Model, "t2v") { // image to video
			if strings.HasPrefix(req.Model, "wan2.5") {
				aliReq.Parameters.Size = "1920*1080"
			} else if strings.HasPrefix(req.Model, "wan2.2") {
				aliReq.Parameters.Size = "1920*1080"
			} else {
				aliReq.Parameters.Size = "1280*720"
			}
		} else {
			if strings.HasPrefix(req.Model, "wan2.6") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(req.Model, "wan2.5") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(req.Model, "wan2.2-i2v-flash") {
				aliReq.Parameters.Resolution = "720P"
			} else if strings.HasPrefix(req.Model, "wan2.2-i2v-plus") {
				aliReq.Parameters.Resolution = "1080P"
			} else {
				aliReq.Parameters.Resolution = "720P"
			}
		}
	}

	// 处理时长
	if req.Duration > 0 {
		aliReq.Parameters.Duration = req.Duration
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		} else {
			aliReq.Parameters.Duration = seconds
		}
	} else {
		aliReq.Parameters.Duration = 5 // 默认5秒
	}

	// 从 metadata 中提取额外参数
	if err := applyAliMetadata(req.Metadata, aliReq); err != nil {
		return nil, err
	}

	if aliReq.Model != upstreamModel {
		return nil, errors.New("can't change model with metadata")
	}

	return aliReq, nil
}

func (a *TaskAdaptor) convertToAliKlingRequest(upstreamModel string, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	audio := false
	aliReq := &AliVideoRequest{
		Model: upstreamModel,
		Input: AliVideoInput{
			Prompt: req.Prompt,
		},
		Parameters: &AliVideoParameters{
			Mode:        taskcommon.DefaultString(req.Mode, "std"),
			AspectRatio: sizeToKlingAspectRatio(req.Size),
			Duration:    5,
			Audio:       &audio,
			Watermark:   true,
		},
	}

	if req.Duration > 0 {
		aliReq.Parameters.Duration = req.Duration
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		}
		aliReq.Parameters.Duration = seconds
	}
	if imageURL := firstNonEmpty(req.InputReference, req.Image); imageURL != "" {
		aliReq.Input.Media = []map[string]interface{}{
			{
				"type": "first_frame",
				"url":  imageURL,
			},
		}
	}

	if err := applyAliMetadata(req.Metadata, aliReq); err != nil {
		return nil, err
	}
	applyKlingOfficialMetadata(req.Metadata, aliReq)
	if aliReq.Model != upstreamModel {
		return nil, errors.New("can't change model with metadata")
	}
	return aliReq, nil
}

func applyAliMetadata(metadata map[string]interface{}, aliReq *AliVideoRequest) error {
	if metadata == nil {
		return nil
	}
	metadataBytes, err := common.Marshal(metadata)
	if err != nil {
		return errors.Wrap(err, "marshal metadata failed")
	}
	if err := common.Unmarshal(metadataBytes, aliReq); err != nil {
		return errors.Wrap(err, "unmarshal metadata failed")
	}
	return nil
}

func isDashScopeKlingModel(model string) bool {
	return strings.HasPrefix(model, "kling/")
}

func applyKlingOfficialMetadata(metadata map[string]interface{}, aliReq *AliVideoRequest) {
	if metadata == nil || aliReq.Parameters == nil {
		return
	}
	if aspectRatio := metadataString(metadata, "aspect_ratio"); aspectRatio != "" {
		aliReq.Parameters.AspectRatio = aspectRatio
	}
	if mode := metadataString(metadata, "mode"); mode != "" {
		aliReq.Parameters.Mode = mode
	}
	if watermark, ok := metadataBool(metadata, "watermark"); ok {
		aliReq.Parameters.Watermark = watermark
	}
	if sound := strings.ToLower(metadataString(metadata, "sound")); sound != "" {
		audio := sound == "on" || sound == "true" || sound == "1"
		aliReq.Parameters.Audio = &audio
	}
	if audio, ok := metadataBool(metadata, "audio"); ok {
		aliReq.Parameters.Audio = &audio
	}
	if duration, ok := metadataInt(metadata, "duration"); ok && duration > 0 {
		aliReq.Parameters.Duration = duration
	}

	if metadataHasAliMedia(metadata) {
		return
	}
	media := aliReq.Input.Media
	if len(media) == 0 {
		if imageURL := metadataString(metadata, "image"); imageURL != "" {
			media = appendKlingMedia(media, "first_frame", imageURL)
		}
	}
	if imageTailURL := metadataString(metadata, "image_tail"); imageTailURL != "" {
		media = appendKlingMedia(media, "last_frame", imageTailURL)
	}
	if imageList, ok := metadata["image_list"].([]interface{}); ok {
		for _, item := range imageList {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if imageURL := metadataString(itemMap, "image_url"); imageURL != "" {
				media = appendKlingMedia(media, "refer", imageURL)
			}
		}
	}
	if videoList, ok := metadata["video_list"].([]interface{}); ok {
		for _, item := range videoList {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			videoURL := firstNonEmpty(metadataString(itemMap, "video_url"), metadataString(itemMap, "url"))
			if videoURL == "" {
				continue
			}
			mediaType := firstNonEmpty(metadataString(itemMap, "refer_type"), metadataString(itemMap, "type"), "base")
			media = appendKlingMedia(media, mediaType, videoURL)
			if keepOriginalSound := metadataString(itemMap, "keep_original_sound"); keepOriginalSound != "" {
				media[len(media)-1]["keep_original_sound"] = keepOriginalSound
			}
		}
	}
	if len(media) > 0 {
		aliReq.Input.Media = media
	}
}

func appendKlingMedia(media []map[string]interface{}, mediaType, url string) []map[string]interface{} {
	return append(media, map[string]interface{}{
		"type": mediaType,
		"url":  url,
	})
}

func metadataHasAliMedia(metadata map[string]interface{}) bool {
	input, ok := metadata["input"].(map[string]interface{})
	if !ok {
		return false
	}
	media, ok := input["media"].([]interface{})
	return ok && len(media) > 0
}

func metadataString(metadata map[string]interface{}, key string) string {
	switch value := metadata[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func metadataBool(metadata map[string]interface{}, key string) (bool, bool) {
	switch value := metadata[key].(type) {
	case bool:
		return value, true
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	}
	return false, false
}

func metadataInt(metadata map[string]interface{}, key string) (int, bool) {
	switch value := metadata[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func metadataListLength(metadata map[string]interface{}, key string) int {
	switch value := metadata[key].(type) {
	case []interface{}:
		return len(value)
	case []map[string]interface{}:
		return len(value)
	default:
		return 0
	}
}

func sizeToKlingAspectRatio(size string) string {
	switch size {
	case "1024x1024", "512x512", "1:1":
		return "1:1"
	case "1280x720", "1920x1080", "16:9":
		return "16:9"
	case "720x1280", "1080x1920", "9:16":
		return "9:16"
	case "":
		return "16:9"
	default:
		if strings.Contains(size, ":") {
			return size
		}
		return "16:9"
	}
}

// EstimateBilling 根据用户请求参数计算 OtherRatios（时长、分辨率等）。
// 在 ValidateRequestAndSetAction 之后、价格计算之前调用。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil
	}

	otherRatios := map[string]float64{
		"seconds": float64(aliReq.Parameters.Duration),
	}
	ratios, err := ProcessAliOtherRatios(aliReq)
	if err != nil {
		return otherRatios
	}
	for k, v := range ratios {
		otherRatios[k] = v
	}
	return otherRatios
}

func (a *TaskAdaptor) EstimateTaskBilling(c *gin.Context, info *relaycommon.RelayInfo) (*types.TaskBillingResult, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil, err
	}
	if !isDashScopeKlingModel(aliReq.Model) {
		return nil, nil
	}

	duration := float64(5)
	mode := "std"
	resolution := ""
	hasAudio := false
	if aliReq.Parameters != nil {
		if aliReq.Parameters.Duration > 0 {
			duration = float64(aliReq.Parameters.Duration)
		}
		if aliReq.Parameters.Mode != "" {
			mode = aliReq.Parameters.Mode
		}
		resolution = aliReq.Parameters.Resolution
		if aliReq.Parameters.Audio != nil {
			hasAudio = *aliReq.Parameters.Audio
		}
	}
	quality := strings.ToLower(strings.TrimSpace(mode))
	if strings.EqualFold(strings.TrimSpace(resolution), "4k") {
		quality = "4k"
	}
	if quality == "" {
		quality = "std"
	}

	imageCount, videoCount := countAliBillingMedia(aliReq, taskReq.Metadata)
	hasVideoInput := videoCount > 0
	action := ""
	if info.TaskRelayInfo != nil {
		action = info.Action
	}
	input := types.TaskBillingInput{
		Model:         info.OriginModelName,
		UpstreamModel: aliReq.Model,
		Action:        action,
		Platform:      a.GetChannelName(),
	}
	input.SetNumber("duration", duration)
	input.SetField("quality", quality)
	input.SetField("mode", mode)
	input.SetField("has_audio", strconv.FormatBool(hasAudio))
	input.SetField("has_video_input", strconv.FormatBool(hasVideoInput))
	input.SetField("image_count", strconv.Itoa(imageCount))
	input.SetField("video_count", strconv.Itoa(videoCount))
	input.SetField("capability", "video_generation")
	return task_billing_setting.Calculate(input, info.PriceData.GroupRatioInfo.GroupRatio)
}

func countAliBillingMedia(aliReq *AliVideoRequest, metadata map[string]interface{}) (int, int) {
	imageCount := 0
	videoCount := 0
	mediaCount := 0
	if aliReq != nil {
		if aliReq.Input.ImgURL != "" || aliReq.Input.FirstFrameURL != "" {
			imageCount++
		}
		if aliReq.Input.LastFrameURL != "" {
			imageCount++
		}
		mediaCount = len(aliReq.Input.Media)
		for _, item := range aliReq.Input.Media {
			mediaType := strings.ToLower(metadataString(item, "type"))
			switch {
			case mediaType == "base", strings.Contains(mediaType, "video"):
				videoCount++
			case strings.Contains(mediaType, "frame"), strings.Contains(mediaType, "image"), strings.Contains(mediaType, "refer"):
				imageCount++
			}
		}
	}
	if mediaCount == 0 {
		imageCount += metadataListLength(metadata, "image_list")
		videoCount += metadataListLength(metadata, "video_list")
	}
	if metadataString(metadata, "video") != "" || metadataString(metadata, "video_url") != "" {
		videoCount++
	}
	return imageCount, videoCount
}

// DoRequest delegates to common helper
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	if taskID, handled, configuredErr := taskcommon.TryHandleConfiguredSubmitResponse(c, responseBody, info); handled || configuredErr != nil {
		return taskID, responseBody, configuredErr
	}

	// 解析阿里响应
	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 检查错误
	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}

	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	if c.GetBool("kling_official_route") {
		c.JSON(http.StatusOK, buildAliKlingOfficialVideoResponse(
			info.PublicTaskID,
			aliResp,
			model.TaskStatusUnknown,
			common.GetTimestamp(),
			0,
			"",
			"",
		))
		return aliResp.Output.TaskID, responseBody, nil
	}

	// 转换为 OpenAI 格式响应
	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	// 返回 OpenAI 格式
	c.JSON(http.StatusOK, openAIResp)

	return aliResp.Output.TaskID, responseBody, nil
}

// FetchTask 查询任务状态
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)
	uri = taskcommon.BuildTaskQueryURL(baseUrl, body, uri)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ParseTaskResult 解析任务结果
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 状态映射
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		// 阿里直接返回视频URL，不需要额外的代理端点
		taskResult.Url = firstNonEmpty(aliResp.Output.VideoURL, aliResp.Output.WatermarkVideoURL)
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal ali response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	// 设置视频URL（核心字段）
	openAIResp.SetMetadata("url", aliResp.Output.VideoURL)

	// 错误处理
	if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Code,
			Message: aliResp.Message,
		}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Output.Code,
			Message: aliResp.Output.Message,
		}
	}

	return common.Marshal(openAIResp)
}

func (a *TaskAdaptor) ConvertToKlingOfficialVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &aliResp); err != nil {
			return nil, errors.Wrap(err, "unmarshal ali response failed")
		}
	}

	resp := buildAliKlingOfficialVideoResponse(
		task.TaskID,
		aliResp,
		task.Status,
		task.CreatedAt,
		task.UpdatedAt,
		task.FailReason,
		task.GetResultURL(),
	)
	return common.Marshal(resp)
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}

func buildAliKlingOfficialVideoResponse(publicTaskID string, aliResp AliVideoResponse, internalStatus model.TaskStatus, createdAt, updatedAt int64, failReason, resultURL string) aliKlingOfficialVideoResponse {
	if publicTaskID == "" {
		publicTaskID = aliResp.Output.TaskID
	}
	taskStatus := aliStatusToKlingStatus(internalStatus, aliResp.Output.TaskStatus)
	if taskStatus == "" {
		taskStatus = "submitted"
	}
	taskStatusMsg := firstNonEmpty(failReason, aliResp.Output.Message, aliResp.Message)
	videoURL := firstNonEmpty(resultURL, aliResp.Output.VideoURL, aliResp.Output.WatermarkVideoURL)

	resp := aliKlingOfficialVideoResponse{
		Code:      0,
		Message:   "",
		TaskID:    publicTaskID,
		RequestID: aliResp.RequestID,
		Data: aliKlingOfficialVideoData{
			TaskID:        publicTaskID,
			TaskStatus:    taskStatus,
			TaskStatusMsg: taskStatusMsg,
			CreatedAt:     createdAt,
			UpdatedAt:     updatedAt,
		},
	}
	if videoURL != "" {
		resp.Data.TaskResult = &aliKlingOfficialVideoResult{
			Videos: []aliKlingOfficialVideo{
				{URL: videoURL},
			},
		}
	}
	return resp
}

func aliStatusToKlingStatus(internalStatus model.TaskStatus, aliStatus string) string {
	switch internalStatus {
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "submitted"
	case model.TaskStatusInProgress:
		return "processing"
	case model.TaskStatusSuccess:
		return "succeed"
	case model.TaskStatusFailure:
		return "failed"
	}

	switch aliStatus {
	case "PENDING":
		return "submitted"
	case "RUNNING":
		return "processing"
	case "SUCCEEDED":
		return "succeed"
	case "FAILED", "CANCELED", "UNKNOWN":
		return "failed"
	default:
		return ""
	}
}

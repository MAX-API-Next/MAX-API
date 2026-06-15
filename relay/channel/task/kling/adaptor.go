package kling

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	taskcommon "github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
)

// ============================
// Request / Response structures
// ============================

type TrajectoryPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type DynamicMask struct {
	Mask         string            `json:"mask,omitempty"`
	Trajectories []TrajectoryPoint `json:"trajectories,omitempty"`
}

type CameraConfig struct {
	Horizontal float64 `json:"horizontal,omitempty"`
	Vertical   float64 `json:"vertical,omitempty"`
	Pan        float64 `json:"pan,omitempty"`
	Tilt       float64 `json:"tilt,omitempty"`
	Roll       float64 `json:"roll,omitempty"`
	Zoom       float64 `json:"zoom,omitempty"`
}

type CameraControl struct {
	Type   string        `json:"type,omitempty"`
	Config *CameraConfig `json:"config,omitempty"`
}

type ImageListItem struct {
	ImageURL string `json:"image_url,omitempty"`
}

type VideoListItem struct {
	VideoURL          string `json:"video_url,omitempty"`
	ReferType         string `json:"refer_type,omitempty"`
	KeepOriginalSound string `json:"keep_original_sound,omitempty"`
}

type requestPayload struct {
	Prompt         string          `json:"prompt,omitempty"`
	Image          string          `json:"image,omitempty"`
	ImageTail      string          `json:"image_tail,omitempty"`
	ImageList      []ImageListItem `json:"image_list,omitempty"`
	VideoList      []VideoListItem `json:"video_list,omitempty"`
	NegativePrompt string          `json:"negative_prompt,omitempty"`
	Mode           string          `json:"mode,omitempty"`
	Resolution     string          `json:"resolution,omitempty"`
	Duration       string          `json:"duration,omitempty"`
	AspectRatio    string          `json:"aspect_ratio,omitempty"`
	ModelName      string          `json:"model_name,omitempty"`
	Model          string          `json:"model,omitempty"` // Compatible with upstreams that only recognize "model"
	Sound          string          `json:"sound,omitempty"`
	Audio          *bool           `json:"audio,omitempty"`
	CfgScale       float64         `json:"cfg_scale,omitempty"`
	StaticMask     string          `json:"static_mask,omitempty"`
	DynamicMasks   []DynamicMask   `json:"dynamic_masks,omitempty"`
	CameraControl  *CameraControl  `json:"camera_control,omitempty"`
	CallbackUrl    string          `json:"callback_url,omitempty"`
	ExternalTaskId string          `json:"external_task_id,omitempty"`
}

type responseVideo struct {
	Id           string `json:"id"`
	Url          string `json:"url"`
	WatermarkUrl string `json:"watermark_url"`
	Duration     string `json:"duration"`
}

type responseImage struct {
	Index        int    `json:"index"`
	Url          string `json:"url"`
	WatermarkUrl string `json:"watermark_url"`
}

type responseTaskResult struct {
	Videos []responseVideo `json:"videos"`
	Images []responseImage `json:"images"`
}

type responseData struct {
	TaskId        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	TaskStatusMsg string `json:"task_status_msg"`
	TaskInfo      struct {
		ExternalTaskId string `json:"external_task_id"`
	} `json:"task_info"`
	WatermarkInfo struct {
		Enabled bool `json:"enabled"`
	} `json:"watermark_info"`
	TaskResult         responseTaskResult `json:"task_result"`
	CreatedAt          int64              `json:"created_at"`
	UpdatedAt          int64              `json:"updated_at"`
	FinalUnitDeduction string             `json:"final_unit_deduction"`
}

type responsePayload struct {
	Code      int          `json:"code"`
	Message   string       `json:"message"`
	TaskId    string       `json:"task_id"`
	RequestId string       `json:"request_id"`
	Data      responseData `json:"data"`
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

	// apiKey format: "access_key|secret_key"
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// Use the standard validation method for TaskSubmitReq
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	path := klingVideoPath(info.Action)

	fallback := fmt.Sprintf("%s%s", a.baseURL, path)
	if isMaxAPIRelay(info.ApiKey) {
		fallback = fmt.Sprintf("%s/kling%s", a.baseURL, path)
	}

	return taskcommon.BuildTaskSubmitURL(info, fallback), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	token, err := a.createJWTToken()
	if err != nil {
		return fmt.Errorf("failed to create JWT token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")
	return nil
}

// BuildRequestBody converts request into Kling specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, err
	}
	switch {
	case len(body.ImageList) > 0 || len(body.VideoList) > 0:
		if body.AspectRatio == "" {
			body.AspectRatio = "16:9"
		}
		c.Set("action", constant.TaskActionKlingOmniVideo)
	case body.Image == "" && body.ImageTail == "":
		if body.AspectRatio == "" {
			body.AspectRatio = "1:1"
		}
		c.Set("action", constant.TaskActionTextGenerate)
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	if action := c.GetString("action"); action != "" {
		info.Action = action
	}
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	if taskID, handled, configuredErr := taskcommon.TryHandleConfiguredSubmitResponse(c, responseBody, info); handled || configuredErr != nil {
		return taskID, responseBody, configuredErr
	}

	var kResp responsePayload
	err = common.Unmarshal(responseBody, &kResp)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}
	if kResp.Code != 0 {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", kResp.Message), "task_failed", http.StatusBadRequest)
		return
	}
	upstreamTaskID := kResp.Data.TaskId
	if upstreamTaskID == "" {
		upstreamTaskID = kResp.TaskId
	}
	if upstreamTaskID == "" {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}
	if c.GetBool("kling_official_route") {
		kResp.TaskId = info.PublicTaskID
		kResp.Data.TaskId = info.PublicTaskID
		if kResp.Data.TaskStatus == "" {
			kResp.Data.TaskStatus = "submitted"
		}
		c.JSON(http.StatusOK, kResp)
		return upstreamTaskID, responseBody, nil
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return upstreamTaskID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	action, ok := body["action"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid action")
	}
	path := klingVideoPath(action)
	url := fmt.Sprintf("%s%s/%s", baseUrl, path, taskID)
	if isMaxAPIRelay(key) {
		url = fmt.Sprintf("%s/kling%s/%s", baseUrl, path, taskID)
	}
	url = taskcommon.BuildTaskQueryURL(baseUrl, body, url)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	token, err := a.createJWTTokenWithKey(key)
	if err != nil {
		token = key
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"kling-v1", "kling-v1-6", "kling-v2-master", "kling-v2-6", "kling-v3", "kling-v3-omni", "kling-video-o1"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "kling"
}

func (a *TaskAdaptor) EstimateTaskBilling(c *gin.Context, info *relaycommon.RelayInfo) (*types.TaskBillingResult, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, err
	}

	action := ""
	if info.TaskRelayInfo != nil {
		action = info.Action
	}
	switch {
	case len(body.ImageList) > 0 || len(body.VideoList) > 0:
		action = constant.TaskActionKlingOmniVideo
	case body.Image == "" && body.ImageTail == "":
		action = constant.TaskActionTextGenerate
	default:
		action = constant.TaskActionGenerate
	}

	duration, err := strconv.ParseFloat(body.Duration, 64)
	if err != nil || duration <= 0 {
		duration = 5
	}
	quality := strings.ToLower(strings.TrimSpace(body.Mode))
	if strings.EqualFold(strings.TrimSpace(body.Resolution), "4k") {
		quality = "4k"
	}
	if quality == "" {
		quality = "std"
	}
	hasAudio := parseEnabled(body.Sound)
	if body.Audio != nil {
		hasAudio = *body.Audio
	}
	hasVideoInput := len(body.VideoList) > 0

	input := types.TaskBillingInput{
		Model:         info.OriginModelName,
		UpstreamModel: body.ModelName,
		Action:        action,
		Platform:      a.GetChannelName(),
	}
	input.SetNumber("duration", duration)
	input.SetField("quality", quality)
	input.SetField("mode", body.Mode)
	input.SetField("has_audio", strconv.FormatBool(hasAudio))
	input.SetField("has_video_input", strconv.FormatBool(hasVideoInput))
	input.SetField("image_count", strconv.Itoa(len(body.ImageList)))
	input.SetField("video_count", strconv.Itoa(len(body.VideoList)))
	input.SetField("capability", "video_generation")
	return task_billing_setting.Calculate(input, info.PriceData.GroupRatioInfo.GroupRatio)
}

// ============================
// helpers
// ============================

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	r := requestPayload{
		Prompt:         req.Prompt,
		Image:          req.Image,
		Mode:           taskcommon.DefaultString(req.Mode, "std"),
		Duration:       fmt.Sprintf("%d", taskcommon.DefaultInt(req.Duration, 5)),
		ModelName:      info.UpstreamModelName,
		StaticMask:     "",
		DynamicMasks:   []DynamicMask{},
		CameraControl:  nil,
		CallbackUrl:    "",
		ExternalTaskId: "",
	}
	if r.ModelName == "" {
		r.ModelName = "kling-v1"
		r.Model = "kling-v1"
	}
	if req.Size != "" {
		r.AspectRatio = a.getAspectRatio(req.Size)
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}
	return &r, nil
}

func (a *TaskAdaptor) getAspectRatio(size string) string {
	switch size {
	case "1024x1024", "512x512":
		return "1:1"
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	default:
		return "1:1"
	}
}

func parseEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on", "enable", "enabled":
		return true
	default:
		return false
	}
}

// ============================
// JWT helpers
// ============================

func (a *TaskAdaptor) createJWTToken() (string, error) {
	return a.createJWTTokenWithKey(a.apiKey)
}

func (a *TaskAdaptor) createJWTTokenWithKey(apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", errors.New("api_key is empty")
	}
	if isMaxAPIRelay(apiKey) {
		return apiKey, nil // max api relay
	}
	keyParts := strings.Split(apiKey, "|")
	accessKey := strings.TrimSpace(keyParts[0])
	if len(keyParts) == 1 {
		return accessKey, nil
	}
	if len(keyParts) != 2 {
		return "", errors.New("invalid api_key, required format is accessKey|secretKey")
	}
	secretKey := strings.TrimSpace(keyParts[1])
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iss": accessKey,
		"exp": now + 1800, // 30 minutes
		"nbf": now - 5,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = "JWT"
	return token.SignedString([]byte(secretKey))
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	taskInfo := &relaycommon.TaskInfo{}
	resPayload := responsePayload{}
	err := common.Unmarshal(respBody, &resPayload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}
	taskInfo.Code = resPayload.Code
	taskInfo.TaskID = resPayload.Data.TaskId
	taskInfo.Reason = resPayload.Data.TaskStatusMsg
	//任务状态，枚举值：submitted（已提交）、processing（处理中）、succeed（成功）、failed（失败）
	status := resPayload.Data.TaskStatus
	switch status {
	case "submitted":
		taskInfo.Status = model.TaskStatusSubmitted
	case "processing":
		taskInfo.Status = model.TaskStatusInProgress
	case "succeed":
		taskInfo.Status = model.TaskStatusSuccess
		if videos := resPayload.Data.TaskResult.Videos; len(videos) > 0 {
			video := videos[0]
			taskInfo.Url = video.Url
		}
		if tokens, err := strconv.ParseFloat(resPayload.Data.FinalUnitDeduction, 64); err == nil {
			rounded := int(math.Ceil(tokens))
			if rounded > 0 {
				taskInfo.CompletionTokens = rounded
				taskInfo.TotalTokens = rounded
			}
		}
	case "failed":
		taskInfo.Status = model.TaskStatusFailure
	default:
		return nil, fmt.Errorf("unknown task status: %s", status)
	}
	return taskInfo, nil
}

func isMaxAPIRelay(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-")
}

func klingVideoPath(action string) string {
	switch action {
	case constant.TaskActionKlingOmniVideo:
		return "/v1/videos/omni-video"
	case constant.TaskActionGenerate:
		return "/v1/videos/image2video"
	default:
		return "/v1/videos/text2video"
	}
}

func taskStatusToKlingStatus(status model.TaskStatus, fallback string) string {
	switch status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "submitted"
	case model.TaskStatusInProgress:
		return "processing"
	case model.TaskStatusSuccess:
		return "succeed"
	case model.TaskStatusFailure:
		return "failed"
	default:
		return fallback
	}
}

func (a *TaskAdaptor) ConvertToKlingOfficialVideo(originTask *model.Task) ([]byte, error) {
	var klingResp responsePayload
	if len(originTask.Data) > 0 {
		if err := common.Unmarshal(originTask.Data, &klingResp); err != nil {
			return nil, errors.Wrap(err, "unmarshal kling task data failed")
		}
	}

	klingResp.Code = 0
	klingResp.TaskId = originTask.TaskID
	klingResp.Data.TaskId = originTask.TaskID
	klingResp.Data.TaskStatus = taskStatusToKlingStatus(originTask.Status, klingResp.Data.TaskStatus)
	if klingResp.Data.TaskStatus == "" {
		klingResp.Data.TaskStatus = "submitted"
	}
	if originTask.FailReason != "" {
		klingResp.Data.TaskStatusMsg = originTask.FailReason
	}
	if klingResp.Data.CreatedAt == 0 {
		klingResp.Data.CreatedAt = originTask.CreatedAt
	}
	if klingResp.Data.UpdatedAt == 0 {
		klingResp.Data.UpdatedAt = originTask.UpdatedAt
	}
	if originTask.GetResultURL() != "" && len(klingResp.Data.TaskResult.Videos) == 0 {
		klingResp.Data.TaskResult.Videos = []responseVideo{{
			Url: originTask.GetResultURL(),
		}}
	}
	return common.Marshal(klingResp)
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var klingResp responsePayload
	if err := common.Unmarshal(originTask.Data, &klingResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal kling task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = klingResp.Data.CreatedAt
	openAIVideo.CompletedAt = klingResp.Data.UpdatedAt

	if len(klingResp.Data.TaskResult.Videos) > 0 {
		video := klingResp.Data.TaskResult.Videos[0]
		if video.Url != "" {
			openAIVideo.SetMetadata("url", video.Url)
		}
		if video.Duration != "" {
			openAIVideo.Seconds = video.Duration
		}
	}

	if klingResp.Code != 0 && klingResp.Message != "" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: klingResp.Message,
			Code:    fmt.Sprintf("%d", klingResp.Code),
		}
	}

	// https://app.klingai.com/cn/dev/document-api/apiReference/model/textToVideo
	if data := klingResp.Data; data.TaskStatus == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: data.TaskStatusMsg,
		}
	}
	return common.Marshal(openAIVideo)
}

package doubao

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type,omitempty"`
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           *string        `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           *string        `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	SafetyIdentifier *string        `json:"safety_identifier,omitempty"`
	Priority         *dto.IntValue  `json:"priority,omitempty"`
	Resolution       *string        `json:"resolution,omitempty"`
	Ratio            *string        `json:"ratio,omitempty"`
	Duration         *dto.IntValue  `json:"duration,omitempty"`
	Frames           *dto.IntValue  `json:"frames,omitempty"`
	Seed             *dto.IntValue  `json:"seed,omitempty"`
	CameraFixed      *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark        *dto.BoolValue `json:"watermark,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"` // task_id
}

type responseTask struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	Tools           []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage json.RawMessage `json:"usage"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

type responseTaskUsage struct {
	CompletionTokens *int `json:"completion_tokens"`
	TotalTokens      *int `json:"total_tokens"`
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

var _ channel.TaskUsageFactProvider = (*TaskAdaptor)(nil)

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if info != nil && info.ChannelMeta != nil && taskcommon.UseConfiguredTaskProtocol(info.ChannelMeta.ChannelOtherSettings) &&
		info.ChannelMeta.ChannelSetting.PassThroughBodyEnabled {
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
		relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, req)
		return nil
	}

	if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
	}

	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" && !hasUsableOfficialContent(req.Content) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt or content is required"), "invalid_request", http.StatusBadRequest)
	}
	if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		req.Images = []string{req.Image}
	}
	relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, req)
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	fallback := fmt.Sprintf("%s/api/v3/contents/generations/tasks", a.baseURL)
	return taskcommon.BuildTaskSubmitURL(info, fallback), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 根据请求 metadata 中的输出分辨率与是否包含视频输入，返回相对基准价的计费 OtherRatio。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolution, hasVideo := a.resolveBillingInputs(c, &req)
	ratio, ok := GetVideoInputRatio(info.OriginModelName, resolution, hasVideo)
	if !ok || ratio == 1.0 {
		return nil
	}
	return map[string]float64{"video_input": ratio}
}

func (a *TaskAdaptor) resolveBillingInputs(c *gin.Context, req *relaycommon.TaskSubmitReq) (string, bool) {
	if req == nil {
		return "", false
	}
	raw := map[string]any{}
	if c != nil && c.Request != nil && c.Request.Body != nil {
		_ = common.UnmarshalBodyReusable(c, &raw)
	}
	if finalBody, ok := relaycommon.GetTaskSubmitRequestBody(c); ok {
		_ = common.Unmarshal(finalBody, &raw)
	}
	hasVideo := hasVideoInMetadata(req.Metadata)
	hasVideo = hasVideo || hasVideoInMetadata(raw)
	resolution := req.Resolution
	if rawResolution := strings.TrimSpace(common.Interface2String(raw["resolution"])); rawResolution != "" {
		resolution = rawResolution
	}
	payload, err := a.convertToRequestPayload(req)
	if err == nil && payload != nil {
		if payload.Resolution != nil && *payload.Resolution != "" {
			resolution = *payload.Resolution
		}
		hasVideo = hasVideo || hasVideoInContent(payload.Content)
	}
	return resolution, hasVideo
}

func hasVideoInContent(content []ContentItem) bool {
	for _, item := range content {
		if item.VideoURL != nil && hasUsableVideoInput(item.VideoURL.URL) {
			return true
		}
	}
	return false
}

func hasUsableOfficialContent(content []map[string]any) bool {
	items, err := topLevelContentItems(content)
	if err != nil {
		return false
	}
	for _, item := range items {
		switch item.Type {
		case "text":
			if strings.TrimSpace(item.Text) != "" {
				return true
			}
		case "image_url":
			if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
				return true
			}
		case "video_url":
			if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
				return true
			}
		case "audio_url":
			if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
				return true
			}
		}
	}
	return false
}

// hasVideoInMetadata 直接检查 metadata 的 content 数组是否包含 video_url 条目，
// 避免构建完整的上游 requestPayload。
func hasVideoInMetadata(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	if videoURL, has := metadata["video_url"]; has && hasUsableVideoInput(videoURL) {
		return true
	}
	if video, has := metadata["video"]; has && hasUsableVideoInput(video) {
		return true
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["type"] == "video_url" && hasUsableVideoInput(itemMap["video_url"]) {
			return true
		}
		if videoURL, has := itemMap["video_url"]; has && hasUsableVideoInput(videoURL) {
			return true
		}
	}
	return false
}

func hasUsableVideoInput(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case bool:
		return v
	case map[string]interface{}:
		if url, has := v["url"]; has {
			return hasUsableVideoInput(url)
		}
		return len(v) > 0
	case map[string]string:
		if url, has := v["url"]; has {
			return strings.TrimSpace(url) != ""
		}
		return len(v) > 0
	default:
		return true
	}
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
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

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", baseUrl, taskID)
	uri = taskcommon.BuildTaskQueryURL(baseUrl, body, uri)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
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

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// Add images if present
	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
			})
		}
	}
	if err := applyTopLevelSeedanceOptions(req, &r); err != nil {
		return nil, err
	}

	metadata := req.Metadata
	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if sec, _ := strconv.Atoi(req.Seconds); sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	}

	if strings.TrimSpace(req.Prompt) != "" {
		r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
		r.Content = append(r.Content, ContentItem{
			Type: "text",
			Text: req.Prompt,
		})
	}

	return &r, nil
}

func applyTopLevelSeedanceOptions(req *relaycommon.TaskSubmitReq, r *requestPayload) error {
	if req == nil || r == nil {
		return nil
	}
	if len(req.Content) > 0 {
		items, err := topLevelContentItems(req.Content)
		if err != nil {
			return err
		}
		r.Content = items
	}
	if req.CallbackURL != nil {
		r.CallbackURL = req.CallbackURL
	}
	if req.ReturnLastFrame != nil {
		r.ReturnLastFrame = lo.ToPtr(dto.BoolValue(*req.ReturnLastFrame))
	}
	if req.ServiceTier != nil {
		r.ServiceTier = req.ServiceTier
	}
	if req.ExecutionExpiresAfter != nil {
		r.ExecutionExpiresAfter = lo.ToPtr(dto.IntValue(*req.ExecutionExpiresAfter))
	}
	if req.Resolution != "" {
		r.Resolution = lo.ToPtr(req.Resolution)
	}
	if req.Ratio != nil {
		ratio := strings.TrimSpace(*req.Ratio)
		r.Ratio = lo.ToPtr(ratio)
	} else if ratio := firstNonEmptyString(req.AspectRatio, req.Size); ratio != "" {
		r.Ratio = lo.ToPtr(ratio)
	}
	if duration := req.DurationValue(); duration > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(duration))
	}
	if req.DurationSeconds != nil {
		r.Duration = lo.ToPtr(dto.IntValue(*req.DurationSeconds))
	}
	if req.GenerateAudio != nil {
		r.GenerateAudio = lo.ToPtr(dto.BoolValue(*req.GenerateAudio))
	} else if req.WithAudio != nil {
		r.GenerateAudio = lo.ToPtr(dto.BoolValue(*req.WithAudio))
	}
	if req.Draft != nil {
		r.Draft = lo.ToPtr(dto.BoolValue(*req.Draft))
	}
	if len(req.Tools) > 0 {
		tools, err := topLevelTools(req.Tools)
		if err != nil {
			return err
		}
		r.Tools = tools
	}
	if req.SafetyIdentifier != nil {
		r.SafetyIdentifier = req.SafetyIdentifier
	}
	if req.Priority != nil {
		r.Priority = lo.ToPtr(dto.IntValue(*req.Priority))
	}
	if req.Frames != nil {
		r.Frames = lo.ToPtr(dto.IntValue(*req.Frames))
	}
	if req.Seed != nil {
		r.Seed = lo.ToPtr(dto.IntValue(*req.Seed))
	}
	if req.CameraFixed != nil {
		r.CameraFixed = lo.ToPtr(dto.BoolValue(*req.CameraFixed))
	}
	if req.Watermark != nil {
		r.Watermark = lo.ToPtr(dto.BoolValue(*req.Watermark))
	}
	return nil
}

func topLevelContentItems(raw []map[string]any) ([]ContentItem, error) {
	data, err := common.Marshal(raw)
	if err != nil {
		return nil, errors.Wrap(err, "marshal top-level content failed")
	}
	var items []ContentItem
	if err := common.Unmarshal(data, &items); err != nil {
		return nil, errors.Wrap(err, "unmarshal top-level content failed")
	}
	return items, nil
}

func topLevelTools(raw []map[string]any) ([]struct {
	Type string `json:"type,omitempty"`
}, error) {
	data, err := common.Marshal(raw)
	if err != nil {
		return nil, errors.Wrap(err, "marshal top-level tools failed")
	}
	var tools []struct {
		Type string `json:"type,omitempty"`
	}
	if err := common.Unmarshal(data, &tools); err != nil {
		return nil, errors.Wrap(err, "unmarshal top-level tools failed")
	}
	return tools, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	if looksLikeSeedanceMediaTask(respBody) {
		if taskResult, ok, err := taskcommon.ParseConfiguredTaskResult(respBody, dto.ChannelOtherSettings{
			TaskProtocol: taskcommon.TaskProtocolGenericVideo,
		}); err != nil {
			return nil, err
		} else if ok {
			if err := a.attachUsageEnvelope(respBody, taskResult); err != nil {
				return nil, err
			}
			return taskResult, nil
		}
	}

	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	envelope, err := buildDoubaoUsageEnvelope(resTask.Usage)
	if err != nil {
		return nil, err
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		// 解析 usage 信息用于按倍率计费
		// Invalid structured facts must not leak into the legacy billing fields:
		// the legacy path may use TotalTokens for a quota recalculation.
		if envelope.Completeness != types.TaskUsageCompletenessInvalid && envelope.Usage != nil {
			if envelope.Usage.CompletionTokens != nil {
				taskResult.CompletionTokens = int(*envelope.Usage.CompletionTokens)
			}
			if envelope.Usage.TotalTokens != nil {
				taskResult.TotalTokens = int(*envelope.Usage.TotalTokens)
			}
		}
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}
	taskResult.UsageEnvelope = envelope
	if envelope != nil {
		taskResult.Usage = types.CloneTaskUsage(envelope.Usage)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) UsageContract() types.TaskUsageContract {
	return taskusage.DoubaoVideoContract()
}

func (a *TaskAdaptor) UsageProducerKind() string {
	return types.TaskUsageProducerKindGoAdapter
}

func (a *TaskAdaptor) ProduceUsage(ctx types.TaskUsageContext) (*types.TaskUsageEnvelope, error) {
	if ctx.Stage != types.TaskUsageSourceProviderResponse {
		return nil, fmt.Errorf("Doubao task usage stage %q is not supported", ctx.Stage)
	}
	payload := ctx.Payload
	for depth := 0; depth < taskcommon.MaxWrappedTaskUnwrapDepth; depth++ {
		nested := taskcommon.WrappedTaskProviderPayload(payload)
		if nested == nil {
			break
		}
		payload = nested
	}
	var response responseTask
	if err := common.Unmarshal(payload, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshal Doubao task usage failed")
	}
	return buildDoubaoUsageEnvelope(response.Usage)
}

func (a *TaskAdaptor) attachUsageEnvelope(respBody []byte, taskResult *relaycommon.TaskInfo) error {
	envelope, err := a.ProduceUsage(types.TaskUsageContext{
		Stage: types.TaskUsageSourceProviderResponse, Payload: respBody,
	})
	if err != nil {
		return err
	}
	taskResult.UsageEnvelope = envelope
	if envelope != nil {
		taskResult.Usage = types.CloneTaskUsage(envelope.Usage)
	}
	return nil
}

func buildDoubaoUsageEnvelope(raw json.RawMessage) (*types.TaskUsageEnvelope, error) {
	var parsed responseTaskUsage
	completeness := types.TaskUsageCompletenessMissing
	switch common.GetJsonType(raw) {
	case "unknown", "null":
		return taskusage.BuildEnvelope(
			types.TaskUsageProducerKindGoAdapter,
			taskusage.DoubaoVideoContract(),
			types.TaskUsageSourceProviderResponse,
			nil,
		)
	case "object":
		if err := common.Unmarshal(raw, &parsed); err != nil {
			completeness = types.TaskUsageCompletenessInvalid
		}
	default:
		completeness = types.TaskUsageCompletenessInvalid
	}
	usage := &types.TaskUsage{
		Source:       types.TaskUsageSourceProviderResponse,
		Completeness: completeness,
	}
	if parsed.CompletionTokens != nil {
		value := int64(*parsed.CompletionTokens)
		usage.CompletionTokens = &value
	}
	if parsed.TotalTokens != nil {
		value := int64(*parsed.TotalTokens)
		usage.TotalTokens = &value
	}
	if completeness != types.TaskUsageCompletenessInvalid {
		switch {
		case parsed.CompletionTokens == nil && parsed.TotalTokens == nil:
			usage = nil
		case (parsed.CompletionTokens != nil && *parsed.CompletionTokens < 0) ||
			(parsed.TotalTokens != nil && *parsed.TotalTokens < 0):
			usage.Completeness = types.TaskUsageCompletenessInvalid
		case parsed.CompletionTokens == nil || parsed.TotalTokens == nil:
			usage.Completeness = types.TaskUsageCompletenessPartial
		case *parsed.TotalTokens < *parsed.CompletionTokens:
			usage.Completeness = types.TaskUsageCompletenessInvalid
		default:
			usage.Completeness = types.TaskUsageCompletenessComplete
		}
	}
	return taskusage.BuildEnvelope(
		types.TaskUsageProducerKindGoAdapter,
		taskusage.DoubaoVideoContract(),
		types.TaskUsageSourceProviderResponse,
		usage,
	)
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	if body, ok, err := taskcommon.ConvertConfiguredTaskToOpenAIVideo(originTask); ok || err != nil {
		return body, err
	}
	if originTask != nil && looksLikeSeedanceMediaTask(originTask.Data) {
		cfg := taskcommon.NormalizeTaskProtocolConfig(nil)
		openAIVideo := dto.NewOpenAIVideo()
		openAIVideo.ID = originTask.TaskID
		openAIVideo.TaskID = originTask.TaskID
		openAIVideo.Status = originTask.Status.ToVideoStatus()
		openAIVideo.SetProgressStr(originTask.Progress)
		urlValue := originTask.GetResultURL()
		if urlValue == "" {
			urlValue = taskcommon.ExtractConfiguredResultURL(originTask.Data, cfg.ResultURLPaths)
		}
		openAIVideo.SetMetadata("url", urlValue)
		openAIVideo.CreatedAt = originTask.CreatedAt
		openAIVideo.CompletedAt = originTask.UpdatedAt
		openAIVideo.Model = originTask.Properties.OriginModelName

		if originTask.Status == model.TaskStatusFailure {
			message := originTask.FailReason
			if message == "" {
				message = taskcommon.StringFromGJSONPath(originTask.Data, cfg.ErrorMessagePath)
			}
			openAIVideo.Error = &dto.OpenAIVideoError{
				Message: message,
				Code:    "upstream_error",
			}
		}
		return common.Marshal(openAIVideo)
	}

	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.SetMetadata("url", dResp.Content.VideoURL)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: dResp.Error.Message,
			Code:    dResp.Error.Code,
		}
	}

	return common.Marshal(openAIVideo)
}

func looksLikeSeedanceMediaTask(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return gjson.GetBytes(data, "task_id").Exists() &&
		(gjson.GetBytes(data, "status").Exists() || gjson.GetBytes(data, "object").String() == "media.task")
}

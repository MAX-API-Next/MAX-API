package hailuo

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
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

// https://platform.minimaxi.com/docs/api-reference/video-generation-intro
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
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if isUnsupportedH3Model(h3RequestModel(&req, info)) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("%s is not supported by this adaptor", H3MaxModel), "unsupported_model", http.StatusBadRequest)
	}
	if h3RequestUsesModel(&req, info) {
		if _, err := buildH3Request(&req, info); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if isUnsupportedH3Model(h3RequestModel(nil, info)) {
		return "", fmt.Errorf("%s is not supported by this adaptor", H3MaxModel)
	}
	endpoint := TextToVideoEndpoint
	if isH3Model(h3RequestModel(nil, info)) {
		endpoint = H3TextToVideoEndpoint
	}
	fallback := fmt.Sprintf("%s%s", strings.TrimRight(a.baseURL, "/"), endpoint)
	return taskcommon.BuildTaskSubmitURL(info, fallback), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
	}
	if isUnsupportedH3Model(h3RequestModel(&req, info)) {
		return nil, fmt.Errorf("%s is not supported by this adaptor", H3MaxModel)
	}

	var (
		body any
		err  error
	)
	if h3RequestUsesModel(&req, info) {
		body, err = buildH3Request(&req, info)
	} else {
		body, err = a.convertToRequestPayload(&req, info)
	}
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

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

	if isH3Model(h3RequestModel(nil, info)) {
		var h3Resp struct {
			TaskID   string      `json:"task_id"`
			Error    *H3APIError `json:"error,omitempty"`
			BaseResp *BaseResp   `json:"base_resp,omitempty"`
		}
		if err := common.Unmarshal(responseBody, &h3Resp); err != nil {
			taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
			return
		}
		if h3Resp.BaseResp != nil && h3Resp.BaseResp.StatusCode != StatusSuccess {
			message := strings.TrimSpace(h3Resp.BaseResp.StatusMsg)
			if message == "" {
				message = "H3 submit failed"
			}
			taskErr = service.TaskErrorWrapper(fmt.Errorf("H3 api error: %s", message), strconv.Itoa(h3Resp.BaseResp.StatusCode), http.StatusBadRequest)
			return
		}
		if h3Resp.Error != nil {
			message := strings.TrimSpace(h3Resp.Error.Message)
			if message == "" {
				message = "H3 submit failed"
			}
			taskErr = service.TaskErrorWrapper(fmt.Errorf("H3 api error: %s", message), strconv.Itoa(h3APIErrorCode(h3Resp.Error)), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(h3Resp.TaskID) == "" {
			taskErr = service.TaskErrorWrapperLocal(errors.New("H3 submit response missing task id"), "missing_upstream_task_id", http.StatusBadGateway)
			return
		}
		ov := dto.NewOpenAIVideo()
		ov.ID = info.PublicTaskID
		ov.TaskID = info.PublicTaskID
		ov.CreatedAt = time.Now().Unix()
		ov.Model = info.OriginModelName
		c.JSON(http.StatusOK, ov)
		return h3Resp.TaskID, responseBody, nil
	}

	var hResp VideoResponse
	if err := common.Unmarshal(responseBody, &hResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if hResp.BaseResp.StatusCode != StatusSuccess {
		taskErr = service.TaskErrorWrapper(
			fmt.Errorf("hailuo api error: %s", hResp.BaseResp.StatusMsg),
			strconv.Itoa(hResp.BaseResp.StatusCode),
			http.StatusBadRequest,
		)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return hResp.TaskID, responseBody, nil
}

func (a *TaskAdaptor) BuildTaskBillingPlan(c *gin.Context, info *relaycommon.RelayInfo) (*types.TaskBillingPlan, error) {
	if !isH3Model(h3RequestModel(nil, info)) {
		return nil, nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	h3Request, err := buildH3Request(&req, info)
	if err != nil {
		return nil, err
	}
	input := task_billing_setting.H3BillingInput{
		Resolution:            h3Request.Resolution,
		OutputDurationSeconds: int64(h3Request.Duration),
	}
	for _, item := range h3Request.Content {
		switch item.Type {
		case "image_url":
			input.InputImageCount++
		case "video_url":
			input.InputVideoCount++
		case "audio_url":
			input.InputAudioCount++
		}
	}
	groupRatio := 1.0
	if info != nil {
		groupRatio = info.PriceData.GroupRatioInfo.GroupRatio
	}
	return task_billing_setting.BuildH3BillingPlan(input, groupRatio)
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	modelName, _ := body["model"].(string)
	uri := fmt.Sprintf("%s%s?task_id=%s", baseUrl, QueryTaskEndpoint, taskID)
	if isH3Model(modelName) {
		uri = fmt.Sprintf("%s%s/%s", strings.TrimRight(baseUrl, "/"), H3QueryTaskEndpoint, url.PathEscape(taskID))
	}
	uri = taskcommon.BuildTaskQueryURL(baseUrl, body, uri)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
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

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*VideoRequest, error) {
	modelConfig := GetModelConfig(info.UpstreamModelName)
	duration := DefaultDuration
	if requestDuration := req.DurationValue(); requestDuration > 0 {
		duration = requestDuration
	}
	resolution := modelConfig.DefaultResolution
	if req.Size != "" {
		resolution = a.parseResolutionFromSize(req.Size, modelConfig)
	}

	videoRequest := &VideoRequest{
		Model:      info.UpstreamModelName,
		Prompt:     req.Prompt,
		Duration:   &duration,
		Resolution: resolution,
	}
	if err := req.UnmarshalMetadata(&videoRequest); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata to video request failed")
	}

	return videoRequest, nil
}

func (a *TaskAdaptor) parseResolutionFromSize(size string, modelConfig ModelConfig) string {
	switch {
	case strings.Contains(size, "1080"):
		return Resolution1080P
	case strings.Contains(size, "768"):
		return Resolution768P
	case strings.Contains(size, "720"):
		return Resolution720P
	case strings.Contains(size, "512"):
		return Resolution512P
	default:
		return modelConfig.DefaultResolution
	}
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	if result, handled, err := parseH3TaskResult(respBody); handled {
		return result, err
	}

	resTask := QueryTaskResponse{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{}

	if resTask.BaseResp.StatusCode == StatusSuccess {
		taskResult.Code = 0
	} else {
		taskResult.Code = resTask.BaseResp.StatusCode
		taskResult.Reason = resTask.BaseResp.StatusMsg
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
	}

	switch resTask.Status {
	case TaskStatusPreparing, TaskStatusQueueing, TaskStatusProcessing:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
		if resTask.Status == TaskStatusProcessing {
			taskResult.Progress = "50%"
		}
	case TaskStatusSuccess:
		// A completed Hailuo task still needs a retrievable file URL. Reporting
		// success without one settles billing while the video proxy has nothing
		// to serve. Keep polling so a transient file-retrieval failure can recover.
		if videoURL := a.buildVideoURL(resTask.TaskID, resTask.FileID); videoURL != "" {
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Progress = "100%"
			taskResult.Url = videoURL
		} else {
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = "90%"
		}
	case TaskStatusFailed:
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ExtractTaskUsage(respBody []byte) (*types.TaskUsage, error) {
	response, handled, err := parseH3Response(respBody)
	if err != nil {
		return nil, err
	}
	if !handled || response.Task == nil {
		return nil, nil
	}
	return normalizeH3Usage(response.Task.Usage), nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var h3Resp H3QueryResponse
	if err := common.Unmarshal(originTask.Data, &h3Resp); err == nil && (h3Resp.Task != nil || h3Resp.Error != nil) {
		openAIVideo := originTask.ToOpenAIVideo()
		if h3Resp.Error != nil {
			openAIVideo.Status = dto.VideoStatusFailed
			openAIVideo.Error = &dto.OpenAIVideoError{
				Message: h3Resp.Error.Message,
				Code:    strconv.Itoa(h3APIErrorCode(h3Resp.Error)),
			}
		} else {
			switch strings.ToLower(strings.TrimSpace(h3Resp.Task.Status)) {
			case "queued":
				openAIVideo.Status = dto.VideoStatusQueued
			case "running":
				openAIVideo.Status = dto.VideoStatusInProgress
			case "succeeded":
				openAIVideo.Status = dto.VideoStatusCompleted
			case "failed", "cancelled":
				openAIVideo.Status = dto.VideoStatusFailed
				openAIVideo.Error = &dto.OpenAIVideoError{
					Message: h3Resp.Task.ErrorMessage(),
					Code:    strconv.Itoa(h3CodeNumber(h3Resp.Task.ErrorCode())),
				}
			}
			if h3Resp.Task.Content != nil && strings.TrimSpace(h3Resp.Task.Content.URL) != "" {
				openAIVideo.SetMetadata("url", strings.TrimSpace(h3Resp.Task.Content.URL))
			}
		}
		jsonData, err := common.Marshal(openAIVideo)
		if err != nil {
			return nil, errors.Wrap(err, "marshal openai video failed")
		}
		return jsonData, nil
	}

	var hailuoResp QueryTaskResponse
	if err := common.Unmarshal(originTask.Data, &hailuoResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal hailuo task data failed")
	}

	openAIVideo := originTask.ToOpenAIVideo()
	if hailuoResp.BaseResp.StatusCode != StatusSuccess {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: hailuoResp.BaseResp.StatusMsg,
			Code:    strconv.Itoa(hailuoResp.BaseResp.StatusCode),
		}
	}

	jsonData, err := common.Marshal(openAIVideo)
	if err != nil {
		return nil, errors.Wrap(err, "marshal openai video failed")
	}

	return jsonData, nil
}

func (a *TaskAdaptor) buildVideoURL(_, fileID string) string {
	if a.apiKey == "" || a.baseURL == "" {
		return ""
	}

	url := fmt.Sprintf("%s/v1/files/retrieve?file_id=%s", a.baseURL, fileID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var retrieveResp RetrieveFileResponse
	if err := common.Unmarshal(responseBody, &retrieveResp); err != nil {
		return ""
	}

	if retrieveResp.BaseResp.StatusCode != StatusSuccess {
		return ""
	}

	return retrieveResp.File.DownloadURL
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsInt(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

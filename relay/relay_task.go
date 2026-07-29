package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int
	Task           *model.Task
	response       *taskResponseSnapshot
	//PerCallPrice   types.PriceData
}

func (r *TaskSubmitResult) WriteResponse(c *gin.Context) error {
	if r == nil {
		return errors.New("task submit result is nil")
	}
	return r.response.writeTo(c)
}

func populateTaskBillingMetadata(task *model.Task, info *relaycommon.RelayInfo) {
	if task == nil || info == nil {
		return
	}
	deltaSettlementDisabled := false
	if info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeDoubaoVideo {
		deltaSettlementDisabled = info.ChannelOtherSettings.DisableTaskDeltaSettlement
	}
	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.BillingRequestId = info.RequestId
	task.PrivateData.SubscriptionId = info.SubscriptionId
	task.PrivateData.TokenId = info.TokenId
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:              info.PriceData.ModelPrice,
		GroupRatio:              info.PriceData.GroupRatioInfo.GroupRatio,
		ModelRatio:              info.PriceData.ModelRatio,
		OtherRatios:             info.PriceData.OtherRatios,
		TaskBilling:             info.TaskBilling,
		OriginModelName:         info.OriginModelName,
		PerCallBilling:          common.StringsContains(constant.TaskPricePatches, info.OriginModelName) || info.PriceData.UsePrice || info.TaskBilling != nil,
		DeltaSettlementDisabled: common.GetPointer(deltaSettlementDisabled),
	}
	task.Action = info.Action
}

func ensureTaskPlaceholder(platform constant.TaskPlatform, info *relaycommon.RelayInfo) (*model.Task, *dto.TaskError) {
	if info == nil || info.TaskRelayInfo == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task relay metadata is unavailable"), "persist_task_failed", http.StatusInternalServerError)
	}
	if info.PersistedTaskID > 0 {
		var task model.Task
		if err := model.DB.First(&task, info.PersistedTaskID).Error; err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "load_persisted_task_failed", http.StatusInternalServerError)
		}
		fresh := model.InitTask(platform, info)
		task.Platform = platform
		task.ChannelId = fresh.ChannelId
		task.Group = fresh.Group
		task.Properties = fresh.Properties
		task.PrivateData.Key = fresh.PrivateData.Key
		task.PrivateData.AwaitingUpstreamID = true
		task.Quota = info.PriceData.Quota
		populateTaskBillingMetadata(&task, info)
		if err := task.Update(); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "update_persisted_task_failed", http.StatusInternalServerError)
		}
		return &task, nil
	}

	task := model.InitTask(platform, info)
	task.Quota = info.PriceData.Quota
	task.PrivateData.AwaitingUpstreamID = true
	populateTaskBillingMetadata(task, info)
	if err := task.Insert(); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "persist_task_failed", http.StatusInternalServerError)
	}
	info.PersistedTaskID = task.ID
	return task, nil
}

type taskBillingEstimator interface {
	EstimateTaskBilling(c *gin.Context, info *relaycommon.RelayInfo) (*types.TaskBillingResult, error)
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, _, maxAPIError := ch.GetNextEnabledKey()
		if maxAPIError != nil {
			return service.TaskErrorWrapper(maxAPIError, "channel_no_available_key", maxAPIError.StatusCode)
		}
		common.SetContextKey(c, constant.ContextKeyChannelKey, key)
		common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			if seconds > relaycommon.MaxTaskDurationSeconds {
				seconds = relaycommon.MaxTaskDurationSeconds
			}
			sizeStr, _ := taskData["size"].(string)
			info.PriceData.AddOtherRatio("seconds", float64(seconds))
			info.PriceData.AddOtherRatio("size", 1)
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.AddOtherRatio("size", 1.666667)
			}
		}
	}

	return nil
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 2.5 应用渠道的模型映射（与同步任务对齐）
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	// 4. 构建最终请求体（含 Param Override），并暴露给任务计费估算。
	requestBody, taskErr := prepareTaskSubmitRequestBody(c, info, adaptor)
	if taskErr != nil {
		return nil, taskErr
	}

	// 5. 价格计算：基础模型价格
	info.OriginModelName = modelName
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
	}
	info.PriceData = priceData

	// 6. Prefer a parameterized rate card when the adaptor can normalize the
	// request. Legacy task models continue to use OtherRatios.
	taskBillingOverride := false
	taskBilling, err := estimateTaskBilling(c, info, adaptor, platform)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "task_billing_rule_error", http.StatusBadRequest)
	}
	if taskBilling != nil {
		info.TaskBilling = taskBilling
		info.PriceData.Quota = taskBilling.Quota
		taskBillingOverride = true
	}

	if !taskBillingOverride {
		if estimatedRatios := adaptor.EstimateBilling(c, info); len(estimatedRatios) > 0 {
			for k, v := range estimatedRatios {
				info.PriceData.AddOtherRatio(k, v)
			}
		}

		if !common.StringsContains(constant.TaskPricePatches, modelName) {
			quotaWithRatios := float64(info.PriceData.Quota)
			for _, ra := range info.PriceData.OtherRatios {
				if ra != 1.0 {
					quotaWithRatios *= ra
				}
			}
			quota, clamp := common.QuotaFromFloatChecked(quotaWithRatios)
			info.PriceData.Quota = quota
			noteTaskQuotaClamp(info, clamp)
		}
	}

	// 7. 预扣费（仅首次 — 重试时 info.Billing 已存在，跳过）
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, info.PriceData.Quota, info); apiErr != nil {
			return nil, service.TaskErrorFromAPIError(apiErr)
		}
	}
	task, taskErr := ensureTaskPlaceholder(platform, info)
	if taskErr != nil {
		return nil, taskErr
	}

	// 9. 发送请求
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		if taskErr := taskErrorFromLocalRelayError(err); taskErr != nil {
			return nil, taskErr
		}
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		taskErr := service.TaskErrorWrapper(fmt.Errorf("%s", string(responseBody)), "fail_to_fetch_task", resp.StatusCode)
		return nil, mapUpstreamTaskError(c, taskErr)
	}
	if resp == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task upstream response is nil"), "empty_upstream_response", http.StatusBadGateway)
	}
	info.UpstreamTaskResponseReceived = true

	// 10-11. Buffer the adapter response until the task row and settlement
	// intent are durable. Adapters may write the body while parsing.
	var upstreamTaskID string
	var taskData []byte
	var responseSnapshot *taskResponseSnapshot
	func() {
		originalWriter := c.Writer
		bufferedWriter := newTaskResponseBuffer(originalWriter)
		c.Writer = bufferedWriter
		defer func() { c.Writer = originalWriter }()

		otherRatios := info.PriceData.OtherRatios
		if otherRatios == nil {
			otherRatios = map[string]float64{}
		}
		ratiosJSON, _ := common.Marshal(otherRatios)
		c.Header("X-Max-Api-Other-Ratios", string(ratiosJSON))
		c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))

		upstreamTaskID, taskData, taskErr = adaptor.DoResponse(c, resp, info)
		responseSnapshot = bufferedWriter.snapshot()
	}()
	if taskErr != nil {
		return nil, mapUpstreamTaskError(c, taskErr)
	}
	if strings.TrimSpace(upstreamTaskID) == "" {
		return nil, service.TaskErrorWrapperLocal(errors.New("task upstream response did not contain a task id"), "missing_upstream_task_id", http.StatusBadGateway)
	}

	// 11. 提交后计费调整：让适配器根据上游实际返回调整 OtherRatios
	finalQuota := info.PriceData.Quota
	if !taskBillingOverride {
		if adjustedRatios := adaptor.AdjustBillingOnSubmit(info, taskData); len(adjustedRatios) > 0 {
			// 基于调整后的 ratios 重新计算 quota
			finalQuota = recalcQuotaFromRatios(info, adjustedRatios)
			info.PriceData.OtherRatios = adjustedRatios
			info.PriceData.Quota = finalQuota
		}
	}
	task.PrivateData.UpstreamTaskID = upstreamTaskID
	task.PrivateData.AwaitingUpstreamID = false
	task.Quota = finalQuota
	task.Data = taskData
	populateTaskBillingMetadata(task, info)

	return &TaskSubmitResult{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		Platform:       platform,
		Quota:          finalQuota,
		Task:           task,
		response:       responseSnapshot,
	}, nil
}

// mapUpstreamTaskError is only called after an upstream response is available.
func mapUpstreamTaskError(c *gin.Context, taskErr *dto.TaskError) *dto.TaskError {
	if c != nil {
		service.ResetTaskStatusCode(taskErr, c.GetString("status_code_mapping"))
	}
	return taskErr
}

func estimateTaskBilling(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.TaskAdaptor, platform constant.TaskPlatform) (*types.TaskBillingResult, error) {
	if estimator, ok := adaptor.(taskBillingEstimator); ok {
		taskBilling, err := estimator.EstimateTaskBilling(c, info)
		if err != nil || taskBilling != nil {
			return taskBilling, err
		}
	}
	channelName := ""
	if adaptor != nil {
		channelName = adaptor.GetChannelName()
	}
	if channelName == "" {
		channelName = string(platform)
	}
	return taskcommon.EstimateGenericTaskBilling(c, info, channelName)
}

func prepareTaskSubmitRequestBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.TaskAdaptor) (io.Reader, *dto.TaskError) {
	relaycommon.ClearTaskSubmitRequestBody(c)
	requestBody, err := buildTaskSubmitRequestBody(c, info, adaptor)
	if err != nil {
		return nil, taskErrorFromBuildRequestError(err)
	}
	if c == nil || c.Request == nil {
		return requestBody, nil
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))), "application/json") &&
		(info == nil || len(info.ParamOverride) == 0) {
		return requestBody, nil
	}
	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}
	if err := taskcommon.SyncTaskRequestContext(c, bodyBytes); err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}
	return bytes.NewReader(bodyBytes), nil
}

func taskErrorFromBuildRequestError(err error) *dto.TaskError {
	if taskcommon.IsTaskParamOverrideError(err) {
		return service.TaskErrorLocalFromAPIError(maxAPIErrorFromParamOverride(err))
	}
	return service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
}

func taskErrorFromLocalRelayError(err error) *dto.TaskError {
	var maxAPIError *types.MaxAPIError
	if !errors.As(err, &maxAPIError) {
		return nil
	}
	switch maxAPIError.GetErrorCode() {
	case types.ErrorCodeChannelParamOverrideInvalid, types.ErrorCodeChannelHeaderOverrideInvalid:
		return service.TaskErrorLocalFromAPIError(maxAPIError)
	default:
		return nil
	}
}

func buildTaskSubmitRequestBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.TaskAdaptor) (io.Reader, error) {
	requestBody, handled, err := taskcommon.BuildConfiguredTaskPassThroughBody(c, info)
	if err != nil {
		return nil, err
	}
	if !handled {
		requestBody, err = adaptor.BuildRequestBody(c, info)
		if err != nil {
			return nil, err
		}
	}
	return taskcommon.ApplyTaskParamOverride(requestBody, info)
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64) int {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	baseQuota := float64(info.PriceData.Quota)
	// 先除掉原有的 OtherRatios 恢复基础额度
	for _, ra := range info.PriceData.OtherRatios {
		if ra != 1.0 && ra > 0 {
			baseQuota /= ra
		}
	}
	// 应用新的 ratios
	result := baseQuota
	for _, ra := range ratios {
		if ra != 1.0 {
			result *= ra
		}
	}
	quota, clamp := common.QuotaFromFloatChecked(result)
	noteTaskQuotaClamp(info, clamp)
	return quota
}

func noteTaskQuotaClamp(info *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if clamp == nil || info == nil {
		return
	}
	if info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")
	isKlingOfficialAPI := c.GetBool("kling_official_route") || strings.HasPrefix(c.Request.RequestURI, "/kling/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isKlingOfficialAPI {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(interface {
			ConvertToKlingOfficialVideo(*model.Task) ([]byte, error)
		}); ok {
			klingData, err := converter.ConvertToKlingOfficialVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_kling_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = klingData
			return
		}
	}

	if isOpenAIVideoAPI {
		if openAIVideoData, ok, err := taskcommon.ConvertConfiguredTaskToOpenAIVideo(originTask); err != nil {
			taskResp = service.TaskErrorWrapper(err, "convert_configured_video_failed", http.StatusInternalServerError)
			return
		} else if ok {
			respBody = openAIVideoData
			return
		}

		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}

	resp, err := adaptor.FetchTask(baseURL, channelModel.Key, taskcommon.WithTaskProtocolConfig(map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   task.Properties.UpstreamModelName,
	}, channelModel.GetOtherSettings()), proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	ti, parsed, parseErr := taskcommon.ParseConfiguredTaskResult(body, channelModel.GetOtherSettings())
	if parseErr != nil {
		return nil
	}
	if !parsed {
		ti, err = adaptor.ParseTaskResult(body)
		if err != nil || ti == nil {
			return nil
		}
	}

	snap := task.Snapshot()

	// 将上游最新状态更新到 task
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}

	// Terminal transitions must go through the polling settlement path. The
	// realtime fetch may report terminal data to this caller, but must not make
	// that state durable without the matching refund/final-settlement intent.
	if !isTerminalTaskStatus(task.Status) && !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

func isTerminalTaskStatus(status model.TaskStatus) bool {
	return status == model.TaskStatusSuccess || status == model.TaskStatusFailure
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}

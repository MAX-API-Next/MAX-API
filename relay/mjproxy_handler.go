package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
)

func writeMidjourneyStatusCode(c *gin.Context, upstreamStatusCode int) {
	c.Writer.WriteHeader(service.MapStatusCode(upstreamStatusCode, c.GetString("status_code_mapping")))
}

func midjourneyTaskLookupErrorDescription(err error) string {
	if errors.Is(err, model.ErrMidjourneyTaskAmbiguous) {
		return "midjourney_task_ambiguous"
	}
	return "midjourney_task_lookup_failed"
}

func classifyMidjourneySubmission(statusCode int, response *dto.MidjourneyResponse) (accepted, ambiguous bool) {
	if response == nil {
		return false, statusCode >= http.StatusInternalServerError
	}
	acceptedCode := response.Code == 1 || response.Code == 21 || response.Code == 22
	hasTaskID := strings.TrimSpace(response.Result) != ""
	if acceptedCode && hasTaskID {
		return true, false
	}
	if acceptedCode || statusCode >= http.StatusInternalServerError {
		return false, true
	}
	return false, false
}

func applyMidjourneyOriginChannel(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) *types.MaxAPIError {
	key, keyIndex, maxAPIError := channel.GetNextEnabledKey()
	if maxAPIError != nil {
		return maxAPIError
	}

	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, channel.GetParamOverride())
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, channel.GetHeaderOverride())
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())
	common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, channel.ChannelInfo.IsMultiKey)
	common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, keyIndex)
	if channel.OpenAIOrganization != nil {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	} else {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, "")
	}

	if info != nil {
		info.InitChannelMeta(c)
	}
	return nil
}

func RelayMidjourneyImage(c *gin.Context) {
	taskId := c.Param("id")
	userID, userErr := strconv.Atoi(c.Query("uid"))
	expiresAt, expiresErr := strconv.ParseInt(c.Query("expires"), 10, 64)
	if userErr != nil || expiresErr != nil ||
		!service.ValidateMidjourneyImageURL(taskId, userID, expiresAt, c.Query("signature"), time.Now()) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "midjourney_image_authorization_failed",
		})
		return
	}
	midjourneyTask, lookupErr := model.GetUniqueMidjourneyByUserAndMJID(userID, taskId)
	if lookupErr != nil && !errors.Is(lookupErr, model.ErrMidjourneyTaskAmbiguous) {
		common.SysError("midjourney image task lookup failed: " + lookupErr.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "midjourney_task_lookup_failed",
		})
		return
	}
	if lookupErr != nil || midjourneyTask == nil {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "midjourney_image_authorization_failed",
		})
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	var httpClient *http.Client
	if channel, err := model.CacheGetChannel(midjourneyTask.ChannelId); err == nil {
		proxy := channel.GetSetting().Proxy
		if proxy != "" {
			if httpClient, err = service.NewProxyHttpClient(proxy); err != nil {
				c.JSON(400, gin.H{
					"error": "proxy_url_invalid",
				})
				return
			}
		}
	}
	if httpClient == nil {
		httpClient = service.GetSSRFProtectedHttpClient()
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(midjourneyTask.ImageUrl, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf("request blocked: %v", err),
		})
		return
	}
	resp, err := httpClient.Get(midjourneyTask.ImageUrl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "http_get_image_failed",
		})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{
			"error": string(responseBody),
		})
		return
	}
	// 从Content-Type头获取MIME类型
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// 如果无法确定内容类型，则默认为jpeg
		contentType = "image/jpeg"
	}
	// 设置响应的内容类型
	c.Writer.Header().Set("Content-Type", contentType)
	// 将图片流式传输到响应体
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Println("Failed to stream image:", err)
	}
	return
}

func RelayMidjourneyNotify(c *gin.Context) *dto.MidjourneyResponse {
	var midjRequest dto.MidjourneyDto
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "bind_request_body_failed",
			Properties:  nil,
			Result:      "",
		}
	}
	midjourneyTask, lookupErr := model.GetUniqueMidjourneyByMJID(midjRequest.MjId)
	if lookupErr != nil {
		return &dto.MidjourneyResponse{
			Code:        constant.MjRequestError,
			Description: midjourneyTaskLookupErrorDescription(lookupErr),
		}
	}
	if midjourneyTask == nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "midjourney_task_not_found",
			Properties:  nil,
			Result:      "",
		}
	}
	err = service.UpdateMidjourneyTaskFromResponse(c.Request.Context(), midjourneyTask, midjRequest)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "update_midjourney_task_failed",
		}
	}

	return nil
}

func coverMidjourneyTaskDto(c *gin.Context, originTask *model.Midjourney) (midjourneyTask dto.MidjourneyDto) {
	midjourneyTask.MjId = originTask.MjId
	midjourneyTask.Progress = originTask.Progress
	midjourneyTask.PromptEn = originTask.PromptEn
	midjourneyTask.State = originTask.State
	midjourneyTask.SubmitTime = originTask.SubmitTime
	midjourneyTask.StartTime = originTask.StartTime
	midjourneyTask.FinishTime = originTask.FinishTime
	midjourneyTask.ImageUrl = ""
	if originTask.ImageUrl != "" && setting.MjForwardUrlEnabled {
		midjourneyTask.ImageUrl = service.BuildMidjourneyImageURL(system_setting.ServerAddress, originTask.MjId, originTask.UserId, time.Now())
		if originTask.Status != "SUCCESS" {
			midjourneyTask.ImageUrl += "&rand=" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	} else {
		midjourneyTask.ImageUrl = originTask.ImageUrl
	}
	if originTask.VideoUrl != "" {
		midjourneyTask.VideoUrl = originTask.VideoUrl
	}
	midjourneyTask.Status = originTask.Status
	midjourneyTask.FailReason = originTask.FailReason
	midjourneyTask.Action = originTask.Action
	midjourneyTask.Description = originTask.Description
	midjourneyTask.Prompt = originTask.Prompt
	if originTask.Buttons != "" {
		var buttons []dto.ActionButton
		err := common.Unmarshal([]byte(originTask.Buttons), &buttons)
		if err == nil {
			midjourneyTask.Buttons = buttons
		}
	}
	if originTask.VideoUrls != "" {
		var videoUrls []dto.ImgUrls
		err := common.Unmarshal([]byte(originTask.VideoUrls), &videoUrls)
		if err == nil {
			midjourneyTask.VideoUrls = videoUrls
		}
	}
	if originTask.Properties != "" {
		var properties dto.Properties
		err := common.Unmarshal([]byte(originTask.Properties), &properties)
		if err == nil {
			midjourneyTask.Properties = &properties
		}
	}
	return
}

func prepareMidjourneyBillingTask(c *gin.Context, info *relaycommon.RelayInfo, action string, priceData types.PriceData, charge bool) (*model.Task, *dto.MidjourneyResponse) {
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	info.Action = action
	info.OriginModelName = service.CovertMjpActionToModelName(action)
	info.PriceData = priceData
	preConsumedQuota := priceData.Quota
	if !charge || priceData.FreeModel {
		info.PriceData.Quota = 0
		info.PriceData.QuotaToPreConsume = 0
		preConsumedQuota = 0
	} else if priceData.GroupRatioInfo.GroupRatio > 0 {
		var err error
		preConsumedQuota, err = helper.ApplyPreConsumedQuotaFloor(priceData.Quota, true)
		if err != nil {
			return nil, &dto.MidjourneyResponse{Code: constant.MjRequestError, Description: err.Error()}
		}
		info.PriceData.QuotaToPreConsume = preConsumedQuota
	}
	if preConsumedQuota > 0 {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, preConsumedQuota, info); apiErr != nil {
			return nil, &dto.MidjourneyResponse{Code: constant.MjRequestError, Description: apiErr.Error()}
		}
	}
	task, taskErr := ensureTaskPlaceholder(constant.TaskPlatformMidjourney, info, preConsumedQuota)
	if taskErr != nil {
		if info.Billing != nil {
			info.Billing.Refund(c)
		}
		return nil, &dto.MidjourneyResponse{Code: constant.MjRequestError, Description: taskErr.Message}
	}
	return task, nil
}

func prepareMidjourneySettlementInput(info *relaycommon.RelayInfo, quota int) (*model.BillingSettlementInput, error) {
	if info == nil || info.Billing == nil {
		return nil, nil
	}
	preparer, ok := info.Billing.(service.SettlementPreparer)
	if !ok {
		return nil, fmt.Errorf("midjourney billing session cannot prepare settlement")
	}
	return preparer.PrepareSettlement(quota)
}

func prepareMidjourneyRefundInput(info *relaycommon.RelayInfo) (*model.BillingSettlementInput, error) {
	if info == nil || info.Billing == nil {
		return nil, nil
	}
	preparer, ok := info.Billing.(service.RefundSettlementPreparer)
	if !ok {
		return nil, fmt.Errorf("midjourney billing session cannot prepare refund")
	}
	return preparer.PrepareRefundSettlement()
}

func failMidjourneySubmission(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, reason string) {
	if task == nil {
		return
	}
	refundInput, err := prepareMidjourneyRefundInput(info)
	if err != nil {
		common.SysError("prepare midjourney submission refund error: " + err.Error())
		_ = model.MarkTaskSubmitAmbiguous(task.ID, reason+"（退款 intent 无法持久化，请人工核对）")
		return
	}
	if err := model.MarkTaskSubmitFailedWithSettlement(task.ID, reason, refundInput); err != nil {
		common.SysError("persist midjourney submission failure error: " + err.Error())
		if info != nil && info.Billing != nil {
			info.Billing.Refund(c)
		}
		return
	}
	if info != nil && info.Billing != nil {
		info.Billing.Refund(c)
	}
}

func markMidjourneySubmissionAmbiguous(info *relaycommon.RelayInfo, task *model.Task, reason string) {
	if info != nil {
		info.UpstreamTaskOutcomeUnknown = true
	}
	if task != nil {
		if err := model.MarkTaskSubmitAmbiguous(task.ID, reason); err != nil {
			common.SysError("mark midjourney submission ambiguous error: " + err.Error())
		}
	}
}

func markMidjourneySubmissionResponseAmbiguous(info *relaycommon.RelayInfo, task *model.Task, midjourneyTask *model.Midjourney, reason string) {
	if info != nil {
		info.UpstreamTaskOutcomeUnknown = true
	}
	if task == nil {
		return
	}
	if midjourneyTask == nil || strings.TrimSpace(midjourneyTask.MjId) == "" {
		if err := model.MarkTaskSubmitAmbiguous(task.ID, reason); err != nil {
			common.SysError("mark midjourney submission ambiguous error: " + err.Error())
		}
		return
	}
	task.PrivateData.UpstreamTaskID = midjourneyTask.MjId
	task.PrivateData.AwaitingUpstreamID = false
	task.SetData(midjourneyTask)
	if err := model.MarkTaskSubmitNeedsReview(task, reason); err != nil {
		common.SysError("mark midjourney submission for manual review error: " + err.Error())
	}
}

type midjourneyBillingSettlementPendingError struct {
	taskID string
	err    error
}

func (e *midjourneyBillingSettlementPendingError) Error() string {
	return fmt.Sprintf("midjourney billing settlement requires reconciliation: %v", e.err)
}

func (e *midjourneyBillingSettlementPendingError) Unwrap() error {
	return e.err
}

func midjourneyBillingSettlementPendingResponse(taskID string) *dto.MidjourneyResponse {
	return &dto.MidjourneyResponse{
		Code:        constant.MjRequestError,
		Description: constant.MjBillingSettlementPending,
		Result:      taskID,
	}
}

func midjourneySubmissionErrorResponse(err error) *dto.MidjourneyResponse {
	var settlementErr *midjourneyBillingSettlementPendingError
	if errors.As(err, &settlementErr) {
		return midjourneyBillingSettlementPendingResponse(settlementErr.taskID)
	}
	return service.MidjourneyErrorWrapper(constant.MjRequestError, "persist_midjourney_task_failed")
}

func finalizeMidjourneySubmission(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, midjourneyTask *model.Midjourney) (bool, error) {
	if task == nil || midjourneyTask == nil {
		return false, errors.New("midjourney submission result is incomplete")
	}
	task.SetData(midjourneyTask)
	settlementInput, err := prepareMidjourneySettlementInput(info, midjourneyTask.Quota)
	if err != nil {
		return false, err
	}
	settlementOperationKey := ""
	if settlementInput != nil {
		settlementInput.Effect = service.BuildTaskSubmissionSettlementEffect(c, info, midjourneyTask.Quota)
		settlementOperationKey = settlementInput.OperationKey
	}
	refundInput, err := prepareMidjourneyRefundInput(info)
	if err != nil {
		return false, err
	}
	created, refundDuplicate, err := model.FinalizeMidjourneySubmission(midjourneyTask, task, settlementInput, refundInput)
	if err != nil {
		task.PrivateData.UpstreamTaskID = midjourneyTask.MjId
		task.PrivateData.AwaitingUpstreamID = false
		markErr := model.MarkTaskSubmitNeedsReview(task, "上游已接受 Midjourney 任务，但本地任务/结算持久化失败："+err.Error())
		if markErr != nil {
			common.SysError("mark midjourney submission for manual review error: " + markErr.Error())
		}
		if info != nil {
			info.UpstreamTaskOutcomeUnknown = true
		}
		return false, err
	}
	if !created {
		if refundDuplicate && info != nil && info.Billing != nil {
			info.Billing.Refund(c)
		}
		return false, nil
	}
	if info != nil && info.Billing != nil {
		if err := service.SettleBilling(c, info, midjourneyTask.Quota); err != nil {
			common.SysError("midjourney billing settlement requires reconciliation: " + err.Error())
			return false, &midjourneyBillingSettlementPendingError{taskID: midjourneyTask.MjId, err: err}
		}
		if settlementOperationKey != "" {
			if err := model.ProcessBillingSettlementEffect(settlementOperationKey); err != nil {
				common.SysError("midjourney billing effect remains pending: " + err.Error())
			}
		}
	}
	return true, nil
}

func RelaySwapFace(c *gin.Context, info *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	var swapFaceRequest dto.SwapFaceRequest
	err := common.UnmarshalBodyReusable(c, &swapFaceRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	info.InitChannelMeta(c)

	if swapFaceRequest.SourceBase64 == "" || swapFaceRequest.TargetBase64 == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "sour_base64_and_target_base64_is_required")
	}
	info.OriginModelName = service.CovertMjpActionToModelName(constant.MjActionSwapFace)
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	billingTask, billingErr := prepareMidjourneyBillingTask(c, info, constant.MjActionSwapFace, priceData, true)
	if billingErr != nil {
		return billingErr
	}
	requestURL := getMjRequestPath(c.Request.URL.String())
	baseURL := c.GetString("base_url")
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)
	mjResp, _, requestSent, err := service.DoMidjourneyHttpRequest(c, time.Second*60, fullRequestURL)
	if err != nil {
		if requestSent {
			markMidjourneySubmissionAmbiguous(info, billingTask, "Midjourney 换脸请求已发送但结果未知，请人工核对")
		} else {
			failMidjourneySubmission(c, info, billingTask, err.Error())
		}
		return &mjResp.Response
	}
	midjResponse := &mjResp.Response
	midjourneyTask := &model.Midjourney{
		UserId:      info.UserId,
		Code:        midjResponse.Code,
		Action:      constant.MjActionSwapFace,
		MjId:        midjResponse.Result,
		Prompt:      "InsightFace",
		PromptEn:    "",
		Description: midjResponse.Description,
		State:       "",
		SubmitTime:  info.StartTime.UnixNano() / int64(time.Millisecond),
		StartTime:   time.Now().UnixNano() / int64(time.Millisecond),
		FinishTime:  0,
		ImageUrl:    "",
		Status:      "",
		Progress:    "0%",
		FailReason:  "",
		ChannelId:   c.GetInt("channel_id"),
		Quota:       info.PriceData.Quota,
	}
	accepted, ambiguous := classifyMidjourneySubmission(mjResp.StatusCode, midjResponse)
	if ambiguous {
		reason := fmt.Sprintf("Midjourney 换脸响应结果不确定（status=%d code=%d），已保留预扣费待人工核对", mjResp.StatusCode, midjResponse.Code)
		markMidjourneySubmissionResponseAmbiguous(info, billingTask, midjourneyTask, reason)
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "midjourney_submission_outcome_unknown")
	}
	if !accepted {
		failMidjourneySubmission(c, info, billingTask, midjResponse.Description)
	} else {
		if _, err := finalizeMidjourneySubmission(c, info, billingTask, midjourneyTask); err != nil {
			return midjourneySubmissionErrorResponse(err)
		}
	}
	writeMidjourneyStatusCode(c, mjResp.StatusCode)
	respBody, err := common.Marshal(midjResponse)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unmarshal_response_body_failed")
	}
	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "copy_response_body_failed")
	}
	return nil
}

func RelayMidjourneyTaskImageSeed(c *gin.Context) *dto.MidjourneyResponse {
	taskId := c.Param("id")
	userId := c.GetInt("id")
	originTask, lookupErr := model.GetUniqueMidjourneyByUserAndMJID(userId, taskId)
	if lookupErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, midjourneyTaskLookupErrorDescription(lookupErr))
	}
	if originTask == nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_no_found")
	}
	channel, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
	}
	if maxAPIError := applyMidjourneyOriginChannel(c, nil, channel); maxAPIError != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, maxAPIError.Error())
	}

	requestURL := getMjRequestPath(c.Request.URL.String())
	fullRequestURL := fmt.Sprintf("%s%s", channel.GetBaseURL(), requestURL)
	midjResponseWithStatus, _, _, err := service.DoMidjourneyHttpRequest(c, time.Second*30, fullRequestURL)
	if err != nil {
		return &midjResponseWithStatus.Response
	}
	midjResponse := &midjResponseWithStatus.Response
	writeMidjourneyStatusCode(c, midjResponseWithStatus.StatusCode)
	respBody, err := common.Marshal(midjResponse)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unmarshal_response_body_failed")
	}
	service.IOCopyBytesGracefully(c, nil, respBody)
	return nil
}

func RelayMidjourneyTask(c *gin.Context, relayMode int) *dto.MidjourneyResponse {
	userId := c.GetInt("id")
	var err error
	var respBody []byte
	switch relayMode {
	case relayconstant.RelayModeMidjourneyTaskFetch:
		taskId := c.Param("id")
		originTask, lookupErr := model.GetUniqueMidjourneyByUserAndMJID(userId, taskId)
		if lookupErr != nil {
			return &dto.MidjourneyResponse{
				Code:        constant.MjRequestError,
				Description: midjourneyTaskLookupErrorDescription(lookupErr),
			}
		}
		if originTask == nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "task_no_found",
			}
		}
		midjourneyTask := coverMidjourneyTaskDto(c, originTask)
		respBody, err = common.Marshal(midjourneyTask)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	case relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		var condition = struct {
			IDs []string `json:"ids"`
		}{}
		err = c.BindJSON(&condition)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "do_request_failed",
			}
		}
		var tasks []dto.MidjourneyDto
		if len(condition.IDs) != 0 {
			originTasks := model.GetByMJIds(userId, condition.IDs)
			for _, originTask := range originTasks {
				midjourneyTask := coverMidjourneyTaskDto(c, originTask)
				tasks = append(tasks, midjourneyTask)
			}
		}
		if tasks == nil {
			tasks = make([]dto.MidjourneyDto, 0)
		}
		respBody, err = common.Marshal(tasks)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")

	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	return nil
}

func RelayMidjourneySubmit(c *gin.Context, relayInfo *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	consumeQuota := true
	var midjRequest dto.MidjourneyRequest
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	relayInfo.InitChannelMeta(c)

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyAction { // midjourney plus，需要从customId中获取任务信息
		mjErr := service.CoverPlusActionToNormalAction(&midjRequest)
		if mjErr != nil {
			return mjErr
		}
		relayInfo.RelayMode = relayconstant.RelayModeMidjourneyChange
	}
	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
		midjRequest.Action = constant.MjActionVideo
	}

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyImagine { //绘画任务，此类任务可重复
		if midjRequest.Prompt == "" {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "prompt_is_required")
		}
		midjRequest.Action = constant.MjActionImagine
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyDescribe { //按图生文任务，此类任务可重复
		midjRequest.Action = constant.MjActionDescribe
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyEdits { //编辑任务，此类任务可重复
		midjRequest.Action = constant.MjActionEdits
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyShorten { //缩短任务，此类任务可重复，plus only
		midjRequest.Action = constant.MjActionShorten
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyBlend { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionBlend
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyUpload { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionUpload
	} else if midjRequest.TaskId != "" { //放大、变换任务，此类任务，如果重复且已有结果，远端api会直接返回最终结果
		mjId := ""
		if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyChange {
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			} else if midjRequest.Index == 0 {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "index_is_required")
			}
			//action = midjRequest.Action
			mjId = midjRequest.TaskId
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneySimpleChange {
			if midjRequest.Content == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_is_required")
			}
			params := service.ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
			}
			mjId = params.TaskId
			midjRequest.Action = params.Action
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyModal {
			//if midjRequest.MaskBase64 == "" {
			//	return service.MidjourneyErrorWrapper(constant.MjRequestError, "mask_base64_is_required")
			//}
			mjId = midjRequest.TaskId
			midjRequest.Action = constant.MjActionModal
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
			midjRequest.Action = constant.MjActionVideo
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			}
			mjId = midjRequest.TaskId
		}

		originTask, lookupErr := model.GetUniqueMidjourneyByUserAndMJID(relayInfo.UserId, mjId)
		if lookupErr != nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, midjourneyTaskLookupErrorDescription(lookupErr))
		}
		if originTask == nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_not_found")
		} else { //原任务的Status=SUCCESS，则可以做放大UPSCALE、变换VARIATION等动作，此时必须使用原来的请求地址才能正确处理
			if setting.MjActionCheckSuccessEnabled {
				if originTask.Status != "SUCCESS" && relayInfo.RelayMode != relayconstant.RelayModeMidjourneyModal {
					return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_status_not_success")
				}
			}
			channel, err := model.GetChannelById(originTask.ChannelId, true)
			if err != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
			}
			if channel.Status != common.ChannelStatusEnabled {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
			}
			if maxAPIError := applyMidjourneyOriginChannel(c, relayInfo, channel); maxAPIError != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, maxAPIError.Error())
			}
			logger.LogDebug(c, "Midjourney action uses origin channel: id=%s, base_url=%s", strconv.Itoa(originTask.ChannelId), channel.GetBaseURL())
		}
		midjRequest.Prompt = originTask.Prompt

		//if channelType == common.ChannelTypeMidjourneyPlus {
		//	// plus
		//} else {
		//	// 普通版渠道
		//
		//}
	}

	if midjRequest.Action == constant.MjActionInPaint || midjRequest.Action == constant.MjActionCustomZoom {
		consumeQuota = false
	}

	//baseURL := common.ChannelBaseURLs[channelType]
	requestURL := getMjRequestPath(c.Request.URL.String())

	baseURL := c.GetString("base_url")

	//midjRequest.NotifyHook = "http://127.0.0.1:3000/mj/notify"

	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	relayInfo.OriginModelName = service.CovertMjpActionToModelName(midjRequest.Action)
	priceData, err := helper.ModelPriceHelperPerCall(c, relayInfo)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	billingTask, billingErr := prepareMidjourneyBillingTask(c, relayInfo, midjRequest.Action, priceData, consumeQuota)
	if billingErr != nil {
		return billingErr
	}

	midjResponseWithStatus, responseBody, requestSent, err := service.DoMidjourneyHttpRequest(c, time.Second*60, fullRequestURL)
	if err != nil {
		if requestSent {
			markMidjourneySubmissionAmbiguous(relayInfo, billingTask, "Midjourney 请求已发送但结果未知，请人工核对")
		} else {
			failMidjourneySubmission(c, relayInfo, billingTask, err.Error())
		}
		return &midjResponseWithStatus.Response
	}
	midjResponse := &midjResponseWithStatus.Response

	// 文档：https://github.com/novicezk/midjourney-proxy/blob/main/docs/api.md
	//1-提交成功
	// 21-任务已存在（处理中或者有结果了） {"code":21,"description":"任务已存在","result":"0741798445574458","properties":{"status":"SUCCESS","imageUrl":"https://xxxx"}}
	// 22-排队中 {"code":22,"description":"排队中，前面还有1个任务","result":"0741798445574458","properties":{"numberOfQueues":1,"discordInstanceId":"1118138338562560102"}}
	// 23-队列已满，请稍后再试 {"code":23,"description":"队列已满，请稍后尝试","result":"14001929738841620","properties":{"discordInstanceId":"1118138338562560102"}}
	// 24-prompt包含敏感词 {"code":24,"description":"可能包含敏感词","properties":{"promptEn":"nude body","bannedWord":"nude"}}
	// other: 提交错误，description为错误描述
	midjourneyTask := &model.Midjourney{
		UserId:      relayInfo.UserId,
		Code:        midjResponse.Code,
		Action:      midjRequest.Action,
		MjId:        midjResponse.Result,
		Prompt:      midjRequest.Prompt,
		PromptEn:    "",
		Description: midjResponse.Description,
		State:       "",
		SubmitTime:  time.Now().UnixNano() / int64(time.Millisecond),
		StartTime:   0,
		FinishTime:  0,
		ImageUrl:    "",
		Status:      "",
		Progress:    "0%",
		FailReason:  "",
		ChannelId:   c.GetInt("channel_id"),
		Quota:       relayInfo.PriceData.Quota,
	}
	if midjResponse.Code == 3 {
		//无实例账号自动禁用渠道（No available account instance）
		channel, err := model.GetChannelById(midjourneyTask.ChannelId, true)
		if err != nil {
			common.SysLog("get_channel_null: " + err.Error())
		} else if channel.GetAutoBan() && common.AutomaticDisableChannelEnabled {
			model.UpdateChannelStatus(midjourneyTask.ChannelId, "", 2, "No available account instance")
		}
	}
	if midjResponse.Code != 1 && midjResponse.Code != 21 && midjResponse.Code != 22 {
		//非1-提交成功,21-任务已存在和22-排队中，则记录错误原因
		midjourneyTask.FailReason = midjResponse.Description
	}

	if midjResponse.Code == 21 { //21-任务已存在（处理中或者有结果了）
		// 将 properties 转换为一个 map
		properties, ok := midjResponse.Properties.(map[string]interface{})
		if ok {
			imageUrl, ok1 := properties["imageUrl"].(string)
			status, ok2 := properties["status"].(string)
			if ok1 && ok2 {
				midjourneyTask.ImageUrl = imageUrl
				midjourneyTask.Status = status
				if status == "SUCCESS" {
					midjourneyTask.Progress = "100%"
					midjourneyTask.StartTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjourneyTask.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjResponse.Code = 1
				}
			}
		}
		//修改返回值
		if midjRequest.Action != constant.MjActionInPaint && midjRequest.Action != constant.MjActionCustomZoom {
			newBody := strings.Replace(string(responseBody), `"code":21`, `"code":1`, -1)
			responseBody = []byte(newBody)
		}
	}
	if midjResponse.Code == 1 && midjRequest.Action == "UPLOAD" {
		midjourneyTask.Progress = "100%"
		midjourneyTask.Status = "SUCCESS"
	}
	if midjResponse.Code == 22 { //22-排队中，说明任务已存在
		//修改返回值
		newBody := strings.Replace(string(responseBody), `"code":22`, `"code":1`, -1)
		responseBody = []byte(newBody)
	}
	accepted, ambiguous := classifyMidjourneySubmission(midjResponseWithStatus.StatusCode, midjResponse)
	if ambiguous {
		reason := fmt.Sprintf("Midjourney 响应结果不确定（status=%d code=%d），已保留预扣费待人工核对", midjResponseWithStatus.StatusCode, midjResponse.Code)
		markMidjourneySubmissionResponseAmbiguous(relayInfo, billingTask, midjourneyTask, reason)
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "midjourney_submission_outcome_unknown")
	}
	if !accepted {
		failMidjourneySubmission(c, relayInfo, billingTask, midjResponse.Description)
	} else if _, err := finalizeMidjourneySubmission(c, relayInfo, billingTask, midjourneyTask); err != nil {
		return midjourneySubmissionErrorResponse(err)
	}
	//resp.Body = io.NopCloser(bytes.NewBuffer(responseBody))
	bodyReader := io.NopCloser(bytes.NewBuffer(responseBody))

	//for k, v := range resp.Header {
	//	c.Writer.Header().Set(k, v[0])
	//}
	writeMidjourneyStatusCode(c, midjResponseWithStatus.StatusCode)

	_, err = io.Copy(c.Writer, bodyReader)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	err = bodyReader.Close()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "close_response_body_failed",
		}
	}
	return nil
}

type taskChangeParams struct {
	ID     string
	Action string
	Index  int
}

func getMjRequestPath(path string) string {
	requestURL := path
	if strings.Contains(requestURL, "/mj-") {
		urls := strings.Split(requestURL, "/mj/")
		if len(urls) < 2 {
			return requestURL
		}
		requestURL = "/mj/" + urls[1]
	}
	return requestURL
}

package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func UpdateMidjourneyTaskBulk() {
	ctx := context.Background()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.LogError(ctx, fmt.Sprintf("Midjourney polling cycle panic: %v", recovered))
				}
			}()
			if err := updateMidjourneyTasksOnce(ctx); err != nil {
				logger.LogError(ctx, "UpdateMidjourneyTask cycle error: "+err.Error())
			}
		}()
	}
}

func updateMidjourneyTasksOnce(ctx context.Context) error {
	tasks := model.GetAllUnFinishTasks()
	if len(tasks) == 0 {
		return nil
	}
	logger.LogInfo(ctx, fmt.Sprintf("检测到未完成的任务数有: %v", len(tasks)))
	taskChannelM := make(map[int][]string)
	taskM := make(map[int]map[string]*model.Midjourney)
	for _, task := range tasks {
		if task.MjId == "" {
			if err := failMidjourneyTask(ctx, task, "上游任务 ID 为空，无法继续轮询"); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null mj_id task error: %v", err))
			}
			continue
		}
		if taskM[task.ChannelId] == nil {
			taskM[task.ChannelId] = make(map[string]*model.Midjourney)
		}
		taskM[task.ChannelId][task.MjId] = task
		taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task.MjId)
	}

	for channelID, taskIDs := range taskChannelM {
		logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelID, len(taskIDs)))
		channel, err := model.CacheGetChannel(channelID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("CacheGetChannel: %v", err))
			continue
		}
		responseItems, err := fetchMidjourneyTaskUpdates(ctx, channel, taskIDs)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Midjourney tasks failed for channel #%d: %v", channelID, err))
			continue
		}
		for _, responseItem := range responseItems {
			if err := applyMidjourneyTaskResponse(ctx, taskM[channelID], responseItem); err != nil {
				logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
			}
		}
	}
	return nil
}

func fetchMidjourneyTaskUpdates(parent context.Context, channel *model.Channel, taskIDs []string) ([]dto.MidjourneyDto, error) {
	key, _, maxAPIError := channel.GetNextEnabledKey()
	if maxAPIError != nil {
		return nil, maxAPIError
	}
	body, err := common.Marshal(map[string]any{"ids": taskIDs})
	if err != nil {
		return nil, err
	}
	requestURL := fmt.Sprintf("%s/mj/task/list-by-condition", channel.GetBaseURL())
	requestCtx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer req.Body.Close()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("mj-api-secret", key)
	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var responseItems []dto.MidjourneyDto
	if err := common.Unmarshal(responseBody, &responseItems); err != nil {
		return nil, fmt.Errorf("parse Midjourney task response: %w", err)
	}
	return responseItems, nil
}

func applyMidjourneyTaskResponse(ctx context.Context, taskM map[string]*model.Midjourney, responseItem dto.MidjourneyDto) error {
	task := taskM[responseItem.MjId]
	if task == nil {
		logger.LogWarn(ctx, fmt.Sprintf("忽略未知 Midjourney 任务 ID: %s", responseItem.MjId))
		return nil
	}
	useTime := time.Now().UnixMilli() - task.SubmitTime
	if useTime > 3600000 && task.Progress != "100%" {
		responseItem.FailReason = "上游任务超时（超过1小时）"
		responseItem.Status = "FAILURE"
	}
	return updateMidjourneyTaskFromResponse(ctx, task, responseItem)
}

func failMidjourneyTask(ctx context.Context, task *model.Midjourney, reason string) error {
	return updateMidjourneyTaskFromResponse(ctx, task, dto.MidjourneyDto{
		MjId: task.MjId, Status: "FAILURE", Progress: "100%", FailReason: reason,
		PromptEn: task.PromptEn, State: task.State, SubmitTime: task.SubmitTime,
		StartTime: task.StartTime, FinishTime: time.Now().UnixMilli(),
		ImageUrl: task.ImageUrl, VideoUrl: task.VideoUrl,
	})
}

func updateMidjourneyTaskFromResponse(ctx context.Context, task *model.Midjourney, responseItem dto.MidjourneyDto) error {
	if !checkMjTaskNeedUpdate(task, responseItem) {
		return nil
	}
	preStatus := task.Status
	task.Code = 1
	if responseItem.Progress != "" {
		task.Progress = responseItem.Progress
	}
	if responseItem.PromptEn != "" {
		task.PromptEn = responseItem.PromptEn
	}
	if responseItem.State != "" {
		task.State = responseItem.State
	}
	if responseItem.SubmitTime > 0 {
		task.SubmitTime = responseItem.SubmitTime
	}
	if responseItem.StartTime > 0 {
		task.StartTime = responseItem.StartTime
	}
	if responseItem.FinishTime > 0 {
		task.FinishTime = responseItem.FinishTime
	}
	if responseItem.ImageUrl != "" {
		task.ImageUrl = responseItem.ImageUrl
	}
	if responseItem.VideoUrl != "" {
		task.VideoUrl = responseItem.VideoUrl
	}
	if responseItem.Status != "" {
		task.Status = responseItem.Status
	}
	if responseItem.FailReason != "" {
		task.FailReason = responseItem.FailReason
	}
	if responseItem.Properties != nil {
		properties, err := common.Marshal(responseItem.Properties)
		if err != nil {
			return err
		}
		task.Properties = string(properties)
	}
	if responseItem.Buttons != nil {
		buttons, err := common.Marshal(responseItem.Buttons)
		if err != nil {
			return err
		}
		task.Buttons = string(buttons)
	}
	if responseItem.VideoUrls != nil {
		videoURLs, err := common.Marshal(responseItem.VideoUrls)
		if err != nil {
			return err
		}
		task.VideoUrls = string(videoURLs)
	}

	isFailure := task.Status == "FAILURE" || task.FailReason != ""
	if isFailure {
		task.Status = "FAILURE"
		task.Progress = "100%"
		logger.LogInfo(ctx, task.MjId+" 构建失败，"+task.FailReason)
	}
	billingTask, err := model.GetMidjourneyBillingTask(task.UserId, task.ChannelId, task.MjId)
	if err != nil {
		return err
	}
	var settlement *model.BillingSettlementInput
	if billingTask != nil {
		billingTask.Status = midjourneyTaskStatus(task.Status, task.Progress)
		billingTask.Progress = task.Progress
		billingTask.StartTime = task.StartTime / 1000
		billingTask.FinishTime = task.FinishTime / 1000
		billingTask.FailReason = task.FailReason
		if task.VideoUrl != "" {
			billingTask.PrivateData.ResultURL = task.VideoUrl
		} else if task.ImageUrl != "" {
			billingTask.PrivateData.ResultURL = task.ImageUrl
		}
		billingTask.SetData(responseItem)
		if isFailure {
			settlement = service.BuildTaskRefundSettlementInput(billingTask, "Midjourney 任务失败："+task.FailReason)
		}
	} else if isFailure && task.Quota != 0 {
		logger.LogError(ctx, fmt.Sprintf("Midjourney 历史任务缺少账务身份，未自动退款，请人工对账: user=%d task=%s quota=%d", task.UserId, task.MjId, task.Quota))
	}

	won, err := task.UpdateWithBillingTaskAndSettlement(preStatus, billingTask, settlement)
	if err != nil || !won {
		return err
	}
	if settlement != nil {
		service.ApplyTaskBillingSettlement(ctx, billingTask, settlement)
	}
	return nil
}

func midjourneyTaskStatus(status, progress string) model.TaskStatus {
	switch status {
	case "SUCCESS":
		return model.TaskStatusSuccess
	case "FAILURE":
		return model.TaskStatusFailure
	case "SUBMITTED", "QUEUED":
		return model.TaskStatusQueued
	default:
		if progress == "0%" || progress == "" {
			return model.TaskStatusSubmitted
		}
		return model.TaskStatusInProgress
	}
}

func checkMjTaskNeedUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
	if oldTask.Code != 1 {
		return true
	}
	if newTask.Progress != "" && oldTask.Progress != newTask.Progress {
		return true
	}
	if newTask.PromptEn != "" && oldTask.PromptEn != newTask.PromptEn {
		return true
	}
	if newTask.State != "" && oldTask.State != newTask.State {
		return true
	}
	if newTask.SubmitTime > 0 && oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if newTask.StartTime > 0 && oldTask.StartTime != newTask.StartTime {
		return true
	}
	if newTask.FinishTime > 0 && oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if newTask.ImageUrl != "" && oldTask.ImageUrl != newTask.ImageUrl {
		return true
	}
	if newTask.Status != "" && oldTask.Status != newTask.Status {
		return true
	}
	if newTask.FailReason != "" && oldTask.FailReason != newTask.FailReason {
		return true
	}
	if oldTask.Progress != "100%" && newTask.FailReason != "" {
		return true
	}
	// 检查 VideoUrl 是否需要更新
	if newTask.VideoUrl != "" && oldTask.VideoUrl != newTask.VideoUrl {
		return true
	}
	if newTask.Properties != nil {
		properties, _ := common.Marshal(newTask.Properties)
		if oldTask.Properties != string(properties) {
			return true
		}
	}
	if newTask.Buttons != nil {
		buttons, _ := common.Marshal(newTask.Buttons)
		if oldTask.Buttons != string(buttons) {
			return true
		}
	}
	// 检查 VideoUrls 是否需要更新
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		newVideoUrlsStr, _ := common.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(newVideoUrlsStr) {
			return true
		}
	}

	return false
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
		QuotaFilter:    c.Query("quota_filter"),
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = service.BuildMidjourneyImageURL(system_setting.ServerAddress, midjourney.MjId, midjourney.UserId, time.Now())
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
		QuotaFilter:    c.Query("quota_filter"),
	}

	items := model.GetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllUserTask(userId, queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = service.BuildMidjourneyImageURL(system_setting.ServerAddress, midjourney.MjId, midjourney.UserId, time.Now())
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

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
	if useTime > 3600000 && task.Progress != "100%" && responseItem.Status != "SUCCESS" {
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
	return service.UpdateMidjourneyTaskFromResponse(ctx, task, responseItem)
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

package service

import (
	"context"
	"fmt"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/model"
)

// UpdateMidjourneyTaskFromResponse advances the legacy Midjourney row and its
// durable billing Task through one CAS-protected transaction.
func UpdateMidjourneyTaskFromResponse(ctx context.Context, task *model.Midjourney, responseItem dto.MidjourneyDto) error {
	legacyNeedsUpdate := midjourneyTaskNeedsUpdate(task, responseItem)
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
		task.FailReason = common.SanitizePersistedLogContent(responseItem.FailReason)
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

	providerSucceeded := responseItem.Status == "SUCCESS" ||
		(responseItem.Status == "" && responseItem.Progress == "100%" && responseItem.FailReason == "")
	if providerSucceeded {
		legacyNeedsUpdate = legacyNeedsUpdate || preStatus != "SUCCESS" || task.FailReason != ""
		task.Status = "SUCCESS"
		task.FailReason = ""
	}
	isFailure := !providerSucceeded && (task.Status == "FAILURE" || task.FailReason != "")
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
		billingFromStatus := billingTask.Status
		billingSnapshotBefore := billingTask.Snapshot()
		billingTask.Status = midjourneyBillingTaskStatus(task.Status, task.Progress)
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
			settlement = BuildTaskRefundSettlementInput(billingTask, "Midjourney 任务失败："+task.FailReason)
		}
		if !legacyNeedsUpdate {
			if billingSnapshotBefore.Equal(billingTask.Snapshot()) {
				return nil
			}
			if (billingFromStatus == model.TaskStatusFailure || billingFromStatus == model.TaskStatusSuccess) && billingFromStatus != billingTask.Status {
				return fmt.Errorf("midjourney billing task terminal state conflicts with provider state: task=%d status=%s provider=%s", billingTask.ID, billingFromStatus, task.Status)
			}
			var won bool
			if settlement != nil {
				won, err = billingTask.UpdateWithStatusAndSettlement(billingFromStatus, *settlement)
			} else {
				won, err = billingTask.UpdateWithStatus(billingFromStatus)
			}
			if err != nil || !won {
				return err
			}
			if settlement != nil {
				ApplyTaskBillingSettlement(ctx, billingTask, settlement)
			}
			return nil
		}
	} else if isFailure && task.Quota != 0 {
		logger.LogError(ctx, fmt.Sprintf("Midjourney 历史任务缺少账务身份，未自动退款，请人工对账: user=%d task=%s quota=%d", task.UserId, task.MjId, task.Quota))
	}
	if !legacyNeedsUpdate {
		return nil
	}

	won, err := task.UpdateWithBillingTaskAndSettlement(preStatus, billingTask, settlement)
	if err != nil || !won {
		return err
	}
	if settlement != nil {
		ApplyTaskBillingSettlement(ctx, billingTask, settlement)
	}
	return nil
}

func midjourneyBillingTaskStatus(status, progress string) model.TaskStatus {
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

func midjourneyTaskNeedsUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
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
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		videoURLs, _ := common.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(videoURLs) {
			return true
		}
	}
	return false
}

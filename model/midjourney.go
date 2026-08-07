package model

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Midjourney struct {
	Id          int    `json:"id"`
	Code        int    `json:"code"`
	UserId      int    `json:"user_id" gorm:"index"`
	Action      string `json:"action" gorm:"type:varchar(40);index"`
	MjId        string `json:"mj_id" gorm:"index"`
	Prompt      string `json:"prompt"`
	PromptEn    string `json:"prompt_en"`
	Description string `json:"description"`
	State       string `json:"state"`
	SubmitTime  int64  `json:"submit_time" gorm:"index"`
	StartTime   int64  `json:"start_time" gorm:"index"`
	FinishTime  int64  `json:"finish_time" gorm:"index"`
	ImageUrl    string `json:"image_url"`
	VideoUrl    string `json:"video_url"`
	VideoUrls   string `json:"video_urls"`
	Status      string `json:"status" gorm:"type:varchar(20);index"`
	Progress    string `json:"progress" gorm:"type:varchar(30);index"`
	FailReason  string `json:"fail_reason"`
	ChannelId   int    `json:"channel_id"`
	Quota       int    `json:"quota"`
	Buttons     string `json:"buttons"`
	Properties  string `json:"properties"`
}

// MidjourneyBillingClaim binds one provider task identity to the single local
// task that owns its pre-consumed quota. Historical Midjourney rows use a zero
// BillingTaskID tombstone so later duplicate submissions cannot be charged.
type MidjourneyBillingClaim struct {
	ID               int64  `gorm:"primaryKey"`
	ChannelID        int    `gorm:"uniqueIndex:idx_midjourney_billing_claim,priority:1;not null"`
	MjID             string `gorm:"type:varchar(180);uniqueIndex:idx_midjourney_billing_claim,priority:2;not null"`
	UserID           int    `gorm:"index;not null"`
	BillingTaskID    int64  `gorm:"index;not null;default:0"`
	BillingRequestID string `gorm:"type:varchar(64);index;not null;default:''"`
	CreatedAt        int64  `gorm:"not null"`
}

var ErrMidjourneyTaskAmbiguous = errors.New("midjourney task identity is ambiguous across channels")

// TaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type TaskQueryParams struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
	QuotaFilter    string
}

func GetAllUserTask(userId int, startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllTasks(startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllUnFinishTasks() []*Midjourney {
	var tasks []*Midjourney
	var err error
	// get all tasks progress is not 100%
	err = DB.Where("progress != ?", "100%").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetByMJIds(userId int, mjIds []string) []*Midjourney {
	var mj []*Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetUniqueMidjourneyByMJID(mjID string) (*Midjourney, error) {
	if mjID == "" {
		return nil, nil
	}
	return findUniqueMidjourney(DB.Where("mj_id = ?", mjID))
}

func GetUniqueMidjourneyByUserAndMJID(userID int, mjID string) (*Midjourney, error) {
	if userID <= 0 || mjID == "" {
		return nil, nil
	}
	return findUniqueMidjourney(DB.Where("user_id = ? AND mj_id = ?", userID, mjID))
}

func findUniqueMidjourney(query *gorm.DB) (*Midjourney, error) {
	var tasks []Midjourney
	if err := query.Order("id").Limit(2).Find(&tasks).Error; err != nil {
		return nil, err
	}
	switch len(tasks) {
	case 0:
		return nil, nil
	case 1:
		return &tasks[0], nil
	default:
		return nil, ErrMidjourneyTaskAmbiguous
	}
}

func GetMjByuId(id int) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("id = ?", id).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func UpdateProgress(id int, progress string) error {
	return DB.Model(&Midjourney{}).Where("id = ?", id).Update("progress", progress).Error
}

func (midjourney *Midjourney) Insert() error {
	var err error
	err = DB.Create(midjourney).Error
	return err
}

func (midjourney *Midjourney) Update() error {
	var err error
	err = DB.Save(midjourney).Error
	return err
}

func midjourneyUpdateValues(midjourney *Midjourney) map[string]interface{} {
	return map[string]interface{}{
		"code": midjourney.Code, "action": midjourney.Action,
		"prompt": midjourney.Prompt, "prompt_en": midjourney.PromptEn,
		"description": midjourney.Description, "state": midjourney.State,
		"submit_time": midjourney.SubmitTime, "start_time": midjourney.StartTime,
		"finish_time": midjourney.FinishTime, "image_url": midjourney.ImageUrl,
		"video_url": midjourney.VideoUrl, "video_urls": midjourney.VideoUrls,
		"status": midjourney.Status, "progress": midjourney.Progress,
		"fail_reason": midjourney.FailReason, "channel_id": midjourney.ChannelId,
		"quota": midjourney.Quota, "buttons": midjourney.Buttons,
		"properties": midjourney.Properties,
	}
}

func midjourneyTaskUpdateValues(task *Task, includeQuota bool) map[string]interface{} {
	values := task.statusUpdateValues()
	values["action"] = task.Action
	values["channel_id"] = task.ChannelId
	if includeQuota {
		values["quota"] = task.Quota
	}
	return values
}

// FinalizeMidjourneySubmission atomically claims the provider task ID, writes
// the legacy Midjourney row, advances the durable billing Task, and stages the
// matching settlement intent. It returns false when another local request (or
// a historical row) already owns the provider task; in that case the supplied
// duplicate refund intent is staged with the losing Task transition.
func FinalizeMidjourneySubmission(midjourney *Midjourney, billingTask *Task, settlementInput, duplicateRefundInput *BillingSettlementInput) (created bool, refundDuplicate bool, err error) {
	if midjourney == nil || midjourney.UserId <= 0 || midjourney.MjId == "" {
		return false, false, errors.New("midjourney user and provider task id are required")
	}
	if utf8.RuneCountInString(midjourney.MjId) > 180 {
		return false, false, errors.New("midjourney provider task id exceeds 180 characters")
	}
	if billingTask == nil || billingTask.ID <= 0 {
		return false, false, errors.New("persisted midjourney billing task is required")
	}
	if settlementInput != nil {
		if err := validateBillingSettlementInput(*settlementInput); err != nil {
			return false, false, err
		}
	}
	if duplicateRefundInput != nil {
		if err := validateBillingSettlementInput(*duplicateRefundInput); err != nil {
			return false, false, err
		}
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing Midjourney
		existingResult := tx.Where("channel_id = ? AND mj_id = ?", midjourney.ChannelId, midjourney.MjId).
			Limit(1).Find(&existing)
		if existingResult.Error != nil {
			return existingResult.Error
		}

		claimTaskID := billingTask.ID
		claimRequestID := billingTask.PrivateData.BillingRequestId
		if existingResult.RowsAffected > 0 {
			claimTaskID = 0
			claimRequestID = ""
		}
		claim := MidjourneyBillingClaim{
			ChannelID: midjourney.ChannelId, MjID: midjourney.MjId, UserID: midjourney.UserId,
			BillingTaskID: claimTaskID, BillingRequestID: claimRequestID,
			CreatedAt: time.Now().Unix(),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "channel_id"}, {Name: "mj_id"}},
			DoNothing: true,
		}).Create(&claim).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_id = ? AND mj_id = ?", midjourney.ChannelId, midjourney.MjId).First(&claim).Error; err != nil {
			return err
		}

		billingTask.PrivateData.UpstreamTaskID = midjourney.MjId
		billingTask.PrivateData.AwaitingUpstreamID = false
		if claim.BillingTaskID != billingTask.ID {
			sameRequestReplay := claim.BillingRequestID != "" && claim.BillingRequestID == billingTask.PrivateData.BillingRequestId
			refundDuplicate = billingTask.Quota != 0 && !sameRequestReplay
			if refundDuplicate && duplicateRefundInput == nil {
				return errors.New("duplicate midjourney submission requires a durable refund intent")
			}
			if refundDuplicate {
				if _, _, err := ensureBillingSettlementRecordDB(tx, *duplicateRefundInput); err != nil {
					return err
				}
			}
			billingTask.Status = TaskStatusFailure
			billingTask.Progress = "100%"
			billingTask.FailReason = "provider task already claimed; duplicate pre-consume refund pending"
			billingTask.Quota = 0
			result := tx.Model(&Task{}).
				Where("id = ? AND status NOT IN ?", billingTask.ID, []string{TaskStatusFailure, TaskStatusSuccess}).
				Updates(midjourneyTaskUpdateValues(billingTask, true))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return taskSubmitResultUpdateMissError(tx, billingTask.ID)
			}
			created = false
			return nil
		}

		if settlementInput != nil {
			if _, _, err := ensureBillingSettlementRecordDB(tx, *settlementInput); err != nil {
				return err
			}
		}
		if existingResult.RowsAffected == 0 {
			if err := tx.Create(midjourney).Error; err != nil {
				return err
			}
		} else {
			midjourney.Id = existing.Id
		}
		billingTask.Status = TaskStatusSubmitted
		billingTask.Progress = midjourney.Progress
		result := tx.Model(&Task{}).
			Where("id = ? AND status NOT IN ?", billingTask.ID, []string{TaskStatusFailure, TaskStatusSuccess}).
			Updates(midjourneyTaskUpdateValues(billingTask, true))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskSubmitResultUpdateMissError(tx, billingTask.ID)
		}
		created = true
		refundDuplicate = false
		return nil
	})
	return created, refundDuplicate, err
}

func GetMidjourneyBillingTask(userID, channelID int, mjID string) (*Task, error) {
	if userID <= 0 || channelID <= 0 || mjID == "" {
		return nil, nil
	}
	var claim MidjourneyBillingClaim
	result := DB.Where("channel_id = ? AND mj_id = ?", channelID, mjID).Limit(1).Find(&claim)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 || claim.UserID != userID || claim.BillingTaskID <= 0 {
		return nil, nil
	}
	var task Task
	if err := DB.First(&task, claim.BillingTaskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateWithBillingTaskAndSettlement commits a Midjourney state transition,
// its shadow Task transition, and an optional terminal settlement intent in one
// transaction. Balance application happens after commit or through the durable
// settlement runner.
func (midjourney *Midjourney) UpdateWithBillingTaskAndSettlement(fromStatus string, billingTask *Task, input *BillingSettlementInput) (bool, error) {
	if midjourney == nil || midjourney.Id <= 0 {
		return false, errors.New("persisted midjourney task is required")
	}
	if input != nil {
		if billingTask == nil || billingTask.ID <= 0 {
			return false, errors.New("billing task is required for midjourney settlement")
		}
		if input.TaskID != billingTask.ID {
			return false, fmt.Errorf("billing settlement task identity mismatch: task=%d input=%d", billingTask.ID, input.TaskID)
		}
		if err := validateBillingSettlementInput(*input); err != nil {
			return false, err
		}
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if input != nil {
			if _, _, err := ensureBillingSettlementRecordDB(tx, *input); err != nil {
				return err
			}
		}
		result := tx.Model(&Midjourney{}).
			Where("id = ? AND status = ?", midjourney.Id, fromStatus).
			Updates(midjourneyUpdateValues(midjourney))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errTaskStatusCASLost
		}
		if billingTask == nil {
			return nil
		}
		result = tx.Model(&Task{}).
			Where("id = ? AND (status NOT IN ? OR status = ?)", billingTask.ID, []string{TaskStatusFailure, TaskStatusSuccess}, billingTask.Status).
			Updates(midjourneyTaskUpdateValues(billingTask, false))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("midjourney billing task did not advance: id=%d", billingTask.ID)
		}
		return nil
	})
	if errors.Is(err, errTaskStatusCASLost) {
		return false, nil
	}
	return err == nil, err
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Uses Model().Select("*").Updates() to avoid GORM Save()'s INSERT fallback.
func (midjourney *Midjourney) UpdateWithStatus(fromStatus string) (bool, error) {
	return midjourney.UpdateWithBillingTaskAndSettlement(fromStatus, nil, nil)
}

func MjBulkUpdate(mjIds []string, params map[string]any) error {
	return DB.Model(&Midjourney{}).
		Where("mj_id in (?)", mjIds).
		Updates(params).Error
}

func MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	return DB.Model(&Midjourney{}).
		Where("id in (?)", taskIDs).
		Updates(params).Error
}

// CountAllTasks returns total midjourney tasks for admin query
func CountAllTasks(queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{})
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func CountAllUserTask(userId int, queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{}).Where("user_id = ?", userId)
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

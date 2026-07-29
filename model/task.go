package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	commonRelay "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"gorm.io/gorm"
)

type TaskStatus string

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart   TaskStatus = "NOT_START"
	TaskStatusSubmitted             = "SUBMITTED"
	TaskStatusQueued                = "QUEUED"
	TaskStatusInProgress            = "IN_PROGRESS"
	TaskStatusFailure               = "FAILURE"
	TaskStatusSuccess               = "SUCCESS"
	TaskStatusUnknown               = "UNKNOWN"
)

type Task struct {
	ID         int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt  int64                 `json:"created_at" gorm:"index"`
	UpdatedAt  int64                 `json:"updated_at"`
	TaskID     string                `json:"task_id" gorm:"type:varchar(191);index"` // 第三方id，不一定有/ song id\ Task id
	Platform   constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId     int                   `json:"user_id" gorm:"index"`
	Group      string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId  int                   `json:"channel_id" gorm:"index"`
	Quota      int                   `json:"quota"`
	Action     string                `json:"action" gorm:"type:varchar(40);index"` // 任务类型, song, lyrics, description-mode
	Status     TaskStatus            `json:"status" gorm:"type:varchar(20);index"` // 任务状态
	FailReason string                `json:"fail_reason"`
	SubmitTime int64                 `json:"submit_time" gorm:"index"`
	StartTime  int64                 `json:"start_time" gorm:"index"`
	FinishTime int64                 `json:"finish_time" gorm:"index"`
	Progress   string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties Properties            `json:"properties" gorm:"type:json"`
	Username   string                `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`

	includeDataInUpdate        bool `gorm:"-"`
	includePrivateDataInUpdate bool `gorm:"-"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
	t.includeDataInUpdate = true
}

func (t *Task) ClearDataForUpdate() {
	t.Data = nil
	t.includeDataInUpdate = true
}

func (t *Task) ClearPrivateDataForUpdate() {
	t.PrivateData = TaskPrivateData{}
	t.includePrivateDataInUpdate = true
}

func (t *Task) IncludePrivateDataInUpdate() {
	t.includePrivateDataInUpdate = true
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		*m = Properties{}
		return nil
	}
	return common.Unmarshal(bytesValue, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	return common.Marshal(m)
}

type TaskPrivateData struct {
	Key            string `json:"key,omitempty"`
	UpstreamTaskID string `json:"upstream_task_id,omitempty"` // 上游真实 task ID
	// AwaitingUpstreamID distinguishes a newly persisted placeholder from
	// legacy rows where TaskID itself is the provider task identifier.
	AwaitingUpstreamID bool   `json:"awaiting_upstream_id,omitempty"`
	ResultURL          string `json:"result_url,omitempty"` // 任务成功后的结果 URL（视频地址等）
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource    string              `json:"billing_source,omitempty"`     // "wallet" 或 "subscription"
	BillingRequestId string              `json:"billing_request_id,omitempty"` // 原始预扣 request id，用于跨周期安全结算
	SubscriptionId   int                 `json:"subscription_id,omitempty"`    // 订阅 ID，用于订阅退款
	TokenId          int                 `json:"token_id,omitempty"`           // 令牌 ID，用于令牌额度退款
	NodeName         string              `json:"node_name,omitempty"`          // 发起任务的节点名，轮询结算阶段据此归属日志而非最后查询节点
	BillingContext   *TaskBillingContext `json:"billing_context,omitempty"`    // 计费参数快照（用于轮询阶段重新计算）
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice              float64                  `json:"model_price,omitempty"`  // 模型单价
	GroupRatio              float64                  `json:"group_ratio,omitempty"`  // 分组倍率
	ModelRatio              float64                  `json:"model_ratio,omitempty"`  // 模型倍率
	OtherRatios             map[string]float64       `json:"other_ratios,omitempty"` // 附加倍率（时长、分辨率等）
	TaskBilling             *types.TaskBillingResult `json:"task_billing,omitempty"`
	OriginModelName         string                   `json:"origin_model_name,omitempty"`         // 模型名称，必须为OriginModelName
	PerCallBilling          bool                     `json:"per_call_billing,omitempty"`          // 按次计费：跳过轮询阶段的差额结算
	DeltaSettlementDisabled *bool                    `json:"delta_settlement_disabled,omitempty"` // 渠道关闭完成态差额结算时按提交快照跳过
}

func (t *Task) DeltaSettlementDisabledForChannel(channelType int, channelSettings ...dto.ChannelOtherSettings) bool {
	if channelType != constant.ChannelTypeDoubaoVideo {
		return false
	}
	if t != nil {
		if bc := t.PrivateData.BillingContext; bc != nil && bc.DeltaSettlementDisabled != nil {
			return *bc.DeltaSettlementDisabled
		}
	}
	for _, settings := range channelSettings {
		if settings.DisableTaskDeltaSettlement {
			return true
		}
	}
	return false
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	if t.PrivateData.AwaitingUpstreamID {
		return ""
	}
	return t.TaskID
}

// GetResultURL 获取任务结果 URL（视频地址等）
// 新数据存在 PrivateData.ResultURL 中；旧数据回退到 FailReason（历史兼容）
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		return nil
	}
	return common.Unmarshal(bytesValue, p)
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	if (p == TaskPrivateData{}) {
		return nil, nil
	}
	return common.Marshal(p)
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
	QuotaFilter    string
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		if relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeGemini ||
			relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeVertexAi {
			privateData.Key = relayInfo.ChannelMeta.ApiKey
		}
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   relayInfo.ChannelId,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	var tasks []*Task
	err := DB.Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetAllUnFinishSyncTasks(limit int) []*Task {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := withRowLock(tx).Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
			Limit(limit).
			Order("updated_at ASC, id ASC").
			Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(tasks))
		claimedAt := time.Now().Unix()
		for _, task := range tasks {
			ids = append(ids, task.ID)
			if task.UpdatedAt >= claimedAt {
				claimedAt = task.UpdatedAt + 1
			}
		}
		for _, task := range tasks {
			task.UpdatedAt = claimedAt
		}
		return tx.Model(&Task{}).
			Where("id IN ? AND status NOT IN ?", ids, []string{TaskStatusFailure, TaskStatusSuccess}).
			Update("updated_at", claimedAt).Error
	})
	if err != nil {
		return nil
	}
	return tasks
}

func GetByOnlyTaskId(taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("task_id = ?", taskId).First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (Task *Task) Insert() error {
	var err error
	err = DB.Create(Task).Error
	return err
}

type taskSnapshot struct {
	Status     TaskStatus
	Progress   string
	StartTime  int64
	FinishTime int64
	FailReason string
	ResultURL  string
	Data       json.RawMessage
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data)
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:     t.Status,
		Progress:   t.Progress,
		StartTime:  t.StartTime,
		FinishTime: t.FinishTime,
		FailReason: t.FailReason,
		ResultURL:  t.PrivateData.ResultURL,
		Data:       t.Data,
	}
}

func (t *Task) statusUpdateValues() map[string]interface{} {
	updatedAt := t.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = time.Now().Unix()
	}
	values := map[string]interface{}{
		"status":      t.Status,
		"progress":    t.Progress,
		"start_time":  t.StartTime,
		"finish_time": t.FinishTime,
		"fail_reason": t.FailReason,
		"updated_at":  updatedAt,
	}
	if t.Data != nil || t.includeDataInUpdate {
		values["data"] = t.Data
	}
	if t.PrivateData != (TaskPrivateData{}) || t.includePrivateDataInUpdate {
		values["private_data"] = t.PrivateData
	}
	return values
}

func (t *Task) submitResultUpdateValues() map[string]interface{} {
	updatedAt := t.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = time.Now().Unix()
	}
	values := map[string]interface{}{
		"quota":      t.Quota,
		"updated_at": updatedAt,
	}
	if t.Data != nil || t.includeDataInUpdate {
		values["data"] = t.Data
	}
	if t.PrivateData != (TaskPrivateData{}) || t.includePrivateDataInUpdate {
		values["private_data"] = t.PrivateData
	}
	return values
}

func (Task *Task) Update() error {
	var err error
	err = DB.Save(Task).Error
	return err
}

func (t *Task) UpdateQuota() error {
	return DB.Model(t).Update("quota", t.Quota).Error
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
//
// Uses a column map instead of Save() because GORM's Save falls back to
// INSERT ON CONFLICT when the WHERE-guarded UPDATE matches zero rows, which
// silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	result := DB.Model(t).Where("status = ?", fromStatus).Updates(t.statusUpdateValues())
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

var errTaskStatusCASLost = errors.New("task status compare-and-swap lost")

// UpdateWithStatusAndSettlement commits a terminal task transition and its
// durable settlement intent atomically. Balance mutations are applied after
// this transaction by ApplyBillingSettlementOnce or its retry runner.
func (t *Task) UpdateWithStatusAndSettlement(fromStatus TaskStatus, input BillingSettlementInput) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, errors.New("persisted task is required")
	}
	if input.TaskID != t.ID {
		return false, fmt.Errorf("billing settlement task identity mismatch: task=%d input=%d", t.ID, input.TaskID)
	}
	if err := validateBillingSettlementInput(input); err != nil {
		return false, err
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, _, err := ensureBillingSettlementRecordDB(tx, input); err != nil {
			return err
		}
		result := tx.Model(t).Where("status = ?", fromStatus).Updates(t.statusUpdateValues())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errTaskStatusCASLost
		}
		return nil
	})
	if errors.Is(err, errTaskStatusCASLost) {
		return false, nil
	}
	return err == nil, err
}

// UpdateWithSettlementIntent persists upstream task identity and the request's
// final billing intent in one transaction before the HTTP success response is released.
func (t *Task) UpdateWithSettlementIntent(input *BillingSettlementInput) error {
	if t == nil || t.ID <= 0 {
		return errors.New("persisted task is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if input != nil {
			if err := validateBillingSettlementInput(*input); err != nil {
				return err
			}
			if _, _, err := ensureBillingSettlementRecordDB(tx, *input); err != nil {
				return err
			}
		}
		result := tx.Model(&Task{}).
			Where("id = ? AND status NOT IN ?", t.ID, []string{TaskStatusFailure, TaskStatusSuccess}).
			Updates(t.submitResultUpdateValues())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskSubmitResultUpdateMissError(tx, t.ID)
		}
		return nil
	})
}

func taskSubmitResultUpdateMissError(tx *gorm.DB, taskID int64) error {
	var existing Task
	if err := tx.Select("id", "status").First(&existing, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("persisted task not found: id=%d", taskID)
		}
		return err
	}
	if existing.Status == TaskStatusFailure || existing.Status == TaskStatusSuccess {
		return fmt.Errorf("task already terminal (status=%s): id=%d", existing.Status, taskID)
	}
	return fmt.Errorf("persisted task not updated: id=%d status=%s", taskID, existing.Status)
}

func MarkTaskSubmitFailed(taskID int64, reason string) error {
	if taskID <= 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id = ? AND status NOT IN ?", taskID, []string{TaskStatusFailure, TaskStatusSuccess}).
		Updates(map[string]interface{}{
			"status":      TaskStatusFailure,
			"progress":    "100%",
			"finish_time": time.Now().Unix(),
			"fail_reason": reason,
			"quota":       0,
			"updated_at":  time.Now().Unix(),
		}).Error
}

// MarkTaskSubmitNeedsReview preserves the pre-consumed quota and any upstream
// identity after the provider accepted a task but local durable finalization
// failed. Refunding this state would create an unbilled upstream task.
func MarkTaskSubmitNeedsReview(task *Task, reason string) error {
	if task == nil || task.ID <= 0 {
		return nil
	}
	task.PrivateData.AwaitingUpstreamID = false
	return DB.Model(&Task{}).
		Where("id = ? AND status <> ?", task.ID, TaskStatusSuccess).
		Updates(map[string]interface{}{
			"status":       TaskStatusFailure,
			"progress":     "100%",
			"finish_time":  time.Now().Unix(),
			"fail_reason":  reason,
			"quota":        task.Quota,
			"private_data": task.PrivateData,
			"data":         task.Data,
			"updated_at":   time.Now().Unix(),
		}).Error
}

func MarkTaskSubmitAmbiguous(taskID int64, reason string) error {
	if taskID <= 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id = ? AND status NOT IN ?", taskID, []string{TaskStatusFailure, TaskStatusSuccess}).
		Updates(map[string]interface{}{
			"status":      TaskStatusFailure,
			"progress":    "100%",
			"finish_time": time.Now().Unix(),
			"fail_reason": reason,
			"updated_at":  time.Now().Unix(),
		}).Error
}

// TaskBulkUpdate performs an unconditional bulk UPDATE by upstream task_id strings.
// Same caveats as TaskBulkUpdateByID — no CAS guard.
func TaskBulkUpdate(taskIds []string, params map[string]any) error {
	if len(taskIds) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("task_id in (?)", taskIds).
		Updates(params).Error
}

// TaskBulkUpdateByID performs an unconditional bulk UPDATE by primary key IDs.
// WARNING: This function has NO CAS (Compare-And-Swap) guard — it will overwrite
// any concurrent status changes. DO NOT use in billing/quota lifecycle flows
// (e.g., timeout, success, failure transitions that trigger refunds or settlements).
// For status transitions that involve billing, use Task.UpdateWithStatus() instead.
func TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id in (?)", ids).
		Updates(params).Error
}

type TaskQuotaUsage struct {
	Mode  string  `json:"mode"`
	Count float64 `json:"count"`
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{})
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	query = applyQuotaFilter(query, "quota", queryParams.QuotaFilter)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	openAIVideo.CompletedAt = t.UpdatedAt
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}

package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	ID         int64                 `json:"id" gorm:"primaryKey;index:idx_task_timeout_cursor,priority:2"`
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
	SubmitTime int64                 `json:"submit_time" gorm:"index;index:idx_task_timeout_cursor,priority:1"`
	StartTime  int64                 `json:"start_time" gorm:"index"`
	FinishTime int64                 `json:"finish_time" gorm:"index"`
	Progress   string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties Properties            `json:"properties" gorm:"type:json"`
	Username   string                `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
	// Set only by the guarded timeout sweep when upstream submission is still
	// unconfirmed and the original reservation remains available.
	SubmitTimeoutRetainedFunding bool `json:"-" gorm:"not null;default:false"`

	includeDataInUpdate               bool   `gorm:"-"`
	includePrivateDataInUpdate        bool   `gorm:"-"`
	preservePrivateDataOnStatusUpdate bool   `gorm:"-"`
	preserveDataOnStatusUpdate        bool   `gorm:"-"`
	privateDataUnreadable             bool   `gorm:"-"`
	quotaWasNull                      bool   `gorm:"-"`
	updatedAtWasNull                  bool   `gorm:"-"`
	privateDataStatusSnapshot         string `gorm:"-"`
}

// PrivateDataUnreadable marks worker-only rows whose original metadata could
// not be decoded. They may advance a scan cursor, but must never be settled.
func (t *Task) PrivateDataUnreadable() bool { return t.privateDataUnreadable }

// The direct field overrides the embedded scanner only for worker queries.
// A malformed row cannot abort its healthy neighbors; ordinary reads stay strict.
type taskWorkerRow struct {
	Task
	PrivateData []byte `gorm:"column:private_data;type:json"`
	Quota       *int   `gorm:"column:quota"`
	UpdatedAt   *int64 `gorm:"column:updated_at"`
}

func decodeTaskWorkerRows(rows []taskWorkerRow) []*Task {
	tasks := make([]*Task, 0, len(rows))
	for i := range rows {
		task := &rows[i].Task
		task.quotaWasNull = rows[i].Quota == nil
		task.updatedAtWasNull = rows[i].UpdatedAt == nil
		if rows[i].UpdatedAt != nil {
			task.UpdatedAt = *rows[i].UpdatedAt
		}
		if rows[i].Quota != nil {
			task.Quota = *rows[i].Quota
		}
		if err := task.PrivateData.Scan(rows[i].PrivateData); err != nil {
			task.privateDataUnreadable = true
			common.SysError(fmt.Sprintf("task worker skipped unreadable private data: id=%d", task.ID))
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// Lists retain public history fields without making invalid metadata available.
// SQL still owns filtering/pagination; count endpoints retain their row totals.
func decodeTaskListRows(rows []taskWorkerRow) []*Task {
	decoded := decodeTaskWorkerRows(rows)
	for _, task := range decoded {
		if task.PrivateDataUnreadable() {
			task.PrivateData = TaskPrivateData{}
		}
	}
	return decoded
}

// IsMiniMaxH3Task reports whether the persisted task has an explicit,
// trusted MiniMax-H3 upstream binding. Client-side origin aliases and legacy
// billing-context names are intentionally not sufficient for H3-only actions.
func IsMiniMaxH3Task(task *Task) bool {
	if task == nil {
		return false
	}
	if upstream := strings.TrimSpace(task.Properties.UpstreamModelName); upstream != "" {
		return strings.EqualFold(upstream, constant.TaskModelMiniMaxH3)
	}
	return false
}

func (t *Task) BeforeSave(tx *gorm.DB) error {
	if t != nil {
		t.FailReason = common.SanitizePersistedLogContent(t.FailReason)
		return guardTaskPluginPersistence(tx, t)
	}
	return nil
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
	// Only unmodeled fields are retained; the pointer keeps the value comparable.
	unmodeled      *taskPrivateUnmodeled
	PluginBinding  *types.TaskPluginBinding `json:"plugin_binding,omitempty"` // host-issued immutable plugin identity; never client-visible
	Key            string                   `json:"key,omitempty"`
	UpstreamTaskID string                   `json:"upstream_task_id,omitempty"` // 上游真实 task ID
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
	// Pending terminal evidence is persisted before task-finalize funding is
	// applied, so a crash cannot strand the task in a non-terminal state.
	PendingTerminalStatus     TaskStatus `json:"pending_terminal_status,omitempty"`
	PendingTerminalProgress   string     `json:"pending_terminal_progress,omitempty"`
	PendingTerminalFinishTime int64      `json:"pending_terminal_finish_time,omitempty"`
	PendingTerminalReason     string     `json:"pending_terminal_reason,omitempty"`
	PendingTerminalResultURL  string     `json:"pending_terminal_result_url,omitempty"`
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice              float64                         `json:"model_price,omitempty"`  // 模型单价
	GroupRatio              float64                         `json:"group_ratio,omitempty"`  // 分组倍率
	ModelRatio              float64                         `json:"model_ratio,omitempty"`  // 模型倍率
	OtherRatios             map[string]float64              `json:"other_ratios,omitempty"` // 附加倍率（时长、分辨率等）
	TaskBilling             *types.TaskBillingResult        `json:"task_billing,omitempty"`
	TaskBillingPlan         *types.TaskBillingPlan          `json:"task_billing_plan,omitempty"`
	TaskUsage               *types.TaskUsage                `json:"task_usage,omitempty"`
	TaskUsageEnvelope       *types.TaskUsageEnvelope        `json:"task_usage_envelope,omitempty"`
	PricingUsageEnvelope    *types.TaskPricingUsageEnvelope `json:"pricing_usage_envelope,omitempty"`
	OriginModelName         string                          `json:"origin_model_name,omitempty"`         // 模型名称，必须为OriginModelName
	PerCallBilling          bool                            `json:"per_call_billing,omitempty"`          // 按次计费：跳过轮询阶段的差额结算
	DeltaSettlementDisabled *bool                           `json:"delta_settlement_disabled,omitempty"` // 渠道关闭完成态差额结算时按提交快照跳过
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
	if t.PrivateDataUnreadable() {
		return ""
	}
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
	var raw []byte
	switch value := val.(type) {
	case nil:
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		return fmt.Errorf("unsupported task private data storage type %T", val)
	}
	var next TaskPrivateData
	if len(raw) == 0 {
		*p = next
		return nil
	}
	if err := common.Unmarshal(raw, &next); err != nil {
		return err
	}
	*p = next
	return nil
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
	var rows []taskWorkerRow
	var err error

	// 初始化查询构建器
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
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&rows).Error
	if err != nil {
		return nil
	}

	return decodeTaskListRows(rows)
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var rows []taskWorkerRow
	var err error

	// 初始化查询构建器
	query := DB.Model(&Task{})
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
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&rows).Error
	if err != nil {
		return nil
	}

	return decodeTaskListRows(rows)
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	tasks, _ := GetTimedOutUnfinishedTasksAfter(cutoffUnix, 0, 0, limit)
	return tasks
}

func GetAppliedTaskFinalizeRecoveryCandidates(cutoffUnix int64, limit int) ([]*Task, error) {
	return GetAppliedTaskFinalizeRecoveryCandidatesAfter(cutoffUnix, 0, 0, limit)
}

// Recovery has its own stable cursor so unreadable rows cannot pin a page.
func GetAppliedTaskFinalizeRecoveryCandidatesAfter(cutoffUnix, afterSubmitTime, afterID int64, limit int) ([]*Task, error) {
	return GetAppliedTaskFinalizeRecoveryCandidatesInRange(cutoffUnix, afterSubmitTime, afterID, 0, 0, limit)
}

func appliedTaskFinalizeRecoveryQuery(cutoffUnix int64) *gorm.DB {
	finalize := DB.Model(&BillingSettlement{}).Select("1").
		Where("operation_key = "+taskFinalizeOperationKeySQL(common.UsingSQLite)).
		Where("status = ?", BillingSettlementStatusApplied)
	return DB.Model(&Task{}).
		Where("status NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Where("EXISTS (?)", finalize)
}

// Capture one pass's inclusive high cursor without decoding private metadata.
func GetAppliedTaskFinalizeRecoveryUpperBound(cutoffUnix int64) (submitTime, id int64, err error) {
	var row struct{ SubmitTime, ID int64 }
	err = appliedTaskFinalizeRecoveryQuery(cutoffUnix).Select("submit_time", "id").
		Order("submit_time DESC, id DESC").Limit(1).Find(&row).Error
	return row.SubmitTime, row.ID, err
}

// A fixed inclusive upper bound lets the caller wrap despite later arrivals.
// A zero upper ID retains the unbounded historical helper's contract.
func GetAppliedTaskFinalizeRecoveryCandidatesInRange(cutoffUnix, afterSubmitTime, afterID, throughSubmitTime, throughID int64, limit int) ([]*Task, error) {
	var rows []taskWorkerRow
	if limit <= 0 {
		limit = 100
	}
	query := appliedTaskFinalizeRecoveryQuery(cutoffUnix)
	if afterID > 0 {
		query = query.Where("(submit_time > ? OR (submit_time = ? AND id > ?))", afterSubmitTime, afterSubmitTime, afterID)
	}
	if throughID > 0 {
		query = query.Where("(submit_time < ? OR (submit_time = ? AND id <= ?))", throughSubmitTime, throughSubmitTime, throughID)
	}
	err := query.Order("submit_time ASC, id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return decodeTaskWorkerRows(rows), nil
}

// GetTimedOutUnfinishedTasksAfter returns timed-out non-terminal tasks after
// the supplied (submit_time, id) cursor in stable ascending order.
func GetTimedOutUnfinishedTasksAfter(cutoffUnix int64, afterSubmitTime int64, afterID int64, limit int) ([]*Task, error) {
	var rows []taskWorkerRow
	err := timedOutUnfinishedTasksQuery(DB, common.UsingSQLite, cutoffUnix, afterSubmitTime, afterID, limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return decodeTaskWorkerRows(rows), nil
}

func timedOutUnfinishedTasksQuery(db *gorm.DB, usingSQLite bool, cutoffUnix int64, afterSubmitTime int64, afterID int64, limit int) *gorm.DB {
	taskFinalizeOwned := db.Model(&BillingSettlement{}).
		Select("1").
		Where("operation_key = "+taskFinalizeOperationKeySQL(usingSQLite)).
		Where("status IN ?", []string{
			"",
			BillingSettlementStatusPending,
			BillingSettlementStatusManual,
			BillingSettlementStatusApplied,
		})
	query := db.Model(&Task{}).
		Where("status NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Where("NOT EXISTS (?)", taskFinalizeOwned)
	query = withoutPendingTaskSubmitFailure(query)
	if afterID > 0 {
		query = query.Where(
			"(submit_time > ? OR (submit_time = ? AND id > ?))",
			afterSubmitTime,
			afterSubmitTime,
			afterID,
		)
	}
	return query.Order("submit_time ASC, id ASC").Limit(limit)
}

func taskFinalizeOperationKeySQL(usingSQLite bool) string {
	if usingSQLite {
		return fmt.Sprintf("'%s' || CAST(tasks.id AS TEXT) || '%s'", billingTaskOperationPrefix, billingRequestFinalizeSuffix)
	}
	return fmt.Sprintf("CONCAT('%s', tasks.id, '%s')", billingTaskOperationPrefix, billingRequestFinalizeSuffix)
}

func GetAllUnFinishSyncTasks(limit int) []*Task {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Transaction(func(tx *gorm.DB) error {
		var rows []taskWorkerRow
		if err := withRowLock(tx).Model(&Task{}).Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
			Limit(limit).
			Order("updated_at ASC, id ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		tasks = decodeTaskWorkerRows(rows)
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
			task.updatedAtWasNull = false
		}
		return tx.Model(&Task{}).
			Where("id IN ? AND status NOT IN ?", ids, []string{TaskStatusFailure, TaskStatusSuccess}).
			Update("updated_at", claimedAt).Error
	})
	if err != nil {
		return nil
	}
	healthy := tasks[:0]
	for _, task := range tasks {
		if !task.PrivateDataUnreadable() {
			healthy = append(healthy, task)
		}
	}
	return healthy
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
	task, err := readTaskSnapshot(DB.Where("user_id = ? and task_id = ?", userId, taskId))
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, nil
}

// GetUserTaskStatusByID keeps public status readable when private metadata
// needs repair. Billing and realtime callers continue to use the strict read.
func GetUserTaskStatusByID(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var row taskWorkerRow
	err := DB.Model(&Task{}).Where("user_id = ? and task_id = ?", userId, taskId).First(&row).Error
	exists, err := RecordExist(err)
	if err != nil || !exists {
		return nil, false, err
	}
	return decodeTaskListRows([]taskWorkerRow{row})[0], true, nil
}

func GetTaskByID(id int64) (*Task, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if id <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var task Task
	if err := DB.First(&task, id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTaskPlaceholderByID keeps strict private decoding and the nullable version
// in one database read. The alias is a projection, not a persisted task column.
func GetTaskPlaceholderByID(id int64) (*Task, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if id <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return readTaskSnapshot(DB.Where("id = ?", id))
}

func readTaskSnapshot(query *gorm.DB) (*Task, error) {
	var row struct {
		Task
		NullableUpdatedAt *int64 `gorm:"column:nullable_updated_at"`
		NullableQuota     *int   `gorm:"column:nullable_quota"`
	}
	err := query.Model(&Task{}).Select("*, updated_at AS nullable_updated_at, quota AS nullable_quota").First(&row).Error
	row.Task.updatedAtWasNull = row.NullableUpdatedAt == nil
	row.Task.quotaWasNull = row.NullableQuota == nil
	return &row.Task, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var rows []taskWorkerRow
	err := DB.Model(&Task{}).Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return decodeTaskListRows(rows), nil
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
		"fail_reason": common.SanitizePersistedLogContent(t.FailReason),
		"updated_at":  updatedAt,
	}
	if t.SubmitTimeoutRetainedFunding {
		values["submit_timeout_retained_funding"] = true
	}
	if !t.preserveDataOnStatusUpdate && (t.Data != nil || t.includeDataInUpdate) {
		values["data"] = t.Data
	}
	if !t.preservePrivateDataOnStatusUpdate && (t.PrivateData != (TaskPrivateData{}) || t.includePrivateDataInUpdate) {
		current, err := common.Marshal(t.PrivateData)
		if t.includePrivateDataInUpdate || t.privateDataStatusSnapshot == "" || err != nil || string(current) != t.privateDataStatusSnapshot {
			values["private_data"] = t.PrivateData
		}
	}
	return values
}

// CapturePrivateDataForStatusUpdate lets polling omit an unchanged private
// value while preserving explicit private edits and opaque historical fields.
func (t *Task) CapturePrivateDataForStatusUpdate() error {
	encoded, err := common.Marshal(t.PrivateData)
	if err != nil {
		return err
	}
	t.privateDataStatusSnapshot = string(encoded)
	return nil
}

// PreservePrivateDataOnStatusUpdate is for status-only timeout transitions.
// The persisted recovery metadata has not changed and must not be re-emitted.
func (t *Task) PreservePrivateDataOnStatusUpdate() {
	t.preservePrivateDataOnStatusUpdate = true
}

// A timeout decides status, not a replacement for the last provider payload.
func (t *Task) PreserveDataOnStatusUpdate() {
	t.preserveDataOnStatusUpdate = true
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

func (t *Task) Update() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return tx.Save(t).Error
	})
}

var ErrTaskPlaceholderConflict = errors.New("task placeholder changed before rewrite")
var ErrTaskFinalizeOwned = errors.New("task finalize decision already owns this lifecycle")
var ErrTaskSubmitRefundIdentityMismatch = errors.New("task submission refund identity does not match its placeholder")

// RewritePlaceholder atomically claims the observed version and rewrites retry
// metadata. A stale caller cannot overwrite a newer manual/worker decision.
func (t *Task) RewritePlaceholder(observed Task) error {
	if t == nil || t.ID <= 0 || t.ID != observed.ID {
		return errors.New("persisted task is required")
	}
	var updatedAt int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Reference-bearing writes take the catalog head before acquiring the
		// task row. Keep the regular write guard on the actual assignments.
		if err := guardTaskPluginPersistenceValue(tx, t, t.PrivateData); err != nil {
			return err
		}
		now := time.Now().Unix()
		finalize := tx.Session(&gorm.Session{NewDB: true}).Model(&BillingSettlement{}).Select("1").
			Where("operation_key = ?", BillingTaskFinalizeOperationKey(t.ID))
		query := tx.Model(&Task{}).Where("id = ? AND status = ? AND status NOT IN ?", t.ID, observed.Status, []string{TaskStatusFailure, TaskStatusSuccess})
		result := observed.whereQuotaSnapshot(observed.whereTimestampSnapshot(query, observed.UpdatedAt)).
			Where("NOT EXISTS (?)", finalize).
			Where("updated_at IS NULL OR updated_at < ?", int64(1<<63-1)).
			UpdateColumn("updated_at", gorm.Expr("CASE WHEN updated_at IS NULL OR updated_at < ? THEN ? ELSE updated_at + 1 END", now, now))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			// MySQL repeatable-read can retain an earlier snapshot from
			// catalog validation. Use a current read to identify the owner.
			var owner BillingSettlement
			err := withRowLock(tx).Select("id").Where("operation_key = ?", BillingTaskFinalizeOperationKey(t.ID)).Take(&owner).Error
			if err == nil {
				return ErrTaskFinalizeOwned
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return ErrTaskPlaceholderConflict
		}
		// The version claim owns the row now, including on SQLite. Inspect the
		// original provider state before replacing the private JSON document.
		var stored Task
		if err := withRowLock(tx).Select("private_data").Take(&stored, t.ID).Error; err != nil {
			return err
		}
		if stored.PrivateData.UpstreamTaskID != observed.PrivateData.UpstreamTaskID ||
			stored.PrivateData.AwaitingUpstreamID != observed.PrivateData.AwaitingUpstreamID {
			return ErrTaskPlaceholderConflict
		}
		// Claim first, then retain that row lock until metadata and readback commit.
		// This also makes unchanged retries independent of MySQL matched-row mode.
		if err := tx.Model(t).Select("*").Omit("updated_at").Updates(t).Error; err != nil {
			return err
		}
		var persisted struct{ UpdatedAt int64 }
		if err := tx.Model(&Task{}).Select("updated_at").Where("id = ?", t.ID).Take(&persisted).Error; err != nil {
			return err
		}
		updatedAt = persisted.UpdatedAt
		return nil
	})
	if err == nil {
		t.UpdatedAt = updatedAt
		t.updatedAtWasNull = false
		t.quotaWasNull = false
	}
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
	return t.updateWithStatusSnapshot(fromStatus, nil)
}

// UpdateWithStatusSnapshot refuses a decision based on older task metadata
// or quota, even when another writer kept the same status. The quota guard
// also covers a funding projection whose timestamp returns to wall time.
func (t *Task) UpdateWithStatusSnapshot(fromStatus TaskStatus, expectedUpdatedAt int64) (bool, error) {
	return t.updateWithStatusSnapshot(fromStatus, &expectedUpdatedAt)
}

// UpdatePollingProgress advances the version on every observed provider change,
// including polls that keep the current status.
func (t *Task) UpdatePollingProgress(fromStatus TaskStatus) (bool, error) {
	if t == nil || t.ID <= 0 || t.UpdatedAt < 0 || t.UpdatedAt == 1<<63-1 {
		return false, errors.New("persisted task and advanceable snapshot are required")
	}
	nextVersion := max(time.Now().Unix(), t.UpdatedAt+1)
	values := t.statusUpdateValues()
	values["updated_at"] = nextVersion
	query := DB.Model(&Task{ID: t.ID}).Where("status = ?", fromStatus)
	result := t.whereQuotaSnapshot(t.whereTimestampSnapshot(query, t.UpdatedAt)).Updates(values)
	if result.Error != nil || result.RowsAffected == 0 {
		return false, result.Error
	}
	t.UpdatedAt = nextVersion
	t.updatedAtWasNull = false
	return true, nil
}

// PublishRecoveredTaskTerminalStatus writes only the terminal evidence owned
// by the already applied settlement. It must not restore an older payload.
func (t *Task) PublishRecoveredTaskTerminalStatus(fromStatus TaskStatus, expectedUpdatedAt int64) (bool, error) {
	if t == nil || t.ID <= 0 || expectedUpdatedAt < 0 || fromStatus == TaskStatusSuccess || fromStatus == TaskStatusFailure ||
		(t.Status != TaskStatusSuccess && t.Status != TaskStatusFailure) {
		return false, errors.New("persisted task and valid terminal evidence are required")
	}
	query := DB.Model(&Task{ID: t.ID}).Where("status = ?", fromStatus)
	query = t.whereQuotaSnapshot(t.whereTimestampSnapshot(query, expectedUpdatedAt))
	result := query.Omit("updated_at").Updates(map[string]interface{}{
		"status":       t.Status,
		"progress":     t.Progress,
		"finish_time":  t.FinishTime,
		"fail_reason":  common.SanitizePersistedLogContent(t.FailReason),
		"private_data": t.PrivateData,
	})
	return result.RowsAffected == 1, result.Error
}

func (t *Task) updateWithStatusSnapshot(fromStatus TaskStatus, expectedUpdatedAt *int64) (bool, error) {
	if t == nil || t.ID <= 0 || expectedUpdatedAt != nil && *expectedUpdatedAt < 0 {
		return false, errors.New("persisted task and valid snapshot are required")
	}
	query := DB.Model(&Task{ID: t.ID}).Where("status = ?", fromStatus)
	if expectedUpdatedAt != nil {
		query = t.whereQuotaSnapshot(t.whereTimestampSnapshot(query, *expectedUpdatedAt))
	}
	result := query.Updates(t.statusUpdateValues())
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (t *Task) whereQuotaSnapshot(query *gorm.DB) *gorm.DB {
	if t.quotaWasNull {
		return query.Where("quota IS NULL")
	}
	return query.Where("quota = ?", t.Quota)
}

func (t *Task) HasSameQuotaSnapshot(other *Task) bool {
	return t.Quota == other.Quota && t.quotaWasNull == other.quotaWasNull
}

func (t *Task) whereTimestampSnapshot(query *gorm.DB, observed int64) *gorm.DB {
	query = withoutPendingTaskSubmitFailure(query)
	if t.updatedAtWasNull {
		return query.Where("updated_at IS NULL")
	}
	return query.Where("updated_at = ?", observed)
}

// UpdateWithStatusAndPendingTerminalEvidence durably records terminal provider
// evidence while keeping the task in fromStatus. The status and updated_at
// guards prevent a stale poller from publishing evidence for another lifecycle.
func (t *Task) UpdateWithStatusAndPendingTerminalEvidence(fromStatus TaskStatus, expectedUpdatedAt int64) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, errors.New("persisted task is required")
	}
	if t.Status != fromStatus {
		return false, errors.New("terminal evidence must keep the task non-terminal")
	}
	if expectedUpdatedAt < 0 {
		return false, errors.New("task updated_at snapshot is invalid")
	}
	updatedAt := t.UpdatedAt
	if updatedAt <= expectedUpdatedAt {
		if expectedUpdatedAt == 1<<63-1 {
			return false, errors.New("task updated_at cannot advance")
		}
		updatedAt = expectedUpdatedAt + 1
	}
	updates := map[string]interface{}{
		"private_data": t.PrivateData,
		"updated_at":   updatedAt,
	}
	if t.Data != nil || t.includeDataInUpdate {
		updates["data"] = t.Data
	}
	// Preserve hook identity without copying attempted assignments into the
	// caller before the guarded update succeeds.
	result := DB.Model(&Task{ID: t.ID}).
		Where("id = ? AND status = ? AND updated_at = ?", t.ID, fromStatus, expectedUpdatedAt).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	t.UpdatedAt = updatedAt
	return true, nil
}

var errTaskStatusCASLost = errors.New("task status compare-and-swap lost")

// UpdateWithStatusAndSettlement commits a terminal task transition and its
// durable settlement intent atomically. Balance mutations are applied after
// this transaction by ApplyBillingSettlementOnce or its retry runner.
func (t *Task) UpdateWithStatusAndSettlement(fromStatus TaskStatus, input BillingSettlementInput) (bool, error) {
	return t.updateWithStatusAndSettlementSnapshot(fromStatus, nil, input)
}

// The intent and guarded terminal update still share the original transaction.
// A stale snapshot rolls back the intent before any funding application.
func (t *Task) UpdateWithStatusAndSettlementSnapshot(fromStatus TaskStatus, expectedUpdatedAt int64, input BillingSettlementInput) (bool, error) {
	return t.updateWithStatusAndSettlementSnapshot(fromStatus, &expectedUpdatedAt, input)
}

func (t *Task) updateWithStatusAndSettlementSnapshot(fromStatus TaskStatus, expectedUpdatedAt *int64, input BillingSettlementInput) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, errors.New("persisted task is required")
	}
	if input.TaskID != t.ID {
		return false, fmt.Errorf("billing settlement task identity mismatch: task=%d input=%d", t.ID, input.TaskID)
	}
	if err := validateBillingSettlementInput(input); err != nil {
		return false, err
	}
	if expectedUpdatedAt != nil && *expectedUpdatedAt < 0 {
		return false, errors.New("task updated_at snapshot is invalid")
	}

	values := t.statusUpdateValues()
	err := DB.Transaction(func(tx *gorm.DB) error {
		if private, ok := values["private_data"]; ok {
			if err := guardTaskPluginPersistenceValue(tx, t, private); err != nil {
				return err
			}
		}
		if _, _, err := ensureBillingSettlementRecordDB(tx, input); err != nil {
			return err
		}
		query := tx.Model(&Task{ID: t.ID}).Where("status = ?", fromStatus)
		if expectedUpdatedAt != nil {
			query = t.whereQuotaSnapshot(t.whereTimestampSnapshot(query, *expectedUpdatedAt))
		}
		result := query.Updates(values)
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

// UpdateWithStatusAndSettlementIntent atomically freezes terminal billing
// evidence and its durable settlement intent while deliberately keeping the
// provider task non-terminal. Funding may then be applied safely; only an
// applied funding state is allowed to advance the provider status to a
// terminal value.
func (t *Task) UpdateWithStatusAndSettlementIntent(fromStatus TaskStatus, expectedUpdatedAt int64, input BillingSettlementInput) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, errors.New("persisted task is required")
	}
	if t.Status != fromStatus {
		return false, errors.New("settlement intent must keep the task non-terminal")
	}
	if expectedUpdatedAt < 0 {
		return false, errors.New("task updated_at snapshot is invalid")
	}
	if input.TaskID != t.ID {
		return false, fmt.Errorf("billing settlement task identity mismatch: task=%d input=%d", t.ID, input.TaskID)
	}
	if err := validateBillingSettlementInput(input); err != nil {
		return false, err
	}
	updatedAt := t.UpdatedAt
	if updatedAt <= expectedUpdatedAt {
		if expectedUpdatedAt == 1<<63-1 {
			return false, errors.New("task updated_at cannot advance")
		}
		updatedAt = expectedUpdatedAt + 1
	}

	if common.UsingSQLite {
		billingSettlementSQLiteWriteMu.Lock()
		defer billingSettlementSQLiteWriteMu.Unlock()
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := guardTaskPluginPersistenceValue(tx, t, t.PrivateData); err != nil {
			return err
		}
		if _, _, err := ensureBillingSettlementRecordDB(tx, input); err != nil {
			return err
		}
		updates := map[string]interface{}{
			"private_data": t.PrivateData,
			"updated_at":   updatedAt,
		}
		if !t.preserveDataOnStatusUpdate && (t.Data != nil || t.includeDataInUpdate) {
			updates["data"] = t.Data
		}
		result := tx.Model(&Task{ID: t.ID}).
			Where("id = ? AND status = ? AND updated_at = ?", t.ID, fromStatus, expectedUpdatedAt).
			Updates(updates)
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
	if err != nil {
		return false, err
	}
	t.UpdatedAt = updatedAt
	return true, nil
}

// UpdateWithStatusAndManualSettlement atomically records a reconciliation-only
// finalize intent while keeping the provider task non-terminal and pollable.
// The status plus updated_at guard prevents concurrent pollers from publishing
// different terminal evidence for the same task lifecycle.
func (t *Task) UpdateWithStatusAndManualSettlement(fromStatus TaskStatus, expectedUpdatedAt int64, input BillingSettlementInput, reason string) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, errors.New("persisted task is required")
	}
	if t.Status != fromStatus {
		return false, errors.New("manual settlement must keep the task non-terminal")
	}
	if expectedUpdatedAt < 0 {
		return false, errors.New("task updated_at snapshot is invalid")
	}
	if input.TaskID != t.ID {
		return false, fmt.Errorf("billing settlement task identity mismatch: task=%d input=%d", t.ID, input.TaskID)
	}
	updatedAt := t.UpdatedAt
	if updatedAt <= expectedUpdatedAt {
		if expectedUpdatedAt == 1<<63-1 {
			return false, errors.New("task updated_at cannot advance")
		}
		updatedAt = expectedUpdatedAt + 1
	}

	if common.UsingSQLite {
		billingSettlementSQLiteWriteMu.Lock()
		defer billingSettlementSQLiteWriteMu.Unlock()
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Use the same acquisition order as a placeholder rewrite before
		// writing either the manual intent or the task snapshot.
		if err := guardTaskPluginPersistenceValue(tx, t, t.PrivateData); err != nil {
			return err
		}
		if _, err := ensureManualBillingSettlementRecordDB(tx, input, reason); err != nil {
			return err
		}
		updates := map[string]interface{}{
			"private_data": t.PrivateData,
			"updated_at":   updatedAt,
		}
		if !t.preserveDataOnStatusUpdate && (t.Data != nil || t.includeDataInUpdate) {
			updates["data"] = t.Data
		}
		result := t.whereTimestampSnapshot(tx.Model(&Task{ID: t.ID}).Where("id = ? AND status = ?", t.ID, fromStatus), expectedUpdatedAt).
			Scopes(t.whereQuotaSnapshot).
			Updates(updates)
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
	if err != nil {
		return false, err
	}
	t.UpdatedAt = updatedAt
	return true, nil
}

// UpdateWithSettlementIntent persists upstream task identity and the request's
// final billing intent in one transaction before the HTTP success response is released.
func (t *Task) UpdateWithSettlementIntent(input *BillingSettlementInput) error {
	if t == nil || t.ID <= 0 {
		return errors.New("persisted task is required")
	}
	var updatedAt int64
	values := t.submitResultUpdateValues()
	err := DB.Transaction(func(tx *gorm.DB) error {
		if input != nil {
			if err := validateBillingSettlementInput(*input); err != nil {
				return err
			}
			if private, ok := values["private_data"]; ok {
				if err := guardTaskPluginPersistenceValue(tx, t, private); err != nil {
					return err
				}
			}
			if _, _, err := ensureBillingSettlementRecordDB(tx, *input); err != nil {
				return err
			}
		}
		now := time.Now().Unix()
		// The caller can be older than the stored task. Advance from the DB
		// value atomically so a timeout based on any earlier version loses CAS.
		values["updated_at"] = gorm.Expr("CASE WHEN updated_at IS NULL OR updated_at < ? THEN ? ELSE updated_at + 1 END", now, now)
		result := tx.Model(&Task{ID: t.ID}).
			Where("id = ? AND status NOT IN ?", t.ID, []string{TaskStatusFailure, TaskStatusSuccess}).
			Where("updated_at IS NULL OR updated_at < ?", int64(1<<63-1)).
			Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskSubmitResultUpdateMissError(tx, t.ID)
		}
		var persisted struct{ UpdatedAt int64 }
		if err := tx.Model(&Task{}).Select("updated_at").Where("id = ?", t.ID).Take(&persisted).Error; err != nil {
			return err
		}
		updatedAt = persisted.UpdatedAt
		return nil
	})
	if err == nil {
		t.UpdatedAt = updatedAt
	}
	return err
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
	_, err := ClaimTaskSubmitFailure(taskID, reason)
	return err
}

// ClaimTaskSubmitFailure authorizes definite request-refund cleanup only for
// the terminal CAS winner. A task-finalize owner or confirmed provider identity
// keeps its task/quota and cannot race an independent request refund.
func ClaimTaskSubmitFailure(taskID int64, reason string) (bool, error) {
	return ClaimTaskSubmitFailureWithSettlement(taskID, reason, nil)
}

// The request-refund intent and terminal claim commit together. The caller may
// apply the existing operation after commit; a crash leaves durable recovery.
func ClaimTaskSubmitFailureWithSettlement(taskID int64, reason string, input *BillingSettlementInput) (bool, error) {
	return claimTaskSubmitFailureWithSettlementDB(DB, taskID, reason, input)
}

func claimTaskSubmitFailureWithSettlementDB(db *gorm.DB, taskID int64, reason string, input *BillingSettlementInput) (bool, error) {
	if taskID <= 0 {
		return false, nil
	}
	if db == nil {
		return false, errors.New("database is not initialized")
	}
	reason = common.SanitizePersistedLogContent(reason)
	if common.UsingSQLite {
		if err := billingSettlementSQLiteWriteMu.lockContext(db.Statement.Context); err != nil {
			return false, err
		}
		defer billingSettlementSQLiteWriteMu.Unlock()
	}
	won := false
	claim := func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "sqlite" {
			// Reserve the WAL writer before any read establishes a snapshot.
			// This keeps unrelated commits from making the terminal claim lose
			// its read-to-write upgrade; no metadata version is changed here.
			if err := tx.Model(&Task{}).Where("id = ?", taskID).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error; err != nil {
				return err
			}
		}
		task, err := readTaskSnapshot(withRowLock(tx).Where("id = ?", taskID))
		if err != nil {
			return err
		}
		unprojectedRetry := task.PrivateData.AwaitingUpstreamID &&
			task.Quota == 0 && task.PrivateData.BillingSource == "" && task.PrivateData.SubscriptionId == 0
		lateRejection := task.Status == TaskStatusFailure && task.SubmitTimeoutRetainedFunding &&
			task.PrivateData.AwaitingUpstreamID && (task.Quota > 0 || unprojectedRetry) && input != nil
		if task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure && !lateRejection || task.PrivateData.UpstreamTaskID != "" {
			return nil
		}
		if input != nil {
			if err := validateBillingSettlementInput(*input); err != nil {
				return err
			}
			if input.UserID != task.UserId || input.TokenID != task.PrivateData.TokenId || task.PrivateData.BillingRequestId == "" ||
				input.OperationKey != BillingRequestFinalizeOperationKey(task.PrivateData.BillingRequestId) || input.TaskID != 0 && input.TaskID != taskID {
				return ErrTaskSubmitRefundIdentityMismatch
			}
			if !unprojectedRetry && (input.Source != task.PrivateData.BillingSource || input.SubscriptionID != task.PrivateData.SubscriptionId) {
				return ErrTaskSubmitRefundIdentityMismatch
			}
		}
		if lateRejection || unprojectedRetry && input != nil {
			if _, err := taskSubmitFailureRecoveryRecord(taskID, reason, *input); err != nil {
				return err
			}
			if unprojectedRetry {
				if err := verifyUnprojectedTaskRetryRefund(tx, task.PrivateData.BillingRequestId, *input); err != nil {
					return err
				}
			}
		}
		finalize := tx.Session(&gorm.Session{NewDB: true}).Model(&BillingSettlement{}).Select("1").Where("operation_key = ?", BillingTaskFinalizeOperationKey(taskID))
		query := tx.Model(&Task{}).Where("id = ? AND NOT EXISTS (?)", taskID, finalize)
		var result *gorm.DB
		if lateRejection {
			// Keep the original completion time and terminal state. Replace
			// the unknown-outcome explanation with the confirmed rejection.
			result = task.whereQuotaSnapshot(query.Where("status = ? AND submit_timeout_retained_funding = ?", TaskStatusFailure, true)).
				Updates(map[string]interface{}{"quota": 0, "submit_timeout_retained_funding": false, "fail_reason": reason})
		} else {
			if unprojectedRetry && input != nil {
				query = task.whereQuotaSnapshot(query)
			}
			result = query.Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).Updates(map[string]interface{}{
				"status":      TaskStatusFailure,
				"progress":    "100%",
				"finish_time": time.Now().Unix(),
				"fail_reason": reason,
				"quota":       0,
				"updated_at":  time.Now().Unix(),
			})
		}
		if result.Error != nil {
			return result.Error
		}
		won = result.RowsAffected == 1
		if won && input != nil {
			if _, _, err := ensureBillingSettlementRecordDB(tx, *input); err != nil {
				return err
			}
		}
		return nil
	}
	err := withTaskSubmitFailureWriteBudget(db, func(conn *gorm.DB) error {
		return conn.Transaction(claim)
	})
	return won && err == nil, err
}

// MarkTaskSubmitFailedWithSettlement commits a definite submit rejection and
// its durable pre-consume refund intent atomically. The task is terminal with a
// zero displayed quota while the settlement runner applies or retries the
// already-recorded balance mutation.
func MarkTaskSubmitFailedWithSettlement(taskID int64, reason string, input *BillingSettlementInput) error {
	if taskID <= 0 {
		if input != nil {
			return fmt.Errorf("cannot record settlement intent for unpersisted task: id=%d", taskID)
		}
		return nil
	}
	won, err := ClaimTaskSubmitFailureWithSettlement(taskID, reason, input)
	if err == nil && !won && input != nil {
		return errors.New("task submission failure did not claim the refund")
	}
	return err
}

// MarkTaskSubmitNeedsReview preserves the pre-consumed quota and any upstream
// identity after the provider accepted a task but local durable finalization
// failed. Refunding this state would create an unbilled upstream task.
func MarkTaskSubmitNeedsReview(task *Task, reason string) error {
	if task == nil || task.ID <= 0 {
		return nil
	}
	reason = common.SanitizePersistedLogContent(reason)
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
	reason = common.SanitizePersistedLogContent(reason)
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
	sanitizeFailReasonUpdateParam(params)
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
	sanitizeFailReasonUpdateParam(params)
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

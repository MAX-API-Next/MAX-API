package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		pattern, err := sanitizeLikePattern(value)
		if err != nil {
			return nil, err
		}
		return tx.Where(column+" LIKE ? ESCAPE '!'", pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

type Log struct {
	Id                int    `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId            int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type;index:idx_logs_type_quota_created_at,priority:3"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type;index:idx_logs_type_quota_created_at,priority:1"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0;index:idx_logs_quota;index:idx_logs_type_quota_created_at,priority:2"`
	PromptTokens      int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int    `json:"completion_tokens" gorm:"default:0"`
	UseTime           int    `json:"use_time" gorm:"default:0"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"->"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
	IsRetry           bool   `json:"is_retry" gorm:"default:false;index"`
	IsErrorRetry      bool   `json:"is_error_retry" gorm:"default:false;index"`
	IsEmptyRetry      bool   `json:"is_empty_retry" gorm:"default:false;index"`
	LogId             int    `json:"log_id,omitempty" gorm:"-"`
}

// BillingLogReceipt keeps task-billing log idempotency off the high-volume
// logs table. The receipt and its log row are committed in one LOG_DB transaction.
type BillingLogReceipt struct {
	ID           int64  `gorm:"primaryKey"`
	OperationKey string `gorm:"type:varchar(191);uniqueIndex;not null"`
	ClaimToken   string `gorm:"type:varchar(36);not null;default:''"`
	CreatedAt    int64  `gorm:"not null"`
}

func (log *Log) BeforeSave(*gorm.DB) error {
	log.syncRetryMarker()
	log.Content = common.SanitizePersistedLogContent(log.Content)
	return nil
}

func (log *Log) syncRetryMarker() {
	if log == nil {
		return
	}
	errorRetry, emptyRetry := logOtherRetryMarkers(log.Other)
	log.IsErrorRetry = errorRetry
	log.IsEmptyRetry = emptyRetry
	log.IsRetry = errorRetry || emptyRetry
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

const (
	LogFilterRetry      = "retry"
	LogFilterErrorRetry = "error_retry"
	LogFilterEmptyRetry = "empty_retry"
)

var ErrLogRetryMarkerBackfillIncomplete = errors.New("log retry marker backfill is not completed")

const (
	LogQuotaFilterAbnormal = "abnormal"
	LogQuotaFilterZero     = "zero"
	LogQuotaFilterNegative = "negative"
)

type LogQueryParams struct {
	UserId            int
	LogType           int
	LogFilter         string
	StartTimestamp    int64
	EndTimestamp      int64
	ModelName         string
	Username          string
	TokenName         string
	StartIdx          int
	Num               int
	Channel           int
	Group             string
	RequestId         string
	UpstreamRequestId string
	QuotaFilter       string
}

func normalizeLogQuotaFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case LogQuotaFilterAbnormal:
		return LogQuotaFilterAbnormal
	case LogQuotaFilterZero:
		return LogQuotaFilterZero
	case LogQuotaFilterNegative:
		return LogQuotaFilterNegative
	default:
		return ""
	}
}

func applyQuotaFilter(tx *gorm.DB, column string, filter string) *gorm.DB {
	switch normalizeLogQuotaFilter(filter) {
	case LogQuotaFilterAbnormal:
		return tx.Where(column+" <= ?", 0)
	case LogQuotaFilterZero:
		return tx.Where(column+" = ?", 0)
	case LogQuotaFilterNegative:
		return tx.Where(column+" < ?", 0)
	default:
		return tx
	}
}

func applyLogTypeFilter(tx *gorm.DB, logType int) *gorm.DB {
	if logType == LogTypeUnknown {
		return tx
	}
	return tx.Where("logs.type = ?", logType)
}

var ensureLogRetryMarkerBackfillCompletedForRead = ensureLogRetryMarkerBackfillCompletedForReadDefault

func applyRetryColumnFilter(tx *gorm.DB, column string) (*gorm.DB, error) {
	if err := ensureLogRetryMarkerBackfillCompletedForRead(); err != nil {
		return nil, err
	}
	switch column {
	case "is_retry", "is_error_retry", "is_empty_retry":
		return tx.Where("logs."+column+" = ?", true), nil
	default:
		return nil, fmt.Errorf("invalid retry log filter column: %s", column)
	}
}

func applyRetryLogFilter(tx *gorm.DB) (*gorm.DB, error) {
	return applyRetryColumnFilter(tx, "is_retry")
}

func applyErrorRetryLogFilter(tx *gorm.DB) (*gorm.DB, error) {
	return applyRetryColumnFilter(tx, "is_error_retry")
}

func applyEmptyRetryLogFilter(tx *gorm.DB) (*gorm.DB, error) {
	return applyRetryColumnFilter(tx, "is_empty_retry")
}

func ensureLogRetryMarkerBackfillCompletedForReadDefault() error {
	completed, err := isLogRetryMarkerBackfillCompleted()
	if err != nil {
		return fmt.Errorf("failed to check log retry marker backfill status before retry log read: %w", err)
	}
	if completed {
		return nil
	}
	return fmt.Errorf("%w; retry log filters will be available after startup backfill finishes", ErrLogRetryMarkerBackfillIncomplete)
}

var (
	logQuotaDataAsyncRunner = func(fn func()) {
		gopool.Go(fn)
	}
	logQuotaDataAsyncWG         sync.WaitGroup
	logQuotaDataShutdownMu      sync.Mutex
	logQuotaDataShutdownStarted bool
)

func enqueueLogQuotaData(params QuotaDataLogParams) {
	logQuotaDataShutdownMu.Lock()
	if logQuotaDataShutdownStarted {
		logQuotaDataShutdownMu.Unlock()
		LogQuotaData(params)
		_ = SaveQuotaDataCache(context.Background())
		return
	}
	logQuotaDataAsyncWG.Add(1)
	logQuotaDataShutdownMu.Unlock()
	logQuotaDataAsyncRunner(func() {
		defer logQuotaDataAsyncWG.Done()
		LogQuotaData(params)
	})
}

func WaitPendingLogQuotaData() {
	logQuotaDataShutdownMu.Lock()
	logQuotaDataShutdownStarted = true
	logQuotaDataShutdownMu.Unlock()
	logQuotaDataAsyncWG.Wait()
}

func applyLogFilter(tx *gorm.DB, filter string) (*gorm.DB, error) {
	switch filter {
	case LogFilterRetry:
		return applyRetryLogFilter(tx)
	case LogFilterErrorRetry:
		return applyErrorRetryLogFilter(tx)
	case LogFilterEmptyRetry:
		return applyEmptyRetryLogFilter(tx)
	default:
		return tx, nil
	}
}

func isRetryLogFilter(filter string) bool {
	switch filter {
	case LogFilterRetry, LogFilterErrorRetry, LogFilterEmptyRetry:
		return true
	default:
		return false
	}
}

func userVisibleAuditInfo(adminInfoValue interface{}) map[string]interface{} {
	if !common.LogRequestContentEnabled && !common.LogResponseContentEnabled {
		return nil
	}
	adminInfo, ok := adminInfoValue.(map[string]interface{})
	if !ok {
		return nil
	}

	auditInfo := make(map[string]interface{})
	copyStringField := func(key string) {
		if value, ok := adminInfo[key].(string); ok && strings.TrimSpace(value) != "" {
			auditInfo[key] = value
		}
	}
	copyBoolField := func(key string) {
		if value, ok := adminInfo[key].(bool); ok && value {
			auditInfo[key] = value
		}
	}

	if common.LogRequestContentEnabled {
		copyStringField("request_content")
		copyBoolField("request_content_truncated")
	}
	if common.LogResponseContentEnabled {
		copyStringField("response_content")
		copyBoolField("response_content_truncated")
	}
	if len(auditInfo) == 0 {
		return nil
	}
	return auditInfo
}

func stripAuditContentFields(auditInfo map[string]interface{}) {
	if auditInfo == nil {
		return
	}
	delete(auditInfo, "request_content")
	delete(auditInfo, "response_content")
}

func stripAuditContentValue(value interface{}) {
	auditInfo, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	stripAuditContentFields(auditInfo)
}

func stripLogAuditContent(log *Log) {
	if log == nil || strings.TrimSpace(log.Other) == "" {
		return
	}
	otherMap, err := common.StrToMap(log.Other)
	if err != nil || otherMap == nil {
		return
	}
	stripAuditContentValue(otherMap["admin_info"])
	stripAuditContentValue(otherMap["audit_info"])
	log.Other = common.MapToJsonStr(otherMap)
}

func stripLogsAuditContent(logs []*Log) {
	for _, log := range logs {
		stripLogAuditContent(log)
	}
}

func logOtherHasRetryMarker(other string) bool {
	errorRetry, emptyRetry := logOtherRetryMarkers(other)
	return errorRetry || emptyRetry
}

func logOtherRetryMarkers(other string) (errorRetry bool, emptyRetry bool) {
	if strings.TrimSpace(other) == "" {
		return false, false
	}
	otherMap, err := common.StrToMap(other)
	if err != nil || otherMap == nil {
		return false, false
	}
	if marker, ok := otherMap["empty_retry"].(bool); ok && marker {
		emptyRetry = true
	}
	if retryLog, ok := otherMap["retry_log"].(bool); ok && retryLog {
		errorRetry = true
	}
	return errorRetry, emptyRetry
}

func formatUserLog(log *Log) {
	if log == nil {
		return
	}
	log.ChannelName = ""
	log.Other = formatUserLogOther(log.Other, false)
}

func formatUserLogOther(other string, stripAuditContent bool) string {
	var otherMap map[string]interface{}
	otherMap, _ = common.StrToMap(other)
	if otherMap != nil {
		auditInfo := userVisibleAuditInfo(otherMap["admin_info"])
		// Remove admin-only debug fields.
		delete(otherMap, "admin_info")
		delete(otherMap, "audit_info")
		// delete(otherMap, "reject_reason")
		// Keep stream_status visible so users can diagnose interrupted streams.
		if auditInfo != nil {
			if stripAuditContent {
				stripAuditContentFields(auditInfo)
			}
			otherMap["audit_info"] = auditInfo
		}
	}
	return common.MapToJsonStr(otherMap)
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		realId := logs[i].Id
		logs[i].ChannelName = ""
		logs[i].Other = formatUserLogOther(logs[i].Other, true)
		logs[i].LogId = realId
		logs[i].Id = startIdx + i + 1
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	content = common.SanitizePersistedLogContent(content)
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	content = common.SanitizePersistedLogContent(content)
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func buildOpField(action string, params map[string]interface{}) map[string]interface{} {
	op := map[string]interface{}{
		"action": action,
	}
	if len(params) > 0 {
		op["params"] = params
	}
	return op
}

func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	content = common.SanitizePersistedLogContent(content)
	other := map[string]interface{}{}
	for k, v := range extra {
		other[k] = v
	}
	other["op"] = buildOpField(action, params)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeLogin,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

func RecordOperationAuditLog(logUserId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	content = common.SanitizePersistedLogContent(content)
	username, _ := GetUsernameById(logUserId, false)
	other := map[string]interface{}{
		"op": buildOpField(action, params),
	}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}
	log := &Log{
		UserId:    logUserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	content = common.SanitizePersistedLogContent(content)
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	content = common.SanitizePersistedLogContent(content)
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	params.Content = common.SanitizePersistedLogContent(params.Content)
	logCtx := context.Background()
	if c != nil {
		logCtx = c
	}
	logger.LogInfo(logCtx, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username, requestId, upstreamRequestId := "", "", ""
	if c != nil {
		username = c.GetString("username")
		requestId = c.GetString(common.RequestIdKey)
		upstreamRequestId = c.GetString(common.UpstreamRequestIdKey)
	}
	if username == "" {
		username, _ = GetUsernameById(userId, false)
	}
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp && c != nil {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(logCtx, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		createdAt := common.GetTimestamp()
		enqueueLogQuotaData(QuotaDataLogParams{
			UserID:    userId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			TokenUsed: params.PromptTokens + params.CompletionTokens,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  common.NodeName,
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId            int
	LogType           int
	Content           string
	ChannelId         int
	ModelName         string
	Quota             int
	TokenId           int
	TokenName         string
	Group             string
	Other             map[string]interface{}
	NodeName          string // 任务发起节点；为空时回退当前节点
	PromptTokens      int
	CompletionTokens  int
	UseTimeSeconds    int
	IsStream          bool
	RequestId         string
	UpstreamRequestId string
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if err := recordTaskBillingLog("", params); err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
}

func RecordTaskBillingLogOnce(operationKey string, params RecordTaskBillingLogParams) error {
	if operationKey == "" {
		return errors.New("task billing log operation key is required")
	}
	return recordTaskBillingLog(operationKey, params)
}

func recordTaskBillingLog(operationKey string, params RecordTaskBillingLogParams) error {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return nil
	}
	if LOG_DB == nil {
		return errors.New("log database is not initialized")
	}
	params.Content = common.SanitizePersistedLogContent(params.Content)
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := params.TokenName
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:            params.UserId,
		Username:          username,
		CreatedAt:         createdAt,
		Type:              params.LogType,
		Content:           params.Content,
		TokenName:         tokenName,
		ModelName:         params.ModelName,
		Quota:             params.Quota,
		PromptTokens:      params.PromptTokens,
		CompletionTokens:  params.CompletionTokens,
		UseTime:           params.UseTimeSeconds,
		IsStream:          params.IsStream,
		ChannelId:         params.ChannelId,
		TokenId:           params.TokenId,
		Group:             params.Group,
		RequestId:         params.RequestId,
		UpstreamRequestId: params.UpstreamRequestId,
		Other:             common.MapToJsonStr(params.Other),
	}
	inserted := true
	if operationKey == "" {
		if err := LOG_DB.Create(log).Error; err != nil {
			return err
		}
	} else {
		err := LOG_DB.Transaction(func(tx *gorm.DB) error {
			claimToken := uuid.NewString()
			receipt := BillingLogReceipt{
				OperationKey: operationKey,
				ClaimToken:   claimToken,
				CreatedAt:    createdAt,
			}
			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "operation_key"}},
				DoNothing: true,
			}).Create(&receipt)
			if result.Error != nil {
				return result.Error
			}
			// MySQL implements DoNothing as a no-op UPDATE. Its RowsAffected can
			// report 1 when clientFoundRows is enabled, so verify ownership using
			// a per-attempt claim instead of relying on driver-specific row counts.
			var storedReceipt BillingLogReceipt
			if err := tx.Select("claim_token").Where("operation_key = ?", operationKey).Take(&storedReceipt).Error; err != nil {
				return err
			}
			if storedReceipt.ClaimToken != claimToken {
				inserted = false
				return nil
			}
			return tx.Create(log).Error
		})
		if err != nil {
			return err
		}
	}
	if !inserted {
		return nil
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		enqueueLogQuotaData(QuotaDataLogParams{
			UserID:    params.UserId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			TokenUsed: params.PromptTokens + params.CompletionTokens,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  nodeName,
		})
	}
	return nil
}

func GetAllLogs(params LogQueryParams) (logs []*Log, total int64, err error) {
	tx, err := applyLogFilter(applyLogTypeFilter(LOG_DB, params.LogType), params.LogFilter)
	if err != nil {
		return nil, 0, err
	}
	tx = applyQuotaFilter(tx, "logs.quota", params.QuotaFilter)

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", params.ModelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", params.Username); err != nil {
		return nil, 0, err
	}
	if params.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", params.TokenName)
	}
	if params.RequestId != "" {
		tx = tx.Where("logs.request_id = ?", params.RequestId)
	}
	if params.UpstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", params.UpstreamRequestId)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", params.EndTimestamp)
	}
	if params.Channel != 0 {
		tx = tx.Where("logs.channel_id = ?", params.Channel)
	}
	if params.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", params.Group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.created_at desc, logs.id desc").Limit(params.Num).Offset(params.StartIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	stripLogsAuditContent(logs)
	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(params LogQueryParams) (logs []*Log, total int64, err error) {
	tx, err := applyLogFilter(applyLogTypeFilter(LOG_DB.Where("logs.user_id = ?", params.UserId), params.LogType), params.LogFilter)
	if err != nil {
		return nil, 0, err
	}
	tx = applyQuotaFilter(tx, "logs.quota", params.QuotaFilter)

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", params.ModelName); err != nil {
		return nil, 0, err
	}
	if params.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", params.TokenName)
	}
	if params.RequestId != "" {
		tx = tx.Where("logs.request_id = ?", params.RequestId)
	}
	if params.UpstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", params.UpstreamRequestId)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", params.EndTimestamp)
	}
	if params.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", params.Group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.id desc").Limit(params.Num).Offset(params.StartIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, params.StartIdx)
	return logs, total, err
}

func GetLogById(id int) (*Log, error) {
	if id <= 0 {
		return nil, errors.New("invalid log id")
	}
	log := &Log{}
	err := LOG_DB.Where("id = ?", id).First(log).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("log not found")
	}
	if err != nil {
		return nil, err
	}
	return log, nil
}

func GetUserLogById(userId int, id int) (*Log, error) {
	if id <= 0 {
		return nil, errors.New("invalid log id")
	}
	log := &Log{}
	err := LOG_DB.Where("id = ? AND user_id = ?", id, userId).First(log).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("log not found")
	}
	if err != nil {
		return nil, err
	}
	formatUserLog(log)
	return log, nil
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(params LogQueryParams) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	tx, err = applyLogFilter(tx, params.LogFilter)
	if err != nil {
		return stat, err
	}
	rpmTpmQuery, err = applyLogFilter(rpmTpmQuery, params.LogFilter)
	if err != nil {
		return stat, err
	}
	tx = applyQuotaFilter(tx, "logs.quota", params.QuotaFilter)
	rpmTpmQuery = applyQuotaFilter(rpmTpmQuery, "logs.quota", params.QuotaFilter)

	if tx, err = applyExplicitLogTextFilter(tx, "username", params.Username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", params.Username); err != nil {
		return stat, err
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", params.TokenName)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", params.ModelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", params.ModelName); err != nil {
		return stat, err
	}
	if params.Channel != 0 {
		tx = tx.Where("channel_id = ?", params.Channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", params.Channel)
	}
	if params.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", params.Group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", params.Group)
	}

	if params.LogType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", params.LogType)
		rpmTpmQuery = rpmTpmQuery.Where("logs.type = ?", params.LogType)
	} else {
		tx = tx.Where("logs.type = ?", LogTypeConsume)
		rpmTpmQuery = rpmTpmQuery.Where("logs.type = ?", LogTypeConsume)
	}

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

const sumUsedTokenSelect = "COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0)"

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select(sumUsedTokenSelect)
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := oldLogCleanupScope(LOG_DB.WithContext(ctx).Model(&Log{}), targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func oldLogCleanupScope(tx *gorm.DB, targetTimestamp int64) *gorm.DB {
	return tx.Where("created_at < ? AND (type <> ? OR type IS NULL)", targetTimestamp, LogTypeManage)
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	ids := make([]int, 0, limit)
	err := oldLogCleanupScope(LOG_DB.WithContext(ctx).Model(&Log{}), targetTimestamp).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		_, err := DeleteOldBillingLogReceiptsBatch(ctx, targetTimestamp, limit)
		if err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var rowsAffected int64
	err = LOG_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := oldLogCleanupScope(tx.Where("id IN ?", ids), targetTimestamp).Delete(&Log{})
		if nil != result.Error {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		_, err := deleteOldBillingLogReceiptsBatchTx(ctx, tx, targetTimestamp, limit)
		return err
	})
	if err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}

	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		rowsAffected, err := DeleteOldLogBatch(ctx, targetTimestamp, limit)
		if err != nil {
			return total, err
		}
		total += rowsAffected
		if rowsAffected < int64(limit) {
			remaining, err := CountOldLog(ctx, targetTimestamp)
			if err != nil {
				return total, err
			}
			if remaining > 0 {
				continue
			}
			if _, err := DeleteOldBillingLogReceipts(ctx, targetTimestamp, limit); err != nil {
				return total, err
			}
			return total, nil
		}
	}
}

func DeleteOldBillingLogReceipts(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}

	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		rowsAffected, err := DeleteOldBillingLogReceiptsBatch(ctx, targetTimestamp, limit)
		if err != nil {
			return total, err
		}
		total += rowsAffected
		if rowsAffected < int64(limit) {
			return total, nil
		}
	}
}

func DeleteOldBillingLogReceiptsBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return deleteOldBillingLogReceiptsBatchTx(ctx, LOG_DB, targetTimestamp, limit)
}

func deleteOldBillingLogReceiptsBatchTx(ctx context.Context, db *gorm.DB, targetTimestamp int64, limit int) (int64, error) {
	if db == nil {
		return 0, errors.New("log database is not initialized")
	}
	ids := make([]int64, 0, limit)
	if err := db.WithContext(ctx).
		Model(&BillingLogReceipt{}).
		Where("created_at < ?", targetTimestamp).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	result := db.WithContext(ctx).Where("id IN ?", ids).Delete(&BillingLogReceipt{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

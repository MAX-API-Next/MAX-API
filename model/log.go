package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"
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
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0"`
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

func (log *Log) BeforeSave(*gorm.DB) error {
	log.syncRetryMarker()
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

func applyLogTypeFilter(tx *gorm.DB, logType int) *gorm.DB {
	if logType == LogTypeUnknown {
		return tx
	}
	return tx.Where("logs.type = ?", logType)
}

var ensureLogRetryMarkerBackfillCompletedForRead = ensureLogRetryMarkerBackfillCompletedForReadDefault

func applyRetryLogFilter(tx *gorm.DB) (*gorm.DB, error) {
	if err := ensureLogRetryMarkerBackfillCompletedForRead(); err != nil {
		return nil, err
	}
	return tx.Where("logs.is_retry = ?", true), nil
}

func applyErrorRetryLogFilter(tx *gorm.DB) (*gorm.DB, error) {
	if err := ensureLogRetryMarkerBackfillCompletedForRead(); err != nil {
		return nil, err
	}
	return tx.Where("logs.is_error_retry = ?", true), nil
}

func applyEmptyRetryLogFilter(tx *gorm.DB) (*gorm.DB, error) {
	if err := ensureLogRetryMarkerBackfillCompletedForRead(); err != nil {
		return nil, err
	}
	return tx.Where("logs.is_empty_retry = ?", true), nil
}

func ensureLogRetryMarkerBackfillCompletedForReadDefault() error {
	completed, err := isLogRetryMarkerBackfillCompleted()
	if err != nil {
		return fmt.Errorf("failed to check log retry marker backfill status before retry log read: %w", err)
	}
	if completed {
		return nil
	}
	if err := backfillLogRetryMarker(); err != nil {
		return fmt.Errorf("failed to backfill log retry markers before retry log read: %w", err)
	}
	return nil
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
		delete(otherMap, "stream_status")
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
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
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
	if common.DataExportEnabled {
		createdAt := common.GetTimestamp()
		LogQuotaData(QuotaDataLogParams{
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
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
	NodeName  string // 任务发起节点；为空时回退当前节点
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:    params.UserId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  nodeName,
		})
	}
}

func GetAllLogs(logType int, logFilter string, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	tx, err := applyLogFilter(applyLogTypeFilter(LOG_DB, logType), logFilter)
	if err != nil {
		return nil, 0, err
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.created_at desc, logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
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

func GetUserLogs(userId int, logType int, logFilter string, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	tx, err := applyLogFilter(applyLogTypeFilter(LOG_DB.Where("logs.user_id = ?", userId), logType), logFilter)
	if err != nil {
		return nil, 0, err
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
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

func SumUsedQuota(logType int, logFilter string, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	tx, err = applyLogFilter(tx, logFilter)
	if err != nil {
		return stat, err
	}
	rpmTpmQuery, err = applyLogFilter(rpmTpmQuery, logFilter)
	if err != nil {
		return stat, err
	}

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	if logType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", logType)
		rpmTpmQuery = rpmTpmQuery.Where("logs.type = ?", logType)
	} else if !isRetryLogFilter(logFilter) {
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

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
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
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	ids := make([]int, 0, limit)
	err := LOG_DB.WithContext(ctx).
		Model(&Log{}).
		Where("created_at < ?", targetTimestamp).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	result := LOG_DB.WithContext(ctx).Where("id IN ?", ids).Delete(&Log{})
	if nil != result.Error {
		return 0, result.Error
	}
	return result.RowsAffected, nil
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
			return total, nil
		}
	}
}

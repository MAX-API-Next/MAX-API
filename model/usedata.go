package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id         int     `json:"id"`
	SnapshotID *string `json:"-" gorm:"-:all"`
	UserID     int     `json:"user_id" gorm:"index"`
	Username   string  `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName  string  `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt  int64   `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup   string  `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID    int     `json:"token_id" gorm:"index;default:0"`
	ChannelID  int     `json:"channel_id" gorm:"index;default:0"`
	NodeName   string  `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed  int     `json:"token_used" gorm:"default:0"`
	Count      int     `json:"count" gorm:"default:0"`
	Quota      int     `json:"quota" gorm:"default:0"`
}

type QuotaDataSnapshot struct {
	SnapshotID string `gorm:"type:varchar(64);primaryKey"`
	CreatedAt  int64  `gorm:"bigint;index"`
}

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	Quota     int
	CreatedAt int64
	TokenUsed int
	UseGroup  string
	TokenID   int
	ChannelID int
	NodeName  string
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}
var cacheQuotaDataSaveLock sync.Mutex

func logQuotaDataCache(quotaData *QuotaData) {
	if quotaData.SnapshotID == nil {
		snapshotID := uuid.NewString()
		quotaData.SnapshotID = &snapshotID
	}
	key := quotaDataCacheKey(quotaData)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func quotaDataCacheKey(quotaData *QuotaData) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
}

func requeueQuotaDataCache(quotaData *QuotaData) {
	if quotaData.SnapshotID == nil {
		snapshotID := uuid.NewString()
		quotaData.SnapshotID = &snapshotID
	}
	baseKey := quotaDataCacheKey(quotaData)
	key := baseKey
	if existing, ok := CacheQuotaData[key]; ok && !sameQuotaDataSnapshot(existing, quotaData) {
		key = baseKey + "\x00retry\x00" + *quotaData.SnapshotID
	}
	if existing, ok := CacheQuotaData[key]; ok && sameQuotaDataSnapshot(existing, quotaData) {
		existing.Count += quotaData.Count
		existing.Quota += quotaData.Quota
		existing.TokenUsed += quotaData.TokenUsed
		return
	}
	CacheQuotaData[key] = quotaData
}

func sameQuotaDataSnapshot(left, right *QuotaData) bool {
	return left != nil && right != nil && left.SnapshotID != nil && right.SnapshotID != nil && *left.SnapshotID == *right.SnapshotID
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		CreatedAt: createdAt,
		UseGroup:  params.UseGroup,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     1,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	cacheQuotaDataSaveLock.Lock()
	defer cacheQuotaDataSaveLock.Unlock()

	CacheQuotaDataLock.Lock()
	snapshot := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	size := len(snapshot)
	failed := make([]*QuotaData, 0)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range snapshot {
		if err := saveQuotaData(quotaData); err != nil {
			common.SysLog(fmt.Sprintf("saveQuotaData error: %s", err))
			failed = append(failed, quotaData)
		}
	}

	if len(failed) > 0 {
		CacheQuotaDataLock.Lock()
		for _, quotaData := range failed {
			requeueQuotaDataCache(quotaData)
		}
		CacheQuotaDataLock.Unlock()
	}
	common.SysLog(fmt.Sprintf("保存数据看板数据完成，成功%d条，待重试%d条", size-len(failed), len(failed)))
}

func saveQuotaData(quotaData *QuotaData) error {
	if quotaData.SnapshotID == nil {
		snapshotID := uuid.NewString()
		quotaData.SnapshotID = &snapshotID
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		marker := &QuotaDataSnapshot{SnapshotID: *quotaData.SnapshotID, CreatedAt: common.GetTimestamp()}
		markerResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(marker)
		if markerResult.Error != nil {
			return markerResult.Error
		}
		if markerResult.RowsAffected == 0 {
			return nil
		}

		var existing QuotaData
		err := tx.Table("quota_data").
			Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
				quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
			First(&existing).Error
		if err == nil {
			return increaseQuotaDataTx(tx, quotaData)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record := *quotaData
		record.Id = 0
		return tx.Table("quota_data").Create(&record).Error
	})
}

func increaseQuotaDataTx(tx *gorm.DB, quotaData *QuotaData) error {
	return tx.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
		Updates(map[string]interface{}{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}).Error
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

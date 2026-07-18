package model

import (
	"crypto/sha256"
	"encoding/hex"
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
	Id           int     `json:"id"`
	AggregateKey *string `json:"-" gorm:"type:varchar(64)"`
	SnapshotID   *string `json:"-" gorm:"-:all"`
	UserID       int     `json:"user_id" gorm:"index"`
	Username     string  `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName    string  `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt    int64   `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup     string  `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID      int     `json:"token_id" gorm:"index;default:0"`
	ChannelID    int     `json:"channel_id" gorm:"index;default:0"`
	NodeName     string  `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed    int     `json:"token_used" gorm:"default:0"`
	Count        int     `json:"count" gorm:"default:0"`
	Quota        int     `json:"quota" gorm:"default:0"`
}

const quotaDataAggregateKeyIndexName = "ux_quota_data_aggregate_key"

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

func quotaDataAggregateKey(quotaData *QuotaData) string {
	digest := sha256.Sum256([]byte(quotaDataCacheKey(quotaData)))
	return hex.EncodeToString(digest[:])
}

type quotaDataAggregateMigrationGroup struct {
	survivor  QuotaData
	ids       []int
	count     int
	quota     int
	tokenUsed int
}

func migrateQuotaDataAggregateKeys() error {
	if DB == nil || !DB.Migrator().HasTable(&QuotaData{}) {
		return nil
	}
	if !DB.Migrator().HasColumn(&QuotaData{}, "aggregate_key") {
		if err := DB.Migrator().AddColumn(&QuotaData{}, "AggregateKey"); err != nil {
			return fmt.Errorf("failed to add quota_data.aggregate_key: %w", err)
		}
	}

	indexExists := DB.Migrator().HasIndex(&QuotaData{}, quotaDataAggregateKeyIndexName)
	needsCleanup, err := quotaDataAggregateKeyCleanupNeeded(indexExists)
	if err != nil {
		return err
	}
	if needsCleanup {
		if indexExists {
			if err := DB.Migrator().DropIndex(&QuotaData{}, quotaDataAggregateKeyIndexName); err != nil {
				return fmt.Errorf("failed to drop stale quota_data aggregate key index: %w", err)
			}
		}
		if err := mergeQuotaDataAggregateRows(); err != nil {
			return err
		}
	}
	if !DB.Migrator().HasIndex(&QuotaData{}, quotaDataAggregateKeyIndexName) {
		if err := createQuotaDataAggregateKeyIndex(); err != nil {
			return err
		}
	}
	return nil
}

func quotaDataAggregateKeyCleanupNeeded(indexExists bool) (bool, error) {
	if !indexExists {
		return true, nil
	}
	var legacyRows int64
	if err := DB.Table("quota_data").
		Where("aggregate_key IS NULL OR aggregate_key = ?", "").
		Count(&legacyRows).Error; err != nil {
		return false, fmt.Errorf("failed to inspect legacy quota_data aggregate keys: %w", err)
	}
	return legacyRows > 0, nil
}

func mergeQuotaDataAggregateRows() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var rows []QuotaData
		if err := tx.Table("quota_data").Order("id ASC").Find(&rows).Error; err != nil {
			return fmt.Errorf("failed to load quota_data rows for aggregate key migration: %w", err)
		}
		groups := make(map[string]*quotaDataAggregateMigrationGroup, len(rows))
		keys := make([]string, 0, len(rows))
		for _, row := range rows {
			key := quotaDataAggregateKey(&row)
			group, ok := groups[key]
			if !ok {
				group = &quotaDataAggregateMigrationGroup{survivor: row}
				groups[key] = group
				keys = append(keys, key)
			}
			group.ids = append(group.ids, row.Id)
			group.count += row.Count
			group.quota += row.Quota
			group.tokenUsed += row.TokenUsed
		}

		for _, key := range keys {
			group := groups[key]
			if group.survivor.Id == 0 {
				continue
			}
			if err := tx.Table("quota_data").
				Where("id = ?", group.survivor.Id).
				Updates(map[string]interface{}{
					"aggregate_key": key,
					"count":         group.count,
					"quota":         group.quota,
					"token_used":    group.tokenUsed,
				}).Error; err != nil {
				return fmt.Errorf("failed to update quota_data aggregate row %d: %w", group.survivor.Id, err)
			}
			if len(group.ids) <= 1 {
				continue
			}
			if err := tx.Where("id IN ?", group.ids[1:]).Delete(&QuotaData{}).Error; err != nil {
				return fmt.Errorf("failed to remove duplicate quota_data aggregate rows: %w", err)
			}
		}
		return nil
	})
}

func createQuotaDataAggregateKeyIndex() error {
	sql := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
		quoteDBIdentifier(quotaDataAggregateKeyIndexName),
		quoteDBIdentifier("quota_data"),
		quoteDBIdentifier("aggregate_key"),
	)
	if err := DB.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to create quota_data aggregate key index: %w", err)
	}
	return nil
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

		return increaseQuotaDataTx(tx, quotaData)
	})
}

func increaseQuotaDataTx(tx *gorm.DB, quotaData *QuotaData) error {
	return quotaDataUpsert(tx, quotaData).Error
}

func quotaDataUpsert(tx *gorm.DB, quotaData *QuotaData) *gorm.DB {
	aggregateKey := quotaDataAggregateKey(quotaData)
	record := *quotaData
	record.Id = 0
	record.AggregateKey = &aggregateKey
	return tx.Table("quota_data").Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "aggregate_key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}),
	}).Create(&record)
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

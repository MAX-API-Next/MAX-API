package model

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	cacheInvalidationKindUser  = "user"
	cacheInvalidationKindToken = "token"
)

// CacheInvalidationTask persists Redis invalidations so process restarts do not
// lose work queued after a committed database mutation.
type CacheInvalidationTask struct {
	ID          int64  `gorm:"primaryKey"`
	Kind        string `gorm:"type:varchar(16);uniqueIndex:idx_cache_invalidation_target,priority:1;not null"`
	EntityKey   string `gorm:"type:varchar(96);uniqueIndex:idx_cache_invalidation_target,priority:2;not null"`
	VersionKey  string `gorm:"type:varchar(96);not null"`
	DeleteEntry bool   `gorm:"uniqueIndex:idx_cache_invalidation_target,priority:3;not null;default:false"`
	Attempts    int    `gorm:"not null;default:0"`
	LastError   string `gorm:"type:text"`
	NextAttempt int64  `gorm:"index;not null;default:0"`
	CreatedAt   int64  `gorm:"not null"`
	UpdatedAt   int64  `gorm:"not null"`
	Revision    int64  `gorm:"not null;default:1"`
}

func persistCacheInvalidationTask(kind, entityKey, versionKey string, deleteEntry bool, cause error) {
	if _, err := upsertCacheInvalidationTask(DB, kind, entityKey, versionKey, deleteEntry, cause); err != nil {
		common.SysLog("failed to persist cache invalidation task: " + err.Error())
	}
}

func stageCacheInvalidationTaskTx(tx *gorm.DB, kind, entityKey, versionKey string, deleteEntry bool) (CacheInvalidationTask, error) {
	if !common.RedisEnabled {
		return CacheInvalidationTask{}, nil
	}
	return upsertCacheInvalidationTask(tx, kind, entityKey, versionKey, deleteEntry, nil)
}

func upsertCacheInvalidationTask(db *gorm.DB, kind, entityKey, versionKey string, deleteEntry bool, cause error) (CacheInvalidationTask, error) {
	if db == nil {
		return CacheInvalidationTask{}, errors.New("database is not initialized")
	}
	if entityKey == "" {
		return CacheInvalidationTask{}, errors.New("cache invalidation entity key is required")
	}
	now := time.Now().Unix()
	lastError := ""
	if cause != nil {
		lastError = cause.Error()
	}
	task := CacheInvalidationTask{
		Kind:        kind,
		EntityKey:   entityKey,
		VersionKey:  versionKey,
		DeleteEntry: deleteEntry,
		LastError:   lastError,
		NextAttempt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Revision:    1,
	}
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "kind"}, {Name: "entity_key"}, {Name: "delete_entry"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"version_key":  versionKey,
			"last_error":   lastError,
			"next_attempt": now,
			"updated_at":   now,
			"attempts":     0,
			"revision":     gorm.Expr("revision + ?", 1),
		}),
	}).Create(&task).Error; err != nil {
		return CacheInvalidationTask{}, err
	}
	var stored CacheInvalidationTask
	if err := db.Where("kind = ? AND entity_key = ? AND delete_entry = ?", kind, entityKey, deleteEntry).Take(&stored).Error; err != nil {
		return CacheInvalidationTask{}, err
	}
	return stored, nil
}

func dispatchStagedCacheInvalidation(task CacheInvalidationTask) {
	if task.ID <= 0 || !common.RedisEnabled {
		return
	}
	if err := executeCacheInvalidationTask(task); err != nil {
		common.SysLog(fmt.Sprintf("cache invalidation remains pending after commit: kind=%s entity=%s error=%v", task.Kind, task.EntityKey, err))
		return
	}
	if err := DB.Where("id = ? AND revision = ?", task.ID, task.Revision).Delete(&CacheInvalidationTask{}).Error; err != nil {
		common.SysLog("failed to complete immediate cache invalidation task: " + err.Error())
	}
}

func dispatchStagedCacheInvalidations(tasks []CacheInvalidationTask) {
	for _, task := range tasks {
		dispatchStagedCacheInvalidation(task)
	}
}

func StartCacheInvalidationTaskRunner() {
	if !common.RedisEnabled || DB == nil {
		return
	}
	gopool.Go(func() {
		processCacheInvalidationTasks()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			processCacheInvalidationTasks()
		}
	})
}

func processCacheInvalidationTasks() {
	now := time.Now().Unix()
	var tasks []CacheInvalidationTask
	if err := DB.Where("next_attempt <= ?", now).Order("id").Limit(100).Find(&tasks).Error; err != nil {
		common.SysLog("failed to load cache invalidation tasks: " + err.Error())
		return
	}
	for _, task := range tasks {
		if err := executeCacheInvalidationTask(task); err != nil {
			attempts := task.Attempts + 1
			delay := 1 << min(attempts, 6)
			if updateErr := DB.Model(&CacheInvalidationTask{}).Where("id = ? AND revision = ?", task.ID, task.Revision).Updates(map[string]interface{}{
				"attempts":     attempts,
				"last_error":   err.Error(),
				"next_attempt": time.Now().Add(time.Duration(delay) * time.Second).Unix(),
				"updated_at":   time.Now().Unix(),
			}).Error; updateErr != nil {
				common.SysLog("failed to reschedule cache invalidation task: " + updateErr.Error())
			}
			continue
		}
		if err := DB.Where("id = ? AND revision = ?", task.ID, task.Revision).Delete(&CacheInvalidationTask{}).Error; err != nil {
			common.SysLog("failed to complete cache invalidation task: " + err.Error())
		}
	}
}

func executeCacheInvalidationTask(task CacheInvalidationTask) error {
	switch task.Kind {
	case cacheInvalidationKindUser:
		userID, err := strconv.Atoi(task.EntityKey)
		if err != nil || userID <= 0 {
			return fmt.Errorf("invalid persisted user cache key: %s", task.EntityKey)
		}
		if task.DeleteEntry {
			return common.RedisDeleteVersionedHash(getUserCacheKey(userID), getUserCacheVersionKey(userID))
		}
		return common.RedisInvalidateVersionedHash(getUserCacheKey(userID), getUserCacheVersionKey(userID))
	case cacheInvalidationKindToken:
		if task.DeleteEntry {
			return common.RedisDeleteVersionedHash(task.EntityKey, task.VersionKey)
		}
		return common.RedisInvalidateVersionedHash(task.EntityKey, task.VersionKey)
	default:
		return fmt.Errorf("unknown cache invalidation kind: %s", task.Kind)
	}
}

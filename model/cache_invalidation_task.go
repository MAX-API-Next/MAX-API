package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
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

var errCacheInvalidationPermanent = errors.New("permanent cache invalidation failure")

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
			"version_key": versionKey,
			"updated_at":  now,
			"revision":    gorm.Expr("revision + ?", 1),
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

// cacheInvalidationTaskPending makes the durable outbox a read-side cache fence.
// A committed DB mutation may be visible before its Redis invalidation is
// delivered; readers must not trust or refill that entity cache while its task
// is still pending.
func cacheInvalidationTaskPending(kind, entityKey string) (bool, error) {
	if !common.RedisEnabled {
		return false, nil
	}
	if DB == nil {
		return false, errors.New("database is not initialized")
	}
	if kind == "" || entityKey == "" {
		return false, errors.New("cache invalidation target is required")
	}
	var task CacheInvalidationTask
	result := DB.Select("id").
		Where("kind = ? AND entity_key = ?", kind, entityKey).
		Limit(1).
		Find(&task)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
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

// DispatchStagedCacheInvalidation executes a cache invalidation task after
// its transaction has committed. It is exported for cross-package flows that
// stage invalidation together with an authentication or identity mutation.
func DispatchStagedCacheInvalidation(task CacheInvalidationTask) {
	dispatchStagedCacheInvalidation(task)
}

func dispatchStagedCacheInvalidations(tasks []CacheInvalidationTask) {
	for _, task := range tasks {
		dispatchStagedCacheInvalidation(task)
	}
}

var cacheInvalidationRunnerOnce sync.Once
var cacheInvalidationRunnerState struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func StartCacheInvalidationTaskRunner() {
	if !common.RedisEnabled || DB == nil {
		return
	}
	cacheInvalidationRunnerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		cacheInvalidationRunnerState.Lock()
		cacheInvalidationRunnerState.cancel = cancel
		cacheInvalidationRunnerState.done = done
		cacheInvalidationRunnerState.Unlock()
		gopool.Go(func() {
			defer close(done)
			processCacheInvalidationTasks()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					processCacheInvalidationTasks()
				}
			}
		})
	})
}

func StopCacheInvalidationTaskRunner(ctx context.Context) error {
	cacheInvalidationRunnerState.Lock()
	cancel := cacheInvalidationRunnerState.cancel
	done := cacheInvalidationRunnerState.done
	cacheInvalidationRunnerState.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
			if errors.Is(err, errCacheInvalidationPermanent) {
				if deleteErr := DB.Where("id = ? AND revision = ?", task.ID, task.Revision).Delete(&CacheInvalidationTask{}).Error; deleteErr != nil {
					common.SysLog("failed to drop permanent cache invalidation task: " + deleteErr.Error())
				}
				common.SysLog(fmt.Sprintf("dropped permanent cache invalidation task: kind=%s entity=%s error=%v", task.Kind, task.EntityKey, err))
				continue
			}
			attempts := task.Attempts + 1
			delay := 1 << min(attempts, 6)
			if updateErr := DB.Model(&CacheInvalidationTask{}).Where("id = ? AND revision = ?", task.ID, task.Revision).Updates(map[string]interface{}{
				"attempts":     attempts,
				"last_error":   err.Error(),
				"next_attempt": time.Now().Add(time.Duration(delay) * time.Second).Unix(),
				"updated_at":   time.Now().Unix(),
				"revision":     gorm.Expr("revision + ?", 1),
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
			return fmt.Errorf("%w: invalid persisted user cache key: %s", errCacheInvalidationPermanent, task.EntityKey)
		}
		if task.DeleteEntry {
			return common.RedisDeleteVersionedHash(getUserCacheKey(userID), getUserCacheVersionKey(userID))
		}
		return common.RedisInvalidateVersionedHash(getUserCacheKey(userID), getUserCacheVersionKey(userID))
	case cacheInvalidationKindToken:
		if task.EntityKey == "" || task.VersionKey == "" {
			return fmt.Errorf("%w: invalid persisted token cache key", errCacheInvalidationPermanent)
		}
		if task.DeleteEntry {
			return common.RedisDeleteVersionedHash(task.EntityKey, task.VersionKey)
		}
		return common.RedisInvalidateVersionedHash(task.EntityKey, task.VersionKey)
	default:
		return fmt.Errorf("%w: unknown cache invalidation kind: %s", errCacheInvalidationPermanent, task.Kind)
	}
}

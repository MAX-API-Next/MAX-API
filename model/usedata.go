package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	quotaDataAggregateMigrationEnv            = "QUOTA_DATA_AGGREGATE_MIGRATION_ENABLED"
	quotaDataAggregateKeyIndexName            = "ux_quota_data_aggregate_key"
	quotaDataAggregateMigrationSummaryTable   = "quota_data_aggregate_key_migration"
	quotaDataAggregateMigrationMemberTable    = "quota_data_aggregate_key_migration_members"
	quotaDataAggregateMigrationMemberKeyIdx   = "idx_quota_data_aggregate_key_migration_members_key"
	quotaDataAggregateMigrationSurvivorIdx    = "idx_quota_data_aggregate_key_migration_survivor"
	quotaDataAggregateMigrationBatchDefault   = 1000
	quotaDataSnapshotRetentionSeconds         = int64(7 * 24 * 60 * 60)
	quotaDataSnapshotCleanupBatchDefault      = 1000
	quotaDataSnapshotCleanupIntervalSeconds   = int64(60 * 60)
	quotaDataOperationLockTable               = "quota_data_operation_locks"
	quotaDataOperationLockName                = "quota_data_aggregate_migration"
	quotaDataOperationLockLeaseSecondsDefault = int64(60)
)

var (
	quotaDataAggregateMigrationBatchSize = quotaDataAggregateMigrationBatchDefault
	quotaDataOperationLockLeaseSeconds   = quotaDataOperationLockLeaseSecondsDefault
	quotaDataOperationLockRetryInterval  = 200 * time.Millisecond
	quotaDataOperationLockMaxWait        = 30 * time.Minute
	quotaDataOperationLockHeartbeatEvery = 15 * time.Second
	quotaDataOperationLockReleaseRetries = 3
	quotaDataOperationLockAttemptHookMu  sync.Mutex
	quotaDataOperationLockAttemptHook    func(owner string)
	quotaDataAggregateKeyIndexCacheMu    sync.RWMutex
	quotaDataAggregateKeyIndexCacheDB    *gorm.DB
	quotaDataAggregateKeyIndexCacheSet   bool
	quotaDataAggregateKeyIndexCacheValue bool
	quotaDataSnapshotCleanupMu           sync.Mutex
	quotaDataSnapshotCleanupLastRun      int64
)

var errQuotaDataOperationLockLost = errors.New("quota_data operation lock lost")

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
			_ = SaveQuotaDataCache(context.Background())
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}
var cacheQuotaDataSaveLock = make(chan struct{}, 1)

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

type quotaDataAggregateMigrationMember struct {
	Id           int64  `gorm:"column:id;primaryKey;type:bigint"`
	AggregateKey string `gorm:"column:aggregate_key;type:varchar(64)"`
}

type quotaDataAggregateMigrationSummary struct {
	AggregateKey string `gorm:"column:aggregate_key;primaryKey;type:varchar(64)"`
	SurvivorID   int64  `gorm:"column:survivor_id;type:bigint"`
	CountSum     int64  `gorm:"column:count_sum;type:bigint"`
	QuotaSum     int64  `gorm:"column:quota_sum;type:bigint"`
	TokenUsedSum int64  `gorm:"column:token_used_sum;type:bigint"`
}

type quotaDataAggregateMigrationSourceRow struct {
	Id        int64  `gorm:"column:id"`
	UserID    int    `gorm:"column:user_id"`
	Username  string `gorm:"column:username"`
	ModelName string `gorm:"column:model_name"`
	CreatedAt int64  `gorm:"column:created_at"`
	UseGroup  string `gorm:"column:use_group"`
	TokenID   int    `gorm:"column:token_id"`
	ChannelID int    `gorm:"column:channel_id"`
	NodeName  string `gorm:"column:node_name"`
	Count     int64  `gorm:"column:count"`
	Quota     int64  `gorm:"column:quota"`
	TokenUsed int64  `gorm:"column:token_used"`
}

type quotaDataOperationLock struct {
	Name      string `gorm:"column:name;primaryKey;type:varchar(64)"`
	Owner     string `gorm:"column:owner;type:varchar(64);not null"`
	ExpiresAt int64  `gorm:"column:expires_at;type:bigint;not null"`
	CreatedAt int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (quotaDataOperationLock) TableName() string {
	return quotaDataOperationLockTable
}

type quotaDataOperationLockGuard struct {
	ctx      context.Context
	owner    string
	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
	lostMu   sync.Mutex
	lostErr  error
}

func (g *quotaDataOperationLockGuard) Owner() string {
	if g == nil {
		return ""
	}
	return g.owner
}

func (g *quotaDataOperationLockGuard) Check() error {
	if g == nil {
		return nil
	}
	if g.ctx != nil {
		select {
		case <-g.ctx.Done():
			return g.ctx.Err()
		default:
		}
	}
	g.lostMu.Lock()
	defer g.lostMu.Unlock()
	return g.lostErr
}

func (g *quotaDataOperationLockGuard) setLost(err error) {
	if err == nil || g == nil {
		return
	}
	g.lostMu.Lock()
	defer g.lostMu.Unlock()
	if g.lostErr == nil {
		g.lostErr = err
	}
}

func (g *quotaDataOperationLockGuard) stopHeartbeat() {
	if g == nil {
		return
	}
	g.stopOnce.Do(func() {
		close(g.stop)
		<-g.stopped
	})
}

func migrateQuotaDataAggregateKeys() error {
	if DB == nil || !DB.Migrator().HasTable(&QuotaData{}) {
		return nil
	}
	if err := ensureQuotaDataOperationLockTable(); err != nil {
		return err
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
		if err := mergeQuotaDataAggregateRows(); err != nil {
			return err
		}
	}
	if !DB.Migrator().HasIndex(&QuotaData{}, quotaDataAggregateKeyIndexName) {
		if err := createQuotaDataAggregateKeyIndex(); err != nil {
			return err
		}
	}
	setQuotaDataAggregateKeyIndexCache(true)
	return nil
}

func migrateQuotaDataAggregateKeysOnStartup() error {
	if DB == nil || !DB.Migrator().HasTable(&QuotaData{}) {
		return nil
	}
	if err := ensureQuotaDataOperationLockTable(); err != nil {
		return err
	}
	if !common.GetEnvOrDefaultBool(quotaDataAggregateMigrationEnv, false) {
		common.SysLog(fmt.Sprintf("skipping quota_data aggregate-key startup migration; set %s=true to run it", quotaDataAggregateMigrationEnv))
		return nil
	}
	return migrateQuotaDataAggregateKeys()
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

func ensureQuotaDataOperationLockTable() error {
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s varchar(64) PRIMARY KEY, %s varchar(64) NOT NULL, %s bigint NOT NULL, %s bigint NOT NULL, %s bigint NOT NULL)",
		quoteDBIdentifier(quotaDataOperationLockTable),
		quoteDBIdentifier("name"),
		quoteDBIdentifier("owner"),
		quoteDBIdentifier("expires_at"),
		quoteDBIdentifier("created_at"),
		quoteDBIdentifier("updated_at"),
	)
	if err := DB.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to ensure quota_data operation lock table: %w", err)
	}
	return nil
}

func mergeQuotaDataAggregateRows() error {
	if err := ensureQuotaDataOperationLockTable(); err != nil {
		return err
	}
	return withQuotaDataOperationLock(context.Background(), "aggregate-key migration", func(lock *quotaDataOperationLockGuard) error {
		if err := resetQuotaDataAggregateMigrationTables(); err != nil {
			return err
		}
		cleanup := true
		defer func() {
			if cleanup {
				if err := dropQuotaDataAggregateMigrationTables(); err != nil {
					common.SysLog("failed to drop quota_data aggregate migration tables: " + err.Error())
				}
			}
		}()

		if err := buildQuotaDataAggregateMigrationTables(lock); err != nil {
			return err
		}
		if err := lock.Check(); err != nil {
			return err
		}
		if err := applyQuotaDataAggregateMigrationTables(); err != nil {
			return err
		}
		if err := lock.Check(); err != nil {
			return err
		}
		if err := dropQuotaDataAggregateMigrationTables(); err != nil {
			return err
		}
		cleanup = false
		return nil
	})
}

func resetQuotaDataAggregateMigrationTables() error {
	if err := dropQuotaDataAggregateMigrationTables(); err != nil {
		return err
	}
	statements := []string{
		fmt.Sprintf("CREATE TABLE %s (%s varchar(64) PRIMARY KEY, %s bigint NOT NULL, %s bigint NOT NULL, %s bigint NOT NULL, %s bigint NOT NULL)",
			quoteDBIdentifier(quotaDataAggregateMigrationSummaryTable),
			quoteDBIdentifier("aggregate_key"),
			quoteDBIdentifier("survivor_id"),
			quoteDBIdentifier("count_sum"),
			quoteDBIdentifier("quota_sum"),
			quoteDBIdentifier("token_used_sum"),
		),
		fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
			quoteDBIdentifier(quotaDataAggregateMigrationSurvivorIdx),
			quoteDBIdentifier(quotaDataAggregateMigrationSummaryTable),
			quoteDBIdentifier("survivor_id"),
		),
		fmt.Sprintf("CREATE TABLE %s (%s bigint PRIMARY KEY, %s varchar(64) NOT NULL)",
			quoteDBIdentifier(quotaDataAggregateMigrationMemberTable),
			quoteDBIdentifier("id"),
			quoteDBIdentifier("aggregate_key"),
		),
		fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
			quoteDBIdentifier(quotaDataAggregateMigrationMemberKeyIdx),
			quoteDBIdentifier(quotaDataAggregateMigrationMemberTable),
			quoteDBIdentifier("aggregate_key"),
		),
	}
	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to create quota_data aggregate migration table: %w", err)
		}
	}
	return nil
}

func dropQuotaDataAggregateMigrationTables() error {
	for _, table := range []string{quotaDataAggregateMigrationMemberTable, quotaDataAggregateMigrationSummaryTable} {
		if err := DB.Exec("DROP TABLE IF EXISTS " + quoteDBIdentifier(table)).Error; err != nil {
			return fmt.Errorf("failed to drop quota_data aggregate migration table %s: %w", table, err)
		}
	}
	return nil
}

func buildQuotaDataAggregateMigrationTables(lock *quotaDataOperationLockGuard) error {
	batchSize := quotaDataAggregateMigrationBatchSize
	if batchSize <= 0 {
		batchSize = quotaDataAggregateMigrationBatchDefault
	}
	lastID := int64(0)
	for {
		if err := lock.Check(); err != nil {
			return err
		}
		rows := make([]quotaDataAggregateMigrationSourceRow, 0, batchSize)
		if err := DB.Table("quota_data").
			Select("id, user_id, username, model_name, created_at, use_group, token_id, channel_id, node_name, count, quota, token_used").
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(batchSize).
			Find(&rows).Error; err != nil {
			return fmt.Errorf("failed to load quota_data rows for aggregate key migration: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		lastID = rows[len(rows)-1].Id
		if err := insertQuotaDataAggregateMigrationBatch(rows); err != nil {
			return err
		}
		if err := lock.Check(); err != nil {
			return err
		}
	}
}

func insertQuotaDataAggregateMigrationBatch(rows []quotaDataAggregateMigrationSourceRow) error {
	members := make([]quotaDataAggregateMigrationMember, 0, len(rows))
	summaries := make(map[string]*quotaDataAggregateMigrationSummary, len(rows))
	for _, row := range rows {
		key := row.quotaDataAggregateKey()
		members = append(members, quotaDataAggregateMigrationMember{Id: row.Id, AggregateKey: key})
		summary, ok := summaries[key]
		if !ok {
			summary = &quotaDataAggregateMigrationSummary{
				AggregateKey: key,
				SurvivorID:   row.Id,
			}
			summaries[key] = summary
		}
		if row.Id < summary.SurvivorID {
			summary.SurvivorID = row.Id
		}
		summary.CountSum += row.Count
		summary.QuotaSum += row.Quota
		summary.TokenUsedSum += row.TokenUsed
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if len(members) > 0 {
			if err := tx.Table(quotaDataAggregateMigrationMemberTable).CreateInBatches(members, len(members)).Error; err != nil {
				return fmt.Errorf("failed to record quota_data aggregate migration members: %w", err)
			}
		}
		for _, summary := range summaries {
			if err := upsertQuotaDataAggregateMigrationSummaryTx(tx, summary); err != nil {
				return err
			}
		}
		return nil
	})
}

func (row quotaDataAggregateMigrationSourceRow) quotaDataAggregateKey() string {
	return quotaDataAggregateKey(&QuotaData{
		UserID:    row.UserID,
		Username:  row.Username,
		ModelName: row.ModelName,
		CreatedAt: row.CreatedAt,
		UseGroup:  row.UseGroup,
		TokenID:   row.TokenID,
		ChannelID: row.ChannelID,
		NodeName:  row.NodeName,
	})
}

func upsertQuotaDataAggregateMigrationSummaryTx(tx *gorm.DB, summary *quotaDataAggregateMigrationSummary) error {
	survivorIDCol := quoteDBIdentifier("survivor_id")
	if err := tx.Table(quotaDataAggregateMigrationSummaryTable).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "aggregate_key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"survivor_id":    gorm.Expr("CASE WHEN "+survivorIDCol+" > ? THEN ? ELSE "+survivorIDCol+" END", summary.SurvivorID, summary.SurvivorID),
			"count_sum":      gorm.Expr(quoteDBIdentifier("count_sum")+" + ?", summary.CountSum),
			"quota_sum":      gorm.Expr(quoteDBIdentifier("quota_sum")+" + ?", summary.QuotaSum),
			"token_used_sum": gorm.Expr(quoteDBIdentifier("token_used_sum")+" + ?", summary.TokenUsedSum),
		}),
	}).Create(summary).Error; err != nil {
		return fmt.Errorf("failed to record quota_data aggregate migration summary: %w", err)
	}
	return nil
}

func applyQuotaDataAggregateMigrationTables() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := clearStaleQuotaDataAggregateKeysTx(tx); err != nil {
			return err
		}
		if err := deleteDuplicateQuotaDataAggregateRowsTx(tx); err != nil {
			return err
		}
		if err := updateQuotaDataAggregateSurvivorsTx(tx); err != nil {
			return err
		}
		return nil
	})
}

func clearStaleQuotaDataAggregateKeysTx(tx *gorm.DB) error {
	quotaTable := quoteDBIdentifier("quota_data")
	memberTable := quoteDBIdentifier(quotaDataAggregateMigrationMemberTable)
	idCol := quoteDBIdentifier("id")
	aggregateKeyCol := quoteDBIdentifier("aggregate_key")
	sql := fmt.Sprintf(
		"UPDATE %s SET %s = NULL WHERE %s IS NOT NULL AND EXISTS (SELECT 1 FROM %s AS m WHERE m.%s = %s.%s AND m.%s <> %s.%s)",
		quotaTable,
		aggregateKeyCol,
		aggregateKeyCol,
		memberTable,
		idCol,
		quotaTable,
		idCol,
		aggregateKeyCol,
		quotaTable,
		aggregateKeyCol,
	)
	if err := tx.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to clear stale quota_data aggregate keys: %w", err)
	}
	return nil
}

func deleteDuplicateQuotaDataAggregateRowsTx(tx *gorm.DB) error {
	quotaTable := quoteDBIdentifier("quota_data")
	memberTable := quoteDBIdentifier(quotaDataAggregateMigrationMemberTable)
	summaryTable := quoteDBIdentifier(quotaDataAggregateMigrationSummaryTable)
	idCol := quoteDBIdentifier("id")
	survivorIDCol := quoteDBIdentifier("survivor_id")
	sql := fmt.Sprintf(
		"DELETE FROM %s WHERE %s IN (SELECT m.%s FROM %s AS m WHERE m.%s NOT IN (SELECT s.%s FROM %s AS s))",
		quotaTable,
		idCol,
		idCol,
		memberTable,
		idCol,
		survivorIDCol,
		summaryTable,
	)
	if err := tx.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to remove duplicate quota_data aggregate rows: %w", err)
	}
	return nil
}

func updateQuotaDataAggregateSurvivorsTx(tx *gorm.DB) error {
	quotaTable := quoteDBIdentifier("quota_data")
	summaryTable := quoteDBIdentifier(quotaDataAggregateMigrationSummaryTable)
	idCol := quoteDBIdentifier("id")
	aggregateKeyCol := quoteDBIdentifier("aggregate_key")
	survivorIDCol := quoteDBIdentifier("survivor_id")
	countCol := quoteDBIdentifier("count")
	quotaCol := quoteDBIdentifier("quota")
	tokenUsedCol := quoteDBIdentifier("token_used")
	countSumCol := quoteDBIdentifier("count_sum")
	quotaSumCol := quoteDBIdentifier("quota_sum")
	tokenUsedSumCol := quoteDBIdentifier("token_used_sum")
	targetID := quotaTable + "." + idCol
	sql := fmt.Sprintf(
		"UPDATE %s SET "+
			"%s = (SELECT s.%s FROM %s AS s WHERE s.%s = %s), "+
			"%s = (SELECT s.%s FROM %s AS s WHERE s.%s = %s), "+
			"%s = (SELECT s.%s FROM %s AS s WHERE s.%s = %s), "+
			"%s = (SELECT s.%s FROM %s AS s WHERE s.%s = %s) "+
			"WHERE %s IN (SELECT s.%s FROM %s AS s)",
		quotaTable,
		aggregateKeyCol, aggregateKeyCol, summaryTable, survivorIDCol, targetID,
		countCol, countSumCol, summaryTable, survivorIDCol, targetID,
		quotaCol, quotaSumCol, summaryTable, survivorIDCol, targetID,
		tokenUsedCol, tokenUsedSumCol, summaryTable, survivorIDCol, targetID,
		idCol, survivorIDCol, summaryTable,
	)
	if err := tx.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to update quota_data aggregate survivor rows: %w", err)
	}
	return nil
}

func quotaDataOperationLockNowSQL(tx *gorm.DB) (string, error) {
	if tx == nil || tx.Dialector == nil {
		return "", fmt.Errorf("quota_data operation lock database dialect is unavailable")
	}
	switch tx.Dialector.Name() {
	case "sqlite":
		return "CAST(strftime('%s','now') AS INTEGER)", nil
	case "mysql":
		return "UNIX_TIMESTAMP()", nil
	case "postgres":
		return "CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT)", nil
	default:
		return "", fmt.Errorf("unsupported quota_data operation lock database dialect: %s", tx.Dialector.Name())
	}
}

func createQuotaDataOperationLockAttempt(tx *gorm.DB, owner, nowSQL string) *gorm.DB {
	return tx.Model(&quotaDataOperationLock{}).Clauses(clause.OnConflict{DoNothing: true}).Create(map[string]interface{}{
		"name":       quotaDataOperationLockName,
		"owner":      owner,
		"expires_at": gorm.Expr("("+nowSQL+") + ?", quotaDataOperationLockLeaseSecondsValue()),
		"created_at": gorm.Expr(nowSQL),
		"updated_at": gorm.Expr(nowSQL),
	})
}

func claimExpiredQuotaDataOperationLockAttempt(tx *gorm.DB, owner, nowSQL string) *gorm.DB {
	return tx.Model(&quotaDataOperationLock{}).
		Where("name = ? AND expires_at < "+nowSQL, quotaDataOperationLockName).
		Updates(map[string]interface{}{
			"owner":      owner,
			"expires_at": gorm.Expr("("+nowSQL+") + ?", quotaDataOperationLockLeaseSecondsValue()),
			"updated_at": gorm.Expr(nowSQL),
		})
}

func renewQuotaDataOperationLockAttempt(tx *gorm.DB, owner, nowSQL string) *gorm.DB {
	return tx.Model(&quotaDataOperationLock{}).
		Where("name = ? AND owner = ?", quotaDataOperationLockName, owner).
		Updates(map[string]interface{}{
			"expires_at": gorm.Expr("("+nowSQL+") + ?", quotaDataOperationLockLeaseSecondsValue()),
			"updated_at": gorm.Expr(nowSQL),
		})
}

func quotaDataOperationLockOwnedBy(tx *gorm.DB, owner string) (bool, error) {
	var owned int64
	if err := tx.Model(&quotaDataOperationLock{}).
		Where("name = ? AND owner = ?", quotaDataOperationLockName, owner).
		Count(&owned).Error; err != nil {
		return false, err
	}
	return owned > 0, nil
}

func acquireQuotaDataOperationLock(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	owner := uuid.NewString()
	retryInterval := quotaDataOperationLockRetryInterval
	if retryInterval <= 0 {
		retryInterval = 200 * time.Millisecond
	}
	maxWait := quotaDataOperationLockMaxWait
	if maxWait <= 0 {
		maxWait = 30 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()
	lockDB := DB.WithContext(waitCtx)
	nowSQL, err := quotaDataOperationLockNowSQL(lockDB)
	if err != nil {
		return "", err
	}

	for {
		if err := waitCtx.Err(); err != nil {
			return "", err
		}
		notifyQuotaDataOperationLockAttempt(owner)
		result := createQuotaDataOperationLockAttempt(lockDB, owner, nowSQL)
		if result.Error != nil {
			if err := waitCtx.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("failed to acquire quota_data operation lock: %w", result.Error)
		}
		owned, err := quotaDataOperationLockOwnedBy(lockDB, owner)
		if err != nil {
			return "", fmt.Errorf("failed to verify quota_data operation lock acquisition: %w", err)
		}
		if owned {
			return owner, nil
		}

		result = claimExpiredQuotaDataOperationLockAttempt(lockDB, owner, nowSQL)
		if result.Error != nil {
			if err := waitCtx.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("failed to claim expired quota_data operation lock: %w", result.Error)
		}
		owned, err = quotaDataOperationLockOwnedBy(lockDB, owner)
		if err != nil {
			return "", fmt.Errorf("failed to verify expired quota_data operation lock claim: %w", err)
		}
		if owned {
			return owner, nil
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return "", waitCtx.Err()
		case <-timer.C:
		}
	}
}

func notifyQuotaDataOperationLockAttempt(owner string) {
	quotaDataOperationLockAttemptHookMu.Lock()
	hook := quotaDataOperationLockAttemptHook
	quotaDataOperationLockAttemptHookMu.Unlock()
	if hook != nil {
		hook(owner)
	}
}

func quotaDataOperationLockLeaseSecondsValue() int64 {
	if quotaDataOperationLockLeaseSeconds <= 0 {
		return 60
	}
	return quotaDataOperationLockLeaseSeconds
}

func quotaDataOperationLockLeaseDuration() time.Duration {
	return time.Duration(quotaDataOperationLockLeaseSecondsValue()) * time.Second
}

func quotaDataOperationLockHeartbeatInterval() time.Duration {
	interval := quotaDataOperationLockHeartbeatEvery
	if interval <= 0 {
		interval = quotaDataOperationLockLeaseDuration() / 4
	}
	if interval <= 0 {
		return time.Second
	}
	if lease := quotaDataOperationLockLeaseDuration(); interval >= lease {
		interval = lease / 2
		if interval <= 0 {
			interval = time.Second
		}
	}
	return interval
}

func refreshQuotaDataOperationLock(ctx context.Context, owner string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockDB := DB.WithContext(ctx)
	nowSQL, err := quotaDataOperationLockNowSQL(lockDB)
	if err != nil {
		return err
	}
	result := renewQuotaDataOperationLockAttempt(lockDB, owner, nowSQL)
	if result.Error != nil {
		return fmt.Errorf("failed to refresh quota_data operation lock: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var owned int64
		if err := DB.WithContext(ctx).Model(&quotaDataOperationLock{}).
			Where("name = ? AND owner = ?", quotaDataOperationLockName, owner).
			Count(&owned).Error; err != nil {
			return fmt.Errorf("failed to verify quota_data operation lock ownership: %w", err)
		}
		if owned == 0 {
			return errQuotaDataOperationLockLost
		}
	}
	return nil
}

func releaseQuotaDataOperationLock(owner string) error {
	retries := quotaDataOperationLockReleaseRetries
	if retries <= 0 {
		retries = 1
	}
	retryInterval := quotaDataOperationLockRetryInterval
	if retryInterval <= 0 {
		retryInterval = 200 * time.Millisecond
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		lastErr = releaseQuotaDataOperationLockOnce(owner)
		if lastErr == nil {
			return nil
		}
		if errors.Is(lastErr, errQuotaDataOperationLockLost) {
			return lastErr
		}
		if attempt+1 < retries {
			time.Sleep(retryInterval)
		}
	}
	return lastErr
}

func releaseQuotaDataOperationLockOnce(owner string) error {
	result := DB.Where("name = ? AND owner = ?", quotaDataOperationLockName, owner).Delete(&quotaDataOperationLock{})
	if result.Error != nil {
		return fmt.Errorf("failed to release quota_data operation lock: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: owner %s no longer holds the lock", errQuotaDataOperationLockLost, owner)
	}
	return nil
}

func startQuotaDataOperationLockHeartbeat(ctx context.Context, operation, owner string) *quotaDataOperationLockGuard {
	if ctx == nil {
		ctx = context.Background()
	}
	guard := &quotaDataOperationLockGuard{
		ctx:     ctx,
		owner:   owner,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	interval := quotaDataOperationLockHeartbeatInterval()
	leaseDuration := quotaDataOperationLockLeaseDuration()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(guard.stopped)

		lastRenewed := time.Now()
		for {
			select {
			case <-ticker.C:
				err := refreshQuotaDataOperationLock(ctx, owner)
				if err == nil {
					lastRenewed = time.Now()
					continue
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					guard.setLost(ctxErr)
					return
				}
				if errors.Is(err, errQuotaDataOperationLockLost) || time.Since(lastRenewed) >= leaseDuration {
					guard.setLost(fmt.Errorf("quota_data operation lock heartbeat failed for %s: %w", operation, err))
					return
				}
			case <-ctx.Done():
				guard.setLost(ctx.Err())
				return
			case <-guard.stop:
				return
			}
		}
	}()
	return guard
}

func withQuotaDataOperationLock(ctx context.Context, operation string, fn func(lock *quotaDataOperationLockGuard) error) (returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lockOwner, err := acquireQuotaDataOperationLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire quota_data operation lock for %s: %w", operation, err)
	}
	lock := startQuotaDataOperationLockHeartbeat(ctx, operation, lockOwner)
	defer func() {
		lock.stopHeartbeat()
		if err := lock.Check(); err != nil && returnErr == nil {
			returnErr = err
		}
		if err := releaseQuotaDataOperationLock(lockOwner); err != nil {
			wrappedErr := fmt.Errorf("failed to release quota_data operation lock for %s: %w", operation, err)
			if returnErr == nil {
				returnErr = wrappedErr
			} else {
				common.SysLog(wrappedErr.Error())
			}
		}
	}()
	return fn(lock)
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
	setQuotaDataAggregateKeyIndexCache(true)
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

func SaveQuotaDataCache(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case cacheQuotaDataSaveLock <- struct{}{}:
		defer func() { <-cacheQuotaDataSaveLock }()
	case <-ctx.Done():
		return ctx.Err()
	}

	CacheQuotaDataLock.Lock()
	snapshot := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	size := len(snapshot)
	failed := make([]*QuotaData, 0)
	var flushErr error
	var saveErr error
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	if len(snapshot) > 0 {
		flushErr = withQuotaDataOperationLock(ctx, "quota cache flush", func(lock *quotaDataOperationLockGuard) error {
			for _, quotaData := range snapshot {
				if err := lock.Check(); err != nil {
					return err
				}
				if err := saveQuotaDataLocked(ctx, quotaData); err != nil {
					common.SysLog(fmt.Sprintf("saveQuotaData error: %s", err))
					failed = append(failed, quotaData)
					if saveErr == nil {
						saveErr = err
					}
				}
				if err := lock.Check(); err != nil {
					return err
				}
			}
			return nil
		})
		if flushErr != nil {
			common.SysLog(fmt.Sprintf("saveQuotaData lock error: %s", flushErr))
			failed = make([]*QuotaData, 0, len(snapshot))
			for _, quotaData := range snapshot {
				failed = append(failed, quotaData)
			}
		}
	}

	if len(failed) > 0 {
		CacheQuotaDataLock.Lock()
		for _, quotaData := range failed {
			requeueQuotaDataCache(quotaData)
		}
		CacheQuotaDataLock.Unlock()
	}
	cleanupErr := maybeCleanupQuotaDataSnapshots(ctx)
	if cleanupErr != nil {
		common.SysLog(fmt.Sprintf("cleanup quota_data snapshot markers error: %s", cleanupErr))
	}
	common.SysLog(fmt.Sprintf("保存数据看板数据完成，成功%d条，待重试%d条", size-len(failed), len(failed)))
	if flushErr != nil {
		return flushErr
	}
	if saveErr != nil {
		return saveErr
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}

func maybeCleanupQuotaDataSnapshots(ctx context.Context) error {
	now := common.GetTimestamp()
	quotaDataSnapshotCleanupMu.Lock()
	if quotaDataSnapshotCleanupLastRun > 0 && now-quotaDataSnapshotCleanupLastRun < quotaDataSnapshotCleanupIntervalSeconds {
		quotaDataSnapshotCleanupMu.Unlock()
		return nil
	}
	quotaDataSnapshotCleanupLastRun = now
	quotaDataSnapshotCleanupMu.Unlock()

	_, err := cleanupQuotaDataSnapshotMarkers(ctx, now-quotaDataSnapshotRetentionSeconds, quotaDataSnapshotCleanupBatchDefault)
	if err != nil {
		quotaDataSnapshotCleanupMu.Lock()
		if quotaDataSnapshotCleanupLastRun == now {
			quotaDataSnapshotCleanupLastRun = 0
		}
		quotaDataSnapshotCleanupMu.Unlock()
	}
	return err
}

func cleanupQuotaDataSnapshotMarkers(ctx context.Context, cutoff int64, batchSize int) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if DB == nil || !DB.Migrator().HasTable(&QuotaDataSnapshot{}) {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = quotaDataSnapshotCleanupBatchDefault
	}
	retryableIDs := quotaDataRetryableSnapshotIDs()
	candidateLimit := batchSize
	if len(retryableIDs) > 0 {
		candidateLimit += len(retryableIDs)
	}
	var ids []string
	if err := DB.WithContext(ctx).
		Model(&QuotaDataSnapshot{}).
		Where("created_at < ?", cutoff).
		Order("created_at ASC").
		Limit(candidateLimit).
		Pluck("snapshot_id", &ids).Error; err != nil {
		return 0, fmt.Errorf("failed to load old quota_data snapshot markers: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	deleteIDs := make([]string, 0, batchSize)
	for _, id := range ids {
		if retryableIDs[id] {
			continue
		}
		deleteIDs = append(deleteIDs, id)
		if len(deleteIDs) >= batchSize {
			break
		}
	}
	if len(deleteIDs) == 0 {
		return 0, nil
	}
	result := DB.WithContext(ctx).Where("snapshot_id IN ?", deleteIDs).Delete(&QuotaDataSnapshot{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old quota_data snapshot markers: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func quotaDataRetryableSnapshotIDs() map[string]bool {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	ids := make(map[string]bool, len(CacheQuotaData))
	for _, quotaData := range CacheQuotaData {
		if quotaData != nil && quotaData.SnapshotID != nil && *quotaData.SnapshotID != "" {
			ids[*quotaData.SnapshotID] = true
		}
	}
	return ids
}

func saveQuotaData(quotaData *QuotaData) error {
	ctx := context.Background()
	return withQuotaDataOperationLock(ctx, "quota write", func(lock *quotaDataOperationLockGuard) error {
		if err := lock.Check(); err != nil {
			return err
		}
		if err := saveQuotaDataLocked(ctx, quotaData); err != nil {
			return err
		}
		return lock.Check()
	})
}

func saveQuotaDataLocked(ctx context.Context, quotaData *QuotaData) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if quotaData.SnapshotID == nil {
		snapshotID := uuid.NewString()
		quotaData.SnapshotID = &snapshotID
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if !quotaDataAggregateKeyIndexAvailable(tx) {
		return increaseQuotaDataByDimensionsTx(tx, quotaData)
	}
	return quotaDataUpsert(tx, quotaData).Error
}

func quotaDataAggregateKeyIndexAvailable(tx *gorm.DB) bool {
	baseDB := DB
	quotaDataAggregateKeyIndexCacheMu.RLock()
	if quotaDataAggregateKeyIndexCacheSet && quotaDataAggregateKeyIndexCacheDB == baseDB {
		available := quotaDataAggregateKeyIndexCacheValue
		quotaDataAggregateKeyIndexCacheMu.RUnlock()
		return available
	}
	quotaDataAggregateKeyIndexCacheMu.RUnlock()

	available := tx.Migrator().HasIndex(&QuotaData{}, quotaDataAggregateKeyIndexName)
	quotaDataAggregateKeyIndexCacheMu.Lock()
	if DB == baseDB {
		quotaDataAggregateKeyIndexCacheDB = baseDB
		quotaDataAggregateKeyIndexCacheSet = true
		quotaDataAggregateKeyIndexCacheValue = available
	}
	quotaDataAggregateKeyIndexCacheMu.Unlock()
	return available
}

func setQuotaDataAggregateKeyIndexCache(available bool) {
	quotaDataAggregateKeyIndexCacheMu.Lock()
	defer quotaDataAggregateKeyIndexCacheMu.Unlock()
	quotaDataAggregateKeyIndexCacheDB = DB
	quotaDataAggregateKeyIndexCacheSet = true
	quotaDataAggregateKeyIndexCacheValue = available
}

func increaseQuotaDataByDimensionsTx(tx *gorm.DB, quotaData *QuotaData) error {
	var existing struct {
		Id int `gorm:"column:id"`
	}
	if err := quotaDataDimensionQuery(tx, quotaData).
		Select("id").
		Order("id ASC").
		Limit(1).
		Scan(&existing).Error; err != nil {
		return err
	}
	if existing.Id > 0 {
		return tx.Table("quota_data").
			Where("id = ?", existing.Id).
			Updates(map[string]interface{}{
				"count":      gorm.Expr("count + ?", quotaData.Count),
				"quota":      gorm.Expr("quota + ?", quotaData.Quota),
				"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
			}).Error
	}

	aggregateKey := quotaDataAggregateKey(quotaData)
	record := *quotaData
	record.Id = 0
	record.AggregateKey = &aggregateKey
	return tx.Table("quota_data").Create(&record).Error
}

func quotaDataDimensionQuery(tx *gorm.DB, quotaData *QuotaData) *gorm.DB {
	return tx.Table("quota_data").Where(
		"user_id = ? AND username = ? AND model_name = ? AND created_at = ? AND use_group = ? AND token_id = ? AND channel_id = ? AND node_name = ?",
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

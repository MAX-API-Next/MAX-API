package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSaveQuotaDataCacheRequeuesFailedSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	CacheQuotaDataLock.Lock()
	oldCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB = oldDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = oldCache
		CacheQuotaDataLock.Unlock()
	})

	LogQuotaData(QuotaDataLogParams{
		UserID: 1, Username: "quota-user", ModelName: "test-model", Quota: 7, CreatedAt: 3601, TokenUsed: 3,
	})
	SaveQuotaDataCache()

	CacheQuotaDataLock.Lock()
	assert.Len(t, CacheQuotaData, 1)
	CacheQuotaDataLock.Unlock()

	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))
	SaveQuotaDataCache()

	CacheQuotaDataLock.Lock()
	assert.Empty(t, CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	var got QuotaData
	require.NoError(t, db.First(&got).Error)
	assert.Equal(t, 1, got.Count)
	assert.Equal(t, 7, got.Quota)
	assert.Equal(t, 3, got.TokenUsed)
}

func TestSaveQuotaDataIsIdempotentForRepeatedSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&QuotaData{}, &QuotaDataSnapshot{}))

	snapshotID := "quota-snapshot-idempotency"
	snapshot := &QuotaData{
		SnapshotID: &snapshotID,
		UserID:     1,
		Username:   "quota-user",
		ModelName:  "test-model",
		CreatedAt:  3600,
		Count:      1,
		Quota:      7,
		TokenUsed:  3,
	}
	require.NoError(t, saveQuotaData(snapshot))
	// A retry after an ambiguous commit must not apply the same counters again.
	require.NoError(t, saveQuotaData(snapshot))

	var rows []QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, 7, rows[0].Quota)
	assert.Equal(t, 3, rows[0].TokenUsed)
}

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

	require.NoError(t, db.AutoMigrate(&QuotaData{}))
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

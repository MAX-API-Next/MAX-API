package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetPerfMetricFlushReceiptKeysBatchesLargeLookups(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PerfMetricFlushReceipt{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })

	receipts := make([]PerfMetricFlushReceipt, 0, 7)
	requested := make([]string, 0, 1_205)
	for index := 0; index < 1_205; index++ {
		key := fmt.Sprintf("receipt-%04d", index)
		requested = append(requested, key)
		if index%200 == 0 {
			receipts = append(receipts, PerfMetricFlushReceipt{
				ReceiptKey: key,
				ClaimToken: fmt.Sprintf("claim-%04d", index),
				CreatedAt:  int64(index + 1),
			})
		}
	}
	require.NoError(t, db.Create(&receipts).Error)

	result, err := GetPerfMetricFlushReceiptKeysContext(context.Background(), requested)

	require.NoError(t, err)
	require.Len(t, result, len(receipts))
	for _, receipt := range receipts {
		require.Contains(t, result, receipt.ReceiptKey)
	}
}

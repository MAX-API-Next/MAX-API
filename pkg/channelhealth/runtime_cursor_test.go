package channelhealth

import (
	"context"
	"strconv"
	"testing"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestListRuntimeStatesPersistsCursorAfterBoundedLegacyScan(t *testing.T) {
	withRuntimeRedis(t)
	pipe := maxcommon.RDB.Pipeline()
	for i := 1; i <= 6000; i++ {
		pipe.HSet(context.Background(), runtimeStateKey(i), map[string]interface{}{"channel_id": strconv.Itoa(i), "penalty": "1"})
	}
	_, err := pipe.Exec(context.Background())
	require.NoError(t, err)
	_, err = ListRuntimeStates(context.Background())
	require.NoError(t, err)
	migrated, err := maxcommon.RDB.Exists(context.Background(), runtimeStateIndexMigratedKey).Result()
	require.NoError(t, err)
	cursor, err := maxcommon.RDB.Get(context.Background(), runtimeStateIndexCursorKey).Result()
	require.NoError(t, err)
	t.Logf("migrated=%d cursor=%s", migrated, cursor)
	require.Zero(t, migrated)
	require.NotEmpty(t, cursor)
}

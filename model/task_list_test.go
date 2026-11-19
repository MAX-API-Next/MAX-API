// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskListsIsolateUnreadableMetadataAndKeepScope(t *testing.T) {
	for _, asText := range []bool{true, false} {
		t.Run(map[bool]string{true: "text", false: "blob"}[asText], func(t *testing.T) {
			truncateTables(t)
			healthy := &Task{TaskID: "healthy-list", UserId: 11, ChannelId: 77, Quota: 7, Status: TaskStatusSubmitted, PrivateData: TaskPrivateData{UpstreamTaskID: "healthy-provider"}}
			bad := &Task{TaskID: "bad-list", UserId: 11, Status: TaskStatusSubmitted}
			foreign := &Task{TaskID: "foreign-list", UserId: 12, ChannelId: 78, Quota: 8}
			for _, row := range []*Task{healthy, bad, foreign} {
				insertTask(t, row)
			}
			var raw any = []byte(`{"upstream_task_id":42}`)
			if asText {
				raw = `{"upstream_task_id":42}`
			}
			require.NoError(t, DB.Model(bad).UpdateColumn("private_data", raw).Error)
			userRows := TaskGetAllUserTask(11, 0, 10, SyncTaskQueryParams{})
			require.Len(t, userRows, 1)
			require.Equal(t, healthy.ID, userRows[0].ID)
			require.Zero(t, userRows[0].ChannelId, "user list must retain its channel_id omission")
			require.Equal(t, 7, userRows[0].Quota)
			adminRows := TaskGetAllTasks(0, 10, SyncTaskQueryParams{})
			require.Len(t, adminRows, 2)
			require.Equal(t, foreign.ID, adminRows[0].ID)
			require.Equal(t, 77, adminRows[1].ChannelId)
			filtered := TaskGetAllTasks(0, 10, SyncTaskQueryParams{UserIDs: []int{11}, TaskID: "healthy-list"})
			require.Len(t, filtered, 1)
			require.Equal(t, healthy.ID, filtered[0].ID)
			require.Len(t, TaskGetAllUserTask(11, 1, 1, SyncTaskQueryParams{}), 1, "pagination still happens in SQL before decoding")
			var strict Task
			require.Error(t, DB.First(&strict, bad.ID).Error, "single-task financial reads remain strict")
		})
	}
}

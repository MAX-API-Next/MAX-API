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
			bad := &Task{TaskID: "bad-list", UserId: 11, Quota: 9, Status: TaskStatusSubmitted, FailReason: "public failure"}
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
			require.Len(t, userRows, 2)
			require.Equal(t, bad.ID, userRows[0].ID)
			require.True(t, userRows[0].PrivateDataUnreadable())
			require.Equal(t, TaskPrivateData{}, userRows[0].PrivateData)
			require.Empty(t, userRows[0].GetResultURL())
			require.Equal(t, "public failure", userRows[0].FailReason)
			require.Equal(t, 9, userRows[0].Quota)
			require.Equal(t, healthy.ID, userRows[1].ID)
			require.Zero(t, userRows[1].ChannelId, "user list must retain its channel_id omission")
			require.Equal(t, 7, userRows[1].Quota)
			adminRows := TaskGetAllTasks(0, 10, SyncTaskQueryParams{})
			require.Len(t, adminRows, 3)
			require.Equal(t, foreign.ID, adminRows[0].ID)
			require.Equal(t, bad.ID, adminRows[1].ID)
			require.Equal(t, 77, adminRows[2].ChannelId)
			require.Len(t, TaskGetAllTasks(0, 10, SyncTaskQueryParams{TaskID: "bad-list"}), 1)
			require.Len(t, TaskGetAllUserTask(12, 0, 10, SyncTaskQueryParams{TaskID: "bad-list"}), 0)
			filtered := TaskGetAllTasks(0, 10, SyncTaskQueryParams{UserIDs: []int{11}, TaskID: "healthy-list"})
			require.Len(t, filtered, 1)
			require.Equal(t, healthy.ID, filtered[0].ID)
			secondPage := TaskGetAllUserTask(11, 1, 1, SyncTaskQueryParams{})
			require.Len(t, secondPage, 1, "pagination still happens in SQL before decoding")
			require.Equal(t, healthy.ID, secondPage[0].ID)
			dispatchedIDs := []int64{}
			for _, dispatched := range GetAllUnFinishSyncTasks(10) {
				dispatchedIDs = append(dispatchedIDs, dispatched.ID)
				require.NotEqual(t, bad.ID, dispatched.ID, "list visibility must not dispatch unreadable private data")
			}
			require.Contains(t, dispatchedIDs, healthy.ID, "healthy neighbors must actually be dispatched")
			batch, err := GetByTaskIds(11, []any{healthy.TaskID, bad.TaskID, foreign.TaskID})
			require.NoError(t, err)
			require.Len(t, batch, 2)
			public, exists, err := GetUserTaskStatusByID(11, bad.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, bad.ID, public.ID)
			require.True(t, public.PrivateDataUnreadable())
			require.Empty(t, public.GetResultURL())
			_, exists, err = GetUserTaskStatusByID(12, bad.TaskID)
			require.NoError(t, err)
			require.False(t, exists)
			ids := []int64{}
			for _, item := range batch {
				ids = append(ids, item.ID)
				if item.ID == bad.ID {
					require.True(t, item.PrivateDataUnreadable())
					require.Empty(t, item.GetResultURL())
				}
			}
			require.Contains(t, ids, healthy.ID)
			require.Contains(t, ids, bad.ID)
			var strict Task
			require.Error(t, DB.First(&strict, bad.ID).Error, "single-task financial reads remain strict")
		})
	}
}

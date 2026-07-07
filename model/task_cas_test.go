package model

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&Token{},
		&Log{},
		&Option{},
		&Channel{},
		&Ability{},
		&Redemption{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&PerfMetric{},
		&SystemTask{},
		&SystemInstance{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM redemptions")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM subscription_pre_consume_records")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM system_tasks")
		DB.Exec("DELETE FROM system_instances")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestGetAllUnFinishSyncTasksIncludesNonTerminalHundredProgress(t *testing.T) {
	truncateTables(t)

	insertTask(t, &Task{
		TaskID:   "task_stuck_progress",
		Status:   TaskStatusInProgress,
		Progress: "100%",
		Data:     json.RawMessage(`{}`),
	})
	insertTask(t, &Task{
		TaskID:   "task_done",
		Status:   TaskStatusSuccess,
		Progress: "100%",
		Data:     json.RawMessage(`{}`),
	})

	tasks := GetAllUnFinishSyncTasks(10)

	require.Len(t, tasks, 1)
	assert.Equal(t, "task_stuck_progress", tasks[0].TaskID)
}

func TestGetTimedOutUnfinishedTasksIncludesNonTerminalHundredProgress(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	insertTask(t, &Task{
		TaskID:     "task_timeout_stuck_progress",
		Status:     TaskStatusInProgress,
		Progress:   "100%",
		SubmitTime: now - 60,
		Data:       json.RawMessage(`{}`),
	})
	insertTask(t, &Task{
		TaskID:     "task_timeout_done",
		Status:     TaskStatusSuccess,
		Progress:   "100%",
		SubmitTime: now - 60,
		Data:       json.RawMessage(`{}`),
	})

	tasks := GetTimedOutUnfinishedTasks(now, 10)

	require.Len(t, tasks, 1)
	assert.Equal(t, "task_timeout_stuck_progress", tasks[0].TaskID)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateQuotaScopesByTaskPrimaryKey(t *testing.T) {
	truncateTables(t)

	target := &Task{
		TaskID: "task_quota_target",
		Status: TaskStatusInProgress,
		Quota:  100,
		Data:   json.RawMessage(`{}`),
	}
	other := &Task{
		TaskID: "task_quota_other",
		Status: TaskStatusInProgress,
		Quota:  200,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, target)
	insertTask(t, other)

	target.Quota = 350
	require.NoError(t, target.UpdateQuota())

	var reloadedTarget Task
	require.NoError(t, DB.First(&reloadedTarget, target.ID).Error)
	assert.Equal(t, 350, reloadedTarget.Quota)

	var reloadedOther Task
	require.NoError(t, DB.First(&reloadedOther, other.ID).Error)
	assert.Equal(t, 200, reloadedOther.Quota)
}

func TestTaskQuotaFilters(t *testing.T) {
	truncateTables(t)

	tasks := []*Task{
		{
			TaskID: "task_quota_zero",
			UserId: 1,
			Status: TaskStatusSuccess,
			Quota:  0,
			Data:   json.RawMessage(`{}`),
		},
		{
			TaskID: "task_quota_negative",
			UserId: 1,
			Status: TaskStatusSuccess,
			Quota:  -50,
			Data:   json.RawMessage(`{}`),
		},
		{
			TaskID: "task_quota_positive",
			UserId: 1,
			Status: TaskStatusSuccess,
			Quota:  100,
			Data:   json.RawMessage(`{}`),
		},
		{
			TaskID: "task_quota_other_user_negative",
			UserId: 2,
			Status: TaskStatusSuccess,
			Quota:  -75,
			Data:   json.RawMessage(`{}`),
		},
	}
	for _, task := range tasks {
		insertTask(t, task)
	}

	zeroTasks := TaskGetAllTasks(0, 10, SyncTaskQueryParams{QuotaFilter: LogQuotaFilterZero})
	require.Len(t, zeroTasks, 1)
	assert.Equal(t, "task_quota_zero", zeroTasks[0].TaskID)

	negativeUserTasks := TaskGetAllUserTask(1, 0, 10, SyncTaskQueryParams{QuotaFilter: LogQuotaFilterNegative})
	require.Len(t, negativeUserTasks, 1)
	assert.Equal(t, "task_quota_negative", negativeUserTasks[0].TaskID)

	assert.EqualValues(t, 3, TaskCountAllTasks(SyncTaskQueryParams{QuotaFilter: LogQuotaFilterAbnormal}))
	assert.EqualValues(t, 2, TaskCountAllUserTask(1, SyncTaskQueryParams{QuotaFilter: LogQuotaFilterAbnormal}))
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

package model

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPollingBatchQuarantinesUnadvanceableVersions(t *testing.T) {
	truncateTables(t)
	quarantined := make(map[int64]int64)
	for _, version := range []int64{10, math.MaxInt64 - 2, math.MaxInt64 - 1, math.MaxInt64} {
		row := &Task{TaskID: fmt.Sprint(version), Status: TaskStatusSubmitted, UpdatedAt: version}
		require.NoError(t, DB.Create(row).Error)
		if version >= math.MaxInt64-1 {
			quarantined[row.ID] = version
		}
	}
	tasks := GetAllUnFinishSyncTasks(10)
	require.Len(t, tasks, 2)
	require.Equal(t, "10", tasks[0].TaskID)
	require.Less(t, tasks[0].UpdatedAt, int64(math.MaxInt64-2))
	require.EqualValues(t, math.MaxInt64-1, tasks[1].UpdatedAt)
	for _, task := range tasks {
		stored, err := GetTaskPlaceholderByID(task.ID)
		require.NoError(t, err)
		require.Equal(t, task.UpdatedAt, stored.UpdatedAt)
	}
	for id, version := range quarantined {
		stored, err := GetTaskPlaceholderByID(id)
		require.NoError(t, err)
		require.Equal(t, version, stored.UpdatedAt)
	}
}

func TestClaimedNullTimestampSupportsFirstManualDecision(t *testing.T) {
	truncateTables(t)
	row := &Task{TaskID: "claim-null-timestamp", Status: TaskStatusSubmitted, Quota: 20}
	insertTask(t, row)
	require.NoError(t, DB.Model(row).UpdateColumn("updated_at", nil).Error)
	claimed := GetAllUnFinishSyncTasks(100)
	require.Len(t, claimed, 1)
	input := BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 20, TaskQuotaTarget: 20}
	won, err := claimed[0].UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, claimed[0].UpdatedAt, input, "manual evidence required")
	require.NoError(t, err)
	require.True(t, won)
	var record BillingSettlement
	require.NoError(t, DB.Where("operation_key = ?", input.OperationKey).First(&record).Error)
	require.Equal(t, BillingSettlementStatusManual, record.Status)
	require.Zero(t, record.AppliedFundingDelta)
	require.Zero(t, record.AppliedTokenDelta)
}

func TestTimeoutCASPreservesQuotaNullability(t *testing.T) {
	previousSQLite := common.UsingSQLite
	common.UsingSQLite = true
	t.Cleanup(func() { common.UsingSQLite = previousSQLite })
	for _, scenario := range []struct {
		name                          string
		initialNull, change, nextNull bool
	}{
		{name: "null-unchanged", initialNull: true},
		{name: "zero-unchanged"},
		{name: "null-to-zero", initialNull: true, change: true},
		{name: "zero-to-null", change: true, nextNull: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			truncateTables(t)
			row := &Task{TaskID: scenario.name, Status: TaskStatusSubmitted, SubmitTime: 1}
			insertTask(t, row)
			if scenario.initialNull {
				require.NoError(t, DB.Model(row).UpdateColumn("quota", nil).Error)
			}
			rows, err := GetTimedOutUnfinishedTasksAfter(1000, 0, 0, 10)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			observed := rows[0]
			if scenario.change {
				var next any = 0
				if scenario.nextNull {
					next = nil
				}
				require.NoError(t, DB.Model(row).UpdateColumn("quota", next).Error)
			}
			observed.Status = TaskStatusFailure
			observed.PreservePrivateDataOnStatusUpdate()
			won, err := observed.UpdateWithStatusSnapshot(TaskStatusSubmitted, observed.UpdatedAt)
			require.NoError(t, err)
			require.Equal(t, !scenario.change, won)
			var stored sql.NullInt64
			require.NoError(t, DB.Model(&Task{}).Select("quota").Where("id = ?", row.ID).Row().Scan(&stored))
			expectedNull := scenario.initialNull
			if scenario.change {
				expectedNull = scenario.nextNull
			}
			require.Equal(t, expectedNull, !stored.Valid, "CAS must not normalize NULL into an explicit zero")
			if stored.Valid {
				require.Zero(t, stored.Int64)
			}
			assertStoredTimeoutStatus(t, row.ID, won)
		})
	}
}

func TestTimeoutCASPreservesTimestampNullability(t *testing.T) {
	for _, withSettlement := range []bool{false, true} {
		for _, change := range []bool{false, true} {
			t.Run(fmt.Sprintf("settlement_%t/change_%t", withSettlement, change), func(t *testing.T) {
				truncateTables(t)
				row := &Task{TaskID: "nullable-version", Status: TaskStatusSubmitted, Quota: 10, SubmitTime: 1}
				insertTask(t, row)
				require.NoError(t, DB.Model(row).UpdateColumn("updated_at", nil).Error)
				rows, err := GetTimedOutUnfinishedTasksAfter(1000, 0, 0, 10)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				observed := rows[0]
				if change {
					require.NoError(t, DB.Model(row).UpdateColumn("updated_at", 0).Error)
				}
				observed.Status = TaskStatusFailure
				observed.PreservePrivateDataOnStatusUpdate()
				var won bool
				if withSettlement {
					won, err = observed.UpdateWithStatusAndSettlementSnapshot(TaskStatusSubmitted, 0, BillingSettlementInput{OperationKey: "task-null:refund", Source: BillingSettlementSourceWallet, UserID: 1, FundingDelta: -10, TokenDelta: -10, TaskID: row.ID, TaskQuota: 10, TaskQuotaTarget: 0})
				} else {
					won, err = observed.UpdateWithStatusSnapshot(TaskStatusSubmitted, 0)
				}
				require.NoError(t, err)
				require.Equal(t, !change, won)
				assertStoredTimeoutStatus(t, row.ID, won)
				var intents int64
				require.NoError(t, DB.Model(&BillingSettlement{}).Count(&intents).Error)
				if withSettlement && !change {
					require.EqualValues(t, 1, intents)
				} else {
					require.Zero(t, intents)
				}
			})
		}
	}
}

func assertStoredTimeoutStatus(t *testing.T, id int64, won bool) {
	t.Helper()
	var stored Task
	require.NoError(t, DB.First(&stored, id).Error)
	if won {
		require.EqualValues(t, TaskStatusFailure, stored.Status)
	} else {
		require.EqualValues(t, TaskStatusSubmitted, stored.Status)
	}
}

func TestManualSettlementCASPreservesTimestampNullability(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(fmt.Sprint(change), func(t *testing.T) {
			truncateTables(t)
			row := &Task{TaskID: "nullable-manual", Status: TaskStatusSubmitted, Quota: 10, SubmitTime: 1}
			insertTask(t, row)
			require.NoError(t, DB.Model(row).UpdateColumn("updated_at", nil).Error)
			rows, err := GetTimedOutUnfinishedTasksAfter(1000, 0, 0, 10)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			observed := rows[0]
			if change {
				require.NoError(t, DB.Model(row).UpdateColumn("updated_at", 0).Error)
			}
			observed.PrivateData.PendingTerminalStatus = TaskStatusSuccess
			observed.PrivateData.PendingTerminalResultURL = "https://example.invalid/new-evidence"
			won, err := observed.UpdateWithStatusAndManualSettlement(TaskStatusSubmitted, 0, BillingSettlementInput{OperationKey: BillingTaskFinalizeOperationKey(row.ID), Source: BillingSettlementSourceWallet, UserID: 1, TaskID: row.ID, TaskQuota: 10, TaskQuotaTarget: 10}, "missing provider evidence")
			require.NoError(t, err)
			require.Equal(t, !change, won)
			stored, err := GetTaskPlaceholderByID(row.ID)
			require.NoError(t, err)
			require.Equal(t, TaskStatus(TaskStatusSubmitted), stored.Status)
			var records []BillingSettlement
			require.NoError(t, DB.Find(&records).Error)
			if change {
				require.Empty(t, records)
				require.Empty(t, stored.PrivateData.PendingTerminalStatus)
				require.Empty(t, stored.PrivateData.PendingTerminalResultURL)
			} else {
				require.Len(t, records, 1)
				require.Equal(t, BillingSettlementStatusManual, records[0].Status)
				require.Zero(t, records[0].AppliedFundingDelta)
				require.Equal(t, TaskStatus(TaskStatusSuccess), stored.PrivateData.PendingTerminalStatus)
				require.Equal(t, "https://example.invalid/new-evidence", stored.PrivateData.PendingTerminalResultURL)
				require.False(t, observed.updatedAtWasNull)
			}
		})
	}
}

func TestTimedOutTaskQueryUsesPortableTaskFinalizeExclusion(t *testing.T) {
	tests := []struct {
		name        string
		open        func(*testing.T) *gorm.DB
		usingSQLite bool
		keySQL      string
	}{
		{
			name: "sqlite",
			open: func(t *testing.T) *gorm.DB {
				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
				require.NoError(t, err)
				return db
			},
			usingSQLite: true,
			keySQL:      "'TASK:' || CAST(TASKS.ID AS TEXT) || ':FINALIZE'",
		},
		{
			name: "mysql",
			open: func(t *testing.T) *gorm.DB {
				conn, err := sql.Open("mysql", "")
				require.NoError(t, err)
				t.Cleanup(func() { _ = conn.Close() })
				db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{
					DryRun: true, DisableAutomaticPing: true,
				})
				require.NoError(t, err)
				return db
			},
			keySQL: "CONCAT('TASK:', TASKS.ID, ':FINALIZE')",
		},
		{
			name: "postgres",
			open: func(t *testing.T) *gorm.DB {
				conn, err := sql.Open("pgx/v5", "")
				require.NoError(t, err)
				t.Cleanup(func() { _ = conn.Close() })
				db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: conn}), &gorm.Config{
					DryRun: true, DisableAutomaticPing: true,
				})
				require.NoError(t, err)
				return db
			},
			keySQL: "CONCAT('TASK:', TASKS.ID, ':FINALIZE')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.open(t)
			statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
				var tasks []*Task
				return timedOutUnfinishedTasksQuery(tx, tt.usingSQLite, 200, 100, 400, 2).Find(&tasks)
			})
			upper := strings.ToUpper(statement)
			require.Contains(t, upper, "NOT EXISTS (SELECT 1 FROM")
			require.Contains(t, upper, tt.keySQL)
			require.Contains(t, upper, "ORDER BY SUBMIT_TIME ASC, ID ASC")
			require.Contains(t, upper, "LIMIT 2")
		})
	}
}

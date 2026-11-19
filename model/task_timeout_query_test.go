package model

import (
	"database/sql"
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

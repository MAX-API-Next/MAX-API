package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type systemTaskAPIResponse struct {
	Success bool                      `json:"success"`
	Message string                    `json:"message"`
	Data    *model.SystemTaskResponse `json:"data"`
}

func setupSystemTaskControllerTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}))

	originalDB := model.DB
	model.DB = db

	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func performCurrentLogCleanupSystemTaskRequest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system-task/log-cleanup/current", nil)

	GetCurrentLogCleanupSystemTask(ctx)
	return recorder
}

func performLogCleanupSystemTaskRequest(t *testing.T, taskID string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "task_id", Value: taskID}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system-task/log-cleanup/"+taskID, nil)

	GetLogCleanupSystemTask(ctx)
	return recorder
}

func decodeSystemTaskAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) systemTaskAPIResponse {
	t.Helper()

	var response systemTaskAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestGetCurrentLogCleanupSystemTaskOnlyReturnsLogCleanupTasks(t *testing.T) {
	setupSystemTaskControllerTestDB(t)

	_, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, model.SystemTaskTypeChannelTest, map[string]any{}, map[string]any{})
	require.NoError(t, err)

	recorder := performCurrentLogCleanupSystemTaskRequest(t)
	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeSystemTaskAPIResponse(t, recorder)
	require.True(t, response.Success)
	require.Nil(t, response.Data)

	logCleanupTask, err := model.CreateSystemTask(model.SystemTaskTypeLogCleanup, model.SystemTaskTypeLogCleanup, map[string]any{}, map[string]any{})
	require.NoError(t, err)

	recorder = performCurrentLogCleanupSystemTaskRequest(t)
	require.Equal(t, http.StatusOK, recorder.Code)
	response = decodeSystemTaskAPIResponse(t, recorder)
	require.True(t, response.Success)
	require.NotNil(t, response.Data)
	require.Equal(t, logCleanupTask.TaskID, response.Data.TaskID)
	require.Equal(t, model.SystemTaskTypeLogCleanup, response.Data.Type)
}

func TestGetLogCleanupSystemTaskRejectsOtherTaskTypes(t *testing.T) {
	setupSystemTaskControllerTestDB(t)

	logCleanupTask, err := model.CreateSystemTask(model.SystemTaskTypeLogCleanup, model.SystemTaskTypeLogCleanup, map[string]any{}, map[string]any{})
	require.NoError(t, err)
	otherTask, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, model.SystemTaskTypeChannelTest, map[string]any{}, map[string]any{})
	require.NoError(t, err)

	recorder := performLogCleanupSystemTaskRequest(t, logCleanupTask.TaskID)
	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeSystemTaskAPIResponse(t, recorder)
	require.True(t, response.Success)
	require.NotNil(t, response.Data)
	require.Equal(t, logCleanupTask.TaskID, response.Data.TaskID)
	require.Equal(t, model.SystemTaskTypeLogCleanup, response.Data.Type)

	recorder = performLogCleanupSystemTaskRequest(t, otherTask.TaskID)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	response = decodeSystemTaskAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Nil(t, response.Data)
}

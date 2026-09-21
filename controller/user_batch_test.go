package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupUserBatchTest(t *testing.T) {
	t.Helper()
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	for _, user := range []model.User{
		{Id: 1, Username: "root", Role: common.RoleRootUser, Status: 1},
		{Id: 2, Username: "admin", Role: common.RoleAdminUser, Status: 1},
		{Id: 3, Username: "common", Role: common.RoleCommonUser, Status: 1, Quota: 10000},
		{Id: 4, Username: "disabled", Role: common.RoleCommonUser, Status: 2},
		{Id: 5, Username: "peer", Role: common.RoleAdminUser, Status: 1},
	} {
		user.AffCode = user.Username
		require.NoError(t, db.Create(&user).Error)
	}
}

func callUserStatusBatch(t *testing.T, actorID, role int, body string) (int, bool, []batchUserStatusResult) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", actorID)
	c.Set("role", role)
	c.Set("username", "test-operator")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage/batch", bytes.NewBufferString(body))
	BatchManageUserStatus(c)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Results []batchUserStatusResult `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	return w.Code, response.Success, response.Data.Results
}

func TestBatchManageUserStatusPartialResultsAndAudit(t *testing.T) {
	setupUserBatchTest(t)
	body := `{"action":"disable","users":[{"id":3,"status":1},{"id":4,"status":2},{"id":1,"status":1},{"id":2,"status":1},{"id":5,"status":1},{"id":999,"status":1}]}`
	code, success, results := callUserStatusBatch(t, 2, common.RoleAdminUser, body)
	require.Equal(t, 200, code)
	require.True(t, success)
	require.Equal(t, []batchUserStatusResult{
		{ID: 3, Outcome: "updated", Status: 2}, {ID: 4, Outcome: "unchanged", Status: 2},
		{ID: 1, Outcome: "rejected", Code: "forbidden"}, {ID: 2, Outcome: "rejected", Code: "forbidden"},
		{ID: 5, Outcome: "rejected", Code: "forbidden"}, {ID: 999, Outcome: "rejected", Code: "not_found"},
	}, results)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Order("id").Find(&logs).Error)
	require.Len(t, logs, 6)
	for _, log := range logs {
		require.Equal(t, model.LogTypeManage, log.Type)
		require.Contains(t, log.Other, `"admin_id":2`)
		require.Contains(t, log.Other, `"batch":true`)
		require.Contains(t, log.Other, `"outcome":`)
	}
	_, success, results = callUserStatusBatch(t, 2, common.RoleAdminUser, body)
	require.True(t, success)
	require.Equal(t, "unchanged", results[0].Outcome)
	_, success, results = callUserStatusBatch(t, 2, common.RoleAdminUser, `{"action":"enable","users":[{"id":3,"status":2}]}`)
	require.True(t, success)
	require.Equal(t, "updated", results[0].Outcome)
	var user model.User
	require.NoError(t, model.DB.First(&user, 3).Error)
	require.EqualValues(t, 10000, user.Quota)
	require.Equal(t, 1, user.Status)
}

func TestBatchManageUserStatusValidatesEntireRequestBeforeWriting(t *testing.T) {
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf(`{"id":%d,"status":1}`, i+3)
	}
	for index, body := range []string{
		`{`, `{"action":"disable","users":[]}`, `{"action":"delete","users":[{"id":3,"status":1}]}`,
		`{"action":"disable","users":[{"id":3,"status":1},{"id":3,"status":1}]}`,
		`{"action":"disable","users":[{"id":3,"status":1},{"id":0,"status":1}]}`,
		`{"action":"disable","users":[{"id":3,"status":1},{"id":4}]}`,
		`{"action":"disable","users":[{"id":3,"status":1},{"id":4,"status":-1}]}`,
		`{"action":"disable","users":[` + strings.Join(tooMany, ",") + `]}`,
		`{"action":"disable","padding":"` + strings.Repeat("x", 17000) + `","users":[{"id":3,"status":1}]}`,
	} {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			setupUserBatchTest(t)
			_, success, results := callUserStatusBatch(t, 2, common.RoleAdminUser, body)
			require.False(t, success)
			require.Empty(t, results)
			var user model.User
			require.NoError(t, model.DB.First(&user, 3).Error)
			require.Equal(t, 1, user.Status)
		})
	}
}

func TestBatchManageUserStatusDoesNotTrustMiddlewareRoleSnapshot(t *testing.T) {
	setupUserBatchTest(t)
	body := `{"action":"disable","users":[{"id":4,"status":2}]}`
	code, success, _ := callUserStatusBatch(t, 3, common.RoleCommonUser, body)
	require.Equal(t, http.StatusForbidden, code)
	require.False(t, success)
	_, success, results := callUserStatusBatch(t, 3, common.RoleRootUser, body)
	require.True(t, success)
	require.Equal(t, "forbidden", results[0].Code)
}

func TestBatchManageUserStatusAcceptsLimitAndReturnsEveryRequestedID(t *testing.T) {
	setupUserBatchTest(t)
	items := make([]string, maxUserStatusBatchSize)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":%d,"status":1}`, 1000+i)
	}
	_, success, results := callUserStatusBatch(t, 2, common.RoleAdminUser,
		`{"action":"disable","users":[`+strings.Join(items, ",")+`]}`)
	require.True(t, success)
	require.Len(t, results, maxUserStatusBatchSize)
	for i, result := range results {
		require.Equal(t, 1000+i, result.ID)
		require.Equal(t, "not_found", result.Code)
	}
}

func TestBatchManageUserStatusDatabaseErrorIsNotConfirmedFailure(t *testing.T) {
	setupUserBatchTest(t)
	// Simulate a persistence failure after the first account was processed.
	_, success, results := callUserStatusBatch(t, 2, common.RoleAdminUser, `{"action":"disable","users":[{"id":3,"status":1},{"id":4,"status":1}]}`)
	require.True(t, success)
	require.Equal(t, "updated", results[0].Outcome)
	require.NoError(t, model.DB.Migrator().DropTable(&model.User{}))
	_, success, results = callUserStatusBatch(t, 2, common.RoleAdminUser, `{"action":"disable","users":[{"id":3,"status":1}]}`)
	require.True(t, success)
	require.Equal(t, "unknown", results[0].Outcome)
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package router

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
)

func TestOfficialTaskPluginRootInstallAndUpgradeGuard(t *testing.T) {
	engine, db, _ := taskPluginManagementEngine(t)
	login := httptest.NewRecorder()
	engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
	cookies := login.Result().Cookies()
	call := func(method, path string, body any, authorized bool) *httptest.ResponseRecorder {
		wire, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(wire))
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
		}
		r := httptest.NewRecorder()
		engine.ServeHTTP(r, req)
		return r
	}
	for _, path := range []string{"/api/task_plugins/bundled", "/api/task_plugins/bundled/kling/1.2.0"} {
		require.Contains(t, call("GET", path, nil, false).Body.String(), `"success":false`)
	}
	r := call("GET", "/api/task_plugins/bundled", nil, true)
	require.Equal(t, 200, r.Code)
	require.Contains(t, r.Body.String(), `"key":"kling"`)
	r = call("GET", "/api/task_plugins/bundled/kling/1.2.0", nil, true)
	require.Equal(t, 200, r.Code)
	var download struct {
		Data struct {
			Document string `json:"document"`
			Pin      string `json:"expected_sha256"`
		}
	}
	require.NoError(t, common.Unmarshal(r.Body.Bytes(), &download))
	require.Equal(t, "f4b949679dba31e287d2b21d4b3b4c09980c9bb744da9ede5153f1801cf84240", download.Data.Pin)
	upload := map[string]any{"document": download.Data.Document, "expected_sha256": download.Data.Pin}
	r = call("POST", "/api/task_plugins/validate", upload, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Contains(t, r.Body.String(), `"format_version":3`)
	require.Contains(t, r.Body.String(), `"source_sha256":"`+download.Data.Pin+`"`)
	upload["action"], upload["expected_revision"], upload["format2_nodes_ready"] = "install", "0", true
	r = call("POST", "/api/task_plugins/mutate", upload, true)
	require.Equal(t, 400, r.Code, "format 2 acknowledgement cannot authorize format 3")
	upload["format3_nodes_ready"] = true
	r = call("POST", "/api/task_plugins/mutate", upload, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	r = call("GET", "/api/task_plugins", nil, true)
	require.Equal(t, 200, r.Code)
	require.Contains(t, r.Body.String(), `"format_version":3`)
	require.Contains(t, r.Body.String(), `"admission_ready":false`)
	require.NotContains(t, r.Body.String(), "export const meta")
	var count int64
	require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.False(t, db.Migrator().HasTable(&model.BillingSettlement{}), "install/read/validation must never create a ledger")
}

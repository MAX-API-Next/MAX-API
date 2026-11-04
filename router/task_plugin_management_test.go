// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package router

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func taskPluginManagementEngine(t *testing.T) (*gin.Engine, *gorm.DB, model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{}))
	oldDB, oldLog, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLog, oldRedis
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	require.NoError(t, service.InitializeTaskPluginCatalog(t.Context(), db))
	admin := model.User{Id: 59123101, Username: "plugin-admin", AffCode: "plugin-admin", Password: "fixture", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default"}
	root := model.User{Id: 59123102, Username: "plugin-root", AffCode: "plugin-root", Password: "fixture", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&root).Error)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("synthetic-plugin-cookie"))))
	engine.GET("/plugin-test-login/:role", func(c *gin.Context) {
		u := admin
		if c.Param("role") == "root" {
			u = root
		}
		s := sessions.Default(c)
		s.Set("id", u.Id)
		s.Set("username", u.Username)
		s.Set("role", u.Role)
		s.Set("status", u.Status)
		s.Set("group", u.Group)
		require.NoError(t, s.Save())
		c.Status(http.StatusNoContent)
	})
	SetApiRouter(engine)
	return engine, db, root
}

func TestTaskPluginManagementRootAPIAndDurableVersionGuards(t *testing.T) {
	engine, db, root := taskPluginManagementEngine(t)
	login := func(role string) []*http.Cookie {
		r := httptest.NewRecorder()
		engine.ServeHTTP(r, httptest.NewRequest("GET", "/plugin-test-login/"+role, nil))
		return r.Result().Cookies()
	}
	request := func(method, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
		wire, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(wire))
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		r := httptest.NewRecorder()
		engine.ServeHTTP(r, req)
		return r
	}
	for _, cookies := range [][]*http.Cookie{nil, login("admin")} {
		for _, path := range []string{"/api/task_plugins", "/api/task_plugins/validate", "/api/task_plugins/mutate"} {
			method := "POST"
			if path == "/api/task_plugins" {
				method = "GET"
			}
			r := request(method, path, map[string]any{}, cookies)
			require.Contains(t, r.Body.String(), `"success":false`)
		}
	}
	cookies := login("root")
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 2, Source: "private-source-marker", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "fixture", Name: "Fixture Plugin", Version: "1.0.0", Models: []string{"fixture-model"}, UsageSchema: map[string]taskplugin.UsageField{"units": {Type: "number", Unit: "credit"}}, UsageProfiles: []taskplugin.UsageProfile{{Models: []string{"fixture-model"}, Schema: map[string]taskplugin.UsageField{"units": {Type: "number", Unit: "credit"}}}}}})
	require.NoError(t, err)
	upload := map[string]any{"document": string(wire), "expected_sha256": fmt.Sprintf("%x", sha256.Sum256(wire))}
	r := request("POST", "/api/task_plugins/validate", upload, cookies)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.NotContains(t, r.Body.String(), "private-source-marker")
	var count int64
	require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
	require.Zero(t, count, "validation must not write")
	upload["action"], upload["expected_revision"] = "install", "0"
	for _, badRevision := range []any{nil, float64(0), "01"} {
		bad := map[string]any{"action": "disable", "key": "fixture", "version": "1.0.0", "expected_revision": badRevision}
		r = request("POST", "/api/task_plugins/mutate", bad, cookies)
		require.Equal(t, 400, r.Code, "missing, numeric and noncanonical revision must not become revision zero")
	}
	r = request("POST", "/api/task_plugins/mutate", upload, cookies)
	require.Equal(t, 400, r.Code, "v2 requires explicit all-node upgrade acknowledgement")
	upload["format2_nodes_ready"] = true
	r = request("POST", "/api/task_plugins/mutate", upload, cookies)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Contains(t, r.Body.String(), `"revision":"1"`)
	r = request("POST", "/api/task_plugins/mutate", upload, cookies)
	require.Equal(t, 409, r.Code)
	r = request("GET", "/api/task_plugins", nil, cookies)
	require.Equal(t, 200, r.Code)
	require.Contains(t, r.Body.String(), `"execution_ready":false`)
	require.Contains(t, r.Body.String(), `"admission_ready":false`)
	require.NotContains(t, r.Body.String(), "private-source-marker")
	changed := bytes.Replace(wire, []byte("private-source-marker"), []byte("different-source"), 1)
	upload["document"], upload["expected_sha256"], upload["expected_revision"] = string(changed), fmt.Sprintf("%x", sha256.Sum256(changed)), "1"
	r = request("POST", "/api/task_plugins/mutate", upload, cookies)
	require.Equal(t, 400, r.Code)
	require.NotContains(t, r.Body.String(), "different-source")
	r = request("POST", "/api/task_plugins/mutate", map[string]any{"action": "revoke", "key": "fixture", "version": "1.0.0", "expected_revision": "1"}, cookies)
	require.Equal(t, 200, r.Code)
	r = request("POST", "/api/task_plugins/mutate", map[string]any{"action": "activate", "key": "fixture", "version": "1.0.0", "expected_revision": "2", "format2_nodes_ready": true}, cookies)
	require.Equal(t, 404, r.Code)
	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	// Generic audit also records validation and rejected administrative attempts.
	successfulChanges := 0
	for _, l := range logs {
		if l.Content == "task_plugin.install" || l.Content == "task_plugin.revoke" {
			successfulChanges++
		}
		require.NotContains(t, l.Content, "private-source-marker")
		require.NotContains(t, l.Other, "private-source-marker")
	}
	require.Equal(t, 2, successfulChanges)
	// A still-valid cookie must not retain root authority after a DB role change.
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", root.Id).Update("role", common.RoleAdminUser).Error)
	r = request("POST", "/api/task_plugins/mutate", map[string]any{"action": "disable", "key": "fixture", "version": "1.0.0", "expected_revision": "2"}, cookies)
	require.Contains(t, r.Body.String(), `"success":false`)
	var head model.TaskPluginCatalogHead
	require.NoError(t, db.First(&head, 1).Error)
	require.EqualValues(t, 2, head.Revision)
}

func TestTaskPluginManagementHistoricalEnableDisableIsInvalidNotConflict(t *testing.T) {
	engine, db, _ := taskPluginManagementEngine(t)
	catalog, err := service.CurrentTaskPluginCatalog()
	require.NoError(t, err)
	for _, version := range []string{"1.0.0", "2.0.0"} {
		document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "synthetic history fixture", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "history-fixture", Name: "History Fixture", Version: version, Models: []string{"fixture-model"}, Routes: []taskplugin.Route{{Method: "POST", Path: "/isolated/history/jobs", Type: "submit", Decode: "submit", Render: "created"}}}})
		require.NoError(t, err)
		pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
		require.NoError(t, err)
		require.NoError(t, catalog.Install(t.Context(), pkg))
	}
	before, revision, err := catalog.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 2, revision)
	login := httptest.NewRecorder()
	engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
	for _, action := range []string{"enable", "disable"} {
		for _, observed := range []string{"1", "2"} {
			t.Run(action+"/"+observed, func(t *testing.T) {
				wire, err := common.Marshal(map[string]any{"action": action, "key": "history-fixture", "version": "1.0.0", "expected_revision": observed})
				require.NoError(t, err)
				request := httptest.NewRequest("POST", "/api/task_plugins/mutate", bytes.NewReader(wire))
				request.Header.Set("Content-Type", "application/json")
				for _, cookie := range login.Result().Cookies() {
					request.AddCookie(cookie)
				}
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				if observed == "1" {
					require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), "catalog changed")
				} else {
					require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), "transition rejected")
				}
				after, revision, err := catalog.ManagementSnapshot(t.Context())
				require.NoError(t, err)
				require.EqualValues(t, 2, revision)
				require.Equal(t, before, after, "rejections must not change any version flags")
				var head model.TaskPluginCatalogHead
				require.NoError(t, db.First(&head, 1).Error)
				require.EqualValues(t, 2, head.Revision)
				active, err := catalog.Host().PricingPackage("POST", "/isolated/history/jobs", "fixture-model")
				require.NoError(t, err, "a rejected transition must not suspend the host")
				require.Equal(t, "2.0.0", active.Manifest().Version)
			})
		}
	}
}

// Explicit, bounded, loopback-only harness for the real TS client/UI against
// these actual routes. Default unit runs skip it; it never uses external DSNs.
func TestTaskPluginManagementUIFixture(t *testing.T) {
	output := os.Getenv("TASK_PLUGIN_ADMIN_UI_ADDR_FILE")
	if output == "" {
		t.Skip("requires explicit local UI fixture output path")
	}
	workspaceTmp, err := filepath.Abs("../.tmp")
	require.NoError(t, err)
	absolute, err := filepath.Abs(output)
	require.NoError(t, err)
	rel, err := filepath.Rel(workspaceTmp, absolute)
	require.NoError(t, err)
	require.False(t, filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)), "fixture address must stay under workspace .tmp")
	engine, _, _ := taskPluginManagementEngine(t)
	stop := make(chan struct{})
	engine.POST("/plugin-test-stop", func(c *gin.Context) {
		select {
		case <-stop:
		default:
			close(stop)
		}
		c.Status(http.StatusNoContent)
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	require.NoError(t, os.WriteFile(absolute, []byte(server.URL), 0600))
	select {
	case <-stop:
	case <-time.After(90 * time.Second):
		t.Fatal("UI fixture expired without stop acknowledgement")
	}
}

func TestTaskPluginManagementClientCancellationCannotSuspendHost(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		t.Run(method, func(t *testing.T) {
			engine, _, _ := taskPluginManagementEngine(t)
			catalog, err := service.CurrentTaskPluginCatalog()
			require.NoError(t, err)
			document := func(key, route string) []byte {
				data, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "synthetic cancellation fixture", Manifest: taskplugin.Manifest{APIVersion: 1, Key: key, Name: key, Version: "1.0.0", Models: []string{"fixture-model"}, Routes: []taskplugin.Route{{Method: "POST", Path: route, Type: "submit", Decode: "submit", Render: "created"}}}})
				require.NoError(t, err)
				return data
			}
			original := document("cancel-fixture", "/isolated/cancel/jobs")
			pkg, err := taskplugin.LoadPackage(bytes.NewReader(original), fmt.Sprintf("%x", sha256.Sum256(original)))
			require.NoError(t, err)
			require.NoError(t, catalog.Install(t.Context(), pkg))
			_, err = catalog.Host().PricingPackage("POST", "/isolated/cancel/jobs", "fixture-model")
			require.NoError(t, err)
			login := httptest.NewRecorder()
			engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
			route := "/api/task_plugins"
			var body []byte
			if method == "POST" {
				route += "/mutate"
				data := document("next-fixture", "/isolated/cancel/next")
				body, err = common.Marshal(map[string]any{"action": "install", "expected_revision": "1", "document": string(data), "expected_sha256": fmt.Sprintf("%x", sha256.Sum256(data))})
				require.NoError(t, err)
			}
			request := httptest.NewRequest(method, route, bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			for _, cookie := range login.Result().Cookies() {
				request.AddCookie(cookie)
			}
			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			request = request.WithContext(ctx)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			_, err = catalog.Host().PricingPackage("POST", "/isolated/cancel/jobs", "fixture-model")
			require.NoError(t, err, "an authorized client's cancellation must not suspend other callers")
			if method == "POST" {
				require.Contains(t, recorder.Body.String(), "\"revision\":\"2\"")
				_, err = catalog.Host().PricingPackage("POST", "/isolated/cancel/next", "fixture-model")
				require.NoError(t, err)
			}
		})
	}
}

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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
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
	require.Equal(t, 422, r.Code)
	require.Contains(t, r.Body.String(), "different content")
	require.NotContains(t, r.Body.String(), "different-source")
	r = request("POST", "/api/task_plugins/mutate", map[string]any{"action": "revoke", "key": "fixture", "version": "1.0.0", "expected_revision": "1"}, cookies)
	require.Equal(t, 400, r.Code, "legacy revoke action is no longer exposed")
	r = request("POST", "/api/task_plugins/mutate", map[string]any{"action": "disable", "key": "fixture", "version": "1.0.0", "expected_revision": "1"}, cookies)
	require.Equal(t, 200, r.Code)
	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	// Generic audit also records validation and rejected administrative attempts.
	successfulChanges := 0
	for _, l := range logs {
		if l.Content == "task_plugin.install" || l.Content == "task_plugin.disable" {
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

func TestTaskPluginManagementDeletionRequiresRootAcknowledgementAndNoReferences(t *testing.T) {
	engine, db, root := taskPluginManagementEngine(t)
	require.NoError(t, db.AutoMigrate(&model.TaskPluginDeletion{}, &model.Task{}, &model.Channel{}, &model.Option{}))
	login := httptest.NewRecorder()
	engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
	cookies := login.Result().Cookies()
	request := func(body map[string]any, authenticated bool) *httptest.ResponseRecorder {
		wire, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/task_plugins/mutate", bytes.NewReader(wire))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("New-Api-User", fmt.Sprint(root.Id))
		if authenticated {
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "private-delete-api-source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "deletable", Name: "Deletable", Version: "1.0.0", Models: []string{"fixture-model"}}})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
	require.NoError(t, err)
	catalog, err := service.CurrentTaskPluginCatalog()
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	command := map[string]any{"action": "delete", "key": "deletable", "version": "1.0.0", "expected_revision": "1"}
	require.Contains(t, request(command, false).Body.String(), `"success":false`)
	require.Equal(t, 400, request(command, true).Code)
	command["deletion_nodes_ready"] = true
	unauthorized := request(command, false)
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code, unauthorized.Body.String())
	entries, revision, err := catalog.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.EqualValues(t, 1, revision)
	command["expected_revision"] = "0"
	require.Equal(t, 409, request(command, true).Code)
	command["expected_revision"] = "1"
	adminLogin := httptest.NewRecorder()
	engine.ServeHTTP(adminLogin, httptest.NewRequest("GET", "/plugin-test-login/admin", nil))
	adminBody, err := common.Marshal(command)
	require.NoError(t, err)
	adminRequest := httptest.NewRequest("POST", "/api/task_plugins/mutate", bytes.NewReader(adminBody))
	adminRequest.Header.Set("Content-Type", "application/json")
	adminRequest.Header.Set("New-Api-User", "59123101")
	for _, cookie := range adminLogin.Result().Cookies() {
		adminRequest.AddCookie(cookie)
	}
	adminResponse := httptest.NewRecorder()
	engine.ServeHTTP(adminResponse, adminRequest)
	require.Equal(t, http.StatusOK, adminResponse.Code, adminResponse.Body.String())
	require.JSONEq(t, `{"success":false,"message":"auth.insufficient_privilege"}`, adminResponse.Body.String())
	unchanged, err := model.NewTaskPluginCatalogStore(db).Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, unchanged.Revision)
	require.Len(t, unchanged.Packages, 1)
	channel := model.Channel{Models: "fixture-model"}
	require.NoError(t, db.Create(&channel).Error)
	response := request(command, true)
	require.Equal(t, 422, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "private-delete-api-source")
	require.NoError(t, db.Delete(&channel).Error)
	legacyID := int64(9007199254740993)
	if strconv.IntSize == 32 {
		legacyID = 9123
	}
	legacy := model.Channel{Id: int(legacyID), Models: "unrelated-model"}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.Model(&legacy).UpdateColumn("model_mapping", `{"private-legacy-name":7}`).Error)
	response = request(command, true)
	require.Equal(t, 422, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), fmt.Sprintf("Channel %d", legacy.Id))
	require.Contains(t, response.Body.String(), `"code":"invalid_channel_mapping"`)
	require.Contains(t, response.Body.String(), fmt.Sprintf(`"channel_id":"%d"`, legacy.Id))
	require.NotContains(t, response.Body.String(), "private-legacy-name")
	require.NoError(t, db.Delete(&legacy).Error)
	malformed := &model.Task{TaskID: "private-provider-id"}
	require.NoError(t, db.Create(malformed).Error)
	require.NoError(t, db.Model(malformed).UpdateColumn("private_data", []byte(`{"upstream_task_id":42,"key":"private-task-secret","notes":"deletable"}`)).Error)
	response = request(command, true)
	require.Equal(t, 422, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"invalid_task_data"`)
	require.Contains(t, response.Body.String(), fmt.Sprintf(`"task_id":"%d"`, malformed.ID))
	require.NotContains(t, response.Body.String(), "private-task-secret")
	require.NotContains(t, response.Body.String(), "private-provider-id")
	require.NoError(t, db.Delete(malformed).Error)
	invalidPrice := &model.Option{Key: billing_setting.PluginBillingExprOption, Value: `{"bad-key":"private-price-expression"}`}
	require.NoError(t, db.Create(invalidPrice).Error)
	response = request(command, true)
	require.Equal(t, 422, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"invalid_plugin_pricing"`)
	require.Contains(t, response.Body.String(), `"option_key":"billing_setting.plugin_billing_expr"`)
	require.NotContains(t, response.Body.String(), "bad-key")
	require.NotContains(t, response.Body.String(), "private-price-expression")
	require.NoError(t, db.Where(map[string]any{"key": invalidPrice.Key}).Delete(&model.Option{}).Error)
	response = request(command, true)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"revision":"2"`)
	var count int64
	require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
	require.Zero(t, count)
	command["expected_revision"] = "2"
	require.Equal(t, 404, request(command, true).Code, "deletion is not replayed as success")
	var logs []model.Log
	require.NoError(t, db.Where("content = ?", "task_plugin.delete").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.NotContains(t, logs[0].Other, "private-delete-api-source")
}

func TestTaskPluginManagementRejectsMalformedDocumentUnicode(t *testing.T) {
	engine, db, root := taskPluginManagementEngine(t)
	login := httptest.NewRecorder()
	engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
	doc, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "unicode", Name: "unicode-name", Version: "1.0.0", Models: []string{"unicode-model"}}})
	require.NoError(t, err)
	request := func(path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("New-Api-User", fmt.Sprint(root.Id))
		for _, cookie := range login.Result().Cookies() {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	for _, path := range []string{"/api/task_plugins/validate", "/api/task_plugins/mutate"} {
		wire, err := common.Marshal(map[string]any{"document": string(doc)})
		require.NoError(t, err)
		if strings.HasSuffix(path, "/mutate") {
			wire, err = common.Marshal(map[string]any{"document": string(doc), "action": "install", "expected_revision": "0"})
			require.NoError(t, err)
		}
		for name, invalid := range map[string][]byte{"invalid_utf8": {0xff}, "lone_high": []byte(`\ud800`), "lone_low": []byte(`\udc00`), "wrong_pair": []byte(`\ud800\u0041`)} {
			t.Run(path+"/"+name, func(t *testing.T) {
				body := bytes.Replace(wire, []byte("unicode-name"), invalid, 1)
				response := request(path, body)
				require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			})
		}
	}
	for _, replacement := range []string{`\uD83D\uDE00`, "\uFFFD"} {
		wire, err := common.Marshal(map[string]any{"document": string(doc)})
		require.NoError(t, err)
		response := request("/api/task_plugins/validate", bytes.Replace(wire, []byte("unicode-name"), []byte(replacement), 1))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	bom := append([]byte{0xef, 0xbb, 0xbf}, doc...)
	wire, err := common.Marshal(map[string]any{"document": string(bom)})
	require.NoError(t, err)
	response := request("/api/task_plugins/validate", wire)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), fmt.Sprintf(`"digest":"%x"`, sha256.Sum256(bom)))
	var count int64
	require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
	require.Zero(t, count, "rejected requests and validation cannot install anything")
}

func TestTaskPluginValidationRejectsDeletedRevocationBeforeInstallation(t *testing.T) {
	engine, db, root := taskPluginManagementEngine(t)
	require.NoError(t, db.AutoMigrate(&model.TaskPluginDeletion{}, &model.Task{}, &model.Channel{}, &model.Option{}))
	login := httptest.NewRecorder()
	engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "private-revoked-source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "legacy-revoked", Name: "Legacy", Version: "1.0.0", Models: []string{"legacy-model"}}})
	require.NoError(t, err)
	pkg, err := taskplugin.LoadUpload(bytes.NewReader(document), "")
	require.NoError(t, err)
	catalog, err := service.CurrentTaskPluginCatalog()
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, catalog.Revoke(t.Context(), "legacy-revoked", "1.0.0"))
	require.NoError(t, catalog.MutateExpected(t.Context(), 2, taskplugin.ManagementMutation{Action: "delete", Key: "legacy-revoked", Version: "1.0.0"}))
	validate := func() *httptest.ResponseRecorder {
		body, err := common.Marshal(map[string]any{"document": string(document)})
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/task_plugins/validate", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("New-Api-User", fmt.Sprint(root.Id))
		for _, cookie := range login.Result().Cookies() {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	response := validate()
	require.Equal(t, 422, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "permanently revoked")
	require.NotContains(t, response.Body.String(), "private-revoked-source")
	store := model.NewTaskPluginCatalogStore(db)
	before, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 3, before.Revision)
	require.Empty(t, before.Packages)
	require.True(t, before.Deletions[0].Revoked)
	// Simulate the user's narrowly authorized local repair, then a backend restart.
	require.NoError(t, db.Model(&model.TaskPluginDeletion{}).Where("plugin_key = ? AND version = ?", "legacy-revoked", "1.0.0").Update("revoked", false).Error)
	require.NoError(t, db.Model(&model.TaskPluginCatalogHead{}).Where("id = ?", 1).Update("revision", 4).Error)
	require.NoError(t, service.InitializeTaskPluginCatalog(t.Context(), db))
	response = validate()
	require.Equal(t, 200, response.Code, response.Body.String())
	after, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 4, after.Revision)
	require.Empty(t, after.Packages, "validation after recovery still does not install")
	require.False(t, after.Deletions[0].Revoked)
}

func TestTaskPluginManagementOptionalUploadPin(t *testing.T) {
	for _, kind := range []string{"javascript", "bom-javascript", "json", "bom-json"} {
		t.Run(kind, func(t *testing.T) {
			javascript := strings.HasSuffix(kind, "javascript")
			bom := strings.HasPrefix(kind, "bom-")
			engine, db, root := taskPluginManagementEngine(t)
			login := httptest.NewRecorder()
			engine.ServeHTTP(login, httptest.NewRequest("GET", "/plugin-test-login/root", nil))
			cookies := login.Result().Cookies()
			request := func(path string, body map[string]any) *httptest.ResponseRecorder {
				wire, err := common.Marshal(body)
				require.NoError(t, err)
				req := httptest.NewRequest("POST", path, bytes.NewReader(wire))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("New-Api-User", fmt.Sprint(root.Id))
				for _, cookie := range cookies {
					req.AddCookie(cookie)
				}
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, req)
				return response
			}
			document := `export const meta = {apiVersion: 1, key: "custom-upload", name: "Custom Upload", version: "1.0.0", models: ["custom-model"]}; export function buildSubmitRequest(){return {}} export function parseSubmitResponse(){return {}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {}}`
			if !javascript {
				data, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "private-custom-source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "custom-upload", Name: "Custom Upload", Version: "1.0.0", Models: []string{"custom-model"}}})
				require.NoError(t, err)
				document = string(data)
			}
			if kind == "bom-javascript" {
				normalized, err := taskplugin.LoadUpload(strings.NewReader(document), "")
				require.NoError(t, err)
				document = string(normalized.Document())
			}
			if bom {
				document = "\ufeff" + document
			}
			upload := map[string]any{"document": document}
			response := request("/api/task_plugins/validate", upload)
			require.Equal(t, 200, response.Code, response.Body.String())
			pin := fmt.Sprintf("%x", sha256.Sum256([]byte(document)))
			var validated struct {
				Data struct {
					Digest       string `json:"digest"`
					SourceDigest string `json:"source_sha256"`
					BOM          bool   `json:"bom_prefixed"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &validated))
			loaded, err := taskplugin.LoadUpload(strings.NewReader(document), pin)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(loaded.Document())), validated.Data.Digest)
			if kind == "javascript" {
				require.Equal(t, pin, validated.Data.SourceDigest)
			} else {
				require.Equal(t, pin, validated.Data.Digest)
			}
			require.Equal(t, bom, validated.Data.BOM)
			var count int64
			require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
			require.Zero(t, count, "validation does not install")
			upload["expected_sha256"] = ""
			require.Equal(t, 200, request("/api/task_plugins/validate", upload).Code)
			for _, invalid := range []any{"invalid", strings.Repeat("0", 64), nil, 7} {
				upload["expected_sha256"] = invalid
				require.Equal(t, 400, request("/api/task_plugins/validate", upload).Code)
			}
			delete(upload, "expected_sha256")
			upload["action"], upload["expected_revision"] = "install", "0"
			if javascript {
				require.Equal(t, 400, request("/api/task_plugins/mutate", upload).Code, "optional SHA does not bypass format-3 compatibility acknowledgement")
				upload["format3_nodes_ready"] = true
			}
			if bom {
				require.Equal(t, 400, request("/api/task_plugins/mutate", upload).Code, "older readers must acknowledge the BOM package")
				upload["bom_nodes_ready"] = true
				if javascript {
					delete(upload, "format3_nodes_ready")
					require.Equal(t, 400, request("/api/task_plugins/mutate", upload).Code, "BOM readiness alone cannot acknowledge executable packages")
					upload["format3_nodes_ready"] = true
				}
			}
			require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
			require.Zero(t, count, "missing readiness acknowledgements cannot install")
			require.Equal(t, 200, request("/api/task_plugins/mutate", upload).Code)
			upload["expected_revision"] = "1"
			upload["expected_sha256"] = strings.Repeat("0", 64)
			require.Equal(t, 400, request("/api/task_plugins/mutate", upload).Code)
			delete(upload, "expected_sha256")
			upload["document"] = strings.ReplaceAll(document, "Custom Upload", "Changed Source")
			changedResponse := request("/api/task_plugins/mutate", upload)
			require.Equal(t, 422, changedResponse.Code, "a content conflict is a known rejection, even without a supplied pin")
			require.Contains(t, changedResponse.Body.String(), "different content")
			var head model.TaskPluginCatalogHead
			require.NoError(t, db.First(&head, 1).Error)
			require.EqualValues(t, 1, head.Revision)
			require.NoError(t, db.Model(&model.TaskPluginVersion{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			if kind == "bom-json" {
				catalog, err := service.CurrentTaskPluginCatalog()
				require.NoError(t, err)
				next := []byte(strings.ReplaceAll(strings.TrimPrefix(document, "\ufeff"), "1.0.0", "2.0.0"))
				plain, err := taskplugin.LoadPackage(bytes.NewReader(next), fmt.Sprintf("%x", sha256.Sum256(next)))
				require.NoError(t, err)
				require.NoError(t, catalog.Install(t.Context(), plain))
				activate := map[string]any{"action": "activate", "key": "custom-upload", "version": "1.0.0", "expected_revision": "2"}
				require.Equal(t, 400, request("/api/task_plugins/mutate", activate).Code)
				assertBOM := func(wantRevision int64, active bool) {
					entries, revision, err := catalog.ManagementSnapshot(t.Context())
					require.NoError(t, err)
					require.Equal(t, wantRevision, revision)
					require.Len(t, entries, 2)
					for _, entry := range entries {
						if entry.BOMPrefixed {
							require.Equal(t, active, entry.Active)
							return
						}
					}
					t.Fatal("missing BOM package")
				}
				assertBOM(2, false)
				activate["bom_nodes_ready"] = true
				require.Equal(t, 200, request("/api/task_plugins/mutate", activate).Code)
				assertBOM(3, true)
			}
		})
	}
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

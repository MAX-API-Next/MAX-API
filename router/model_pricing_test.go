// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package router

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestModelPricingRoutesRequireRootAndReadsHaveNoWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.Channel{}, &model.Log{}))
	oldDB, oldLogDB, oldRedis, oldQPU := model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.QuotaPerUnit = 1000
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedis
		common.QuotaPerUnit = oldQPU
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	admin := model.User{Id: 59123001, Username: "pricing-fixture-admin", AffCode: "price-admin", Password: "synthetic-hash", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default"}
	root := model.User{Id: 59123002, Username: "pricing-fixture-root", AffCode: "price-root", Password: "synthetic-hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&root).Error)
	channel := model.Channel{Type: constant.ChannelTypeKling, Status: 1, Models: "kling-v1", Key: "synthetic-only", Name: "pricing fixture", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("pricing-fixture-cookie-secret"))))
	engine.GET("/test/pricing-login/:role", func(c *gin.Context) {
		user := admin
		if c.Param("role") == "root" {
			user = root
		}
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	SetApiRouter(engine)
	login := func(role string) []*http.Cookie {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test/pricing-login/"+role, nil))
		return recorder.Result().Cookies()
	}
	request := func(method, path string, payload any, cookies []*http.Cookie) *httptest.ResponseRecorder {
		data, err := common.Marshal(payload)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}
	for _, route := range []struct{ method, path string }{{http.MethodGet, "/api/option/model_pricing?model=kling-v1"}, {http.MethodPatch, "/api/option/model_pricing"}, {http.MethodPost, "/api/option/model_pricing/preview"}, {http.MethodPost, "/api/option/model_pricing/quote"}} {
		for _, cookies := range [][]*http.Cookie{nil, login("admin")} {
			response := request(route.method, route.path, map[string]any{}, cookies)
			require.Contains(t, response.Body.String(), `"success":false`)
			require.NotContains(t, response.Body.String(), `"committed":true`)
		}
	}
	cookies := login("root")
	writes := 0
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("readonly_price_create", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("readonly_price_update", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("readonly_price_delete", func(*gorm.DB) { writes++ }))
	response := request(http.MethodGet, "/api/option/model_pricing?model=kling-v1", nil, cookies)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Success bool                       `json:"success"`
		Data    model.ModelPricingSnapshot `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, response.Body.String())
	require.Len(t, envelope.Data.Entries, 1)
	entry := envelope.Data.Entries[0]
	draft := map[string]any{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}
	response = request(http.MethodPost, "/api/option/model_pricing/preview", map[string]any{"model_name": "kling-v1", "pricing": draft}, cookies)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = request(http.MethodPost, "/api/option/model_pricing/quote", map[string]any{"model_name": "kling-v1", "context_id": fmt.Sprintf("channel:%d", channel.Id), "expected_version": entry.Version, "expected_contract_digest": entry.ContractDigest, "expected_binding_version": entry.BindingVersion, "pricing": draft, "group": "default", "submit": map[string]any{"units": 1}, "actual": map[string]any{"units": 0.6}}, cookies)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"quota_after_group":60`)
	require.Zero(t, writes)
	require.NoError(t, db.Callback().Create().Remove("readonly_price_create"))
	require.NoError(t, db.Callback().Update().Remove("readonly_price_update"))
	require.NoError(t, db.Callback().Delete().Remove("readonly_price_delete"))
}

package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserAuthRejectsStaleSessionGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))
	if !db.Migrator().HasColumn(&model.User{}, "session_generation") {
		require.NoError(t, db.Exec("ALTER TABLE users ADD COLUMN session_generation INTEGER NOT NULL DEFAULT 0").Error)
	}
	user := model.User{
		Id:       73001,
		Username: "stale-session-user",
		Password: "hashed-password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)

	t.Cleanup(func() {
		model.DB = oldDB
		common.RedisEnabled = oldRedisEnabled
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("session-generation-test"))))
	engine.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		session.Set("session_generation", 0)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	engine.GET("/protected", UserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	engine.GET("/try-user", TryUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetInt("id")})
	})
	engine.GET("/token-or-user", TokenOrUserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	loginRecorder := httptest.NewRecorder()
	engine.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)

	currentRequest := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		currentRequest.AddCookie(sessionCookie)
	}
	currentRecorder := httptest.NewRecorder()
	engine.ServeHTTP(currentRecorder, currentRequest)
	require.Equal(t, http.StatusNoContent, currentRecorder.Code)

	require.NoError(t, db.Model(&model.User{}).
		Where("id = ?", user.Id).
		UpdateColumn("session_generation", 1).Error)

	protectedRequest := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		protectedRequest.AddCookie(sessionCookie)
	}
	protectedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(protectedRecorder, protectedRequest)

	require.Equal(t, http.StatusUnauthorized, protectedRecorder.Code)

	tryUserRequest := httptest.NewRequest(http.MethodGet, "/try-user", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		tryUserRequest.AddCookie(sessionCookie)
	}
	tryUserRecorder := httptest.NewRecorder()
	engine.ServeHTTP(tryUserRecorder, tryUserRequest)

	require.Equal(t, http.StatusOK, tryUserRecorder.Code)
	require.JSONEq(t, `{"id":0}`, tryUserRecorder.Body.String())

	tokenOrUserRequest := httptest.NewRequest(http.MethodGet, "/token-or-user", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		tokenOrUserRequest.AddCookie(sessionCookie)
	}
	tokenOrUserRecorder := httptest.NewRecorder()
	engine.ServeHTTP(tokenOrUserRecorder, tokenOrUserRequest)

	require.Equal(t, http.StatusUnauthorized, tokenOrUserRecorder.Code)
}

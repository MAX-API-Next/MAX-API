package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSecureVerificationRejectsMarkerFromAnotherUser(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verification-user-binding-test"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set("secure_verified_user_id", 1001)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET(
		"/sensitive",
		func(c *gin.Context) {
			c.Set("id", 2002)
			c.Next()
		},
		SecureVerificationRequired(),
		func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		},
	)

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sensitive", nil)
	for _, sessionCookie := range seedRecorder.Result().Cookies() {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"VERIFICATION_INVALID"`)
}

func TestPasswordVerificationIsRestrictedToMatchingScope(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verification-scope-test"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationUserSessionKey, 1001)
		session.Set(secureVerificationMethodSessionKey, secureVerificationMethodPassword)
		session.Set(secureVerificationScopeSessionKey, "access_token")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	setUser := func(c *gin.Context) {
		c.Set("id", 1001)
		c.Next()
	}
	router.GET("/token", setUser, SecureVerificationRequired("access_token"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/other-sensitive", setUser, SecureVerificationRequired(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.DELETE("/account", setUser, SecureVerificationRequired("account_delete"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	perform := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, nil)
		for _, sessionCookie := range seedRecorder.Result().Cookies() {
			request.AddCookie(sessionCookie)
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, perform(http.MethodGet, "/token").Code)
	otherRecorder := perform(http.MethodGet, "/other-sensitive")
	require.Equal(t, http.StatusForbidden, otherRecorder.Code)
	require.Contains(t, otherRecorder.Body.String(), `"code":"VERIFICATION_REQUIRED"`)
	deleteRecorder := perform(http.MethodDelete, "/account")
	require.Equal(t, http.StatusForbidden, deleteRecorder.Code)
	require.Contains(t, deleteRecorder.Body.String(), `"code":"VERIFICATION_REQUIRED"`)
}

func TestNonPasswordVerificationIsRestrictedToMatchingScope(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verification-passkey-scope-test"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationUserSessionKey, 1001)
		session.Set(secureVerificationMethodSessionKey, "passkey")
		session.Set(secureVerificationScopeSessionKey, "access_token")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	setUser := func(c *gin.Context) {
		c.Set("id", 1001)
		c.Next()
	}
	router.GET("/token", setUser, SecureVerificationRequired("access_token"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.DELETE("/account", setUser, SecureVerificationRequired("account_delete"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	perform := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, nil)
		for _, sessionCookie := range seedRecorder.Result().Cookies() {
			request.AddCookie(sessionCookie)
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, perform(http.MethodGet, "/token").Code)
	deleteRecorder := perform(http.MethodDelete, "/account")
	require.Equal(t, http.StatusForbidden, deleteRecorder.Code)
	require.Contains(t, deleteRecorder.Body.String(), `"code":"VERIFICATION_REQUIRED"`)
}

func TestPasskeyDeletionRequiresCredentialVerificationScope(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verification-passkey-delete-scope-test"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationUserSessionKey, 1001)
		session.Set(secureVerificationMethodSessionKey, secureVerificationMethodPasskey)
		session.Set(secureVerificationScopeSessionKey, "access_token")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.DELETE(
		"/passkey",
		func(c *gin.Context) {
			c.Set("id", 1001)
			c.Next()
		},
		SecureVerificationRequired("credentials"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/passkey", nil)
	for _, sessionCookie := range seedRecorder.Result().Cookies() {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"VERIFICATION_REQUIRED"`)
}

func TestOAuthVerificationIsRestrictedToCredentialScope(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verification-oauth-scope-test"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationUserSessionKey, 1001)
		session.Set(secureVerificationMethodSessionKey, secureVerificationMethodOAuth)
		session.Set(secureVerificationScopeSessionKey, "credentials")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	setUser := func(c *gin.Context) {
		c.Set("id", 1001)
		c.Next()
	}
	router.POST("/credentials", setUser, SecureVerificationRequired("credentials"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.POST("/api-token", setUser, SecureVerificationRequired("api_token"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.POST("/unscoped", setUser, SecureVerificationRequired(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	perform := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		for _, sessionCookie := range seedRecorder.Result().Cookies() {
			request.AddCookie(sessionCookie)
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, perform("/credentials").Code)
	apiTokenRecorder := perform("/api-token")
	require.Equal(t, http.StatusForbidden, apiTokenRecorder.Code)
	require.Contains(t, apiTokenRecorder.Body.String(), `"code":"VERIFICATION_REQUIRED"`)
	unscopedRecorder := perform("/unscoped")
	require.Equal(t, http.StatusForbidden, unscopedRecorder.Code)
	require.Contains(t, unscopedRecorder.Body.String(), `"code":"VERIFICATION_REQUIRED"`)
}

func TestSecureVerificationRejectsLoginMethodMarker(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verification-login-marker-test"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationUserSessionKey, 1001)
		session.Set(secureVerificationMethodSessionKey, "login:password")
		session.Set(secureVerificationScopeSessionKey, "credentials")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.POST(
		"/credentials",
		func(c *gin.Context) {
			c.Set("id", 1001)
			c.Next()
		},
		SecureVerificationRequired("credentials"),
		func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		},
	)

	seedRecorder := httptest.NewRecorder()
	router.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/credentials", nil)
	for _, sessionCookie := range seedRecorder.Result().Cookies() {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"VERIFICATION_INVALID"`)
}

func TestUserAuthRejectsStaleSessionGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))
	if !db.Migrator().HasColumn(&model.User{}, "session_generation") {
		require.NoError(t, db.Migrator().AddColumn(&model.User{}, "SessionGeneration"))
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
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
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

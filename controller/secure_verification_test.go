package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUniversalVerifyPasswordCreatesUserBoundSession(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.PasskeyCredential{},
		&model.Log{},
	))

	const password = "Password123"
	passwordHash, err := common.Password2Hash(password)
	require.NoError(t, err)
	user := model.User{
		Id:          1001,
		Username:    "password-verification-user",
		Password:    passwordHash,
		DisplayName: "Password Verification User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("password-verification-test"))))
	router.POST("/verify", func(c *gin.Context) {
		c.Set("id", user.Id)
		UniversalVerify(c)
	})
	router.GET("/verification-session", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"verified_at":      session.Get(SecureVerificationSessionKey),
			"verified_method":  session.Get(secureVerificationMethodSessionKey),
			"verified_user_id": session.Get(secureVerificationUserSessionKey),
			"verified_scope":   session.Get(secureVerificationScopeSessionKey),
		})
	})

	payload := []byte(`{"method":"password","password":"Password123","scope":"access_token"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	inspectRecorder := httptest.NewRecorder()
	inspectRequest := httptest.NewRequest(http.MethodGet, "/verification-session", nil)
	for _, sessionCookie := range recorder.Result().Cookies() {
		inspectRequest.AddCookie(sessionCookie)
	}
	router.ServeHTTP(inspectRecorder, inspectRequest)

	require.Equal(t, http.StatusOK, inspectRecorder.Code)
	require.Contains(t, inspectRecorder.Body.String(), `"verified_method":"password"`)
	require.Contains(t, inspectRecorder.Body.String(), `"verified_user_id":1001`)
	require.Contains(t, inspectRecorder.Body.String(), `"verified_scope":"access_token"`)
}

func TestUniversalVerifyPasswordRequiresAccessTokenScope(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.PasskeyCredential{},
	))

	passwordHash, err := common.Password2Hash("Password123")
	require.NoError(t, err)
	user := model.User{
		Id:       1004,
		Username: "unscoped-password-user",
		Password: passwordHash,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/verify",
		bytes.NewReader([]byte(`{"method":"password","password":"Password123"}`)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	UniversalVerify(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.Contains(t, recorder.Body.String(), "密码验证不可用")
}

func TestSecureVerificationScopeAllowlist(t *testing.T) {
	require.True(t, isSupportedSecureVerificationScope(""))
	require.True(t, isSupportedSecureVerificationScope(secureVerificationScopeAccessToken))
	require.True(t, isSupportedSecureVerificationScope(secureVerificationScopeAccountDelete))
	require.True(t, isSupportedSecureVerificationScope("credentials"))
	require.True(t, isSupportedSecureVerificationScope("api_token"))
	require.False(t, isSupportedSecureVerificationScope("admin_delete"))
	require.True(t, passwordVerificationAllowed(secureVerificationScopeAccountDelete))
	require.True(t, passwordVerificationAllowed("credentials"))
	require.True(t, passwordVerificationAllowed("api_token"))
	require.False(t, passwordVerificationAllowed(""))
}

func TestVerificationMethodsUsePasswordOnlyAsFallback(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.PasskeyCredential{},
	))

	passwordHash, err := common.Password2Hash("Password123")
	require.NoError(t, err)
	user := model.User{
		Id:       1002,
		Username: "password-fallback-user",
		Password: passwordHash,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)

	methods, err := loadSecureVerificationMethods(&user, false)
	require.NoError(t, err)
	require.False(t, methods.hasPassword)

	methods, err = loadSecureVerificationMethods(&user, true)
	require.NoError(t, err)
	require.True(t, methods.hasPassword)
	require.False(t, methods.has2FA)
	require.False(t, methods.hasPasskey)

	require.NoError(t, db.Create(&model.TwoFA{
		UserId:    user.Id,
		Secret:    "test-secret",
		IsEnabled: true,
	}).Error)

	methods, err = loadSecureVerificationMethods(&user, true)
	require.NoError(t, err)
	require.True(t, methods.has2FA)
	require.False(t, methods.hasPassword)
}

func TestSetupLoginClearsPreviousSecureVerification(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	passwordHash, err := common.Password2Hash("Password123")
	require.NoError(t, err)
	user := model.User{
		Id:          1003,
		Username:    "new-session-user",
		Password:    passwordHash,
		DisplayName: "New Session User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("login-clears-verification-test"))))
	router.GET("/api/user/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, int64(123))
		session.Set(secureVerificationMethodSessionKey, secureVerificationMethod2FA)
		session.Set(secureVerificationUserSessionKey, 999)
		session.Set(secureVerificationScopeSessionKey, secureVerificationScopeAccessToken)
		session.Set(PasskeyReadySessionKey, int64(123))
		setupLogin(&user, c)
	})
	router.GET("/verification-session", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"verified_at":      session.Get(SecureVerificationSessionKey),
			"verified_method":  session.Get(secureVerificationMethodSessionKey),
			"verified_user_id": session.Get(secureVerificationUserSessionKey),
			"verified_scope":   session.Get(secureVerificationScopeSessionKey),
			"passkey_ready_at": session.Get(PasskeyReadySessionKey),
		})
	})

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/api/user/login", nil))
	require.Equal(t, http.StatusOK, loginRecorder.Code)

	inspectRecorder := httptest.NewRecorder()
	inspectRequest := httptest.NewRequest(http.MethodGet, "/verification-session", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		inspectRequest.AddCookie(sessionCookie)
	}
	router.ServeHTTP(inspectRecorder, inspectRequest)

	require.Equal(t, http.StatusOK, inspectRecorder.Code)
	require.JSONEq(t, `{
		"verified_at": null,
		"verified_method": null,
		"verified_user_id": null,
		"verified_scope": null,
		"passkey_ready_at": null
	}`, inspectRecorder.Body.String())
}

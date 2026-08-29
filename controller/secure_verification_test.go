package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestOAuthLoginCreatesCredentialScopedSecureVerification(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	user := model.User{
		Id:          1004,
		Username:    "oauth-reauth-user",
		DisplayName: "OAuth Reauth User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-credential-verification-test"))))
	router.GET("/api/oauth/:provider", func(c *gin.Context) {
		setupLogin(&user, c)
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

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/api/oauth/github", nil))
	require.Equal(t, http.StatusOK, loginRecorder.Code)

	inspectRecorder := httptest.NewRecorder()
	inspectRequest := httptest.NewRequest(http.MethodGet, "/verification-session", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		inspectRequest.AddCookie(sessionCookie)
	}
	router.ServeHTTP(inspectRecorder, inspectRequest)

	require.Equal(t, http.StatusOK, inspectRecorder.Code)
	var verificationSession struct {
		VerifiedAt     int64  `json:"verified_at"`
		VerifiedMethod string `json:"verified_method"`
		VerifiedUserID int    `json:"verified_user_id"`
		VerifiedScope  string `json:"verified_scope"`
	}
	require.NoError(t, common.Unmarshal(inspectRecorder.Body.Bytes(), &verificationSession))
	require.WithinDuration(t, time.Now(), time.Unix(verificationSession.VerifiedAt, 0), 2*time.Second)
	require.Equal(t, secureVerificationMethodOAuth, verificationSession.VerifiedMethod)
	require.Equal(t, user.Id, verificationSession.VerifiedUserID)
	require.Equal(t, secureVerificationScopeCredentials, verificationSession.VerifiedScope)
}

func TestResetPasswordReportsRevokedApiTokens(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	user := model.User{
		Id:       9910,
		Username: "password-recovery-notice-user",
		Email:    "password-recovery-notice@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.Token{
		Id: 99101, UserId: user.Id, Name: "recovery-token", Key: "recovery-token-key", Status: common.TokenStatusEnabled,
	}).Error)

	code := "password-recovery-notice-code"
	common.RegisterVerificationCodeWithKey(user.Email, code, common.PasswordResetPurpose)
	t.Cleanup(func() { common.DeleteKey(user.Email, common.PasswordResetPurpose) })
	payload, err := common.Marshal(PasswordResetRequest{Email: user.Email, Token: code})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ResetPassword(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success          bool   `json:"success"`
		Message          string `json:"message"`
		Data             string `json:"data"`
		ApiTokensRevoked bool   `json:"api_tokens_revoked"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotEmpty(t, response.Data)
	require.True(t, response.ApiTokensRevoked)
	require.Contains(t, response.Message, "all existing API tokens were revoked")

	var token model.Token
	require.NoError(t, db.First(&token, 99101).Error)
	require.Equal(t, common.TokenStatusDisabled, token.Status)
}

func TestPreserveCurrentSessionAfterSecurityChangeUpdatesGenerationAndClearsStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("preserve-current-session-test"))))
	engine.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 9911)
		session.Set("session_generation", int64(1))
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationMethodSessionKey, "password")
		session.Set(secureVerificationUserSessionKey, 9911)
		session.Set(secureVerificationScopeSessionKey, secureVerificationScopeCredentials)
		session.Set(PasskeyReadySessionKey, time.Now().Unix())
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	engine.POST("/preserve", func(c *gin.Context) {
		require.NoError(t, preserveCurrentSessionAfterSecurityChange(c, 9911, 2))
		c.Status(http.StatusNoContent)
	})
	engine.GET("/inspect", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"id":                 session.Get("id"),
			"generation":         session.Get("session_generation"),
			"secure_verified_at": session.Get(SecureVerificationSessionKey),
			"secure_method":      session.Get(secureVerificationMethodSessionKey),
			"secure_user":        session.Get(secureVerificationUserSessionKey),
			"secure_scope":       session.Get(secureVerificationScopeSessionKey),
			"passkey_ready_at":   session.Get(PasskeyReadySessionKey),
		})
	})

	seedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusNoContent, seedRecorder.Code)

	preserveRequest := httptest.NewRequest(http.MethodPost, "/preserve", nil)
	for _, sessionCookie := range seedRecorder.Result().Cookies() {
		preserveRequest.AddCookie(sessionCookie)
	}
	preserveRecorder := httptest.NewRecorder()
	engine.ServeHTTP(preserveRecorder, preserveRequest)
	require.Equal(t, http.StatusNoContent, preserveRecorder.Code)

	inspectRequest := httptest.NewRequest(http.MethodGet, "/inspect", nil)
	for _, sessionCookie := range preserveRecorder.Result().Cookies() {
		inspectRequest.AddCookie(sessionCookie)
	}
	inspectRecorder := httptest.NewRecorder()
	engine.ServeHTTP(inspectRecorder, inspectRequest)
	require.Equal(t, http.StatusOK, inspectRecorder.Code)
	require.JSONEq(t, `{"generation":2,"id":9911,"secure_verified_at":null,"secure_method":null,"secure_user":null,"secure_scope":null,"passkey_ready_at":null}`, inspectRecorder.Body.String())
}

func TestRevokeOtherSessionsReturnsCommittedSuccessWhenSessionPreservationFails(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	user := model.User{Id: 9912, Username: "revoke-session-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)

	recorder := performUserSecurityChangeWithFailingSession(t, user.Id, `{}`, RevokeOtherSessions)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.EqualValues(t, 1, user.SessionGeneration)
}

func TestPasswordChangeReturnsCommittedSuccessWhenSessionPreservationFails(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	hash, err := common.Password2Hash("old-password-123")
	require.NoError(t, err)
	user := model.User{
		Id: 9913, Username: "password-session-user", Password: hash,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := performUserSecurityChangeWithFailingSession(
		t,
		user.Id,
		`{"original_password":"old-password-123","password":"new-password-123"}`,
		UpdateSelf,
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.True(t, common.ValidatePasswordAndHash("new-password-123", user.Password))
	require.EqualValues(t, 1, user.SessionGeneration)
}

func performUserSecurityChangeWithFailingSession(
	t *testing.T,
	userID int,
	body string,
	handler gin.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(sessions.DefaultKey, &failingSaveSession{values: map[interface{}]interface{}{}})
		c.Next()
	})
	router.POST("/security-change", func(c *gin.Context) {
		c.Set("id", userID)
		handler(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/security-change", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

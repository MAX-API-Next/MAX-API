package controller

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	appi18n "github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
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
	require.True(t, isSupportedSecureVerificationScope(model.SecureVerificationScopeAccessToken))
	require.True(t, isSupportedSecureVerificationScope(model.SecureVerificationScopeAccountDelete))
	require.True(t, isSupportedSecureVerificationScope(model.SecureVerificationScopeCredentials))
	require.True(t, isSupportedSecureVerificationScope(model.SecureVerificationScopeAPIToken))
	require.True(t, isSupportedSecureVerificationScope(model.SecureVerificationScopePasskeyRegister))
	require.False(t, isSupportedSecureVerificationScope(model.SecureVerificationScopeOAuthReauthentication))
	require.False(t, isSupportedSecureVerificationScope("admin_delete"))
	require.True(t, passwordVerificationAllowed(model.SecureVerificationScopeAccountDelete))
	require.True(t, passwordVerificationAllowed(model.SecureVerificationScopeCredentials))
	require.True(t, passwordVerificationAllowed(model.SecureVerificationScopeAPIToken))
	require.True(t, passwordVerificationAllowed(model.SecureVerificationScopePasskeyRegister))
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
		session.Set(secureVerificationMethodSessionKey, model.SecureVerificationMethod2FA)
		session.Set(secureVerificationUserSessionKey, 999)
		session.Set(secureVerificationScopeSessionKey, model.SecureVerificationScopeAccessToken)
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

func TestOAuthLoginCreatesNarrowReauthenticationScope(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.TwoFA{}, &model.PasskeyCredential{}))

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
	require.Equal(t, model.SecureVerificationMethodOAuth, verificationSession.VerifiedMethod)
	require.Equal(t, user.Id, verificationSession.VerifiedUserID)
	require.Equal(t, model.SecureVerificationScopeOAuthReauthentication, verificationSession.VerifiedScope)
}

func TestOAuthLoginDoesNotGrantReauthenticationWhenCredentialsExist(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.TwoFA{}, &model.PasskeyCredential{}))

	user := model.User{
		Id:          1006,
		Username:    "oauth-existing-credential-user",
		Password:    "existing-password-hash",
		DisplayName: "OAuth Existing Credential User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-existing-credential-test"))))
	router.GET("/api/oauth/:provider", func(c *gin.Context) {
		setupLogin(&user, c)
	})
	router.GET("/verification-session", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"verified_at":     session.Get(SecureVerificationSessionKey),
			"verified_method": session.Get(secureVerificationMethodSessionKey),
			"verified_scope":  session.Get(secureVerificationScopeSessionKey),
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
	require.JSONEq(t, `{"verified_at":null,"verified_method":null,"verified_scope":null}`, inspectRecorder.Body.String())
}

func TestTelegramLoginDoesNotCreateSecureVerificationGrant(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	user := model.User{
		Id: 1005, Username: "telegram-no-step-up-user", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("telegram-no-step-up-test"))))
	router.GET("/api/oauth/telegram/login", func(c *gin.Context) { setupLogin(&user, c) })
	router.GET("/verification-session", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"verified_at":     session.Get(SecureVerificationSessionKey),
			"verified_method": session.Get(secureVerificationMethodSessionKey),
			"verified_scope":  session.Get(secureVerificationScopeSessionKey),
		})
	})

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/login", nil))
	require.Equal(t, http.StatusOK, loginRecorder.Code)
	inspectRecorder := httptest.NewRecorder()
	inspectRequest := httptest.NewRequest(http.MethodGet, "/verification-session", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		inspectRequest.AddCookie(sessionCookie)
	}
	router.ServeHTTP(inspectRecorder, inspectRequest)
	require.JSONEq(t, `{"verified_at":null,"verified_method":null,"verified_scope":null}`, inspectRecorder.Body.String())
}

func TestResetPasswordReportsRevokedApiTokens(t *testing.T) {
	require.NoError(t, appi18n.Init())
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
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

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
	require.Contains(t, response.Message, "所有现有 API 令牌已被撤销")
	require.NotContains(t, response.Message, "如有需要")

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
		session.Set(secureVerificationScopeSessionKey, model.SecureVerificationScopeCredentials)
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

func TestPasswordChangePreservesCommittedSessionGeneration(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	hash, err := common.Password2Hash("old-password-123")
	require.NoError(t, err)
	user := model.User{
		Id: 9914, Username: "password-generation-session-user", Password: hash,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, SessionGeneration: 4,
	}
	require.NoError(t, db.Create(&user).Error)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("password-generation-session-test"))))
	engine.POST("/api/user/self", func(c *gin.Context) {
		c.Set("id", user.Id)
		UpdateSelf(c)
	})
	engine.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"id":         session.Get("id"),
			"generation": session.Get("session_generation"),
		})
	})

	updateRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/user/self",
		bytes.NewBufferString(`{"original_password":"old-password-123","password":"new-password-123"}`),
	)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRecorder := httptest.NewRecorder()
	engine.ServeHTTP(updateRecorder, updateRequest)
	require.Equal(t, http.StatusOK, updateRecorder.Code)

	inspectRequest := httptest.NewRequest(http.MethodGet, "/session", nil)
	for _, sessionCookie := range updateRecorder.Result().Cookies() {
		inspectRequest.AddCookie(sessionCookie)
	}
	inspectRecorder := httptest.NewRecorder()
	engine.ServeHTTP(inspectRecorder, inspectRequest)
	require.Equal(t, http.StatusOK, inspectRecorder.Code)
	require.JSONEq(t, `{"generation":5,"id":9914}`, inspectRecorder.Body.String())

	require.NoError(t, db.First(&user, user.Id).Error)
	require.EqualValues(t, 5, user.SessionGeneration)
	require.True(t, common.ValidatePasswordAndHash("new-password-123", user.Password))
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

type failingTaskBillingSettler struct {
	err error
}

func (s *failingTaskBillingSettler) Settle(int) error         { return s.err }
func (s *failingTaskBillingSettler) Refund(*gin.Context)      {}
func (s *failingTaskBillingSettler) NeedsRefund() bool        { return true }
func (s *failingTaskBillingSettler) GetPreConsumedQuota() int { return 10 }
func (s *failingTaskBillingSettler) Reserve(int) error        { return nil }

type successfulTaskBillingSettler struct {
	settleCalls int
	refundCalls int
}

func (s *successfulTaskBillingSettler) Settle(int) error {
	s.settleCalls++
	return nil
}
func (s *successfulTaskBillingSettler) Refund(*gin.Context)      { s.refundCalls++ }
func (s *successfulTaskBillingSettler) NeedsRefund() bool        { return true }
func (s *successfulTaskBillingSettler) GetPreConsumedQuota() int { return 10 }
func (s *successfulTaskBillingSettler) Reserve(int) error        { return nil }

type recordingSelectedGroupBillingSettler struct {
	preConsumed int
	reserves    []int
}

func (s *recordingSelectedGroupBillingSettler) Settle(int) error    { return nil }
func (s *recordingSelectedGroupBillingSettler) Refund(*gin.Context) {}
func (s *recordingSelectedGroupBillingSettler) NeedsRefund() bool   { return true }
func (s *recordingSelectedGroupBillingSettler) GetPreConsumedQuota() int {
	return s.preConsumed
}
func (s *recordingSelectedGroupBillingSettler) Reserve(targetQuota int) error {
	s.reserves = append(s.reserves, targetQuota)
	if targetQuota > s.preConsumed {
		s.preConsumed = targetQuota
	}
	return nil
}

func TestFinalizeTaskSubmissionDoesNotWriteSuccessAfterSettlementFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	settlementErr := errors.New("user quota is not enough")
	info := &relaycommon.RelayInfo{
		UserId:  71,
		TokenId: 72,
		Billing: &failingTaskBillingSettler{err: settlementErr},
	}
	result := &relay.TaskSubmitResult{
		Quota: 20,
		Task:  &model.Task{TaskID: "task-settlement-review"},
	}
	writeCount := 0

	taskErr := finalizeTaskSubmission(c, info, result, "", func() error {
		writeCount++
		return nil
	})

	require.NotNil(t, taskErr)
	assert.Equal(t, constant.MjBillingSettlementPending, taskErr.Code)
	assert.Equal(t, http.StatusConflict, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, map[string]string{"task_id": "task-settlement-review"}, taskErr.Data)
	assert.Zero(t, writeCount)
}

func TestPendingTaskSettlementDoesNotMarkAcceptedTaskAsManualFailure(t *testing.T) {
	taskErr := &dto.TaskError{Code: constant.MjBillingSettlementPending}

	assert.False(t, shouldMarkTaskSubmitNeedsReview(taskErr, true, true))
	assert.True(t, shouldMarkTaskSubmitNeedsReview(taskErr, false, true))
	assert.True(t, shouldMarkTaskSubmitNeedsReview(taskErr, true, false))
	assert.True(t, shouldMarkTaskSubmitNeedsReview(&dto.TaskError{Code: "persist_task_failed"}, true, true))
}

func TestFinalizeTaskSubmissionReportsCommittedResponseWriteFailure(t *testing.T) {
	setupUserSettingControllerTestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	settler := &successfulTaskBillingSettler{}
	info := &relaycommon.RelayInfo{
		UserId:        81,
		TokenId:       82,
		Billing:       settler,
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "submit"},
	}
	result := &relay.TaskSubmitResult{
		Quota: 20,
		Task:  &model.Task{TaskID: "task-response-write-failed"},
	}

	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = oldLogConsumeEnabled })

	writeErr := errors.New("client connection closed")
	taskErr := finalizeTaskSubmission(c, info, result, "", func() error {
		return writeErr
	})

	require.NotNil(t, taskErr)
	assert.Equal(t, "write_task_response_failed", taskErr.Code)
	assert.Equal(t, http.StatusConflict, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.ErrorIs(t, taskErr.Error, writeErr)
	assert.Equal(t, map[string]string{"task_id": "task-response-write-failed"}, taskErr.Data)
	assert.False(t, shouldRetryTaskRelay(c, 1, taskErr, 1))
	assert.False(t, shouldMarkTaskSubmitNeedsReview(taskErr, true, true))
	assert.Equal(t, 1, settler.settleCalls)
	assert.Zero(t, settler.refundCalls)

	respondTaskError(c, taskErr)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"write_task_response_failed"`)
	assert.Contains(t, recorder.Body.String(), `"task_id":"task-response-write-failed"`)
}

func TestMidjourneyRelayErrorStatusCodeReportsSettlementPendingAsConflict(t *testing.T) {
	assert.Equal(t, http.StatusConflict, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{
		Code:        constant.MjRequestError,
		Description: constant.MjBillingSettlementPending,
	}))
	assert.Equal(t, http.StatusTooManyRequests, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{Code: 30}))
	assert.Equal(t, http.StatusBadRequest, midjourneyRelayErrorStatusCode(&dto.MidjourneyResponse{Code: constant.MjRequestError}))
}

func TestPrepareAlphaSearchPreConsumedQuotaAppliesFloorAfterSurcharge(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	common.QuotaPerUnit = 1_000
	common.PreConsumedQuota = 500

	toolPrices := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	originalPrices := make(map[string]float64, len(toolPrices.Prices))
	for key, value := range toolPrices.Prices {
		originalPrices[key] = value
	}
	toolPrices.Prices = map[string]float64{"web_search_preview": 10}
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		toolPrices.Prices = originalPrices
		operation_setting.RebuildToolPriceIndex()
	})

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "alpha-floor-test",
	}

	for _, test := range []struct {
		name         string
		baseQuota    int
		freeModel    bool
		expected     int
		expectedFree bool
	}{
		{name: "total remains below floor", baseQuota: 200, expected: 500},
		{name: "surcharge crosses floor", baseQuota: 495, expected: 505},
		{name: "tool surcharge makes free model billable", freeModel: true, expected: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			priceData, err := prepareAlphaSearchPreConsumedQuota(types.PriceData{
				FreeModel:         test.freeModel,
				QuotaToPreConsume: test.baseQuota,
				GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 1},
			}, info)

			require.NoError(t, err)
			require.Equal(t, test.expected, priceData.QuotaToPreConsume)
			require.Equal(t, test.expectedFree, priceData.FreeModel)
		})
	}
}

func TestPrepareBillingForSelectedGroupRepricesAlphaSearchCrossGroupRetry(t *testing.T) {
	const modelName = "alpha-cross-group-retry"
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPreConsumedQuota := common.PreConsumedQuota
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	common.QuotaPerUnit = 1_000
	common.PreConsumedQuota = 2_500
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"alpha-cross-group-retry":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"initial":1,"retry":3}`))

	toolPrices := config.GlobalConfig.Get("tool_price_setting").(*operation_setting.ToolPriceSetting)
	originalPrices := make(map[string]float64, len(toolPrices.Prices))
	for key, value := range toolPrices.Prices {
		originalPrices[key] = value
	}
	toolPrices.Prices = map[string]float64{"web_search_preview": 10}
	operation_setting.RebuildToolPriceIndex()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.PreConsumedQuota = originalPreConsumedQuota
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		toolPrices.Prices = originalPrices
		operation_setting.RebuildToolPriceIndex()
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "retry")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "retry")
	billing := &recordingSelectedGroupBillingSettler{preConsumed: 2_500}
	info := &relaycommon.RelayInfo{
		RelayMode:             relayconstant.RelayModeAlphaSearch,
		OriginModelName:       modelName,
		UsingGroup:            "initial",
		Billing:               billing,
		FinalPreConsumedQuota: 2_500,
	}

	require.Nil(t, prepareBillingForSelectedGroup(c, info, 1_000, &types.TokenCountMeta{}))

	require.Equal(t, "retry", info.UsingGroup)
	require.Equal(t, 3.0, info.PriceData.GroupRatioInfo.GroupRatio)
	require.Equal(t, 3_030, info.PriceData.QuotaToPreConsume)
	require.Equal(t, []int{3_030}, billing.reserves)
	require.Equal(t, 3_030, info.FinalPreConsumedQuota)
}

func TestPrepareBillingForSelectedGroupClearsFallbackQuotaForFreeRetry(t *testing.T) {
	const modelName = "free-cross-group-retry"
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	quotaSetting := operation_setting.GetQuotaSetting()
	originalFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"free-cross-group-retry":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"initial":1,"free":0}`))
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = originalFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "free")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "free")
	billing := &recordingSelectedGroupBillingSettler{preConsumed: 2_500}
	info := &relaycommon.RelayInfo{
		OriginModelName:       modelName,
		UsingGroup:            "initial",
		Billing:               billing,
		FinalPreConsumedQuota: 2_500,
	}

	require.Nil(t, prepareBillingForSelectedGroup(c, info, 1_000, &types.TokenCountMeta{}))

	require.Equal(t, "free", info.UsingGroup)
	require.True(t, info.PriceData.FreeModel)
	require.Zero(t, info.FinalPreConsumedQuota)
	require.Same(t, billing, info.Billing)
	require.Equal(t, 2_500, billing.GetPreConsumedQuota())
	require.Empty(t, billing.reserves)
}

func signedTelegramParams(t *testing.T, token string, authDate time.Time) url.Values {
	t.Helper()
	params := url.Values{
		"id":         {"123456"},
		"first_name": {"Security"},
		"auth_date":  {strconv.FormatInt(authDate.Unix(), 10)},
	}
	signTelegramParams(t, token, params)
	return params
}

func signTelegramParams(t *testing.T, token string, params url.Values) {
	t.Helper()
	params.Del("hash")
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+params.Get(key))
	}
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	_, err := mac.Write([]byte(strings.Join(lines, "\n")))
	require.NoError(t, err)
	params.Set("hash", hex.EncodeToString(mac.Sum(nil)))
}

func TestTelegramAuthorizationRejectsStaleSignedPayload(t *testing.T) {
	const token = "123456:telegram-test-token"
	params := signedTelegramParams(t, token, time.Now().Add(-time.Hour))
	require.False(t, checkTelegramAuthorization(params, token))
}

func TestTelegramAuthorizationAcceptsFreshSignedPayload(t *testing.T) {
	const token = "123456:telegram-test-token"
	params := signedTelegramParams(t, token, time.Now().Add(-time.Minute))
	require.True(t, checkTelegramAuthorization(params, token))
}

func TestTelegramAuthorizationIgnoresLocalBindState(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(-time.Minute))
	params.Set("state", "local-bind-state")

	require.True(t, checkTelegramAuthorizationAt(params, token, now))
}

func TestTelegramAuthorizationRejectsTamperedPayload(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(-time.Minute))
	params.Set("id", "654321")

	require.False(t, checkTelegramAuthorizationAt(params, token, now))
}

func TestTelegramAuthorizationRejectsDuplicateParameterValues(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(-time.Minute))
	params.Add("first_name", "Duplicate")

	require.False(t, checkTelegramAuthorizationAt(params, token, now))
}

func TestTelegramAuthorizationRejectsForeignBotToken(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(-time.Minute))

	require.False(t, checkTelegramAuthorizationAt(
		params,
		"987654:another-telegram-test-token",
		now,
	))
}

func TestTelegramAuthPayloadPreservesUnknownSignedFields(t *testing.T) {
	const token = "123456:telegram-forward-compatible-test"
	now := time.Unix(1_800_000_000, 0)
	params := url.Values{
		"id":                {"123456"},
		"first_name":        {"Security"},
		"auth_date":         {strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)},
		"future_auth_field": {"future-value"},
	}
	signTelegramParams(t, token, params)

	body, err := common.Marshal(map[string]any{
		"id":                int64(123456),
		"first_name":        "Security",
		"auth_date":         now.Add(-time.Minute).Unix(),
		"future_auth_field": "future-value",
		"hash":              params.Get("hash"),
		"state":             "local-bind-state",
	})
	require.NoError(t, err)

	var payload telegramAuthPayload
	require.NoError(t, common.DecodeJson(bytes.NewReader(body), &payload))
	values := payload.values()
	require.Equal(t, "future-value", values.Get("future_auth_field"))
	require.NotContains(t, values, "state")
	require.Equal(t, "local-bind-state", payload.State)
	require.True(t, checkTelegramAuthorizationAt(values, token, now))
}

func TestTelegramAuthorizationRejectsAuthDateBeyondClockSkew(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(telegramClockSkew+time.Second))

	require.False(t, checkTelegramAuthorizationAt(params, token, now))
}

func TestGenerateTelegramBindStateRejectsNonPositiveUserID(t *testing.T) {
	setupControllerAuthFlowDB(t)
	require.NoError(t, appi18n.Init())
	originalEnabled := common.TelegramOAuthEnabled
	common.TelegramOAuthEnabled = true
	t.Cleanup(func() { common.TelegramOAuthEnabled = originalEnabled })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/oauth/telegram/bind/state", nil)

	GenerateTelegramBindState(c)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	var count int64
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTelegramLoginRequiresBrowserBoundStateAndConsumesItOnce(t *testing.T) {
	setupControllerAuthFlowDB(t)
	require.NoError(t, appi18n.Init())
	gin.SetMode(gin.TestMode)

	const token = "123456:telegram-login-state-test"
	originalEnabled := common.TelegramOAuthEnabled
	originalToken := common.TelegramBotToken
	common.TelegramOAuthEnabled = true
	common.TelegramBotToken = token
	t.Cleanup(func() {
		common.TelegramOAuthEnabled = originalEnabled
		common.TelegramBotToken = originalToken
	})

	user := model.User{
		Id: 99001, Username: "telegram-login-user", TelegramId: "123456",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("telegram-login-browser-state"))))
	router.POST("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/telegram/login", TelegramLogin)

	stateRecorder := httptest.NewRecorder()
	stateRequest := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"telegram","intent":"login"}`))
	stateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(stateRecorder, stateRequest)
	require.Equal(t, http.StatusOK, stateRecorder.Code)
	var stateResponse struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(stateRecorder.Body.Bytes(), &stateResponse))
	require.True(t, stateResponse.Success)

	params := signedTelegramParams(t, token, time.Now())
	params.Set("state", stateResponse.Data)
	callbackURL := "/api/oauth/telegram/login?" + params.Encode()

	crossBrowserRecorder := httptest.NewRecorder()
	router.ServeHTTP(crossBrowserRecorder, httptest.NewRequest(http.MethodGet, callbackURL, nil))
	require.Equal(t, http.StatusForbidden, crossBrowserRecorder.Code)

	callbackRequest := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	for _, sessionCookie := range stateRecorder.Result().Cookies() {
		callbackRequest.AddCookie(sessionCookie)
	}
	callbackRecorder := httptest.NewRecorder()
	router.ServeHTTP(callbackRecorder, callbackRequest)
	require.Equal(t, http.StatusOK, callbackRecorder.Code, callbackRecorder.Body.String())
	require.Contains(t, callbackRecorder.Body.String(), `"success":true`)

	replayRequest := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	for _, sessionCookie := range stateRecorder.Result().Cookies() {
		replayRequest.AddCookie(sessionCookie)
	}
	replayRecorder := httptest.NewRecorder()
	router.ServeHTTP(replayRecorder, replayRequest)
	require.Equal(t, http.StatusForbidden, replayRecorder.Code)
	_, err := model.GetAuthFlow(stateResponse.Data, telegramLoginFlowMatch)
	require.ErrorIs(t, err, model.ErrAuthFlowConsumed)
}

func TestTelegramLoginRejectsProviderAssertionReplayAcrossStates(t *testing.T) {
	setupControllerAuthFlowDB(t)
	require.NoError(t, appi18n.Init())
	gin.SetMode(gin.TestMode)

	const token = "123456:telegram-assertion-replay-test"
	originalEnabled := common.TelegramOAuthEnabled
	originalToken := common.TelegramBotToken
	common.TelegramOAuthEnabled = true
	common.TelegramBotToken = token
	t.Cleanup(func() {
		common.TelegramOAuthEnabled = originalEnabled
		common.TelegramBotToken = originalToken
	})

	user := model.User{
		Id: 99002, Username: "telegram-assertion-replay-user", TelegramId: "123456",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("telegram-assertion-replay"))))
	router.POST("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/telegram/login", TelegramLogin)

	generateState := func() (string, []*http.Cookie) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"telegram","intent":"login"}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool   `json:"success"`
			Data    string `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		return response.Data, recorder.Result().Cookies()
	}

	stateA, cookiesA := generateState()
	stateB, cookiesB := generateState()
	params := signedTelegramParams(t, token, time.Now())

	paramsA := params.Encode()
	paramsWithStateA, err := url.ParseQuery(paramsA)
	require.NoError(t, err)
	paramsWithStateA.Set("state", stateA)
	callbackA := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/login?"+paramsWithStateA.Encode(), nil)
	for _, sessionCookie := range cookiesA {
		callbackA.AddCookie(sessionCookie)
	}
	recorderA := httptest.NewRecorder()
	router.ServeHTTP(recorderA, callbackA)
	require.Equal(t, http.StatusOK, recorderA.Code, recorderA.Body.String())

	paramsWithStateB, err := url.ParseQuery(paramsA)
	require.NoError(t, err)
	paramsWithStateB.Set("state", stateB)
	callbackB := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/login?"+paramsWithStateB.Encode(), nil)
	for _, sessionCookie := range cookiesB {
		callbackB.AddCookie(sessionCookie)
	}
	recorderB := httptest.NewRecorder()
	router.ServeHTTP(recorderB, callbackB)
	require.Equal(t, http.StatusForbidden, recorderB.Code)

	_, err = model.GetAuthFlow(stateB, telegramLoginFlowMatch)
	require.NoError(t, err)
}

type failingSaveSession struct {
	values map[interface{}]interface{}
}

func (*failingSaveSession) ID() string { return "failing-save-session" }

func (session *failingSaveSession) Get(key interface{}) interface{} {
	return session.values[key]
}

func (session *failingSaveSession) Set(key interface{}, value interface{}) {
	session.values[key] = value
}

func (session *failingSaveSession) Delete(key interface{}) {
	delete(session.values, key)
}

func (session *failingSaveSession) Clear() {
	clear(session.values)
}

func (*failingSaveSession) AddFlash(interface{}, ...string) {}

func (*failingSaveSession) Flashes(...string) []interface{} { return nil }

func (*failingSaveSession) Options(sessions.Options) {}

func (*failingSaveSession) Save() error {
	return errors.New("forced session save failure")
}

func setupTwoFASessionFailureTest(
	t *testing.T,
	enabled bool,
) (*model.User, string) {
	t.Helper()

	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.Log{},
	))

	user := &model.User{
		Id:       71001,
		Username: "twofa-session-save-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	key, err := common.GenerateTOTPSecret(user.Username)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.TwoFA{
		UserId:    user.Id,
		Secret:    key.Secret(),
		IsEnabled: enabled,
	}).Error)
	if enabled {
		require.NoError(t, model.CreateBackupCodes(user.Id, []string{"OLD1-CODE"}))
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)
	return user, code
}

func performTwoFARequestWithFailingSessionSave(
	t *testing.T,
	userID int,
	code string,
	handler gin.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(sessions.DefaultKey, &failingSaveSession{
			values: map[interface{}]interface{}{},
		})
		c.Next()
	})
	router.POST("/twofa", func(c *gin.Context) {
		c.Set("id", userID)
		handler(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/twofa",
		bytes.NewReader([]byte(`{"code":"`+code+`"}`)),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestEnable2FAReturnsCommittedSuccessWhenSessionPreservationFails(t *testing.T) {
	user, code := setupTwoFASessionFailureTest(t, false)

	recorder := performTwoFARequestWithFailingSessionSave(t, user.Id, code, Enable2FA)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	twoFA, err := model.GetTwoFAByUserId(user.Id)
	require.NoError(t, err)
	require.NotNil(t, twoFA)
	require.True(t, twoFA.IsEnabled)
}

func TestDisable2FAReturnsCommittedSuccessWhenSessionPreservationFails(t *testing.T) {
	user, code := setupTwoFASessionFailureTest(t, true)

	recorder := performTwoFARequestWithFailingSessionSave(t, user.Id, code, Disable2FA)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	twoFA, err := model.GetTwoFAByUserId(user.Id)
	require.NoError(t, err)
	require.Nil(t, twoFA)
}

func TestRegenerateBackupCodesReturnsCodesWhenSessionPreservationFails(t *testing.T) {
	user, code := setupTwoFASessionFailureTest(t, true)

	recorder := performTwoFARequestWithFailingSessionSave(
		t,
		user.Id,
		code,
		RegenerateBackupCodes,
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.Contains(t, recorder.Body.String(), `"backup_codes"`)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	require.EqualValues(t, 1, stored.SessionGeneration)
}

func TestGet2FAStatusAlwaysReturnsBackupCodeCount(t *testing.T) {
	user, _ := setupTwoFASessionFailureTest(t, false)
	router := gin.New()
	router.GET("/twofa/status", func(c *gin.Context) {
		c.Set("id", user.Id)
		Get2FAStatus(c)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/twofa/status", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{
		"success": true,
		"message": "",
		"data": {
			"enabled": false,
			"locked": false,
			"backup_codes_remaining": 0
		}
	}`, recorder.Body.String())
}

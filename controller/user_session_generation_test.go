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

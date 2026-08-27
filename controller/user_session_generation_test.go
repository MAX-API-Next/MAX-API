package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	require.JSONEq(t, `{"generation":2,"id":9911,"secure_verified_at":null}`, inspectRecorder.Body.String())
}

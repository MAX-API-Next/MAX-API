package middleware

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

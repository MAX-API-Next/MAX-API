package controller

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGenerateAccessTokenDoesNotWaitForOAuthIdentityLock(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)

	user := model.User{
		Id:          7101,
		Username:    "access-token-controller-user",
		DisplayName: "Access Token Controller User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Email:       "access-token-controller@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	lockHeld := make(chan struct{})
	releaseLock := make(chan struct{})
	lockDone := make(chan error, 1)
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseLock) })
	}
	t.Cleanup(release)

	go func() {
		lockDone <- model.WithUserOAuthIdentityWriteTx("held-lock@example.com", func(_ *gorm.DB) error {
			close(lockHeld)
			<-releaseLock
			return nil
		})
	}()

	select {
	case <-lockHeld:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OAuth identity lock holder")
	}

	router := gin.New()
	router.POST("/token", func(c *gin.Context) {
		c.Set("id", user.Id)
		GenerateAccessToken(c)
	})

	type responseResult struct {
		code int
		body string
	}
	done := make(chan responseResult, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/token", nil)
		router.ServeHTTP(recorder, request)
		done <- responseResult{code: recorder.Code, body: recorder.Body.String()}
	}()

	select {
	case result := <-done:
		require.Equal(t, http.StatusOK, result.code)
		require.Contains(t, result.body, `"success":true`)
	case <-time.After(200 * time.Millisecond):
		release()
		require.NoError(t, <-lockDone)
		t.Fatal("access token generation waited for OAuth identity mutation lock")
	}

	release()
	require.NoError(t, <-lockDone)

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.NotEmpty(t, stored.GetAccessToken())
}

package middleware

import (
	"context"
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

func TestAdminStatusChangeBlocksExistingSessionAndTokenWithoutDeletingThem(t *testing.T) {
	const actorID, userID = 2011, 2012
	for _, user := range []model.User{
		{Id: actorID, Username: "batch-status-operator", AffCode: "batch-operator", Role: common.RoleAdminUser, Status: 1},
		{Id: userID, Username: "batch-status-account", AffCode: "batch-account", Role: common.RoleCommonUser, Status: 1, Group: "default", Quota: 12345},
	} {
		require.NoError(t, model.DB.Create(&user).Error)
	}
	t.Cleanup(func() {
		model.DB.Unscoped().Where("user_id = ?", userID).Delete(&model.Token{})
		model.DB.Unscoped().Where("id IN ?", []int{actorID, userID}).Delete(&model.User{})
	})
	token := seedMiddlewareToken(t, userID, "batchstatuscontracttoken")
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("batch-status-session-test-secret"))))
	cookies := loginMiddlewareSession(t, router, &model.User{Id: userID, Username: "batch-status-account", Role: common.RoleCommonUser, Status: 1, Group: "default"})
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	router.GET("/token", TokenAuth(), ok)
	router.GET("/readonly-token", TokenAuthReadOnly(), ok)
	router.GET("/session", UserAuth(), ok)
	assertAccess := func(allowed bool) {
		t.Helper()
		for _, path := range []string{"/token", "/readonly-token", "/session"} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if path == "/session" {
				for _, cookie := range cookies {
					req.AddCookie(cookie)
				}
			} else {
				req.Header.Set("Authorization", "Bearer sk-"+token.Key)
			}
			router.ServeHTTP(w, req)
			if allowed {
				require.Equal(t, http.StatusNoContent, w.Code, "%s: %s", path, w.Body.String())
			} else if path == "/session" {
				require.NotEqual(t, http.StatusNoContent, w.Code)
				require.Contains(t, w.Body.String(), `"success":false`)
			} else {
				require.Equal(t, http.StatusForbidden, w.Code, "%s: %s", path, w.Body.String())
			}
		}
	}
	assertAccess(true)
	_, err := model.SetUserStatusByAdmin(context.Background(), actorID, userID, 1, 2)
	require.NoError(t, err)
	assertAccess(false)
	_, err = model.SetUserStatusByAdmin(context.Background(), actorID, userID, 2, 1)
	require.NoError(t, err)
	assertAccess(true)
	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	require.Equal(t, token.Status, stored.Status)
	require.Equal(t, token.RemainQuota, stored.RemainQuota)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.EqualValues(t, 12345, user.Quota)
}

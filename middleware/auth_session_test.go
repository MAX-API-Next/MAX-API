package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
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

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open middleware test db: " + err.Error())
	}
	model.DB = db
	model.LOG_DB = db
	if err := db.AutoMigrate(&model.User{}, &model.Token{}); err != nil {
		panic("failed to migrate middleware test db: " + err.Error())
	}

	os.Exit(m.Run())
}

func seedMiddlewareUser(t *testing.T, id int, role int, status int, group string) {
	t.Helper()

	require.NoError(t, model.DB.Unscoped().Where("user_id = ?", id).Delete(&model.Token{}).Error)
	require.NoError(t, model.DB.Unscoped().Where("id = ?", id).Delete(&model.User{}).Error)
	user := &model.User{
		Id:          id,
		Username:    "tester",
		Password:    "hashed-password",
		DisplayName: "Tester",
		Role:        role,
		Status:      status,
		Group:       group,
	}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", id).UpdateColumn("status", status).Error)
}

func seedMiddlewareToken(t *testing.T, userID int, key string) *model.Token {
	t.Helper()

	token := &model.Token{
		UserId:         userID,
		Name:           "test-token",
		Key:            key,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: true,
		Group:          "default",
	}
	require.NoError(t, model.DB.Create(token).Error)
	return token
}

func loginMiddlewareSession(t *testing.T, router *gin.Engine, user *model.User) []*http.Cookie {
	t.Helper()

	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("id", user.Id)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	return recorder.Result().Cookies()
}

func TestAdminAuthRejectsDemotedStaleSession(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleAdminUser, common.UserStatusEnabled, "default")

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("auth-session-test"))))
	cookies := loginMiddlewareSession(t, router, &model.User{
		Id:       1,
		Username: "tester",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	})
	router.GET("/admin", AdminAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("role", common.RoleCommonUser).Error)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("Max-Api-User", "1")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, request)

	require.NotEqual(t, http.StatusNoContent, recorder.Code)
}

func TestUserAuthAllowsSessionWithoutUserHeader(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleCommonUser, common.UserStatusEnabled, "default")

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("user-session-without-header-test"))))
	cookies := loginMiddlewareSession(t, router, &model.User{
		Id:       1,
		Username: "tester",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	})
	router.GET("/self", UserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/self", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestUserAuthAllowsSessionWithStaleUserHeader(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleCommonUser, common.UserStatusEnabled, "default")

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("user-session-stale-header-test"))))
	cookies := loginMiddlewareSession(t, router, &model.User{
		Id:       1,
		Username: "tester",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	})
	router.GET("/self", UserAuth(), func(c *gin.Context) {
		require.Equal(t, 1, c.GetInt("id"))
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/self", nil)
	request.Header.Set("Max-Api-User", "999")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestUserAuthRejectsNonEnabledStatus(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleCommonUser, 0, "default")

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("user-session-non-enabled-test"))))
	cookies := loginMiddlewareSession(t, router, &model.User{
		Id:       1,
		Username: "tester",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	})
	router.GET("/self", UserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/self", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, request)

	require.NotEqual(t, http.StatusNoContent, recorder.Code)
}

func TestTokenOrUserAuthRejectsDisabledStaleSession(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleCommonUser, common.UserStatusEnabled, "default")

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("token-or-user-session-test"))))
	cookies := loginMiddlewareSession(t, router, &model.User{
		Id:       1,
		Username: "tester",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	})
	router.GET("/video", TokenOrUserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("status", common.UserStatusDisabled).Error)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/video", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestTokenAuthRejectsDisabledTokenOwner(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleCommonUser, common.UserStatusDisabled, "default")
	token := seedMiddlewareToken(t, 1, "disabledownertoken")

	router := gin.New()
	router.GET("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestTokenAuthRejectsDeletedTokenOwner(t *testing.T) {
	seedMiddlewareUser(t, 1, common.RoleCommonUser, common.UserStatusEnabled, "default")
	token := seedMiddlewareToken(t, 1, "deletedownertoken")
	require.NoError(t, model.DB.Delete(&model.User{}, 1).Error)

	router := gin.New()
	router.GET("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

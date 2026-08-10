package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
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

var universalVerifyRateLimitTestRun uint64

func TestUserTokenRouteUsesPostOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	SetApiRouter(engine)

	var hasPost bool
	var hasGet bool
	for _, route := range engine.Routes() {
		if route.Path != "/api/user/token" {
			continue
		}
		switch route.Method {
		case "POST":
			hasPost = true
		case "GET":
			hasGet = true
		}
	}

	if !hasPost {
		t.Fatal("expected POST /api/user/token route to be registered")
	}
	if hasGet {
		t.Fatal("did not expect GET /api/user/token route to be registered")
	}
}

func TestOAuthStateRouteUsesPostOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	var hasPost bool
	var hasGet bool
	for _, route := range engine.Routes() {
		if route.Path != "/api/oauth/state" {
			continue
		}
		hasPost = hasPost || route.Method == http.MethodPost
		hasGet = hasGet || route.Method == http.MethodGet
	}
	require.True(t, hasPost)
	require.False(t, hasGet)
}

func TestOAuthStateOpenAPIUsesPostWithBoundRequest(t *testing.T) {
	documentBytes, err := os.ReadFile("../docs/openapi/api.json")
	require.NoError(t, err)

	var document struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(documentBytes, &document))
	oauthPath := document.Paths["/api/oauth/state"]
	require.Contains(t, oauthPath, "post")
	require.NotContains(t, oauthPath, "get")
	requestBody := oauthPath["post"]["requestBody"].(map[string]any)
	content := requestBody["content"].(map[string]any)
	schema := content["application/json"].(map[string]any)["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	require.Contains(t, properties, "provider")
	require.Contains(t, properties, "intent")
}

func TestUserTokenOpenAPIUsesPostOnly(t *testing.T) {
	documentBytes, err := os.ReadFile("../docs/openapi/api.json")
	require.NoError(t, err)

	var document struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(documentBytes, &document))

	tokenPath, ok := document.Paths["/api/user/token"]
	require.True(t, ok, "expected /api/user/token in OpenAPI document")
	require.Contains(t, tokenPath, "post")
	require.NotContains(t, tokenPath, "get")
}

func TestVerificationMethodsRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	SetApiRouter(engine)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/verify/methods" {
			return
		}
	}
	t.Fatal("expected GET /api/verify/methods route to be registered")
}

func TestHealthRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	SetApiRouter(engine)

	expected := map[string]bool{
		"/health":       false,
		"/health/live":  false,
		"/health/ready": false,
	}
	for _, route := range engine.Routes() {
		if route.Method != http.MethodGet {
			continue
		}
		if _, ok := expected[route.Path]; ok {
			expected[route.Path] = true
		}
	}
	for path, registered := range expected {
		require.Truef(t, registered, "expected GET %s route to be registered", path)
	}
}

func TestUniversalVerifyRateLimitFollowsUserAcrossIPs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&universalVerifyRateLimitTestRun, 1)

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldGlobalAPIRateLimitEnable := common.GlobalApiRateLimitEnable
	oldCriticalRateLimitEnable := common.CriticalRateLimitEnable
	oldCriticalRateLimitNum := common.CriticalRateLimitNum
	oldCriticalRateLimitDuration := common.CriticalRateLimitDuration

	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.GlobalApiRateLimitEnable = false
	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = 1
	common.CriticalRateLimitDuration = 20 * 60

	db, err := gorm.Open(sqlite.Open("file:router_verify_rate_limit?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.PasskeyCredential{},
	))
	user := model.User{
		Id:       987654 + int(testRun),
		Username: fmt.Sprintf("verify-rate-limit-user-%d", testRun),
		Password: "hashed-password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)

	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.GlobalApiRateLimitEnable = oldGlobalAPIRateLimitEnable
		common.CriticalRateLimitEnable = oldCriticalRateLimitEnable
		common.CriticalRateLimitNum = oldCriticalRateLimitNum
		common.CriticalRateLimitDuration = oldCriticalRateLimitDuration
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("verify-rate-limit-session"))))
	engine.GET("/test/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	SetApiRouter(engine)

	loginRecorder := httptest.NewRecorder()
	engine.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/test/login", nil))
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)

	performVerify := func(remoteAddr string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/verify",
			strings.NewReader(`{"method":"invalid"}`),
		)
		request.RemoteAddr = remoteAddr
		request.Header.Set("Content-Type", "application/json")
		for _, sessionCookie := range loginRecorder.Result().Cookies() {
			request.AddCookie(sessionCookie)
		}
		engine.ServeHTTP(recorder, request)
		return recorder
	}

	require.Equal(t, http.StatusOK, performVerify(fmt.Sprintf("[2001:db8:%x::1]:1234", testRun)).Code)
	require.Equal(t, http.StatusTooManyRequests, performVerify(fmt.Sprintf("[2001:db8:%x::2]:1234", testRun)).Code)
}

func TestCriticalAccountRoutesRateLimitSameUserAcrossIPs(t *testing.T) {
	for _, path := range []string{"/api/user/token", "/api/user/aff_transfer"} {
		path := path
		t.Run(path, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			testRun := atomic.AddUint64(&universalVerifyRateLimitTestRun, 1)

			oldDB := model.DB
			oldLogDB := model.LOG_DB
			oldRedisEnabled := common.RedisEnabled
			oldMemoryCacheEnabled := common.MemoryCacheEnabled
			oldGlobalAPIRateLimitEnable := common.GlobalApiRateLimitEnable
			oldCriticalRateLimitEnable := common.CriticalRateLimitEnable
			oldCriticalRateLimitNum := common.CriticalRateLimitNum
			oldCriticalRateLimitDuration := common.CriticalRateLimitDuration

			common.RedisEnabled = false
			common.MemoryCacheEnabled = false
			common.GlobalApiRateLimitEnable = false
			common.CriticalRateLimitEnable = true
			common.CriticalRateLimitNum = 1
			common.CriticalRateLimitDuration = 20 * 60

			dsn := fmt.Sprintf("file:critical_account_route_%d?mode=memory&cache=shared", testRun)
			db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
			require.NoError(t, err)
			model.DB = db
			model.LOG_DB = db
			require.NoError(t, db.AutoMigrate(&model.User{}))
			user := model.User{
				Id: 1200000 + int(testRun), Username: fmt.Sprintf("critical-user-%d", testRun),
				Password: "hashed-password", Role: common.RoleCommonUser,
				Status: common.UserStatusEnabled, Group: "default",
			}
			require.NoError(t, db.Create(&user).Error)

			t.Cleanup(func() {
				if sqlDB, dbErr := db.DB(); dbErr == nil {
					_ = sqlDB.Close()
				}
				model.DB = oldDB
				model.LOG_DB = oldLogDB
				common.RedisEnabled = oldRedisEnabled
				common.MemoryCacheEnabled = oldMemoryCacheEnabled
				common.GlobalApiRateLimitEnable = oldGlobalAPIRateLimitEnable
				common.CriticalRateLimitEnable = oldCriticalRateLimitEnable
				common.CriticalRateLimitNum = oldCriticalRateLimitNum
				common.CriticalRateLimitDuration = oldCriticalRateLimitDuration
			})

			engine := gin.New()
			engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("critical-account-session"))))
			engine.GET("/test/login", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("id", user.Id)
				session.Set("username", user.Username)
				session.Set("role", user.Role)
				session.Set("status", user.Status)
				session.Set("group", user.Group)
				require.NoError(t, session.Save())
				c.Status(http.StatusNoContent)
			})
			SetApiRouter(engine)

			loginRecorder := httptest.NewRecorder()
			engine.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/test/login", nil))
			require.Equal(t, http.StatusNoContent, loginRecorder.Code)

			perform := func(remoteAddr string) *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"quota":1}`))
				request.RemoteAddr = remoteAddr
				request.Header.Set("Content-Type", "application/json")
				for _, sessionCookie := range loginRecorder.Result().Cookies() {
					request.AddCookie(sessionCookie)
				}
				engine.ServeHTTP(recorder, request)
				return recorder
			}

			first := perform(fmt.Sprintf("[2001:db8:%x::1]:1234", testRun))
			require.NotEqual(t, http.StatusTooManyRequests, first.Code)
			require.Equal(t, http.StatusTooManyRequests, perform(fmt.Sprintf("[2001:db8:%x::2]:1234", testRun)).Code)
		})
	}
}

func TestSecureVerificationOpenAPIIncludesScopeAndRequestBody(t *testing.T) {
	documentBytes, err := os.ReadFile("../docs/openapi/api.json")
	require.NoError(t, err)

	var document struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(documentBytes, &document))

	methodsOperation := document.Paths["/api/verify/methods"]["get"]
	parameters, ok := methodsOperation["parameters"].([]any)
	require.True(t, ok)
	require.Condition(t, func() bool {
		for _, parameter := range parameters {
			value, isMap := parameter.(map[string]any)
			if !isMap || value["name"] != "scope" || value["in"] != "query" {
				continue
			}
			schema, schemaOK := value["schema"].(map[string]any)
			values, valuesOK := schema["enum"].([]any)
			return schemaOK && valuesOK && len(values) == 2 &&
				values[0] == "access_token" && values[1] == "account_delete"
		}
		return false
	}, "expected supported scope query parameter for GET /api/verify/methods")

	verifyOperation := document.Paths["/api/verify"]["post"]
	requestBody, ok := verifyOperation["requestBody"].(map[string]any)
	require.True(t, ok, "expected requestBody for POST /api/verify")
	content := requestBody["content"].(map[string]any)
	mediaType := content["application/json"].(map[string]any)
	schema := mediaType["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, property := range []string{"method", "code", "password", "scope"} {
		require.Contains(t, properties, property)
	}
}

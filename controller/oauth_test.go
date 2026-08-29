package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	appi18n "github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type deletedUserOAuthProvider struct{}

func (*deletedUserOAuthProvider) GetName() string { return "deleted-test" }
func (*deletedUserOAuthProvider) IsEnabled() bool { return true }
func (*deletedUserOAuthProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return nil, nil
}

func setupControllerAuthFlowDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AuthFlow{}, &model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestGenerateOAuthCodeCreatesTelegramLoginState(t *testing.T) {
	setupControllerAuthFlowDB(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("telegram-state-create-test"))))
	router.POST("/api/oauth/state", GenerateOAuthCode)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"telegram","intent":"login"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotEmpty(t, response.Data)
	_, err := model.GetAuthFlow(response.Data, model.AuthFlowMatch{
		Purpose: model.AuthFlowPurposeOAuth, Provider: "telegram", Intent: model.AuthFlowIntentLogin,
	})
	require.NoError(t, err)
}

func TestGenerateOAuthCodeCreatesProviderBoundOpaqueState(t *testing.T) {
	setupControllerAuthFlowDB(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-state-create-test"))))
	router.POST("/api/oauth/state", GenerateOAuthCode)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"github","intent":"login","aff":"invite"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 43)
	flow, err := model.GetAuthFlow(response.Data, model.AuthFlowMatch{
		Purpose: model.AuthFlowPurposeOAuth, Provider: "github", Intent: model.AuthFlowIntentLogin,
	})
	require.NoError(t, err)
	require.NotEqual(t, response.Data, flow.TokenHash)
	require.Contains(t, flow.Payload, "invite")
}

func TestHandleOAuthRejectsStateFromDifferentBrowserSession(t *testing.T) {
	setupControllerAuthFlowDB(t)
	require.NoError(t, appi18n.Init())
	originalGitHubOAuthEnabled := common.GitHubOAuthEnabled
	common.GitHubOAuthEnabled = true
	t.Cleanup(func() { common.GitHubOAuthEnabled = originalGitHubOAuthEnabled })
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-browser-binding-test"))))
	router.POST("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/:provider", HandleOAuth)

	stateRecorder := httptest.NewRecorder()
	stateRequest := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"github","intent":"login"}`))
	stateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(stateRecorder, stateRequest)
	require.Equal(t, http.StatusOK, stateRecorder.Code)

	var stateResponse struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(stateRecorder.Body.Bytes(), &stateResponse))
	require.True(t, stateResponse.Success)
	require.NotEmpty(t, stateResponse.Data)
	require.NotEmpty(t, stateRecorder.Result().Cookies())

	callbackRecorder := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/oauth/github?state="+stateResponse.Data+"&error=access_denied", nil)
	// Deliberately omit browser A's signed session cookie.
	router.ServeHTTP(callbackRecorder, callbackRequest)

	require.Equal(t, http.StatusForbidden, callbackRecorder.Code)

	callbackRecorder = httptest.NewRecorder()
	callbackRequest = httptest.NewRequest(http.MethodGet, "/api/oauth/github?state="+stateResponse.Data+"&error=access_denied", nil)
	for _, sessionCookie := range stateRecorder.Result().Cookies() {
		callbackRequest.AddCookie(sessionCookie)
	}
	router.ServeHTTP(callbackRecorder, callbackRequest)
	require.Equal(t, http.StatusOK, callbackRecorder.Code)
	_, err := model.GetAuthFlow(stateResponse.Data, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	require.ErrorIs(t, err, model.ErrAuthFlowConsumed)
}

func TestGenerateOAuthCodeRejectsAnonymousBind(t *testing.T) {
	require.NoError(t, appi18n.Init())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"github","intent":"bind"}`))
	c.Request.Header.Set("Accept-Language", "en")

	GenerateOAuthCode(c)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, "Unauthorized", response.Message)
}

func TestGenerateOAuthCodeRejectsTelegramBindIntent(t *testing.T) {
	setupControllerAuthFlowDB(t)
	require.NoError(t, appi18n.Init())
	originalEnabled := common.TelegramOAuthEnabled
	common.TelegramOAuthEnabled = true
	t.Cleanup(func() { common.TelegramOAuthEnabled = originalEnabled })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"telegram","intent":"bind"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 123)

	GenerateOAuthCode(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	var count int64
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestHandleOAuthRejectsConsumedStateBeforeProviderExchange(t *testing.T) {
	setupControllerAuthFlowDB(t)
	require.NoError(t, appi18n.Init())
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-consumed-state-test"))))
	router.POST("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/:provider", HandleOAuth)

	stateRecorder := httptest.NewRecorder()
	stateRequest := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"github","intent":"login"}`))
	stateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(stateRecorder, stateRequest)
	require.Equal(t, http.StatusOK, stateRecorder.Code)
	var stateResponse struct {
		Data string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(stateRecorder.Body.Bytes(), &stateResponse))
	state := stateResponse.Data
	require.NotEmpty(t, state)

	var err error
	_, err = model.ConsumeAuthFlow(state, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth, Provider: "github"})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/oauth/github?state="+state+"&code=unused", nil)
	for _, sessionCookie := range stateRecorder.Result().Cookies() {
		callbackRequest.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, callbackRequest)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}
func (*deletedUserOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return nil, nil
}
func (*deletedUserOAuthProvider) IsUserIDTaken(string) (bool, error) { return true, nil }
func (*deletedUserOAuthProvider) FillUserByProviderID(*model.User, string) error {
	return model.ErrUserDeleted
}
func (*deletedUserOAuthProvider) SetProviderUserID(*model.User, string) {}
func (*deletedUserOAuthProvider) GetProviderPrefix() string             { return "deleted_" }

func TestOAuthProviderUserUpdateField(t *testing.T) {
	tests := []struct {
		name     string
		provider oauth.Provider
		want     model.UserUpdateField
	}{
		{name: "github", provider: &oauth.GitHubProvider{}, want: model.UserUpdateFieldGitHubId},
		{name: "discord", provider: &oauth.DiscordProvider{}, want: model.UserUpdateFieldDiscordId},
		{name: "oidc", provider: &oauth.OIDCProvider{}, want: model.UserUpdateFieldOidcId},
		{name: "linuxdo", provider: &oauth.LinuxDOProvider{}, want: model.UserUpdateFieldLinuxDOId},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := oauthProviderUserUpdateField(tt.provider)
			if !ok {
				t.Fatalf("expected provider field mapping")
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestFindOrCreateOAuthUserMapsDeletedUserDomainError(t *testing.T) {
	_, err := findOrCreateOAuthUser(nil, &deletedUserOAuthProvider{}, &oauth.OAuthUser{
		ProviderUserID: "deleted-user-id",
	}, "")

	var deletedErr *OAuthUserDeletedError
	if !errors.As(err, &deletedErr) {
		t.Fatalf("expected OAuthUserDeletedError, got %v", err)
	}
}

type failingLookupOAuthProvider struct {
	deletedUserOAuthProvider
	err error
}

func (p *failingLookupOAuthProvider) IsUserIDTaken(string) (bool, error) {
	if p.err != nil {
		return false, p.err
	}
	return false, errors.New("oauth uniqueness lookup unavailable")
}

func TestFindOrCreateOAuthUserPropagatesUniquenessLookupError(t *testing.T) {
	lookupErr := errors.New("oauth uniqueness lookup unavailable")
	provider := &failingLookupOAuthProvider{err: lookupErr}
	_, err := findOrCreateOAuthUser(nil, provider, &oauth.OAuthUser{
		ProviderUserID: "unverified-user-id",
	}, "")

	if err == nil || !errors.Is(err, lookupErr) || !strings.Contains(err.Error(), "identity lookup failed") {
		t.Fatalf("expected identity lookup domain error, got %v", err)
	}
}

type publicLookupFailingOAuthProvider struct {
	deletedUserOAuthProvider
}

type publicFillFailingOAuthProvider struct {
	publicLookupFailingOAuthProvider
}

func (*publicFillFailingOAuthProvider) IsUserIDTaken(string) (bool, error) { return true, nil }
func (*publicFillFailingOAuthProvider) FillUserByProviderID(*model.User, string) error {
	return errors.New("internal database host db.internal:5432")
}

func TestFindOrCreateOAuthUserWrapsProviderFillDatabaseError(t *testing.T) {
	_, err := findOrCreateOAuthUser(nil, &publicFillFailingOAuthProvider{}, &oauth.OAuthUser{
		ProviderUserID: "existing-user-id",
		Extra:          map[string]any{},
	}, "")
	var lookupErr *oauthIdentityLookupError
	if !errors.As(err, &lookupErr) {
		t.Fatalf("expected masked identity lookup error, got %v", err)
	}
	if strings.Contains(err.Error(), "db.internal") {
		t.Fatalf("public error leaked database details: %q", err.Error())
	}
}

func (*publicLookupFailingOAuthProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{AccessToken: "test-token"}, nil
}

func (*publicLookupFailingOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{ProviderUserID: "unverified-user-id", Extra: map[string]any{}}, nil
}

func (*publicLookupFailingOAuthProvider) IsUserIDTaken(string) (bool, error) {
	return false, errors.New("internal database host db.internal:5432")
}

func TestOAuthBindMasksUniquenessLookupDatabaseError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/test?code=test-code", nil)

	handleOAuthBind(c, &publicLookupFailingOAuthProvider{}, &model.AuthFlow{}, "")

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	if response.Success {
		t.Fatal("expected OAuth bind lookup to fail")
	}
	if response.Message == "" {
		t.Fatal("expected a public error message")
	}
	if strings.Contains(response.Message, "db.internal") {
		t.Fatalf("database details leaked in OAuth response: %q", response.Message)
	}
}

package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	appi18n "github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func signedTelegramParams(t *testing.T, token string, authDate time.Time) url.Values {
	t.Helper()
	params := url.Values{
		"id":         {"123456"},
		"first_name": {"Security"},
		"auth_date":  {strconv.FormatInt(authDate.Unix(), 10)},
	}
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
	return params
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

func TestTelegramAuthorizationRejectsAuthDateBeyondClockSkew(t *testing.T) {
	const token = "123456:telegram-test-token"
	now := time.Unix(1_800_000_000, 0)
	params := signedTelegramParams(t, token, now.Add(telegramClockSkew+time.Second))

	require.False(t, checkTelegramAuthorizationAt(params, token, now))
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

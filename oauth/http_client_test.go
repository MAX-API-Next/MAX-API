package oauth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func configureOAuthFetchTest(t *testing.T, enabled bool) {
	t.Helper()
	settings := system_setting.GetFetchSetting()
	original := *settings
	t.Cleanup(func() { *settings = original })
	settings.EnableSSRFProtection = enabled
	settings.AllowPrivateIp = false
	settings.DomainFilterMode = false
	settings.IpFilterMode = false
	settings.DomainList = nil
	settings.IpList = nil
	settings.AllowedPorts = []string{"80", "443"}
	settings.ApplyIPFilterForDomain = true
}

func TestNewOAuthHTTPClientRejectsPrivateEndpoint(t *testing.T) {
	configureOAuthFetchTest(t, true)

	client, err := newOAuthHTTPClient("http://127.0.0.1/token", time.Second)

	require.Nil(t, client)
	require.ErrorContains(t, err, "private IP address not allowed")
}

func TestGenericOAuthDebugLogsDoNotContainResponseSecrets(t *testing.T) {
	configureOAuthFetchTest(t, false)
	originalDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() { common.DebugEnabled = originalDebug })

	const (
		accessToken  = "access-secret-value"
		refreshToken = "refresh-secret-value"
		idToken      = "id-secret-value"
		email        = "secret-email@example.com"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","refresh_token":"` + refreshToken + `","id_token":"` + idToken + `","token_type":"Bearer"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"provider-user","username":"tester","name":"Test User","email":"` + email + `"}`))
	}))
	defer server.Close()

	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalWriter
		common.LogWriterMu.Unlock()
	})

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:             "Test OAuth",
		Slug:             "test-oauth",
		Enabled:          true,
		TokenEndpoint:    server.URL + "/token",
		UserInfoEndpoint: server.URL + "/userinfo",
		ClientId:         "client-id",
		ClientSecret:     "client-secret",
		UserIdField:      "id",
		UsernameField:    "username",
		DisplayNameField: "name",
		EmailField:       "email",
	})
	token, err := provider.ExchangeToken(context.Background(), "authorization-code", nil)
	require.NoError(t, err)
	_, err = provider.GetUserInfo(context.Background(), token)
	require.NoError(t, err)

	output := logs.String()
	for _, secret := range []string{accessToken, refreshToken, idToken, email, "authorization-code"} {
		require.False(t, strings.Contains(output, secret), "debug log leaked %q", secret)
	}
}

func TestOIDCGetUserInfoAllowsSubjectWithoutEmail(t *testing.T) {
	configureOAuthFetchTest(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"subject-only-user","preferred_username":"tester"}`))
	}))
	defer server.Close()

	settings := system_setting.GetOIDCSettings()
	original := *settings
	settings.UserInfoEndpoint = server.URL
	t.Cleanup(func() { *settings = original })

	user, err := (&OIDCProvider{}).GetUserInfo(context.Background(), &OAuthToken{AccessToken: "access-token"})

	require.NoError(t, err)
	require.Equal(t, "subject-only-user", user.ProviderUserID)
	require.Equal(t, "tester", user.Username)
	require.Empty(t, user.Email)
}

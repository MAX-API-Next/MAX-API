package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/oauth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deletedUserOAuthProvider struct{}

func (*deletedUserOAuthProvider) GetName() string { return "deleted-test" }
func (*deletedUserOAuthProvider) IsEnabled() bool { return true }
func (*deletedUserOAuthProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return nil, nil
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
	}, nil)

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
	}, nil)

	if err == nil || !errors.Is(err, lookupErr) || !strings.Contains(err.Error(), "identity lookup failed") {
		t.Fatalf("expected identity lookup domain error, got %v", err)
	}
}

type publicLookupFailingOAuthProvider struct {
	deletedUserOAuthProvider
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

	handleOAuthBind(c, &publicLookupFailingOAuthProvider{})

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

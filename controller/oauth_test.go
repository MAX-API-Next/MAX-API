package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/oauth"
	"github.com/gin-gonic/gin"
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
func (*deletedUserOAuthProvider) IsUserIDTaken(string) bool { return true }
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

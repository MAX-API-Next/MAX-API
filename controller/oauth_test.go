package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/oauth"
)

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

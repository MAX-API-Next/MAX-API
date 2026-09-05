package controller

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	oauthAuthFlowTTL        = 10 * time.Minute
	oauthStateSessionKey    = "oauth_state"
	authSessionIDSessionKey = "auth_session_id"
)

type oauthStateRequest struct {
	Provider string `json:"provider"`
	Intent   string `json:"intent"`
	Aff      string `json:"aff,omitempty"`
}

type oauthFlowPayload struct {
	AffiliateCode string `json:"affiliate_code,omitempty"`
}

func oauthSessionStateMatches(c *gin.Context, state string) bool {
	if state == "" {
		return false
	}
	expected, ok := sessions.Default(c).Get(oauthStateSessionKey).(string)
	return ok && len(expected) == len(state) && subtle.ConstantTimeCompare([]byte(expected), []byte(state)) == 1
}

func clearOAuthSessionState(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete(oauthStateSessionKey)
	if err := session.Save(); err != nil {
		common.SysError("failed to clear OAuth state from session: " + err.Error())
	}
}

func authSessionID(session sessions.Session) string {
	value, ok := session.Get(authSessionIDSessionKey).(string)
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if len(value) != 48 {
		return ""
	}
	return value
}

func ensureAuthSessionID(session sessions.Session) (string, error) {
	if session == nil {
		return "", errors.New("auth session is unavailable")
	}
	if value := authSessionID(session); value != "" {
		return value, nil
	}
	value, err := common.GenerateRandomKey(48)
	if err != nil {
		return "", fmt.Errorf("generate auth session id: %w", err)
	}
	session.Set(authSessionIDSessionKey, value)
	return value, nil
}

// providerParams returns map with Provider key for i18n templates
func providerParams(name string) map[string]any {
	return map[string]any{"Provider": name}
}

func supportsOAuthState(provider string, intent string) bool {
	if provider == "telegram" {
		return common.TelegramOAuthEnabled && intent == model.AuthFlowIntentLogin
	}
	return oauth.GetProvider(provider) != nil
}

func oauthProviderUserUpdateField(provider oauth.Provider) (model.UserUpdateField, bool) {
	switch provider.(type) {
	case *oauth.GitHubProvider:
		return model.UserUpdateFieldGitHubId, true
	case *oauth.DiscordProvider:
		return model.UserUpdateFieldDiscordId, true
	case *oauth.OIDCProvider:
		return model.UserUpdateFieldOidcId, true
	case *oauth.LinuxDOProvider:
		return model.UserUpdateFieldLinuxDOId, true
	default:
		return "", false
	}
}

func handleOAuthUserLookupError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, model.ErrUserDeleted) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "用户已注销"})
	} else {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
	}
	return true
}

type oauthIdentityLookupError struct {
	provider string
	err      error
}

func (e *oauthIdentityLookupError) Error() string {
	return "OAuth identity lookup failed"
}

func (e *oauthIdentityLookupError) Unwrap() error {
	return e.err
}

func handleOAuthIdentityLookupError(c *gin.Context, provider string, err error) bool {
	if err == nil {
		return false
	}
	var lookupErr *oauthIdentityLookupError
	if errors.As(err, &lookupErr) {
		if lookupErr.provider != "" {
			provider = lookupErr.provider
		}
		err = lookupErr.err
	}
	common.SysError(fmt.Sprintf("OAuth identity lookup failed (provider=%s): %v", provider, err))
	common.ApiErrorI18n(c, i18n.MsgOAuthGetUserErr)
	return true
}

// GenerateOAuthCode generates a state code for OAuth CSRF protection
func GenerateOAuthCode(c *gin.Context) {
	var request oauthStateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	request.Provider = strings.ToLower(strings.TrimSpace(request.Provider))
	request.Intent = strings.TrimSpace(request.Intent)
	request.Aff = strings.TrimSpace(request.Aff)
	if !supportsOAuthState(request.Provider, request.Intent) ||
		(request.Intent != model.AuthFlowIntentLogin && request.Intent != model.AuthFlowIntentBind) ||
		len(request.Aff) > 32 ||
		(request.Intent == model.AuthFlowIntentBind && request.Aff != "") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userID := 0
	if request.Intent == model.AuthFlowIntentBind {
		userID = c.GetInt("id")
		if userID <= 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": i18n.T(c, i18n.MsgUnauthorized)})
			return
		}
	}
	session := sessions.Default(c)
	sessionID, err := ensureAuthSessionID(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	payload, err := common.Marshal(oauthFlowPayload{AffiliateCode: request.Aff})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiresAt := time.Now().Add(oauthAuthFlowTTL)
	state, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  request.Provider,
		Intent:    request.Intent,
		UserId:    userID,
		SessionId: sessionID,
		Payload:   string(payload),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session.Set(oauthStateSessionKey, state)
	if err := session.Save(); err != nil {
		if _, consumeErr := model.ConsumeAuthFlow(state, model.AuthFlowMatch{
			Purpose:   model.AuthFlowPurposeOAuth,
			Provider:  request.Provider,
			Intent:    request.Intent,
			UserId:    userID,
			SessionId: sessionID,
		}); consumeErr != nil {
			common.SysError("failed to invalidate OAuth flow after session save failure: " + consumeErr.Error())
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

// HandleOAuth handles OAuth callback for all standard OAuth providers
func HandleOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}

	// 1. Validate state (CSRF protection)
	state := c.Query("state")
	if !oauthSessionStateMatches(c, state) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	pendingFlow, err := model.GetAuthFlow(state, model.AuthFlowMatch{
		Purpose:  model.AuthFlowPurposeOAuth,
		Provider: providerName,
	})
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	sessionID := authSessionID(sessions.Default(c))
	if pendingFlow.SessionId != "" && pendingFlow.SessionId != sessionID {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}

	consumeMatch := model.AuthFlowMatch{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  providerName,
		Intent:    pendingFlow.Intent,
		SessionId: pendingFlow.SessionId,
	}
	if pendingFlow.Intent == model.AuthFlowIntentBind {
		userID := c.GetInt("id")
		if userID <= 0 || userID != pendingFlow.UserId {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
			return
		}
		consumeMatch.UserId = userID
	} else if pendingFlow.Intent != model.AuthFlowIntentLogin {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 3. Check if provider is enabled
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// 4. Handle error from provider
	errorCode := c.Query("error")
	if errorCode != "" {
		if _, err := model.ConsumeAuthFlow(state, consumeMatch); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
			return
		}
		clearOAuthSessionState(c)
		errorDescription := c.Query("error_description")
		if errorDescription == "" {
			errorDescription = errorCode
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}
	if pendingFlow.Intent == model.AuthFlowIntentBind {
		handleOAuthBind(c, provider, pendingFlow, state)
		return
	}

	// 5. Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 6. Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}
	flow, err := model.ConsumeAuthFlow(state, consumeMatch)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		return
	}
	clearOAuthSessionState(c)

	// 7. Find or create user
	var payload oauthFlowPayload
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := findOrCreateOAuthUser(c, provider, oauthUser, payload.AffiliateCode)
	if err != nil {
		var lookupErr *oauthIdentityLookupError
		if errors.As(err, &lookupErr) {
			handleOAuthIdentityLookupError(c, provider.GetName(), lookupErr)
			return
		}
		switch err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		case *OAuthEmailAlreadyTakenError:
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		default:
			common.ApiError(c, err)
		}
		return
	}

	// 8. Check user status
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 9. Setup login
	setupLogin(user, c)
}

// handleOAuthBind handles binding OAuth account to existing user
func handleOAuthBind(c *gin.Context, provider oauth.Provider, pendingFlow *model.AuthFlow, flowToken string) {
	// Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Check if this OAuth account is already bound (check both new ID and legacy ID)
	taken, err := provider.IsUserIDTaken(oauthUser.ProviderUserID)
	if handleOAuthIdentityLookupError(c, provider.GetName(), err) {
		return
	}
	if taken {
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
		return
	}
	// Also check legacy ID to prevent duplicate bindings during migration period
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		legacyTaken, lookupErr := provider.IsUserIDTaken(legacyID)
		if handleOAuthIdentityLookupError(c, provider.GetName(), lookupErr) {
			return
		}
		if legacyTaken {
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
			return
		}
	}

	// Resolve the mutation path before consuming the one-time flow so an
	// unsupported provider can never burn a valid state.
	genericProvider, isGenericProvider := provider.(*oauth.GenericOAuthProvider)
	updateField, hasUpdateField := oauthProviderUserUpdateField(provider)
	if !isGenericProvider && !hasUpdateField {
		common.ApiError(c, fmt.Errorf("unsupported built-in OAuth provider: %s", provider.GetName()))
		return
	}

	var cacheTask model.CacheInvalidationTask
	bindMatch := model.AuthFlowMatch{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  pendingFlow.Provider,
		Intent:    model.AuthFlowIntentBind,
		UserId:    pendingFlow.UserId,
		SessionId: pendingFlow.SessionId,
	}
	err = model.WithUserOAuthIdentityMutationLock(nil, func(conn *gorm.DB) error {
		return conn.Transaction(func(tx *gorm.DB) error {
			_, consumeErr := model.ConsumeAuthFlowWithActionTx(tx, flowToken, bindMatch, func(identityTx *gorm.DB, _ *model.AuthFlow) error {
				var user model.User
				if err := identityTx.First(&user, pendingFlow.UserId).Error; err != nil {
					return err
				}
				if isGenericProvider {
					// Custom provider: keep the binding mutation in the same transaction
					// as AuthFlow consumption.
					return model.UpdateUserOAuthBindingWithTx(identityTx, user.Id, genericProvider.GetProviderId(), oauthUser.ProviderUserID)
				}
				if taken, err := model.IsOAuthIdentityTakenWithTx(identityTx, updateField, oauthUser.ProviderUserID, user.Id); err != nil {
					return err
				} else if taken {
					return model.ErrOAuthIdentityAlreadyTaken
				}
				if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && strings.TrimSpace(legacyID) != "" {
					if taken, err := model.IsOAuthIdentityTakenWithTx(identityTx, updateField, legacyID, user.Id); err != nil {
						return err
					} else if taken {
						return model.ErrOAuthIdentityAlreadyTaken
					}
				}

				// Built-in provider: update the user identity and durable cache fence
				// in the same transaction as AuthFlow consumption.
				provider.SetProviderUserID(&user, oauthUser.ProviderUserID)
				var updateErr error
				cacheTask, updateErr = user.UpdateFieldsWithTxAndCache(identityTx, false, updateField)
				return updateErr
			})
			return consumeErr
		})
	})
	if err != nil {
		if errors.Is(err, model.ErrOAuthIdentityAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
			return
		}
		handleAuthFlowConsumeError(c, err)
		return
	}
	model.DispatchStagedCacheInvalidation(cacheTask)
	clearOAuthSessionState(c)

	common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{
		"action": "bind",
	})
}

// findOrCreateOAuthUser finds existing user or creates new user
func findOrCreateOAuthUser(c *gin.Context, provider oauth.Provider, oauthUser *oauth.OAuthUser, affiliateCode string) (*model.User, error) {
	user := &model.User{}

	// Check if user already exists with new ID
	taken, err := provider.IsUserIDTaken(oauthUser.ProviderUserID)
	if err != nil {
		return nil, &oauthIdentityLookupError{provider: provider.GetName(), err: err}
	}
	if taken {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if err != nil {
			if errors.Is(err, model.ErrUserDeleted) {
				return nil, &OAuthUserDeletedError{}
			}
			return nil, &oauthIdentityLookupError{provider: provider.GetName(), err: err}
		}
		// Check if user has been deleted
		if user.Id == 0 {
			return nil, &OAuthUserDeletedError{}
		}
		return user, nil
	}

	// Try to find user with legacy ID (for GitHub migration from login to numeric ID)
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		legacyTaken, lookupErr := provider.IsUserIDTaken(legacyID)
		if lookupErr != nil {
			return nil, &oauthIdentityLookupError{provider: provider.GetName(), err: lookupErr}
		}
		if legacyTaken {
			err := provider.FillUserByProviderID(user, legacyID)
			if err != nil {
				if errors.Is(err, model.ErrUserDeleted) {
					return nil, &OAuthUserDeletedError{}
				}
				return nil, &oauthIdentityLookupError{provider: provider.GetName(), err: err}
			}
			if user.Id != 0 {
				// Found user with legacy ID, migrate to new ID
				common.SysLog(fmt.Sprintf("[OAuth] Migrating user %d from legacy_id=%s to new_id=%s",
					user.Id, legacyID, oauthUser.ProviderUserID))
				if err := user.UpdateGitHubId(oauthUser.ProviderUserID); err != nil {
					common.SysError(fmt.Sprintf("[OAuth] Failed to migrate user %d: %s", user.Id, err.Error()))
					// Continue with login even if migration fails
				}
				return user, nil
			}
		}
	}

	// User doesn't exist, create new user if registration is enabled
	if !common.RegisterEnabled {
		return nil, &OAuthRegistrationDisabledError{}
	}

	// Set up new user
	user.Username = provider.GetProviderPrefix() + strconv.Itoa(model.GetMaxUserId()+1)

	if oauthUser.Username != "" {
		if exists, err := model.CheckUserExistOrDeleted(oauthUser.Username, ""); err == nil && !exists {
			// 防止索引退化
			if len(oauthUser.Username) <= model.UserNameMaxLength {
				user.Username = oauthUser.Username
			}
		}
	}

	if oauthUser.DisplayName != "" {
		user.DisplayName = oauthUser.DisplayName
	} else if oauthUser.Username != "" {
		user.DisplayName = oauthUser.Username
	} else {
		user.DisplayName = provider.GetName() + " User"
	}
	if oauthUser.Email != "" {
		user.Email = model.NormalizeEmail(oauthUser.Email)
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				return nil, &OAuthEmailAlreadyTakenError{}
			}
			return nil, err
		}
	}
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled

	// Handle affiliate code
	inviterId := 0
	if affiliateCode != "" {
		inviterId, _ = model.GetUserIdByAffCode(affiliateCode)
	}

	// Use transaction to ensure user creation and OAuth binding are atomic
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: create user and binding in a transaction
		err := model.WithNormalizedEmailWriteTx(user.Email, func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}

			// Create OAuth binding
			binding := &model.UserOAuthBinding{
				UserId:         user.Id,
				ProviderId:     genericProvider.GetProviderId(),
				ProviderUserId: oauthUser.ProviderUserID,
			}
			if err := model.CreateUserOAuthBindingWithTx(tx, binding); err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks (logs, sidebar config, inviter rewards)
		user.FinalizeOAuthUserCreation(inviterId)
	} else {
		// Built-in provider: create user and update provider ID in a transaction
		err := model.WithUserOAuthIdentityWriteTx(user.Email, func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}

			// Set the provider user ID on the user model and update
			provider.SetProviderUserID(user, oauthUser.ProviderUserID)
			if err := user.ValidateOAuthIdentityLengths(); err != nil {
				return err
			}
			if err := tx.Model(user).Updates(map[string]interface{}{
				"github_id":   user.GitHubId,
				"discord_id":  user.DiscordId,
				"oidc_id":     user.OidcId,
				"linux_do_id": user.LinuxDOId,
				"wechat_id":   user.WeChatId,
				"telegram_id": user.TelegramId,
			}).Error; err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks
		user.FinalizeOAuthUserCreation(inviterId)
	}

	return user, nil
}

// Error types for OAuth
type OAuthUserDeletedError struct{}

func (e *OAuthUserDeletedError) Error() string {
	return "user has been deleted"
}

type OAuthRegistrationDisabledError struct{}

func (e *OAuthRegistrationDisabledError) Error() string {
	return "registration is disabled"
}

type OAuthEmailAlreadyTakenError struct{}

func (e *OAuthEmailAlreadyTakenError) Error() string {
	return "email is already in use"
}

// handleOAuthError handles OAuth errors and returns translated message
func handleOAuthError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *oauth.OAuthError:
		if e.Params != nil {
			common.ApiErrorI18n(c, e.MsgKey, e.Params)
		} else {
			common.ApiErrorI18n(c, e.MsgKey)
		}
	case *oauth.AccessDeniedError:
		common.ApiErrorMsg(c, e.Message)
	case *oauth.TrustLevelError:
		common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
	default:
		common.ApiError(c, err)
	}
}

func handleAuthFlowConsumeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrAuthFlowInvalid),
		errors.Is(err, model.ErrAuthFlowExpired),
		errors.Is(err, model.ErrAuthFlowConsumed):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
	default:
		common.ApiError(c, err)
	}
}

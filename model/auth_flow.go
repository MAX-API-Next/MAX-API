package model

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AuthFlowPurposeOAuth             = "oauth"
	AuthFlowPurposeTwoFALogin        = "2fa_login"
	AuthFlowPurposePasskeyLogin      = "passkey_login"
	AuthFlowPurposePasskeyRegister   = "passkey_register"
	AuthFlowPurposePasskeyStepUp     = "passkey_step_up"
	AuthFlowPurposeTelegramBind      = "telegram_bind"
	AuthFlowPurposeTelegramAssertion = "telegram_assertion"
	AuthFlowIntentLogin              = "login"
	AuthFlowIntentBind               = "bind"
	AuthFlowTokenBytes               = 32
	AuthFlowDefaultCleanupRetention  = 24 * time.Hour

	SecureVerificationMethod2FA      = "2fa"
	SecureVerificationMethodPasskey  = "passkey"
	SecureVerificationMethodPassword = "password"
	SecureVerificationMethodOAuth    = "oauth"

	SecureVerificationScopeAccessToken           = "access_token"
	SecureVerificationScopeAccountDelete         = "account_delete"
	SecureVerificationScopeCredentials           = "credentials"
	SecureVerificationScopeAPIToken              = "api_token"
	SecureVerificationScopeOAuthReauthentication = "oauth_reauthentication"
	SecureVerificationScopePasskeyRegister       = "passkey_register"
)

var (
	ErrAuthFlowInvalid  = errors.New("auth flow is invalid")
	ErrAuthFlowExpired  = errors.New("auth flow has expired")
	ErrAuthFlowConsumed = errors.New("auth flow has already been consumed")
)

// AuthFlow stores one-time authentication state. The opaque token is never persisted.
type AuthFlow struct {
	Id         int64      `json:"id" gorm:"primaryKey"`
	TokenHash  string     `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	Purpose    string     `json:"purpose" gorm:"type:varchar(32);not null;index:idx_auth_flow_purpose_expiry"`
	Provider   string     `json:"provider,omitempty" gorm:"type:varchar(64)"`
	Intent     string     `json:"intent,omitempty" gorm:"type:varchar(16)"`
	UserId     int        `json:"user_id,omitempty" gorm:"index"`
	SessionId  string     `json:"session_id,omitempty" gorm:"type:varchar(64);index"`
	Payload    string     `json:"-" gorm:"type:text"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at" gorm:"not null;index:idx_auth_flow_purpose_expiry"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty" gorm:"index"`
}

func (AuthFlow) TableName() string { return "auth_flows" }

type AuthFlowCreate struct {
	Purpose   string
	Provider  string
	Intent    string
	UserId    int
	SessionId string
	Payload   string
	ExpiresAt time.Time
}

type AuthFlowMatch struct {
	Purpose   string
	Provider  string
	Intent    string
	UserId    int
	SessionId string
}

func authFlowTokenHash(token string) string {
	return common.GenerateHMACWithKey([]byte("auth-flow-v1:"+common.SessionSecret), token)
}

func applyAuthFlowMatch(query *gorm.DB, token string, match AuthFlowMatch) *gorm.DB {
	query = query.Where("token_hash = ? AND purpose = ?", authFlowTokenHash(token), match.Purpose)
	if match.Provider != "" {
		query = query.Where("provider = ?", match.Provider)
	}
	if match.Intent != "" {
		query = query.Where("intent = ?", match.Intent)
	}
	if match.UserId != 0 {
		query = query.Where("user_id = ?", match.UserId)
	}
	if match.SessionId != "" {
		query = query.Where("session_id = ?", match.SessionId)
	}
	return query
}

func generateAuthFlowToken(randomReader io.Reader) (string, error) {
	random := make([]byte, AuthFlowTokenBytes)
	if _, err := io.ReadFull(randomReader, random); err != nil {
		return "", fmt.Errorf("generate auth flow token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func CreateAuthFlow(input AuthFlowCreate) (string, *AuthFlow, error) {
	if strings.TrimSpace(input.Purpose) == "" || input.ExpiresAt.IsZero() || !input.ExpiresAt.After(time.Now()) {
		return "", nil, ErrAuthFlowInvalid
	}
	token, err := generateAuthFlowToken(rand.Reader)
	if err != nil {
		return "", nil, err
	}
	flow := &AuthFlow{
		TokenHash: authFlowTokenHash(token),
		Purpose:   input.Purpose,
		Provider:  input.Provider,
		Intent:    input.Intent,
		UserId:    input.UserId,
		SessionId: input.SessionId,
		Payload:   input.Payload,
		ExpiresAt: input.ExpiresAt,
	}
	if err := DB.Create(flow).Error; err != nil {
		return "", nil, err
	}
	return token, flow, nil
}

func GetAuthFlow(token string, match AuthFlowMatch) (*AuthFlow, error) {
	return getAuthFlowWithDB(DB, token, match)
}

func getAuthFlowWithDB(db *gorm.DB, token string, match AuthFlowMatch) (*AuthFlow, error) {
	if token == "" || match.Purpose == "" {
		return nil, ErrAuthFlowInvalid
	}
	var flow AuthFlow
	if err := applyAuthFlowMatch(db, token, match).First(&flow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAuthFlowInvalid
		}
		return nil, err
	}
	if flow.ConsumedAt != nil {
		return nil, ErrAuthFlowConsumed
	}
	if !flow.ExpiresAt.After(time.Now()) {
		return nil, ErrAuthFlowExpired
	}
	return &flow, nil
}

func ConsumeAuthFlow(token string, match AuthFlowMatch) (*AuthFlow, error) {
	return consumeAuthFlowWithDB(DB, token, match)
}

// ConsumeAuthFlowWithAction atomically consumes a flow and executes action in
// the same database transaction. If action fails, the flow remains usable.
// Optional match fields are enforced when non-zero, including SessionId.
func ConsumeAuthFlowWithAction(token string, match AuthFlowMatch, action func(tx *gorm.DB, flow *AuthFlow) error) (*AuthFlow, error) {
	if token == "" || match.Purpose == "" || DB == nil {
		return nil, ErrAuthFlowInvalid
	}
	var flow AuthFlow
	err := DB.Transaction(func(tx *gorm.DB) error {
		consumed, err := ConsumeAuthFlowWithActionTx(tx, token, match, action)
		if consumed != nil {
			flow = *consumed
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &flow, nil
}

// ConsumeAuthFlowWithActionTx consumes a flow and executes its action inside
// an existing transaction. The caller owns transaction begin/commit, which
// allows an outer connection-level lock to remain held until commit.
func ConsumeAuthFlowWithActionTx(tx *gorm.DB, token string, match AuthFlowMatch, action func(tx *gorm.DB, flow *AuthFlow) error) (*AuthFlow, error) {
	if tx == nil || token == "" || match.Purpose == "" {
		return nil, ErrAuthFlowInvalid
	}
	var flow AuthFlow
	query := lockAuthFlowQuery(applyAuthFlowMatch(tx, token, match))
	if err := query.First(&flow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAuthFlowInvalid
		}
		return nil, err
	}
	if flow.ConsumedAt != nil {
		return nil, ErrAuthFlowConsumed
	}
	now := time.Now()
	if !flow.ExpiresAt.After(now) {
		return nil, ErrAuthFlowExpired
	}
	result := tx.Model(&AuthFlow{}).
		Where("id = ? AND consumed_at IS NULL AND expires_at > ?", flow.Id, now).
		Update("consumed_at", now)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrAuthFlowConsumed
	}
	flow.ConsumedAt = &now
	if action != nil {
		if err := action(tx, &flow); err != nil {
			return nil, err
		}
	}
	return &flow, nil
}

// ClaimExternalAuthAssertion atomically records a provider assertion as
// consumed. The assertion itself is never persisted; its HMAC is protected by
// the unique token_hash index so replay is rejected on all supported databases.
func ClaimExternalAuthAssertion(purpose, assertion string, expiresAt time.Time) error {
	if DB == nil {
		return ErrAuthFlowInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return ClaimExternalAuthAssertionWithTx(tx, purpose, assertion, expiresAt)
	})
}

// ClaimExternalAuthAssertionWithTx performs the assertion claim in a caller
// transaction so replay protection can commit with the resulting state change.
func ClaimExternalAuthAssertionWithTx(tx *gorm.DB, purpose, assertion string, expiresAt time.Time) error {
	purpose = strings.TrimSpace(purpose)
	assertion = strings.TrimSpace(assertion)
	now := time.Now()
	if tx == nil || purpose == "" || assertion == "" || !expiresAt.After(now) {
		return ErrAuthFlowInvalid
	}
	claimToken := uuid.NewString()
	flow := AuthFlow{
		TokenHash:  authFlowTokenHash("external:" + purpose + ":" + assertion),
		Purpose:    purpose,
		Payload:    claimToken,
		ExpiresAt:  expiresAt,
		ConsumedAt: &now,
	}
	dialect := ""
	if tx.Dialector != nil {
		dialect = tx.Dialector.Name()
	}
	result := tx.Clauses(authFlowAssertionConflictClause(dialect)).Create(&flow)
	if result.Error != nil {
		return result.Error
	}
	claimed, err := ownsExternalAuthAssertionClaim(tx, dialect, flow.TokenHash, claimToken, result.RowsAffected)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrAuthFlowConsumed
	}
	return nil
}

func ownsExternalAuthAssertionClaim(tx *gorm.DB, dialect, tokenHash, claimToken string, rowsAffected int64) (bool, error) {
	if dialect != "mysql" {
		return rowsAffected == 1, nil
	}

	// MySQL may report a duplicate-key no-op as affected when clientFoundRows
	// is enabled. The random payload proves that this attempt inserted the row.
	var stored AuthFlow
	if err := tx.Select("payload").Where("token_hash = ?", tokenHash).Take(&stored).Error; err != nil {
		return false, err
	}
	return stored.Payload == claimToken, nil
}

func authFlowAssertionConflictClause(dialect string) clause.OnConflict {
	conflict := clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_hash"}},
		DoNothing: true,
	}
	if dialect == "mysql" {
		// gorm.io/driver/mysql v1.4.3 translates DoNothing into
		// ON DUPLICATE KEY UPDATE id=id. Avoid touching AuthFlow.Id because
		// MySQL can reject that auto-increment no-op with error 1869.
		conflict.DoNothing = false
		conflict.DoUpdates = clause.AssignmentColumns([]string{"token_hash"})
	}
	return conflict
}

func lockAuthFlowQuery(query *gorm.DB) *gorm.DB {
	if query == nil || query.Dialector == nil || query.Dialector.Name() == "sqlite" {
		return query
	}
	return query.Clauses(clause.Locking{Strength: "UPDATE"})
}

func consumeAuthFlowWithDB(db *gorm.DB, token string, match AuthFlowMatch) (*AuthFlow, error) {
	return ConsumeAuthFlowWithActionTx(db, token, match, nil)
}

func DeleteExpiredAuthFlows(now time.Time) error {
	cutoff := now.Add(-AuthFlowDefaultCleanupRetention)
	return DB.Where("expires_at < ? OR (consumed_at IS NOT NULL AND consumed_at < ?)", cutoff, cutoff).
		Delete(&AuthFlow{}).Error
}

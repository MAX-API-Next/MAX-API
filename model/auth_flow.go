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
	"gorm.io/gorm"
)

const (
	AuthFlowPurposeOAuth            = "oauth"
	AuthFlowIntentLogin             = "login"
	AuthFlowIntentBind              = "bind"
	AuthFlowTokenBytes              = 32
	AuthFlowDefaultCleanupRetention = 24 * time.Hour
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
	Payload   string
	ExpiresAt time.Time
}

type AuthFlowMatch struct {
	Purpose  string
	Provider string
	Intent   string
	UserId   int
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
		Payload:   input.Payload,
		ExpiresAt: input.ExpiresAt,
	}
	if err := DB.Create(flow).Error; err != nil {
		return "", nil, err
	}
	return token, flow, nil
}

func GetAuthFlow(token string, match AuthFlowMatch) (*AuthFlow, error) {
	if token == "" || match.Purpose == "" {
		return nil, ErrAuthFlowInvalid
	}
	var flow AuthFlow
	if err := applyAuthFlowMatch(DB, token, match).First(&flow).Error; err != nil {
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
	flow, err := GetAuthFlow(token, match)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	result := DB.Model(&AuthFlow{}).
		Where("id = ? AND consumed_at IS NULL AND expires_at > ?", flow.Id, now).
		Update("consumed_at", now)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrAuthFlowConsumed
	}
	flow.ConsumedAt = &now
	return flow, nil
}

func DeleteExpiredAuthFlows(now time.Time) error {
	cutoff := now.Add(-AuthFlowDefaultCleanupRetention)
	return DB.Where("expires_at < ? OR (consumed_at IS NOT NULL AND consumed_at < ?)", cutoff, cutoff).
		Delete(&AuthFlow{}).Error
}

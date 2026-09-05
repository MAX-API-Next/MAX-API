package model

import (
	"time"

	"gorm.io/gorm"
)

// AuthzUserOverride stores one explicit permission decision for a user.
// Absence means that the user's role baseline applies; Allowed=false is an
// explicit deny and therefore must not be represented by a nullable/omitted
// field.
type AuthzUserOverride struct {
	ID        int64  `gorm:"primaryKey"`
	UserID    int    `gorm:"not null;uniqueIndex:ux_authz_user_permission,priority:1;index"`
	Resource  string `gorm:"type:varchar(64);not null;uniqueIndex:ux_authz_user_permission,priority:2"`
	Action    string `gorm:"type:varchar(64);not null;uniqueIndex:ux_authz_user_permission,priority:3"`
	Allowed   bool   `gorm:"not null"`
	CreatedAt int64  `gorm:"not null"`
	UpdatedAt int64  `gorm:"not null"`
}

func (AuthzUserOverride) TableName() string {
	return "authz_user_overrides"
}

func (override *AuthzUserOverride) BeforeCreate(_ *gorm.DB) error {
	if override.CreatedAt == 0 {
		override.CreatedAt = time.Now().Unix()
	}
	if override.UpdatedAt == 0 {
		override.UpdatedAt = override.CreatedAt
	}
	return nil
}

func (override *AuthzUserOverride) BeforeUpdate(_ *gorm.DB) error {
	override.UpdatedAt = time.Now().Unix()
	return nil
}

// DeleteAuthzUserOverridesTx removes authorization overrides as part of the
// owning user's deletion transaction.
func DeleteAuthzUserOverridesTx(tx *gorm.DB, userID int) error {
	if tx == nil {
		return gorm.ErrInvalidTransaction
	}
	if userID <= 0 {
		return gorm.ErrInvalidData
	}
	return tx.Where("user_id = ?", userID).Delete(&AuthzUserOverride{}).Error
}

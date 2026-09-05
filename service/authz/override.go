package authz

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ReplaceUserOverrides(userID int, permissions PermissionsMap) error {
	if model.DB == nil {
		return errors.New("database is not initialized")
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		return replaceUserOverridesInTx(tx, userID, permissions)
	}); err != nil {
		return err
	}
	InvalidateUser(userID)
	return nil
}

func replaceUserOverridesInTx(tx *gorm.DB, userID int, permissions PermissionsMap) error {
	if userID <= 0 {
		return errors.New("user id is required")
	}
	rows := make([]model.AuthzUserOverride, 0)
	for resource, actions := range permissions {
		for actionName, allowed := range actions {
			permission := Permission{Resource: resource, Action: actionName}
			if !isKnownPermission(permission) {
				return fmt.Errorf("%w: %s.%s", ErrUnknownPermission, resource, actionName)
			}
			rows = append(rows, model.AuthzUserOverride{
				UserID:    userID,
				Resource:  resource,
				Action:    actionName,
				Allowed:   allowed,
				CreatedAt: time.Now().Unix(),
				UpdatedAt: time.Now().Unix(),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Resource == rows[j].Resource {
			return rows[i].Action < rows[j].Action
		}
		return rows[i].Resource < rows[j].Resource
	})
	if err := tx.Where("user_id = ?", userID).Delete(&model.AuthzUserOverride{}).Error; err != nil {
		return err
	}
	if len(rows) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return err
		}
	}
	return nil
}

func ReplaceUserOverridesInTx(tx *gorm.DB, userID int, permissions PermissionsMap) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	return replaceUserOverridesInTx(tx, userID, permissions)
}

func ClearUserOverrides(userID int) error {
	if model.DB == nil {
		return errors.New("database is not initialized")
	}
	if userID <= 0 {
		return errors.New("user id is required")
	}
	if err := model.DB.Where("user_id = ?", userID).Delete(&model.AuthzUserOverride{}).Error; err != nil {
		return err
	}
	InvalidateUser(userID)
	return nil
}

package model

import (
	"errors"

	"github.com/MAX-API-Next/MAX-API/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func withRowLock(tx *gorm.DB) *gorm.DB {
	if tx == nil || common.UsingSQLite {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

func ensureUserUpdateMatchedTx(tx *gorm.DB, result *gorm.DB, userId int, missingErr error) error {
	if result == nil {
		return missingErr
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	if tx == nil || userId == 0 {
		return missingErr
	}

	var user User
	err := tx.Select("id").Where("id = ?", userId).First(&user).Error
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return missingErr
	}
	return err
}

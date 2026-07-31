package model

import (
	"fmt"

	"github.com/MAX-API-Next/MAX-API/common"

	"gorm.io/gorm"
)

const (
	vendorNameKeyIndex       = "uk_vendors_active_name_key"
	modelNameKeyIndex        = "uk_models_active_name_key"
	legacyVendorNameKeyIndex = "uk_vendor_name_delete_at"
	legacyModelNameKeyIndex  = "uk_model_name_delete_at"
)

func activeMetadataNameKey(name string) string {
	return name
}

func retiredMetadataNameKey(kind string, id int) string {
	return fmt.Sprintf("deleted:%s:%d", kind, id)
}

func softDeleteMetadataWithNameKey(tx *gorm.DB, entity interface{}, id int, nameKey string) error {
	result := tx.Model(entity).Where("id = ?", id).UpdateColumn("name_key", nameKey)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return tx.Delete(entity, id).Error
}

// migrateMetadataNameKeys replaces the nullable deleted_at composite indexes
// with non-null active-name keys. MySQL and PostgreSQL treat NULLs as distinct
// in unique indexes, so the old index cannot prevent concurrent active names.
func migrateMetadataNameKeys() error {
	if DB == nil {
		return nil
	}
	if common.UsingMySQL {
		if err := backfillMetadataNameKeys(DB); err != nil {
			return err
		}
		if err := ensureMetadataNameKeyIndexes(DB); err != nil {
			return err
		}
		return dropLegacyMetadataNameIndexes(DB)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := backfillMetadataNameKeys(tx); err != nil {
			return err
		}
		if err := ensureMetadataNameKeyIndexes(tx); err != nil {
			return err
		}
		return dropLegacyMetadataNameIndexes(tx)
	})
}

func backfillMetadataNameKeys(tx *gorm.DB) error {
	var vendors []Vendor
	if err := tx.Unscoped().Find(&vendors).Error; err != nil {
		return fmt.Errorf("load vendors for name-key migration: %w", err)
	}
	for _, vendor := range vendors {
		nameKey := activeMetadataNameKey(vendor.Name)
		if vendor.DeletedAt.Valid {
			nameKey = retiredMetadataNameKey("vendor", vendor.Id)
		}
		if vendor.NameKey == nameKey {
			continue
		}
		if err := tx.Unscoped().Model(&Vendor{}).Where("id = ?", vendor.Id).UpdateColumn("name_key", nameKey).Error; err != nil {
			return fmt.Errorf("backfill vendor %d name key: %w", vendor.Id, err)
		}
	}

	var models []Model
	if err := tx.Unscoped().Find(&models).Error; err != nil {
		return fmt.Errorf("load models for name-key migration: %w", err)
	}
	for _, model := range models {
		nameKey := activeMetadataNameKey(model.ModelName)
		if model.DeletedAt.Valid {
			nameKey = retiredMetadataNameKey("model", model.Id)
		}
		if model.NameKey == nameKey {
			continue
		}
		if err := tx.Unscoped().Model(&Model{}).Where("id = ?", model.Id).UpdateColumn("name_key", nameKey).Error; err != nil {
			return fmt.Errorf("backfill model %d name key: %w", model.Id, err)
		}
	}
	return nil
}

func ensureMetadataNameKeyIndexes(db *gorm.DB) error {
	if !db.Migrator().HasIndex(&Vendor{}, vendorNameKeyIndex) {
		if err := createUniqueIndex(db, "vendors", vendorNameKeyIndex, "name_key"); err != nil {
			return fmt.Errorf("create vendor active-name unique index: %w", err)
		}
	}
	if !db.Migrator().HasIndex(&Model{}, modelNameKeyIndex) {
		if err := createUniqueIndex(db, "models", modelNameKeyIndex, "name_key"); err != nil {
			return fmt.Errorf("create model active-name unique index: %w", err)
		}
	}
	return nil
}

func dropLegacyMetadataNameIndexes(db *gorm.DB) error {
	if db.Migrator().HasIndex(&Vendor{}, legacyVendorNameKeyIndex) {
		if err := db.Migrator().DropIndex(&Vendor{}, legacyVendorNameKeyIndex); err != nil {
			return fmt.Errorf("drop legacy vendor name unique index: %w", err)
		}
	}
	if db.Migrator().HasIndex(&Model{}, legacyModelNameKeyIndex) {
		if err := db.Migrator().DropIndex(&Model{}, legacyModelNameKeyIndex); err != nil {
			return fmt.Errorf("drop legacy model name unique index: %w", err)
		}
	}
	return nil
}

func createUniqueIndex(db *gorm.DB, table, index, column string) error {
	return db.Exec(fmt.Sprintf(
		"CREATE UNIQUE INDEX %s ON %s (%s)",
		quoteDBIdentifier(index),
		quoteDBIdentifier(table),
		quoteDBIdentifier(column),
	)).Error
}

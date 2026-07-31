package model

import (
	"errors"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMetadataNameKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := DB
	previousSQLite := common.UsingSQLite
	previousMySQL := common.UsingMySQL
	previousPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		DB = previousDB
		common.UsingSQLite = previousSQLite
		common.UsingMySQL = previousMySQL
		common.UsingPostgreSQL = previousPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()
	require.NoError(t, db.AutoMigrate(&Vendor{}, &Model{}))
	return db
}

func TestMetadataNameKeyMigrationEnforcesActiveUniquenessAndAllowsReuseAfterDelete(t *testing.T) {
	db := setupMetadataNameKeyTestDB(t)

	vendor := &Vendor{Name: "OpenAI", Status: 1}
	require.NoError(t, vendor.Insert())
	model := &Model{ModelName: "gpt-test", VendorID: vendor.Id, Status: 1, SyncOfficial: 1}
	require.NoError(t, model.Insert())
	require.NoError(t, db.Model(&Vendor{}).Where("id = ?", vendor.Id).UpdateColumn("name_key", "").Error)
	require.NoError(t, db.Model(&Model{}).Where("id = ?", model.Id).UpdateColumn("name_key", "").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX "+legacyVendorNameKeyIndex+" ON vendors (name, deleted_at)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX "+legacyModelNameKeyIndex+" ON models (model_name, deleted_at)").Error)

	require.NoError(t, migrateMetadataNameKeys())
	require.True(t, db.Migrator().HasIndex(&Vendor{}, vendorNameKeyIndex))
	require.True(t, db.Migrator().HasIndex(&Model{}, modelNameKeyIndex))
	require.False(t, db.Migrator().HasIndex(&Vendor{}, legacyVendorNameKeyIndex))
	require.False(t, db.Migrator().HasIndex(&Model{}, legacyModelNameKeyIndex))

	duplicateVendor := &Vendor{Name: "OpenAI", Status: 1}
	require.Error(t, duplicateVendor.Insert())
	duplicateModel := &Model{ModelName: "gpt-test", Status: 1, SyncOfficial: 1}
	require.Error(t, duplicateModel.Insert())

	require.NoError(t, model.Delete())
	secondModel := &Model{ModelName: "gpt-test", Status: 1, SyncOfficial: 1}
	require.NoError(t, secondModel.Insert())

	require.NoError(t, secondModel.Delete())
	require.NoError(t, vendor.Delete())
	secondVendor := &Vendor{Name: "OpenAI", Status: 1}
	require.NoError(t, secondVendor.Insert())
}

func TestMetadataNameKeyMigrationRetiresDuplicateActiveKeysDeterministically(t *testing.T) {
	db := setupMetadataNameKeyTestDB(t)

	firstVendor := &Vendor{Name: "Duplicate Vendor", Status: 1}
	secondVendor := &Vendor{Name: "Duplicate Vendor", Status: 1}
	require.NoError(t, db.Create(firstVendor).Error)
	require.NoError(t, db.Create(secondVendor).Error)
	firstModel := &Model{ModelName: "duplicate-model", Status: 1, SyncOfficial: 1}
	secondModel := &Model{ModelName: "duplicate-model", Status: 1, SyncOfficial: 1}
	require.NoError(t, db.Create(firstModel).Error)
	require.NoError(t, db.Create(secondModel).Error)

	require.NoError(t, migrateMetadataNameKeys())

	var vendors []Vendor
	require.NoError(t, db.Order("id ASC").Find(&vendors).Error)
	require.Len(t, vendors, 2)
	require.Equal(t, "Duplicate Vendor", vendors[0].NameKey)
	require.Equal(t, retiredMetadataNameKey("vendor", vendors[1].Id), vendors[1].NameKey)

	var models []Model
	require.NoError(t, db.Order("id ASC").Find(&models).Error)
	require.Len(t, models, 2)
	require.Equal(t, "duplicate-model", models[0].NameKey)
	require.Equal(t, retiredMetadataNameKey("model", models[1].Id), models[1].NameKey)
}

func TestModelDeleteRollsBackNameKeyChangeWhenDeleteFails(t *testing.T) {
	db := setupMetadataNameKeyTestDB(t)
	model := &Model{ModelName: "rollback-model", Status: 1, SyncOfficial: 1}
	require.NoError(t, model.Insert())
	require.NoError(t, migrateMetadataNameKeys())
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_model_soft_delete BEFORE UPDATE OF deleted_at ON models WHEN NEW.deleted_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'delete blocked'); END`).Error)

	require.Error(t, model.Delete())
	var stored Model
	require.NoError(t, db.Unscoped().First(&stored, model.Id).Error)
	require.Equal(t, "rollback-model", stored.NameKey)
	require.False(t, stored.DeletedAt.Valid)
}

func TestVendorDeleteRejectsReferencedModels(t *testing.T) {
	setupMetadataNameKeyTestDB(t)
	vendor := &Vendor{Name: "Referenced Vendor", Status: 1}
	require.NoError(t, vendor.Insert())
	model := &Model{ModelName: "referenced-model", VendorID: vendor.Id, Status: 1, SyncOfficial: 1}
	require.NoError(t, model.Insert())

	err := vendor.Delete()
	require.True(t, errors.Is(err, ErrVendorHasModels))

	var count int64
	require.NoError(t, DB.Model(&Vendor{}).Where("id = ?", vendor.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

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

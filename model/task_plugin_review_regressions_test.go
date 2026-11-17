// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestTaskPluginDeletedModelIndexCapacity(t *testing.T) {
	field, ok := any((TaskPluginDeletion{}).Models).(interface {
		GormDBDataType(*gorm.DB, *schema.Field) string
	})
	require.True(t, ok, "the model index needs dialect-aware capacity, not MySQL TEXT")
	for dialect, expected := range map[string]string{"mysql": "longtext", "sqlite": "text", "postgres": "text"} {
		db := &gorm.DB{Config: &gorm.Config{Dialector: taskPluginTestDialect{name: dialect}}}
		require.Equal(t, expected, field.GormDBDataType(db, nil))
	}
	db := deletionDB(t)
	names := make([]string, 300)
	for i := range names {
		names[i] = fmt.Sprintf("%03d-", i) + strings.Repeat("x", 236)
	}
	names[0] = "模型😀-" + strings.Repeat("x", 230)
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "wide", Name: "wide", Version: "1.0.0", Models: names}})
	require.NoError(t, err)
	require.Less(t, len(document), taskplugin.MaxPackageBytes)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
	require.NoError(t, err)
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "wide", Version: "1.0.0"}))
	var receipt TaskPluginDeletion
	require.NoError(t, db.First(&receipt).Error)
	require.Greater(t, len(receipt.Models), 65535)
	var stored string
	require.NoError(t, db.Model(&TaskPluginDeletion{}).Select("models").Scan(&stored).Error)
	require.True(t, strings.HasPrefix(stored, taskPluginDocumentPrefix))
	require.NotRegexp(t, `[^\x00-\x7F]`, stored, "the index remains storable in legacy MySQL character sets")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, catalog.MutateExpected(t.Context(), 3, taskplugin.ManagementMutation{Action: "delete", Key: "wide", Version: "1.0.0"}))
	require.NoError(t, db.Model(&TaskPluginDeletion{}).Select("models").Scan(&stored).Error)
	require.True(t, strings.HasPrefix(stored, taskPluginDocumentPrefix), "repeated deletion must keep the encoded index")
	require.NotRegexp(t, `[^\x00-\x7F]`, stored)
	var legacy taskPluginModelIndex
	require.NoError(t, legacy.Scan(string(receipt.Models)))
	require.Equal(t, receipt.Models, legacy, "older raw JSON model indexes remain readable")
	require.NoError(t, legacy.Scan(nil), "a newly migrated legacy row may have a NULL index")
	require.Empty(t, legacy)
	require.NoError(t, db.Model(&TaskPluginDeletion{}).Where("identity = ?", receipt.Identity).UpdateColumn("models", nil).Error)
	var oldRow TaskPluginDeletion
	require.NoError(t, db.First(&oldRow).Error)
	require.Empty(t, oldRow.Models)
}

func TestTaskPluginDeletionRejectsOversizedEscapedModelIndex(t *testing.T) {
	db := deletionDB(t)
	name := strings.Repeat("<", 200000)
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: "source", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "escaped", Name: "escaped", Version: "1.0.0", Models: []string{name}}})
	require.NoError(t, err)
	document = bytes.ReplaceAll(document, []byte(`\u003c`), []byte("<"))
	require.Less(t, len(document), taskplugin.MaxPackageBytes)
	pkg, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", sha256.Sum256(document)))
	require.NoError(t, err)
	store := NewTaskPluginCatalogStore(db)
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	before, err := store.Load(t.Context())
	require.NoError(t, err)
	require.ErrorIs(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "escaped", Version: "1.0.0"}), taskplugin.ErrCatalogInvalid)
	after, err := store.Load(t.Context())
	require.NoError(t, err, "failed deletion must leave the catalog readable")
	require.Equal(t, before, after, "failed deletion must preserve source, head and receipts")
	require.NoError(t, db.Create(&Channel{Models: "unrelated"}).Error, "a rejected deletion must not poison channel writes")
}

func TestTaskPluginModelIndexCodecBounds(t *testing.T) {
	for _, size := range []int{taskplugin.MaxPackageBytes - 1, taskplugin.MaxPackageBytes, taskplugin.MaxPackageBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			index := taskPluginModelIndex(strings.Repeat("x", size))
			value, err := index.Value()
			if size > taskplugin.MaxPackageBytes {
				require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
				require.Nil(t, value)
			} else {
				require.NoError(t, err)
				var decoded taskPluginModelIndex
				require.NoError(t, decoded.Scan(value))
				require.Equal(t, index, decoded)
			}
			encoded, err := taskPluginDocument(index).Value()
			require.NoError(t, err)
			for _, raw := range []any{string(index), encoded} {
				var decoded taskPluginModelIndex
				err := decoded.Scan(raw)
				if size > taskplugin.MaxPackageBytes {
					require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
				} else {
					require.NoError(t, err)
					require.Equal(t, index, decoded)
				}
			}
		})
	}
}

func TestTaskPluginCatalogMixedDeletionReadIsConflict(t *testing.T) {
	db := deletionDB(t)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "source")
	store := NewTaskPluginCatalogStore(db)
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	injected := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("simulate_read_committed_deletion", func(tx *gorm.DB) {
		if tx.Statement.Table != db.NamingStrategy.TableName("TaskPluginCatalogHead") || injected {
			return
		}
		injected = true
		writer := tx.Session(&gorm.Session{NewDB: true})
		tx.AddError(writer.Model(&TaskPluginCatalogHead{}).Where("id = ?", 1).Updates(map[string]any{"revision": 2, "last_deletion_revision": 2, "mutation_id": "simulated-delete"}).Error)
		tx.AddError(writer.Create(&TaskPluginDeletion{Identity: taskPluginIdentity("stored", "1.0.0"), PluginKey: "stored", Version: "1.0.0", Digest: pkg.Digest()}).Error)
		tx.AddError(writer.Where("identity = ?", taskPluginIdentity("stored", "1.0.0")).Delete(&TaskPluginVersion{}).Error)
	}))
	_, err = store.Load(t.Context())
	require.True(t, injected)
	require.ErrorIs(t, err, taskplugin.ErrCatalogConflict, "mixed head/receipt observations retry rather than reporting corruption")
}

func TestTaskPluginPricingAndDeletionLockOrder(t *testing.T) {
	db := deletionDB(t)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "source")
	store := NewTaskPluginCatalogStore(db)
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	var order []string
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("record_price_writer_lock", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Option); ok && row.Key == modelPricingLockOption {
			order = append(order, "price")
		}
	}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("record_catalog_channel_locks", func(tx *gorm.DB) {
		switch tx.Statement.Table {
		case db.NamingStrategy.TableName("TaskPluginCatalogHead"):
			order = append(order, "head")
		case db.NamingStrategy.TableName("Channel"):
			order = append(order, "channel")
		case db.NamingStrategy.TableName("TaskPluginVersion"):
			if _, ok := tx.Statement.Clauses["FOR"]; !ok {
				tx.AddError(errors.New("catalog facts must use a current locking read"))
			}
		}
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := lockModelPricingRows(tx); err != nil {
			return err
		}
		authority, err := readPricingAuthority(tx, true)
		if err != nil {
			return err
		}
		require.Len(t, authority.plugins, 1)
		return nil
	}))
	index := func(name string) int {
		for i, item := range order {
			if item == name {
				return i
			}
		}
		return -1
	}
	require.NotEqual(t, -1, index("head"))
	require.NotEqual(t, -1, index("channel"))
	require.Less(t, index("price"), index("head"))
	require.Less(t, index("head"), index("channel"))
	require.NoError(t, db.Callback().Query().Remove("record_catalog_channel_locks"))
	order = nil
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("deletion_requires_price_lock_first", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginCatalogHead") && index("price") < 0 {
			tx.AddError(errors.New("deletion must lock price writer first"))
		}
	}))
	_, err = store.DeleteVersion(t.Context(), 1, pkg.Manifest().Key, pkg.Manifest().Version)
	require.NoError(t, err)
}

func TestTaskPluginDeletedPricingBindingRejectsWithoutPriceWrites(t *testing.T) {
	db := deletionDB(t)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "price-binding")
	store := NewTaskPluginCatalogStore(db)
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	beforeEntry := pricingEntryForTest(t, db, "catalog-model")
	_, err = store.DeleteVersion(t.Context(), 1, pkg.Manifest().Key, pkg.Manifest().Version)
	require.NoError(t, err)
	var before []Option
	require.NoError(t, db.Order("key").Find(&before).Error)
	result, err := updateModelPricing(db, []types.ModelPricingChange{pricingChangeForTest(beforeEntry, types.ModelPricingValues{"ModelPrice": float64(7)})})
	require.ErrorIs(t, err, ErrModelPricingConflict)
	require.Nil(t, result)
	var after []Option
	require.NoError(t, db.Order("key").Find(&after).Error)
	require.Equal(t, before, after, "a stale package binding cannot persist any price override")
}

func TestTaskPluginDeletedSourceRejectsStalePricingWrite(t *testing.T) {
	db := deletionDB(t)
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "source")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	entry := pricingEntryForTest(t, db, "catalog-model")
	require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	var before []Option
	require.NoError(t, db.Order("key").Find(&before).Error)
	_, err = updateModelPricing(db, []types.ModelPricingChange{pricingChangeForTest(entry, types.ModelPricingValues{"ModelPrice": float64(0)})})
	require.ErrorIs(t, err, ErrModelPricingConflict, "the real pricing writer must reject an old source binding, including an explicit zero draft")
	var after []Option
	require.NoError(t, db.Order("key").Find(&after).Error)
	require.Equal(t, before, after, "rejection changes neither price values nor coordination metadata")
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func TestTaskPluginSaveFallbackRetainsReferenceLock(t *testing.T) {
	for _, kind := range []string{"task", "channel", "channel_without_key"} {
		t.Run(kind, func(t *testing.T) {
			db := deletionDB(t)
			previousDB := DB
			DB = db
			t.Cleanup(func() { DB = previousDB })
			pkg, entry := catalogFixture(t, "stored", "1.0.0", "fallback-source")
			store := NewTaskPluginCatalogStore(db)
			_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
			require.NoError(t, err)
			target := db.NamingStrategy.TableName("Channel")
			if kind == "task" {
				target = db.NamingStrategy.TableName("Task")
			}
			observed, locked := false, false
			require.NoError(t, db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("observe_missing_row_fallback", func(tx *gorm.DB) {
				if tx.Statement.Table != target || tx.RowsAffected != 0 || tx.Error != nil {
					return
				}
				observed = true
				_, locked = tx.Statement.ConnPool.(gorm.TxCommitter)
				if !locked {
					_, err := store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
					tx.AddError(err)
				}
			}))
			if kind == "task" {
				task := Task{ID: 10001, TaskID: "retained", PrivateData: TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: pkg.Digest()}}}
				err = task.Update()
			} else {
				channel := Channel{Id: 10001, Models: "catalog-model"}
				if kind == "channel" {
					err = channel.Save()
				} else {
					err = channel.SaveWithoutKey()
				}
			}
			if kind == "channel_without_key" {
				require.ErrorContains(t, err, "channels.key", "missing-row fallback must preserve the NOT NULL key failure")
			} else {
				require.NoError(t, err)
			}
			require.True(t, observed, "exercise the actual zero-row update before GORM fallback insert")
			snapshot, err := store.Load(t.Context())
			require.NoError(t, err)
			require.Len(t, snapshot.Packages, 1, "a restored reference must not outlive the package deleted between Save transactions")
			require.True(t, locked, "the guarded update must retain its transaction through fallback insert")
			_, err = store.DeleteVersion(t.Context(), snapshot.Revision, "stored", "1.0.0")
			if kind == "channel_without_key" {
				require.NoError(t, err, "the failed fallback must not leave a channel reference")
			} else {
				require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
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
			require.Equal(t, "UPDATE", tx.Statement.Clauses["FOR"].Expression.(clause.Locking).Strength)
			order = append(order, "head")
		case db.NamingStrategy.TableName("Channel"):
			require.Equal(t, "UPDATE", tx.Statement.Clauses["FOR"].Expression.(clause.Locking).Strength)
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
	require.NotEqual(t, -1, index("price"))
	require.Less(t, index("price"), index("head"))
	require.Less(t, index("head"), index("channel"))
	require.NoError(t, db.Callback().Query().Remove("record_catalog_channel_locks"))
	order = nil
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("deletion_requires_price_lock_first", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginCatalogHead") {
			order = append(order, "deletion-head")
			require.Equal(t, "UPDATE", tx.Statement.Clauses["FOR"].Expression.(clause.Locking).Strength)
			if index("price") < 0 {
				tx.AddError(errors.New("deletion must lock price writer first"))
			}
		}
	}))
	_, err = store.DeleteVersion(t.Context(), 1, pkg.Manifest().Key, pkg.Manifest().Version)
	require.NoError(t, err)
	require.NotEqual(t, -1, index("deletion-head"))
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

func TestTaskPluginCatalogCASRejectsOmittedReinstalledVersion(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	pkg, first := catalogFixture(t, "stored", "1.0.0", "source")
	_, other := catalogFixture(t, "other", "1.0.0", "other-source")
	_, replacement := catalogFixture(t, "replacement", "1.0.0", "replacement-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{first, other})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg), "identical reinstall remains supported")
	before, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 3, before.Revision)
	var head TaskPluginCatalogHead
	require.NoError(t, db.First(&head, 1).Error)
	for name, proposed := range map[string][]taskplugin.StoredPackage{
		"all omitted":              nil,
		"reinstalled omitted":      {other},
		"omitted with replacement": {other, replacement},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := store.CompareAndSwap(t.Context(), before.Revision, proposed)
			require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
			after, err := store.Load(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, after, "rejected omission must preserve packages, receipts and revision")
			var actualHead TaskPluginCatalogHead
			require.NoError(t, db.First(&actualHead, 1).Error)
			require.Equal(t, head, actualHead)
		})
	}
	updated := append([]taskplugin.StoredPackage(nil), before.Packages...)
	updated[0].Enabled = false
	revision, err := store.CompareAndSwap(t.Context(), before.Revision, updated)
	require.NoError(t, err, "ordinary metadata updates keep every installed identity")
	require.EqualValues(t, 4, revision)
}

func TestTaskPluginFrozenEnvelopePersistenceAfterDeletion(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	deleted, first := catalogFixture(t, "stored", "1.0.0", "source")
	live, other := catalogFixture(t, "other", "1.0.0", "other-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{first, other})
	require.NoError(t, err)
	frozen := TaskPrivateData{BillingContext: &TaskBillingContext{PricingUsageEnvelope: &types.TaskPricingUsageEnvelope{PackageDigest: deleted.Digest()}}}
	task := Task{TaskID: "frozen-before-delete", PrivateData: frozen}
	require.NoError(t, db.Create(&task).Error)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.ErrorIs(t, err, taskplugin.ErrCatalogInUse, "committed frozen references block deletion")
	require.NoError(t, db.Delete(&task).Error)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	plain := Task{TaskID: "plain-after-delete"}
	require.NoError(t, db.Create(&plain).Error)
	raw, err := common.Marshal(frozen)
	require.NoError(t, err)
	for name, value := range map[string]any{"typed": frozen, "pointer": &frozen, "JSON string": string(raw), "JSON bytes": raw} {
		t.Run(name, func(t *testing.T) {
			err := db.Model(&Task{}).Where("id = ?", plain.ID).Updates(map[string]any{"private_data": value}).Error
			require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
		})
	}
	for _, value := range []any{Task{PrivateData: frozen}, &Task{PrivateData: frozen}} {
		require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(value).Error, taskplugin.ErrBindingRejected)
		require.NoError(t, db.Model(&Task{}).Where("id = ?", plain.ID).Omit("private_data").Updates(value).Error)
	}
	late := Task{TaskID: "frozen-after-delete", PrivateData: frozen}
	require.ErrorIs(t, db.Create(&late).Error, taskplugin.ErrBindingRejected)
	withBinding := frozen
	withBinding.PluginBinding = &types.TaskPluginBinding{PluginKey: "other", Version: "1.0.0", PackageDigest: live.Digest()}
	require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(map[string]any{"private_data": withBinding}).Error, taskplugin.ErrBindingRejected, "a live binding cannot hide a stale frozen digest")
	withPlan := TaskPrivateData{BillingContext: &TaskBillingContext{
		TaskBillingPlan:      &types.TaskBillingPlan{ExpressionPlan: &types.TaskExpressionPlan{PluginKey: "other", PluginVersion: "1.0.0", PackageDigest: live.Digest()}},
		PricingUsageEnvelope: frozen.BillingContext.PricingUsageEnvelope,
	}}
	require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(map[string]any{"private_data": withPlan}).Error, taskplugin.ErrBindingRejected, "a live plan cannot hide a stale frozen digest")
	var unchanged Task
	require.NoError(t, db.First(&unchanged, plain.ID).Error)
	require.Equal(t, TaskPrivateData{}, unchanged.PrivateData)
	var count int64
	require.NoError(t, db.Model(&Task{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "rejections must not insert a task")
	valid := TaskPrivateData{BillingContext: &TaskBillingContext{PricingUsageEnvelope: &types.TaskPricingUsageEnvelope{PackageDigest: live.Digest()}}}
	require.NoError(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(map[string]any{"private_data": valid}).Error)
	_, err = store.DeleteVersion(t.Context(), 2, "other", "1.0.0")
	require.ErrorIs(t, err, taskplugin.ErrCatalogInUse, "a still-installed frozen digest remains persistable and protects its version")
}

func TestTaskPluginReferenceUpdateDestinations(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.Callback().Update().Remove("guard_catalog_update_columns"))
	store := NewTaskPluginCatalogStore(db)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "destination-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	channel := Channel{Name: "before", Models: "ordinary"}
	task := Task{TaskID: "destination-task"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&task).Error)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	private := TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: pkg.Digest()}}
	channelMap := map[string]any{"Models": "catalog-model"}
	privateMap := map[string]any{"PrivateData": private}
	for name, destination := range map[string]any{
		"channel struct": struct{ Models string }{Models: "catalog-model"},
		"channel tagged pointer": &struct {
			Value string `gorm:"column:model_mapping"`
		}{Value: `{"alias":"catalog-model"}`},
		"channel pointer map": &channelMap,
		"task pointer map":    &privateMap,
		"task struct":         struct{ PrivateData TaskPrivateData }{PrivateData: private},
		"task tagged pointer": &struct {
			Value TaskPrivateData `gorm:"column:private_data"`
		}{Value: private},
	} {
		t.Run(name, func(t *testing.T) {
			if strings.HasPrefix(name, "channel") {
				require.ErrorIs(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Updates(destination).Error, taskplugin.ErrBindingRejected)
				require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Omit("models", "model_mapping").Updates(destination).Error)
			} else {
				require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", task.ID).Updates(destination).Error, taskplugin.ErrBindingRejected)
				require.NoError(t, db.Model(&Task{}).Where("id = ?", task.ID).Omit("private_data").Updates(destination).Error)
			}
		})
	}
	var afterChannel Channel
	var afterTask Task
	require.NoError(t, db.First(&afterChannel, channel.Id).Error)
	require.NoError(t, db.First(&afterTask, task.ID).Error)
	require.Equal(t, channel.Models, afterChannel.Models)
	require.Nil(t, afterChannel.ModelMapping)
	require.Equal(t, TaskPrivateData{}, afterTask.PrivateData)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Updates(struct{ Name string }{Name: "after"}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Select("models").Updates(struct{ Models string }{}).Error, "selected zero values are still emitted")
	require.NoError(t, db.First(&afterChannel, channel.Id).Error)
	require.Equal(t, "after", afterChannel.Name)
	require.Empty(t, afterChannel.Models)
	// Batch creates use the current hook receiver, not the whole slice.
	require.ErrorIs(t, db.Create(&[]Channel{{Models: "ordinary"}, {Models: "catalog-model"}}).Error, taskplugin.ErrBindingRejected)
	var count int64
	require.NoError(t, db.Model(&Channel{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "a rejected batch must roll back every channel")
}

func TestTaskPluginSelectedPlanKeyPersistence(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, removed := catalogFixture(t, "stored", "1.0.0", "selected-deleted")
	live, present := catalogFixture(t, "live", "1.0.0", "selected-live")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{removed, present})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	for name, plan := range map[string]*types.TaskExpressionPlan{
		"selected only":           {SelectedPluginKey: "stored"},
		"different live identity": {SelectedPluginKey: "stored", PluginKey: "live", PluginVersion: "1.0.0", PackageDigest: live.Digest()},
	} {
		t.Run(name, func(t *testing.T) {
			task := Task{TaskID: name, PrivateData: TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "live", Version: "1.0.0", PackageDigest: live.Digest()}, BillingContext: &TaskBillingContext{TaskBillingPlan: &types.TaskBillingPlan{ExpressionPlan: plan}}}}
			require.ErrorIs(t, db.Create(&task).Error, taskplugin.ErrBindingRejected, "a valid binding cannot hide a divergent selected key")
		})
	}
	valid := Task{TaskID: "selected-valid", PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{TaskBillingPlan: &types.TaskBillingPlan{ExpressionPlan: &types.TaskExpressionPlan{SelectedPluginKey: "live", PluginKey: "live", PluginVersion: "1.0.0", PackageDigest: live.Digest()}}}}}
	require.NoError(t, db.Create(&valid).Error)
	_, err = store.DeleteVersion(t.Context(), 2, "live", "1.0.0")
	require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
	ordinary := Task{TaskID: "ordinary-plan", PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{TaskBillingPlan: &types.TaskBillingPlan{ExpressionPlan: &types.TaskExpressionPlan{Driver: "go"}}}}}
	require.NoError(t, db.Create(&ordinary).Error, "plans without plugin references remain unchanged")
}

func TestTaskPluginDeletionUsesTypedJSONReferences(t *testing.T) {
	for _, shape := range []string{"binding", "plan", "envelope", "legacy array"} {
		for _, upper := range []bool{false, true} {
			if shape == "legacy array" && upper {
				continue
			}
			t.Run(fmt.Sprintf("%s/uppercase=%t", shape, upper), func(t *testing.T) {
				db := deletionDB(t)
				store := NewTaskPluginCatalogStore(db)
				pkg, entry := catalogFixture(t, "stored", "1.0.0", "json-reference-source")
				_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
				require.NoError(t, err)
				private := TaskPrivateData{}
				switch shape {
				case "binding":
					private.PluginBinding = &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: pkg.Digest()}
				case "plan":
					private.BillingContext = &TaskBillingContext{TaskBillingPlan: &types.TaskBillingPlan{ExpressionPlan: &types.TaskExpressionPlan{PluginKey: "stored", PluginVersion: "1.0.0", PackageDigest: pkg.Digest(), SelectedPluginKey: "stored"}}}
				case "envelope":
					private.BillingContext = &TaskBillingContext{PricingUsageEnvelope: &types.TaskPricingUsageEnvelope{PackageDigest: pkg.Digest()}}
				}
				raw, err := common.Marshal(private)
				require.NoError(t, err)
				if upper {
					for _, key := range []string{"plugin_binding", "plugin_key", "version", "package_digest", "billing_context", "task_billing_plan", "expression_plan", "plugin_version", "selected_plugin_key", "pricing_usage_envelope"} {
						raw = bytes.ReplaceAll(raw, []byte(`"`+key+`"`), []byte(`"`+strings.ToUpper(key)+`"`))
					}
				}
				task := Task{TaskID: "json-reference"}
				require.NoError(t, db.Create(&task).Error)
				if shape == "legacy array" {
					raw = []byte(`[{"historical":{"plugin_key":"stored","version":"1.0.0"}}]`)
					require.NoError(t, db.Model(&Task{}).Where("id = ?", task.ID).UpdateColumn("private_data", string(raw)).Error)
				} else {
					require.NoError(t, db.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{"private_data": string(raw)}).Error, "exercise the actual typed write guard without canonicalizing stored keys")
				}
				before, err := store.Load(t.Context())
				require.NoError(t, err)
				_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
				require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
				after, err := store.Load(t.Context())
				require.NoError(t, err)
				require.Equal(t, before, after, "reference rejection rolls back head, source and receipt")
			})
		}
	}
}

func TestTaskPluginDeletionOwnsSnapshotBoundary(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "snapshot-boundary-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := NewTaskPluginCatalogStore(tx).DeleteVersion(t.Context(), 1, "stored", "1.0.0")
		require.ErrorIs(t, err, taskplugin.ErrCatalogUnavailable, "deletion cannot inherit a caller's old transaction snapshot")
		return nil
	}))
	metadata := 0
	metadataBeforeHead := 0
	nonlockingBeforeHead := 0
	headLocked := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("check_deletion_read_view", func(tx *gorm.DB) {
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); !inTransaction {
			return
		}
		_, currentRead := tx.Statement.Clauses["FOR"]
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginCatalogHead") && currentRead {
			headLocked = true
		} else if !headLocked && !currentRead {
			nonlockingBeforeHead++
		}
	}))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("check_metadata_snapshot_boundary", func(tx *gorm.DB) {
		if !strings.Contains(tx.Statement.SQL.String(), "sqlite_master") {
			return
		}
		metadata++
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); inTransaction && !headLocked {
			metadataBeforeHead++
		}
	}))
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	require.Positive(t, metadata, "exercise real migrator metadata queries")
	require.True(t, headLocked, "deletion acquired a current-read head lock")
	require.Zero(t, metadataBeforeHead, "schema preflight cannot establish a pre-lock read view")
	require.Zero(t, nonlockingBeforeHead, "the first consistent read follows the catalog head lock")
}

func TestTaskPluginStatusOnlyUpdateIgnoresUnsavedReferences(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "excluded-private-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	task := Task{TaskID: "status-only"}
	require.NoError(t, db.Create(&task).Error)
	stale := TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: pkg.Digest()}}
	for _, selectOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(selectOnly), func(t *testing.T) {
			task := Task{TaskID: fmt.Sprintf("status-only-%v", selectOnly)}
			require.NoError(t, db.Create(&task).Error)
			receiver := task
			receiver.PrivateData = stale // This value is not persisted by the patch.
			query := db.Model(&receiver)
			if selectOnly {
				query = query.Select("status")
			} else {
				query = query.Omit("private_data")
			}
			require.NoError(t, query.Updates(Task{Status: TaskStatusSuccess, PrivateData: stale}).Error)
			var stored Task
			require.NoError(t, db.First(&stored, task.ID).Error)
			require.Equal(t, TaskStatus(TaskStatusSuccess), stored.Status)
			require.Equal(t, TaskPrivateData{}, stored.PrivateData)
		})
	}
	require.ErrorIs(t, db.Model(&task).Select("private_data").Updates(Task{PrivateData: stale}).Error, taskplugin.ErrBindingRejected, "an emitted reference is still rejected")
}

func TestTaskPluginSerializedLegacyReferencesRespectDeletion(t *testing.T) {
	for name, field := range map[string]string{
		"key_version":      `"plugin_key":"stored","version":"1.0.0"`,
		"explicit_version": `"plugin_key":"stored","version":"other","plugin_version":"1.0.0"`,
		"key_only":         `"plugin_key":"stored"`,
		"selected_key":     `"selected_plugin_key":"stored"`,
		"digest_only":      `"package_digest":"%s"`,
		"nested_array":     `"children":[{"plugin_key":"stored","version":"1.0.0"}]`,
	} {
		for _, binary := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bytes=%v", name, binary), func(t *testing.T) {
				db := deletionDB(t)
				store := NewTaskPluginCatalogStore(db)
				pkg, entry := catalogFixture(t, "stored", "1.0.0", "legacy-reference-source")
				_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
				require.NoError(t, err)
				task := Task{TaskID: "serialized-reference"}
				require.NoError(t, db.Create(&task).Error)
				content := field
				if name == "digest_only" {
					content = fmt.Sprintf(field, pkg.Digest())
				}
				raw := `{"historical":{` + content + `}}`
				var value any = raw
				if binary {
					value = []byte(raw)
				}
				write := func() error { return db.Model(&task).Updates(map[string]any{"private_data": value}).Error }
				require.NoError(t, write(), "retained legacy references remain writable")
				_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
				require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
				require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": `{}`}).Error)
				_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
				require.NoError(t, err)
				before, err := store.Load(t.Context())
				require.NoError(t, err)
				require.ErrorIs(t, write(), taskplugin.ErrBindingRejected, "raw unknown fields must not recreate a deleted reference")
				var persisted string
				require.NoError(t, db.Model(&Task{}).Select("private_data").Where("id = ?", task.ID).Scan(&persisted).Error)
				require.Equal(t, `{}`, persisted)
				after, err := store.Load(t.Context())
				require.NoError(t, err)
				require.Equal(t, before, after, "rejection leaves head, source and deletion receipts unchanged")
				require.NoError(t, db.Model(&task).Select("status").Updates(map[string]any{"status": TaskStatusSuccess, "private_data": value}).Error)
				require.NoError(t, db.Model(&task).Omit("private_data").Updates(map[string]any{"status": TaskStatusSuccess, "private_data": value}).Error)
				_, err = store.CompareAndSwap(t.Context(), 2, []taskplugin.StoredPackage{entry})
				require.NoError(t, err)
				require.NoError(t, write(), "identical explicit reinstall permits references despite the retained receipt")
			})
		}
	}
}

func TestTaskPluginSerializedReferencesAreAllValidated(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	pkg, entry := catalogFixture(t, "retained", "1.0.0", "retained-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	task := Task{TaskID: "mixed-reference"}
	require.NoError(t, db.Create(&task).Error)
	raw := fmt.Sprintf(`{"plugin_binding":{"plugin_key":"retained","version":"1.0.0","package_digest":%q},"history":[{"plugin_key":"missing"}]}`, pkg.Digest())
	require.ErrorIs(t, db.Model(&task).Updates(map[string]any{"private_data": raw}).Error, taskplugin.ErrBindingRejected)
	for _, malformed := range []any{"", []byte(nil), []byte("["), `[{"historical":{"plugin_key":"retained","version":"1.0.0"}}]`} {
		require.ErrorIs(t, db.Model(&task).Updates(map[string]any{"private_data": malformed}).Error, taskplugin.ErrCatalogInvalid)
	}
	require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": "null"}).Error)
	plain := `{"historical":{"unrelated":"value"},"values":[0,false,null]}`
	require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": plain}).Error)
	var persisted string
	require.NoError(t, db.Model(&Task{}).Select("private_data").Where("id = ?", task.ID).Scan(&persisted).Error)
	require.Equal(t, plain, persisted, "reference-free unknown data still round-trips unchanged")
}

func TestTaskPluginSerializedLegacyIdentityDimensions(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	removed, first := catalogFixture(t, "stored", "1.0.0", "first-source")
	retained, second := catalogFixture(t, "stored", "2.0.0", "second-source")
	first.Active, first.Enabled = false, false
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{first, second})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	task := Task{TaskID: "legacy-dimensions"}
	require.NoError(t, db.Create(&task).Error)
	for _, raw := range []string{
		`{"history":{"plugin_key":"stored"}}`,
		`{"history":{"selected_plugin_key":"stored"}}`,
		`{"history":{"plugin_key":"stored","version":"2.0.0"}}`,
		fmt.Sprintf(`{"history":{"package_digest":%q}}`, retained.Digest()),
	} {
		require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": raw}).Error)
	}
	for _, raw := range []string{
		`{"history":{"plugin_key":"stored","version":"1.0.0"}}`,
		`{"history":{"plugin_key":"stored","version":"3.0.0"}}`,
		fmt.Sprintf(`{"history":{"package_digest":%q}}`, removed.Digest()),
	} {
		require.ErrorIs(t, db.Model(&task).Updates(map[string]any{"private_data": raw}).Error, taskplugin.ErrBindingRejected)
	}
}

func TestTaskPluginPinnedSelectionDoesNotReferenceOtherVersions(t *testing.T) {
	for _, shape := range []string{"typed", "serialized", "mixed-case"} {
		t.Run(shape, func(t *testing.T) {
			db := deletionDB(t)
			store := NewTaskPluginCatalogStore(db)
			_, old := catalogFixture(t, "stored", "1.0.0", "old-source")
			pkg, current := catalogFixture(t, "stored", "2.0.0", "current-source")
			old.Active, old.Enabled = false, false
			_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{old, current})
			require.NoError(t, err)
			private := TaskPrivateData{BillingContext: &TaskBillingContext{TaskBillingPlan: &types.TaskBillingPlan{ExpressionPlan: &types.TaskExpressionPlan{SelectedPluginKey: "stored", PluginKey: "stored", PluginVersion: "2.0.0", PackageDigest: pkg.Digest()}}}}
			task := Task{TaskID: "pinned-v2"}
			if shape == "typed" {
				task.PrivateData = private
			}
			require.NoError(t, db.Create(&task).Error)
			if shape != "typed" {
				data, err := common.Marshal(private)
				require.NoError(t, err)
				if shape == "mixed-case" {
					data = bytes.ReplaceAll(data, []byte(`"plugin_key"`), []byte(`"PLUGIN_KEY"`))
				}
				require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": string(data)}).Error)
			}
			_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
			require.NoError(t, err, "a pinned v2 plan does not own unrelated v1")
			_, err = store.DeleteVersion(t.Context(), 2, "stored", "2.0.0")
			require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
		})
	}
}

func TestTaskPluginReferenceFreeHugeNumbersRemainWritable(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "huge-number-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	task := Task{TaskID: "reference-free-number"}
	require.NoError(t, db.Create(&task).Error)
	raw := `{"history":{"value":1e400,"tiny":1e-4000}}`
	require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": raw}).Error)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	for _, malformed := range []string{`{} {}`, `{"history":1e}`, `[] trailing`} {
		require.ErrorIs(t, db.Model(&task).Updates(map[string]any{"private_data": malformed}).Error, taskplugin.ErrCatalogInvalid)
	}
}

func TestTaskPluginHeadCannotMaskOrphanedRestore(t *testing.T) {
	for _, source := range []string{"channel", "pricing"} {
		t.Run(source, func(t *testing.T) {
			db := deletionDB(t)
			store := NewTaskPluginCatalogStore(db)
			_, entry := catalogFixture(t, "stored", "1.0.0", "orphan-source")
			_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
			require.NoError(t, err)
			require.NoError(t, db.Delete(&TaskPluginCatalogHead{}, 1).Error)
			if source == "channel" {
				require.ErrorIs(t, db.Create(&Channel{Models: "ordinary-model"}).Error, taskplugin.ErrCatalogInvalid)
			} else {
				err = db.Transaction(func(tx *gorm.DB) error { _, _, err := readPricingCatalog(tx, true); return err })
				require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
			}
			var count int64
			require.NoError(t, db.Model(&TaskPluginCatalogHead{}).Count(&count).Error)
			require.Zero(t, count, "a failed initialization must not bless an orphaned generation")
			_, err = store.Load(t.Context())
			require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
		})
	}
}

func TestTaskPluginPricingRejectsLostReceiptSet(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "receipt-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	require.NoError(t, db.Where("1 = 1").Delete(&TaskPluginDeletion{}).Error)
	for _, lock := range []bool{false, true} {
		err = db.Transaction(func(tx *gorm.DB) error { _, _, err := readPricingCatalog(tx, lock); return err })
		require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
	}
}

func TestTaskPluginReferenceDigestUsesExactBytesOnCaseInsensitiveStorage(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.Migrator().DropTable(&TaskPluginVersion{}))
	require.NoError(t, db.Exec(`CREATE TABLE task_plugin_versions (identity text PRIMARY KEY, digest text COLLATE NOCASE NOT NULL, document text NOT NULL, active numeric NOT NULL, enabled numeric NOT NULL, revoked numeric NOT NULL)`).Error)
	store := NewTaskPluginCatalogStore(db)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "case-insensitive-digest")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	upper := strings.ToUpper(pkg.Digest())
	require.NotEqual(t, pkg.Digest(), upper)
	task := Task{TaskID: "digest-equality"}
	require.NoError(t, db.Create(&task).Error)
	for _, raw := range []any{
		fmt.Sprintf(`{"history":{"package_digest":%q}}`, upper),
		TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: upper}},
	} {
		require.ErrorIs(t, db.Model(&task).Updates(map[string]any{"private_data": raw}).Error, taskplugin.ErrBindingRejected)
	}
	require.NoError(t, db.Model(&task).Updates(map[string]any{"private_data": fmt.Sprintf(`{"history":{"package_digest":%q}}`, pkg.Digest())}).Error)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
}

func TestTaskPluginInvalidLegacyMappingIsKnownPrecondition(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "unrelated-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	channel := Channel{Models: "ordinary-model"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Model(&channel).UpdateColumn("model_mapping", `{"private-legacy-alias":7}`).Error)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	var mapping *taskplugin.CatalogChannelMappingError
	require.ErrorAs(t, err, &mapping)
	require.Equal(t, channel.Id, mapping.ChannelID)
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
	require.NotContains(t, err.Error(), "private-legacy-alias")
	snapshot, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Packages, 1)
}

func TestTaskPluginHeadBootstrapIgnoresMatchedRowCount(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "matched-row-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("emulate_mysql_found_rows", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginCatalogHead") && tx.Error == nil {
			tx.RowsAffected = 1 // MySQL clientFoundRows can report the matched existing row.
		}
	}))
	require.NoError(t, db.Create(&Channel{Models: "ordinary-model"}).Error)
	snapshot, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Packages, 1)
}

func TestTaskPluginCASCannotRepairAnInvalidPreviousMarker(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "previous-marker")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	require.NoError(t, db.Model(&TaskPluginCatalogHead{}).Where("id = ?", 1).UpdateColumn("revision", 1).Error)
	_, err = store.CompareAndSwap(t.Context(), 1, nil)
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
	var head TaskPluginCatalogHead
	require.NoError(t, db.First(&head, 1).Error)
	require.EqualValues(t, 1, head.Revision)
	require.EqualValues(t, 2, head.LastDeletionRevision)
}

func TestTaskPluginPricingReadDetectsChangedHeadBeforeMarkerValidation(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "read-interleave")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	interleaved := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("simulate_committed_catalog_change", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginDeletion") && !interleaved {
			interleaved = true
			fresh := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true})
			tx.AddError(fresh.Model(&TaskPluginCatalogHead{}).Where("id = ?", 1).Updates(map[string]any{"revision": 2, "last_deletion_revision": 2}).Error)
			tx.AddError(fresh.Create(&TaskPluginDeletion{Identity: taskPluginIdentity("stored", "1.0.0"), PluginKey: "stored", Version: "1.0.0", Digest: entry.Digest, Models: "[]"}).Error)
		}
	}))
	err = db.Transaction(func(tx *gorm.DB) error { _, _, err := readPricingCatalog(tx, false); return err })
	require.ErrorIs(t, err, ErrModelPricingConflict)
	require.True(t, interleaved, "controlled query interleaving must actually change the head")
}

func TestTaskPluginPricingReadDetectsConcurrentBootstrap(t *testing.T) {
	db := deletionDB(t)
	_, entry := catalogFixture(t, "stored", "1.0.0", "bootstrap-interleave")
	interleaved := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("pricing_bootstrap_change", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginVersion") && !interleaved {
			interleaved = true
			fresh := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true})
			tx.AddError(fresh.Create(&TaskPluginCatalogHead{ID: 1, Revision: 1, MutationID: "concurrent-bootstrap"}).Error)
			tx.AddError(fresh.Create(&TaskPluginVersion{Identity: taskPluginIdentity("stored", "1.0.0"), Digest: entry.Digest, Document: entry.Document, Active: true}).Error)
		}
	}))
	err := db.Transaction(func(tx *gorm.DB) error { _, _, err := readPricingCatalog(tx, false); return err })
	require.True(t, interleaved)
	require.ErrorIs(t, err, ErrModelPricingConflict)
}

func TestTaskPluginTypedAndSerializedNestedReferencesMatch(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "nested-facts")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.NoError(t, err)
	private := TaskPrivateData{BillingContext: &TaskBillingContext{TaskBilling: &types.TaskBillingResult{Fields: map[string]string{"plugin_key": "stored", "version": "1.0.0"}}}}
	raw, err := common.Marshal(private)
	require.NoError(t, err)
	for name, value := range map[string]any{"typed": private, "serialized": raw} {
		t.Run(name, func(t *testing.T) {
			row := &Task{TaskID: name}
			require.NoError(t, db.Create(row).Error)
			err := db.Model(row).Updates(map[string]any{"private_data": value}).Error
			require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
		})
	}
}

func TestTaskPluginHistoricalBindingCanCompleteButCannotChange(t *testing.T) {
	db := deletionDB(t)
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "completion-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	row := &Task{TaskID: "legacy-completion", UserId: 71, Quota: 20, Status: TaskStatusSubmitted, PrivateData: TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", UserID: 71}}}
	require.ErrorIs(t, db.Create(row).Error, taskplugin.ErrBindingRejected, "new key-only bindings remain rejected")
	row.ID = 0
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(row).Error, "seed an existing historical row")
	var loaded Task
	require.NoError(t, db.First(&loaded, row.ID).Error)
	loaded.Status, loaded.PrivateData.ResultURL = TaskStatusSuccess, "https://example.invalid/result"
	won, err := loaded.UpdateWithStatus(TaskStatusSubmitted)
	require.NoError(t, err)
	require.True(t, won)
	var saved Task
	require.NoError(t, db.First(&saved, row.ID).Error)
	require.EqualValues(t, TaskStatusSuccess, saved.Status)
	require.Equal(t, loaded.PrivateData.ResultURL, saved.PrivateData.ResultURL)
	require.Equal(t, row.PrivateData.PluginBinding, saved.PrivateData.PluginBinding)
	require.Equal(t, 20, saved.Quota)
	won, err = loaded.UpdateWithStatus(TaskStatusSubmitted)
	require.NoError(t, err)
	require.False(t, won, "stale completion cannot win twice")
	saved.PrivateData.PluginBinding.UserID++
	_, err = saved.UpdateWithStatus(TaskStatusSuccess)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected, "even the same key cannot change the persisted owner")
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.ErrorIs(t, err, taskplugin.ErrCatalogInUse, "the unchanged historical binding still blocks deletion")
}

func TestTaskPluginMalformedHistoricalTaskIsRepairablePrecondition(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "healthy-source")
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(t.Context(), pkg))
	actor := taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "repairable-task"}
	binding, err := catalog.Host().Bind(actor, "GET", "/stored/one", "catalog-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	row := &Task{TaskID: "private-provider-task"}
	require.NoError(t, db.Create(row).Error)
	require.NoError(t, db.Model(row).UpdateColumn("private_data", []byte(`{"upstream_task_id":42,"key":"private-secret"}`)).Error)
	err = catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"})
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
	require.NotErrorIs(t, err, taskplugin.ErrCatalogUnavailable)
	require.NotContains(t, err.Error(), "private-secret")
	_, hostErr := catalog.Host().Resolve(binding, actor)
	require.NoError(t, hostErr, "a repairable task precondition must retain the healthy Host")
}

type hiddenDeletionDialect struct{ gorm.Dialector }

func (hiddenDeletionDialect) Name() string { return "postgres" }
func (dialect hiddenDeletionDialect) Migrator(db *gorm.DB) gorm.Migrator {
	return hiddenDeletionMigrator{dialect.Dialector.Migrator(db)}
}

type hiddenDeletionMigrator struct{ gorm.Migrator }

func (m hiddenDeletionMigrator) HasTable(value any) bool {
	if _, deletion := value.(*TaskPluginDeletion); deletion {
		return false
	}
	return m.Migrator.HasTable(value)
}

func TestTaskPluginHiddenDeletionTableDoesNotBypassWriters(t *testing.T) {
	db := deletionDB(t)
	_, entry := catalogFixture(t, "stored", "1.0.0", "hidden-schema")
	_, err := NewTaskPluginCatalogStore(db).CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	original := db.Dialector
	db.Dialector = hiddenDeletionDialect{original}
	t.Cleanup(func() { db.Dialector = original })
	probes := 0
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("simulate_pg_hidden_table", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "SELECT to_regclass(") {
			probes++
			tx.Statement.SQL.Reset()
			tx.Statement.SQL.WriteString("SELECT 1")
			tx.Statement.Vars = nil
		}
	}))
	require.ErrorIs(t, db.Create(&Channel{Models: "new-model"}).Error, taskplugin.ErrCatalogUnavailable)
	require.ErrorIs(t, db.Create(&Task{TaskID: "hidden-task", PrivateData: TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: entry.Digest}}}).Error, taskplugin.ErrCatalogUnavailable)
	require.Equal(t, 2, probes, "both guards must distinguish hidden from absent; this is a controlled SQLite probe, not real PostgreSQL grants")
}

func TestTaskPluginPricingMetadataHonorsDeadline(t *testing.T) {
	doc, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 3, Source: "while (true) {}", Manifest: taskplugin.Manifest{APIVersion: 1, Key: "loop", Name: "Loop", Version: "1.0.0", Models: []string{"any-model"}}})
	require.NoError(t, err)
	authority := &pricingAuthority{plugins: []taskplugin.StoredPackage{{Document: doc, Digest: fmt.Sprintf("%x", sha256.Sum256(doc)), Active: true}}}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = authority.contextsContext(ctx, "any-model")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 2*time.Second)
}

func TestTaskPluginRetainedDeclarationsAreMetadataReferences(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, retired := catalogFixture(t, "stored", "1.0.0", "retired-declaration")
	_, active := catalogFixture(t, "stored", "2.0.0", "active-declaration")
	retired.Active = false
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{retired, active})
	require.NoError(t, err)
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "2.0.0")
	require.NoError(t, err)
	require.NoError(t, db.Create(&Channel{Models: "catalog-model"}).Error, "retired declaration intentionally retains configuration metadata")
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	_, err = catalog.Host().PricingPackage("GET", "/stored/job", "catalog-model")
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected, "metadata acceptance does not enable a retired route")
}

func TestTaskPluginStoreMetadataLoadHonorsDeadline(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	_, entry := catalogFixture(t, "stored", "1.0.0", "original-source")
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	var document taskplugin.PackageDocument
	require.NoError(t, common.Unmarshal(entry.Document, &document))
	document.FormatVersion = 3
	document.Source = `for(;;){}; export const meta={};`
	raw, err := common.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, db.Model(&TaskPluginVersion{}).Where("identity = ?", taskPluginIdentity("stored", "1.0.0")).Updates(map[string]any{"document": taskPluginDocument(raw), "digest": fmt.Sprintf("%x", sha256.Sum256(raw))}).Error)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = store.Load(ctx)
	require.Error(t, err)
	require.Less(t, time.Since(started), 2*time.Second, "storage-side metadata validation shares the caller budget")
}

func TestTaskPluginSQLiteSchemaProbeKeepsWriteIntent(t *testing.T) {
	db := deletionDB(t)
	var journalMode string
	require.NoError(t, db.Raw("PRAGMA journal_mode=WAL").Scan(&journalMode).Error)
	require.Equal(t, "wal", strings.ToLower(journalMode))
	var location struct{ File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&location).Error)
	other, err := gorm.Open(sqlite.Open(location.File+"?_pragma=busy_timeout(20)"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, other.Create(&Option{Key: "outside-wal-probe", Value: "1"}).Error)
	var sameDatabase int64
	require.NoError(t, db.Model(&Option{}).Where("key = ?", "outside-wal-probe").Count(&sameDatabase).Error)
	require.EqualValues(t, 1, sameDatabase)
	probed := false
	var probeError error
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("probe_sqlite_read_upgrade", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginCatalogHead") && !probed {
			probed = true
			probeError = other.Create(&Option{Key: "unrelated-wal-probe", Value: "1"}).Error
		}
	}))
	channel := Channel{Name: "schema-race", Models: "ordinary-model"}
	require.NoError(t, db.Create(&channel).Error, "a concurrent commit after schema reads must not break the new channel writer")
	require.True(t, probed)
	var busy interface{ Code() int }
	require.ErrorAs(t, probeError, &busy)
	require.Equal(t, 5, busy.Code()&255, "the second writer must report SQLITE_BUSY")
	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, channel.Name, stored.Name)
}

func TestTaskPluginDeletionPreservesDeclaredTaskDefault(t *testing.T) {
	for _, alternate := range []bool{false, true} {
		t.Run(fmt.Sprint(alternate), func(t *testing.T) {
			db := setupModelPricingTest(t)
			require.NoError(t, db.AutoMigrate(&Task{}, &TaskPluginDeletion{}))
			rows := installPricingPluginForTest(t, db, "1.0.0", "credit", nil, 0)
			store := NewTaskPluginCatalogStore(db)
			revision := int64(1)
			if alternate {
				var document taskplugin.PackageDocument
				require.NoError(t, common.Unmarshal(rows[0].Document, &document))
				document.Manifest.Key, document.Manifest.Name = "alternate", "Alternate"
				data, err := common.Marshal(document)
				require.NoError(t, err)
				rows = append(rows, taskplugin.StoredPackage{Document: data, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Active: true, Enabled: true})
				_, err = store.CompareAndSwap(t.Context(), 1, rows)
				require.NoError(t, err)
				revision++
			}
			entry := pricingEntryForTest(t, db, "kling-v1")
			draft := types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}
			_, err := UpdateModelPricing([]types.ModelPricingChange{pricingChangeForTest(entry, draft)})
			require.NoError(t, err)
			before, err := store.Load(t.Context())
			require.NoError(t, err)
			_, err = store.DeleteVersion(t.Context(), revision, "kling", "1.0.0")
			if alternate {
				require.NoError(t, err, "another declared source keeps the unchanged default valid")
				pricingEntryForTest(t, db, "kling-v1")
			} else {
				require.ErrorIs(t, err, taskplugin.ErrCatalogInUse, "deletion must not strand a valid stored task default")
				after, err := store.Load(t.Context())
				require.NoError(t, err)
				require.Equal(t, before, after)
			}
			require.Equal(t, `u("units") * 0.1`, rawPricingObjectForTest(t, db, "billing_setting.billing_expr")["kling-v1"])
		})
	}
}

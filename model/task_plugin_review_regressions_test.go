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

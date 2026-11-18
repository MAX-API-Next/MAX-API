// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func deletionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "deletion.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}, &TaskPluginDeletion{}, &Task{}, &Channel{}, &Option{}))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("guard_catalog_update_columns", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil {
			return
		}
		if values, ok := tx.Statement.Dest.(map[string]any); ok {
			for column := range values {
				field := tx.Statement.Schema.LookUpField(column)
				if field == nil || field.DBName != column {
					tx.AddError(errors.New("unknown catalog update column: " + column))
				}
			}
		}
	}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestTaskPluginDeletionReferencesRollbackAndRefresh(t *testing.T) {
	db := deletionDB(t)
	ctx := t.Context()
	store := NewTaskPluginCatalogStore(db)
	catalog, err := taskplugin.OpenCatalog(ctx, store)
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "private-deleted-source")
	require.NoError(t, catalog.Install(ctx, pkg))
	remove := func(revision int64) error {
		return catalog.MutateExpected(ctx, revision, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"})
	}
	servingChannel := Channel{Models: "catalog-model"}
	require.NoError(t, db.Create(&servingChannel).Error)
	require.ErrorIs(t, remove(1), taskplugin.ErrCatalogInUse, "serving channel references prevent deletion")
	require.NoError(t, db.Delete(&servingChannel).Error)
	require.NoError(t, catalog.SetEnabled(ctx, "stored", false))
	otherNode, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	before, err := store.Load(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, before.Revision)
	require.ErrorIs(t, remove(1), taskplugin.ErrCatalogConflict)

	task := Task{TaskID: "historical", Status: TaskStatusSuccess, PrivateData: TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: pkg.Digest()}}}
	require.NoError(t, db.Create(&task).Error)
	require.ErrorIs(t, remove(2), taskplugin.ErrCatalogInUse, "completed tasks retain references too")
	require.NoError(t, db.Delete(&task).Error)
	channel := Channel{Models: "catalog-model", Status: 2}
	require.NoError(t, db.Create(&channel).Error)
	require.ErrorIs(t, remove(2), taskplugin.ErrCatalogInUse, "disabled channel configuration remains a reference")
	require.NoError(t, db.Delete(&channel).Error)
	mapping := `{"alias":"catalog-model"}`
	channel = Channel{Models: "alias", ModelMapping: &mapping}
	require.NoError(t, db.Create(&channel).Error)
	require.ErrorIs(t, remove(2), taskplugin.ErrCatalogInUse, "mapped targets count")
	require.NoError(t, db.Delete(&channel).Error)
	option := Option{Key: billing_setting.PluginBillingExprOption, Value: `{"stored::catalog-model":"0"}`}
	require.NoError(t, db.Create(&option).Error)
	require.ErrorIs(t, remove(2), taskplugin.ErrCatalogInUse, "explicit free pricing still references the plugin")
	require.NoError(t, db.Where(map[string]any{"key": option.Key}).Delete(&Option{}).Error)
	unchanged, err := store.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "all rejections roll back head, package and receipt")

	callback := "synthetic_task_plugin_delete_failure"
	packageTable := db.NamingStrategy.TableName("TaskPluginVersion")
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == packageTable {
			tx.AddError(errors.New("synthetic deletion failure"))
		}
	}))
	require.ErrorIs(t, remove(2), taskplugin.ErrCatalogUnavailable)
	require.NoError(t, db.Callback().Delete().Remove(callback))
	unchanged, err = store.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "failure after receipt insertion must also roll it back")
	require.NoError(t, remove(2))
	after, err := store.Load(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, after.Revision)
	require.Empty(t, after.Packages)
	require.Equal(t, []taskplugin.PackageDeletion{{Key: "stored", Version: "1.0.0", Digest: pkg.Digest()}}, after.Deletions)
	var count int64
	require.NoError(t, db.Model(&TaskPluginVersion{}).Count(&count).Error)
	require.Zero(t, count, "physical row containing source is gone")
	require.False(t, db.Migrator().HasColumn(&TaskPluginDeletion{}, "document"))
	require.NoError(t, otherNode.Refresh(ctx), "another node accepts only the durable deletion receipt")
	restarted, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	list, revision, err := restarted.ManagementSnapshot(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
	require.EqualValues(t, 3, revision)

	task.ID = 0
	require.ErrorIs(t, db.Create(&task).Error, taskplugin.ErrBindingRejected, "late persistence of a deleted binding is rejected")
	var tasks int64
	require.NoError(t, db.Model(&Task{}).Count(&tasks).Error)
	require.Zero(t, tasks)
	plain := Task{TaskID: "plain"}
	require.NoError(t, db.Create(&plain).Error)
	require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(map[string]any{"private_data": task.PrivateData}).Error, taskplugin.ErrBindingRejected, "map updates cannot attach a deleted binding either")
	serialized, err := common.Marshal(task.PrivateData)
	require.NoError(t, err)
	for _, value := range []any{string(serialized), serialized} {
		require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(map[string]any{"private_data": value}).Error, taskplugin.ErrBindingRejected, "serialized bindings obey the same persistence guard")
	}
	for _, value := range []any{Task{PrivateData: task.PrivateData}, &Task{PrivateData: task.PrivateData}} {
		require.ErrorIs(t, db.Model(&Task{}).Where("id = ?", plain.ID).Updates(value).Error, taskplugin.ErrBindingRejected, "struct updates cannot attach a deleted binding")
		require.NoError(t, db.Model(&Task{}).Where("id = ?", plain.ID).Omit("private_data").Updates(value).Error)
		require.NoError(t, db.Model(&Task{}).Where("id = ?", plain.ID).Select("status").Updates(value).Error)
	}
	var unchangedTask Task
	require.NoError(t, db.First(&unchangedTask, plain.ID).Error)
	require.Nil(t, unchangedTask.PrivateData.PluginBinding)
	changed, _ := catalogFixture(t, "stored", "1.0.0", "different-source")
	require.Error(t, restarted.Install(ctx, changed), "deletion does not permit changed bytes under the same identity")
	require.NoError(t, restarted.Install(ctx, pkg), "exact unrevoked source can be installed again explicitly")
	require.NoError(t, restarted.Revoke(ctx, "stored", "1.0.0"))
	_, revision, err = restarted.ManagementSnapshot(ctx)
	require.NoError(t, err)
	require.NoError(t, restarted.MutateExpected(ctx, revision, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	require.Error(t, restarted.Install(ctx, pkg), "deleting a revoked version cannot undo its revocation")
	require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&TaskPluginDeletion{}).Error)
	_, err = store.Load(ctx)
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid, "lost deletion receipts are not interpreted as a fresh catalog on restart")
	_, entry := catalogFixture(t, "stored", "1.0.0", "changed-after-lost-receipt")
	_, err = store.CompareAndSwap(ctx, revision+1, []taskplugin.StoredPackage{entry})
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid, "direct CAS cannot bypass lost revocation receipts either")
}

func TestTaskPluginDeletionDoesNotRequireDisableOrRevocation(t *testing.T) {
	db := deletionDB(t)
	store := NewTaskPluginCatalogStore(db)
	catalog, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "direct-delete")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	snapshot, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Revision)
	require.Empty(t, snapshot.Packages)
	require.Len(t, snapshot.Deletions, 1)
	require.False(t, snapshot.Deletions[0].Revoked, "ordinary deletion is not a permanent revocation")
	restarted, err := taskplugin.OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	require.NoError(t, restarted.Install(t.Context(), pkg), "an explicitly reimported identical file is allowed")
}

func TestTaskPluginDeletionSerializesWithNewTaskBinding(t *testing.T) {
	db := deletionDB(t)
	pkg, entry := catalogFixture(t, "stored", "1.0.0", "source")
	store := NewTaskPluginCatalogStore(db)
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	locked, release, deleting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var databases []struct{ Name, File string }
	require.NoError(t, db.Raw("PRAGMA database_list").Scan(&databases).Error)
	require.NotEmpty(t, databases)
	require.NotEmpty(t, databases[0].File)
	probe, err := gorm.Open(sqlite.Open(databases[0].File+"?_pragma=busy_timeout(50)"), &gorm.Config{Logger: db.Logger})
	require.NoError(t, err)
	probeSQL, err := probe.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = probeSQL.Close() })
	const probeKey = "task_plugin_reference_lock_probe"
	require.NoError(t, probe.Create(&Option{Key: probeKey, Value: "outside"}).Error)
	var outside Option
	require.NoError(t, db.Where(map[string]any{"key": probeKey}).First(&outside).Error)
	require.Equal(t, "outside", outside.Value)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	taskTable := db.NamingStrategy.TableName("Task")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("hold_plugin_task", func(tx *gorm.DB) {
		if tx.Statement.Table == taskTable {
			close(locked)
			<-release
		}
	}))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("observe_plugin_delete", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Option); ok && row.Key == modelPricingLockOption {
			close(deleting)
		}
	}))
	created, removed := make(chan error, 1), make(chan error, 1)
	go func() {
		task := Task{TaskID: "concurrent", PrivateData: TaskPrivateData{PluginBinding: &types.TaskPluginBinding{PluginKey: "stored", Version: "1.0.0", PackageDigest: pkg.Digest()}}}
		created <- db.WithContext(t.Context()).Create(&task).Error
	}()
	select {
	case <-locked:
	case err := <-created:
		t.Fatalf("task creation ended before its reference lock: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("task reference lock was not reached")
	}
	go func() { _, err := store.DeleteVersion(t.Context(), 1, "stored", "1.0.0"); removed <- err }()
	select {
	case <-deleting:
	case err := <-removed:
		t.Fatalf("deletion ended before its writer lock: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("deletion writer lock was not reached")
	}
	probeErr := probe.Model(&Option{}).Where(map[string]any{"key": probeKey}).UpdateColumn("value", "blocked").Error
	var sqliteCode interface{ Code() int }
	require.ErrorAs(t, probeErr, &sqliteCode)
	require.Equal(t, 5, sqliteCode.Code()&255, "the paused task must already hold SQLite's write lock")
	close(release)
	require.NoError(t, <-created)
	require.ErrorIs(t, <-removed, taskplugin.ErrCatalogInUse, "the reference scan must reject after the concurrent binding commits")
	snapshot, err := store.Load(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Packages, 1)
	require.Empty(t, snapshot.Deletions)
}

func TestTaskPluginDeletionFrozenReferenceAndMissingSchema(t *testing.T) {
	for _, raw := range []string{
		`{"billing_context":{"task_billing_plan":{"expression_plan":{"plugin_key":"stored","plugin_version":"1.0.0"}}}}`,
		`{"billing_context":{"pricing_usage_envelope":{"package_digest":"DIGEST"}}}`,
		`{"plugin_binding":{"plugin_key":"stored"}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			db := deletionDB(t)
			pkg, entry := catalogFixture(t, "stored", "1.0.0", "source")
			entry.Enabled = false
			store := NewTaskPluginCatalogStore(db)
			_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
			require.NoError(t, err)
			value := strings.ReplaceAll(raw, "DIGEST", pkg.Digest())
			require.NoError(t, db.Exec("INSERT INTO tasks (private_data) VALUES (?)", value).Error)
			_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
			require.ErrorIs(t, err, taskplugin.ErrCatalogInUse)
		})
	}
	db := deletionDB(t)
	_, entry := catalogFixture(t, "stored", "1.0.0", "source")
	entry.Enabled = false
	store := NewTaskPluginCatalogStore(db)
	_, err := store.CompareAndSwap(t.Context(), 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	require.NoError(t, db.Migrator().DropTable(&TaskPluginDeletion{}))
	_, err = store.DeleteVersion(t.Context(), 1, "stored", "1.0.0")
	require.ErrorIs(t, err, taskplugin.ErrCatalogUnavailable)
	snapshot, err := store.Load(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Packages, 1)
}

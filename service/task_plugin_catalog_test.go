/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskPluginCatalogStartupRestoresBeforeExposure(t *testing.T) {
	previous := taskPluginCatalog.Load()
	taskPluginCatalog.Store(nil)
	t.Cleanup(func() { taskPluginCatalog.Store(previous) })
	_, err := CurrentTaskPluginCatalog()
	require.ErrorIs(t, err, taskplugin.ErrCatalogUnavailable)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "startup.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.Error(t, InitializeTaskPluginCatalog(context.Background(), db), "missing tables must stop startup")
	_, err = CurrentTaskPluginCatalog()
	require.Error(t, err)
	require.NoError(t, db.AutoMigrate(&model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{}))
	manifest := taskplugin.Manifest{APIVersion: 1, Key: "startup", Name: "Startup", Version: "1.0.0", Models: []string{"model"}}
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Manifest: manifest, Source: "never executed"})
	require.NoError(t, err)
	hash := sha256.Sum256(document)
	p, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", hash))
	require.NoError(t, err)
	seed, err := taskplugin.OpenCatalog(context.Background(), model.NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	require.NoError(t, seed.Install(context.Background(), p))
	require.NoError(t, seed.Revoke(context.Background(), "startup", "1.0.0"))
	require.NoError(t, InitializeTaskPluginCatalog(context.Background(), db))
	catalog, err := CurrentTaskPluginCatalog()
	require.NoError(t, err)
	info, revision, err := catalog.List(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, revision)
	require.Len(t, info, 1)
	require.True(t, info[0].Revoked)
}

func TestTaskPluginCatalogReplicaWaitsForMasterBeforeExposure(t *testing.T) {
	previous := taskPluginCatalog.Load()
	taskPluginCatalog.Store(nil)
	t.Cleanup(func() { taskPluginCatalog.Store(previous) })
	path := filepath.Join(t.TempDir(), "replica-startup.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		pool, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, pool.Close()) })
		return db
	}
	master, replica := open(), open()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		err := waitForTaskPluginCatalogSchema(ctx, replica, time.Millisecond)
		if err == nil {
			err = InitializeTaskPluginCatalog(ctx, replica)
		}
		finished <- err
	}()
	// The replica must observe both tables; it must not create either one.
	require.False(t, model.NewTaskPluginCatalogStore(replica).SchemaReady(ctx))
	require.NoError(t, master.AutoMigrate(&model.TaskPluginCatalogHead{}))
	require.False(t, model.NewTaskPluginCatalogStore(replica).SchemaReady(ctx))
	_, err := CurrentTaskPluginCatalog()
	require.ErrorIs(t, err, taskplugin.ErrCatalogUnavailable)
	select {
	case err := <-finished:
		t.Fatalf("replica exposed an incomplete schema: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	require.NoError(t, master.AutoMigrate(&model.TaskPluginVersion{}))
	require.NoError(t, <-finished)
	catalog, err := CurrentTaskPluginCatalog()
	require.NoError(t, err)
	info, revision, err := catalog.List(ctx)
	require.NoError(t, err)
	require.Empty(t, info)
	require.Zero(t, revision)
}

func TestTaskPluginCatalogSchemaWaitRespectsCancellationAndDoesNotMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wait.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	for _, mode := range []string{"cancelled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			expected := context.DeadlineExceeded
			if mode == "cancelled" {
				cancel()
				expected = context.Canceled
			}
			require.ErrorIs(t, waitForTaskPluginCatalogSchema(ctx, db, time.Millisecond), expected)
			require.False(t, db.Migrator().HasTable(&model.TaskPluginCatalogHead{}))
			require.False(t, db.Migrator().HasTable(&model.TaskPluginVersion{}))
		})
	}
	require.ErrorIs(t, WaitForTaskPluginCatalogSchema(nil, db), taskplugin.ErrCatalogUnavailable)
	require.ErrorIs(t, WaitForTaskPluginCatalogSchema(context.Background(), nil), taskplugin.ErrCatalogUnavailable)
}

func TestTaskPluginCatalogReadySchemaStillRejectsCorruption(t *testing.T) {
	previous := taskPluginCatalog.Load()
	taskPluginCatalog.Store(nil)
	t.Cleanup(func() { taskPluginCatalog.Store(previous) })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "corrupt.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	require.NoError(t, db.AutoMigrate(&model.TaskPluginCatalogHead{}, &model.TaskPluginVersion{}))
	require.NoError(t, db.Create(&model.TaskPluginCatalogHead{ID: 1, Revision: 0, MutationID: "corrupt"}).Error)
	require.NoError(t, WaitForTaskPluginCatalogSchema(context.Background(), db))
	require.ErrorIs(t, InitializeTaskPluginCatalog(context.Background(), db), taskplugin.ErrCatalogUnavailable)
	_, err = CurrentTaskPluginCatalog()
	require.ErrorIs(t, err, taskplugin.ErrCatalogUnavailable)
}

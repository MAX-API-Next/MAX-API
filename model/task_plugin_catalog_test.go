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

package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func catalogFixture(t *testing.T, key, version, source string) (*taskplugin.Package, taskplugin.StoredPackage) {
	t.Helper()
	manifest := taskplugin.Manifest{APIVersion: 1, Key: key, Name: key, Version: version, Models: []string{"catalog-model"}, Routes: []taskplugin.Route{{Method: "GET", Path: "/" + key + "/:id", Type: "query", Render: "render"}}}
	document, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Manifest: manifest, Source: source})
	require.NoError(t, err)
	hash := sha256.Sum256(document)
	p, err := taskplugin.LoadPackage(bytes.NewReader(document), fmt.Sprintf("%x", hash))
	require.NoError(t, err)
	return p, taskplugin.StoredPackage{Document: document, Digest: p.Digest(), Active: true, Enabled: true}
}

func runTaskPluginCatalogStoreContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store := NewTaskPluginCatalogStore(db)
	require.False(t, store.SchemaReady(ctx), "readiness must not migrate missing tables")
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}))
	require.False(t, store.SchemaReady(ctx), "both catalog tables are required")
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}))
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}))
	require.True(t, store.SchemaReady(ctx))
	require.False(t, store.SchemaReady(nil))
	empty, err := store.Load(ctx)
	require.NoError(t, err)
	require.Zero(t, empty.Revision)
	require.Empty(t, empty.Packages)
	first, entry := catalogFixture(t, "stored", "1.0.0", strings.Repeat("opaque", 15000))
	require.Greater(t, len(entry.Document), 65535, "exercise MySQL TEXT limit")
	revision, err := store.CompareAndSwap(ctx, 0, []taskplugin.StoredPackage{entry})
	require.NoError(t, err)
	require.EqualValues(t, 1, revision)
	restarted, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	actor := taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "stored-request"}
	binding, err := restarted.Host().Bind(actor, "GET", "/stored/one", "catalog-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	loaded, err := store.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, entry.Document, loaded.Packages[0].Document)
	returned := first.Document()
	returned[0] ^= 1
	require.Equal(t, entry.Document, first.Document(), "caller must not mutate pinned bytes")
	_, err = store.CompareAndSwap(ctx, 0, []taskplugin.StoredPackage{entry})
	require.ErrorIs(t, err, taskplugin.ErrCatalogConflict)
	second, _ := catalogFixture(t, "stored", "2.0.0", "second")
	require.NoError(t, restarted.Install(ctx, second))
	require.NoError(t, restarted.SetEnabled(ctx, "stored", false))
	fresh, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	_, err = fresh.Host().Bind(actor, "GET", "/stored/one", "catalog-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	_, err = fresh.Host().Resolve(binding, actor)
	require.NoError(t, err, "disabled admission must preserve historical tasks")
	require.NoError(t, fresh.Revoke(ctx, "stored", "1.0.0"))
	loaded, err = store.Load(ctx)
	require.NoError(t, err)
	_, err = store.CompareAndSwap(ctx, loaded.Revision, nil)
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
	for i := range loaded.Packages {
		loaded.Packages[i].Revoked = false
	}
	_, err = store.CompareAndSwap(ctx, loaded.Revision, loaded.Packages)
	require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)

	// Fail an INSERT after the head CAS and earlier metadata writes. Both
	// the revision and all metadata must roll back with the package write.
	before, err := store.Load(ctx)
	require.NoError(t, err)
	_, extra := catalogFixture(t, "extra", "1.0.0", "insert fails")
	candidate := append(append([]taskplugin.StoredPackage(nil), before.Packages...), extra)
	for i := range before.Packages {
		if !candidate[i].Revoked {
			candidate[i].Enabled = true
		}
	}
	callback := "test_task_plugin_catalog_insert_failure"
	packageTable := db.NamingStrategy.TableName("TaskPluginVersion")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == packageTable {
			tx.AddError(errors.New("synthetic package write failure"))
		}
	}))
	_, writeErr := store.CompareAndSwap(ctx, before.Revision, candidate)
	require.NoError(t, db.Callback().Create().Remove(callback))
	require.Error(t, writeErr)
	after, err := store.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)

	final, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	_, err = final.Host().Resolve(binding, actor)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected, "revocation survives restart")
	_, err = final.Host().Bind(actor, "GET", "/stored/one", "catalog-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	require.NoError(t, final.Activate(ctx, "stored", "2.0.0"))
	info, _, err := final.List(ctx)
	require.NoError(t, err)
	require.Len(t, info, 2)
}

func TestTaskPluginCatalogStoreSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "catalog.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runTaskPluginCatalogStoreContract(t, db)
}

func runTaskPluginCatalogConcurrentBootstrap(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}))
	_, alpha := catalogFixture(t, "alpha", "1.0.0", "alpha")
	_, beta := catalogFixture(t, "beta", "1.0.0", "beta")
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, entry := range []taskplugin.StoredPackage{alpha, beta} {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := NewTaskPluginCatalogStore(db).CompareAndSwap(context.Background(), 0, []taskplugin.StoredPackage{entry})
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	snapshot, err := NewTaskPluginCatalogStore(db).Load(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Packages, 1)
	// Existing-row CAS must also have one winner; row locking alone must
	// never allow a stale writer to replace a newer metadata revision.
	start = make(chan struct{})
	results = make(chan error, 2)
	for i := range 2 {
		entry := snapshot.Packages[0]
		if i == 0 {
			entry.Enabled = false
		} else {
			entry.Active = false
		}
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := NewTaskPluginCatalogStore(db).CompareAndSwap(context.Background(), snapshot.Revision, []taskplugin.StoredPackage{entry})
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	winners = 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	updated, err := NewTaskPluginCatalogStore(db).Load(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Revision)
}

func TestTaskPluginCatalogSQLiteReopensFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		return db
	}
	db := open()
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}))
	catalog, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	p, _ := catalogFixture(t, "reopened", "1.0.0", "original")
	require.NoError(t, catalog.Install(ctx, p))
	actor := taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "before-close"}
	binding, err := catalog.Host().Bind(actor, "GET", "/reopened/one", "catalog-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	require.NoError(t, catalog.Revoke(ctx, "reopened", "1.0.0"))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	db = open()
	sqlDB, err = db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	restarted, err := taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	_, err = restarted.Host().Resolve(binding, actor)
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
	// Altered stored bytes must prevent a fresh process from opening the host.
	require.NoError(t, db.Model(&TaskPluginVersion{}).Where("digest = ?", p.Digest()).Update("document", "tampered").Error)
	_, err = taskplugin.OpenCatalog(ctx, NewTaskPluginCatalogStore(db))
	require.Error(t, err)
}

type taskPluginTestDialect struct {
	gorm.Dialector
	name string
}

func (d taskPluginTestDialect) Name() string { return d.name }

func TestTaskPluginCatalogPayloadDialectTypes(t *testing.T) {
	for dialect, expected := range map[string]string{"mysql": "longtext", "sqlite": "text", "postgres": "text"} {
		db := &gorm.DB{Config: &gorm.Config{Dialector: taskPluginTestDialect{name: dialect}}}
		require.Equal(t, expected, taskPluginDocument("").GormDBDataType(db, nil))
	}
}

func TestTaskPluginCatalogConcurrentBootstrapSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "concurrent.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runTaskPluginCatalogConcurrentBootstrap(t, db)
}

func TestTaskPluginCatalogLoadDistinguishesBootstrapConflictAndOrphans(t *testing.T) {
	for _, appeared := range []bool{true, false} {
		t.Run(fmt.Sprintf("head_appeared_%t", appeared), func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "observations.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pool.Close()) })
			require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}))
			_, entry := catalogFixture(t, "observed", "1.0.0", "original")
			store := NewTaskPluginCatalogStore(db)
			_, err = store.CompareAndSwap(context.Background(), 0, []taskplugin.StoredPackage{entry})
			require.NoError(t, err)
			if !appeared {
				require.NoError(t, db.Delete(&TaskPluginCatalogHead{}, 1).Error)
			}
			headReads := 0
			callback := "test_catalog_head_observations"
			headTable := db.NamingStrategy.TableName("TaskPluginCatalogHead")
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table != headTable {
					return
				}
				headReads++
				if appeared && headReads == 1 {
					// Supply the first READ COMMITTED observation: the head was
					// absent, then a complete bootstrap became visible.
					*tx.Statement.Dest.(*TaskPluginCatalogHead) = TaskPluginCatalogHead{}
					tx.RowsAffected = 0
					tx.Error = gorm.ErrRecordNotFound
				}
			}))
			t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove(callback)) })
			_, err = store.Load(context.Background())
			if appeared {
				require.ErrorIs(t, err, taskplugin.ErrCatalogConflict)
			} else {
				require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid, "genuine orphan rows must remain rejected")
			}
			require.Equal(t, 2, headReads, "classify the snapshot only after checking whether the head changed")
		})
	}
}

func runTaskPluginCatalogReadCommittedBootstrap(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&TaskPluginCatalogHead{}, &TaskPluginVersion{}))
	_, entry := catalogFixture(t, "read-committed", "1.0.0", "original")
	installed := false
	var writerErr error
	callback := "test_catalog_bootstrap_between_reads"
	headTable := db.NamingStrategy.TableName("TaskPluginCatalogHead")
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if installed || tx.Statement.Table != headTable || !errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return
		}
		installed = true
		// The reader holds a dedicated connection and transaction. Bootstrap
		// commits on another connection before its version query can start.
		_, writerErr = NewTaskPluginCatalogStore(db).CompareAndSwap(context.Background(), 0, []taskplugin.StoredPackage{entry})
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove(callback)) })
	require.NoError(t, db.Connection(func(reader *gorm.DB) error {
		// Apply only to the next transaction on this connection, leaving the
		// pool's session isolation unchanged for subsequent tests.
		if err := reader.Exec("SET TRANSACTION ISOLATION LEVEL READ COMMITTED").Error; err != nil {
			return err
		}
		_, err := NewTaskPluginCatalogStore(reader).Load(context.Background())
		require.True(t, installed)
		require.NoError(t, writerErr)
		require.ErrorIs(t, err, taskplugin.ErrCatalogConflict, "committed bootstrap is a retryable change, not corruption")
		return nil
	}))
	catalog, err := taskplugin.OpenCatalog(context.Background(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	info, revision, err := catalog.List(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, revision)
	require.Len(t, info, 1)
	require.Equal(t, "read-committed", info[0].Key)
}

// Opt-in, isolated two-table fixtures. Existing billing/task/user tables are
// untouched; cleanup drops only this invocation's random-prefixed tables.
func TestTaskPluginCatalogMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_TASK_PLUGIN_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("set TEST_TASK_PLUGIN_MYSQL_DSN for isolated catalog fixtures")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	allowed := strings.TrimSpace(os.Getenv("TEST_TASK_PLUGIN_MYSQL_DATABASE"))
	if allowed == "" || config.DBName != allowed {
		t.Fatal("explicit catalog database authorization required")
	}
	_, sqlDB := openMySQLAcceptanceDB(t, dsn)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	for _, name := range []string{"contract", "concurrent", "read_committed_bootstrap"} {
		t.Run(name, func(t *testing.T) {
			prefix := "tpc_" + common.GetUUID()[:12] + "_"
			db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&TaskPluginVersion{}, &TaskPluginCatalogHead{})) })
			switch name {
			case "contract":
				runTaskPluginCatalogStoreContract(t, db)
			case "concurrent":
				runTaskPluginCatalogConcurrentBootstrap(t, db)
			case "read_committed_bootstrap":
				runTaskPluginCatalogReadCommittedBootstrap(t, db)
			}
		})
	}
}

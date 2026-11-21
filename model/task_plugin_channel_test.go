// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func TestTaskPluginOnlyGroupAllowsOrdinaryRouteCacheRefresh(t *testing.T) {
	clearPreferredOwnerTables(t)
	enabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		clearPreferredOwnerTables(t)
		InitChannelCache()
		common.MemoryCacheEnabled = enabled
	})
	insertChannelSelectionCandidate(t, 41, constant.ChannelTypeOpenAI, "", "ordinary-model", "default", 1)
	plugin := &Channel{Id: 42, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled,
		Name: "configuration only", Group: "plugin-only", Models: "preview-model"}
	// Seed a persisted channel without requiring the catalog writer in this cache fixture.
	require.NoError(t, DB.Exec("INSERT INTO channels (id, type, status, name, "+commonGroupCol+", models, "+commonKeyCol+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		plugin.Id, plugin.Type, plugin.Status, plugin.Name, plugin.Group, plugin.Models, "fixture").Error)
	InitChannelCache()
	cached, err := CacheGetChannel(plugin.Id)
	require.NoError(t, err)
	require.Equal(t, plugin.Name, cached.Name)
	ordinary, err := GetRandomSatisfiedChannel("default", "ordinary-model", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.Equal(t, 41, ordinary.Id)
	require.Empty(t, group2model2channels["plugin-only"])
}

func TestQuotedUnrelatedTableAcceptsMapCreate(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, registerTaskPluginTypedCreateGuard(db))
	require.NoError(t, db.Exec(`CREATE TABLE "channels archive" (name text)`).Error)
	require.NoError(t, db.Table(`"channels archive"`).Create(map[string]any{"name": "archived"}).Error)
}

func TestChannelSettingsRepairDoesNotRewriteMalformedMapping(t *testing.T) {
	for _, field := range []string{"setting", "other_settings"} {
		t.Run(field, func(t *testing.T) {
			db := deletionDB(t)
			previous := DB
			DB = db
			t.Cleanup(func() { DB = previous })
			bad := "{"
			row := &Channel{Name: "legacy-settings", Models: "ordinary", Setting: &bad, OtherSettings: bad, Key: "unchanged-fixture-key"}
			require.NoError(t, db.Create(row).Error)
			require.NoError(t, db.Model(row).UpdateColumn("model_mapping", bad).Error)
			var loaded Channel
			require.NoError(t, db.First(&loaded, row.Id).Error)
			if field == "setting" {
				loaded.GetSetting()
			} else {
				loaded.GetOtherSettings()
			}
			require.Equal(t, &bad, loaded.Setting)
			require.Equal(t, bad, loaded.OtherSettings)
			var fresh Channel
			require.NoError(t, db.First(&fresh, row.Id).Error)
			if field == "setting" {
				require.Nil(t, fresh.Setting)
				require.Equal(t, bad, fresh.OtherSettings)
			} else {
				require.Equal(t, "{}", fresh.OtherSettings)
				require.Equal(t, &bad, fresh.Setting)
			}
			require.Equal(t, &bad, fresh.ModelMapping)
			require.Equal(t, "unchanged-fixture-key", fresh.Key)
			require.ErrorIs(t, db.Model(&Channel{Id: row.Id}).Updates(map[string]any{"model_mapping": bad}).Error, taskplugin.ErrCatalogInvalid)
		})
	}
}

func TestChannelSettingsRepairPreservesConcurrentValidValue(t *testing.T) {
	for _, column := range []string{"setting", "settings"} {
		t.Run(column, func(t *testing.T) {
			db := deletionDB(t)
			previous := DB
			DB = db
			t.Cleanup(func() { DB = previous })
			bad := "{"
			row := &Channel{Setting: &bad, OtherSettings: bad}
			require.NoError(t, db.Create(row).Error)
			var stale Channel
			require.NoError(t, db.First(&stale, row.Id).Error)
			const valid = `{"concurrent":true}`
			require.NoError(t, db.Model(row).UpdateColumn(column, valid).Error)
			if column == "setting" {
				stale.GetSetting()
			} else {
				stale.GetOtherSettings()
			}
			var value *string
			require.NoError(t, db.Model(row).Select(column).Row().Scan(&value))
			require.NotNil(t, value)
			require.Equal(t, valid, *value)
		})
	}
}

func TestChannelReferencesRejectLostWholeReceiptSet(t *testing.T) {
	for _, mode := range []string{"rows-lost", "table-lost", "legacy-index"} {
		t.Run(mode, func(t *testing.T) {
			db := deletionDB(t)
			previous := DB
			DB = db
			t.Cleanup(func() { DB = previous })
			catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
			require.NoError(t, err)
			pkg, _ := catalogFixture(t, "stored", "1.0.0", "receipt-loss")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
			switch mode {
			case "rows-lost":
				require.NoError(t, db.Where("1 = 1").Delete(&TaskPluginDeletion{}).Error)
			case "table-lost":
				require.NoError(t, db.Migrator().DropTable(&TaskPluginDeletion{}))
			case "legacy-index":
				require.NoError(t, db.Model(&TaskPluginDeletion{}).Where("1 = 1").UpdateColumn("models", "").Error)
			}
			err = db.Create(&Channel{Name: mode, Models: "catalog-model"}).Error
			if mode == "legacy-index" {
				require.NoError(t, err, "legacy receipts with unknown model names retain their documented exception")
			} else {
				require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
			}
		})
	}
}

func TestChannelBatchValidatesReceiptUnionOncePerStatement(t *testing.T) {
	db := deletionDB(t)
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "batch-union")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	reads := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count-batch-receipts", func(tx *gorm.DB) {
		if tx.Statement.Table == db.NamingStrategy.TableName("TaskPluginDeletion") {
			reads++
		}
	}))
	rows := []Channel{{Name: "a", Models: "ordinary-a"}, {Name: "b", Models: "ordinary-b"}, {Name: "c", ModelMapping: common.GetPointer(`{"alias":"ordinary-c"}`)}}
	require.NoError(t, db.Create(&rows).Error)
	require.Equal(t, 1, reads)
	reads = 0
	more := []Channel{{Name: "d", Models: "ordinary-d"}, {Name: "e", Models: "ordinary-e"}}
	require.NoError(t, db.Create(&more).Error)
	require.Equal(t, 1, reads, "another statement cannot reuse a previous validation")
	bad := []Channel{{Name: "safe-member", Models: "ordinary"}, {Name: "deleted-member", Models: "catalog-model"}}
	reads = 0
	require.ErrorIs(t, db.Create(&bad).Error, taskplugin.ErrBindingRejected)
	require.Equal(t, 1, reads, "a rejected union must also scan only once per execution")
	var count int64
	require.NoError(t, db.Model(&Channel{}).Where("name IN ?", []string{"safe-member", "deleted-member"}).Count(&count).Error)
	require.Zero(t, count, "one invalid union member rolls back the whole statement")
	mappingOnly := []Channel{{Name: "mapping-safe", Models: "ordinary"}, {Name: "mapping-deleted", Models: "ordinary-alias", ModelMapping: common.GetPointer(`{"ordinary-alias":"catalog-model"}`)}}
	reads = 0
	require.ErrorIs(t, db.Create(&mappingOnly).Error, taskplugin.ErrBindingRejected)
	require.Equal(t, 1, reads)
	require.NoError(t, db.Model(&Channel{}).Where("name IN ?", []string{"mapping-safe", "mapping-deleted"}).Count(&count).Error)
	require.Zero(t, count, "a later mapping-only reference rolls back the entire batch")
	ignored := []Channel{{Name: "omitted-a", Models: "catalog-model"}, {Name: "omitted-b", Models: "catalog-model"}}
	require.NoError(t, db.Omit("models").Create(&ignored).Error)
	var persisted []Channel
	require.NoError(t, db.Where("name IN ?", []string{"omitted-a", "omitted-b"}).Find(&persisted).Error)
	require.Len(t, persisted, 2)
	for _, row := range persisted {
		require.Empty(t, row.Models)
	}
	reads = 0
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		first := []Channel{{Name: "tx-first-a", Models: "ordinary"}, {Name: "tx-first-b", Models: "ordinary"}}
		if err := tx.Create(&first).Error; err != nil {
			return err
		}
		second := []Channel{{Name: "tx-second-a", Models: "ordinary"}, {Name: "tx-second-b", Models: "ordinary"}}
		return tx.Create(&second).Error
	}))
	require.Equal(t, 2, reads, "even statements in the same transaction must not share a validation result")
}

func TestChannelBatchRevalidatesReusedInitializedHandle(t *testing.T) {
	db := deletionDB(t)
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "reused-query")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	err = db.Transaction(func(tx *gorm.DB) error {
		q := tx.Omit("remark")
		safe := []Channel{{Name: "first-safe", Models: "ordinary-a"}, {Name: "second-safe", Models: "ordinary-b"}}
		if err := q.Create(&safe).Error; err != nil {
			return err
		}
		bad := []Channel{{Name: "later-safe", Models: "ordinary"}, {Name: "later-deleted", Models: "catalog-model"}}
		err := q.Create(&bad).Error
		require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
		return err
	})
	require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
}

func TestChannelMapCreatesAreRejected(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			db := deletionDB(t)
			row := map[string]any{"name": "untyped-create", "models": "ordinary"}
			var dest any = row
			if batch {
				dest = []map[string]any{row}
			}
			require.ErrorIs(t, db.Model(&Channel{}).Create(dest).Error, taskplugin.ErrCatalogInvalid)
			require.ErrorIs(t, db.Table(db.NamingStrategy.TableName("Channel")).Create(dest).Error, taskplugin.ErrCatalogInvalid)
			require.ErrorIs(t, db.Table(strings.ToUpper(db.NamingStrategy.TableName("Channel"))).Create(dest).Error, taskplugin.ErrCatalogInvalid)
			var count int64
			require.NoError(t, db.Model(&Channel{}).Count(&count).Error)
			require.Zero(t, count)
			channel := &Channel{Name: "typed", Models: "ordinary"}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, db.Model(channel).Updates(map[string]any{"models": "ordinary-updated"}).Error)
		})
	}
}

func TestTaskMapCreatesAreRejected(t *testing.T) {
	db := deletionDB(t)
	row := map[string]any{"task_id": "untyped-task", "private_data": `{"legacy":{"plugin_key":"removed","version":"1.0.0"}}`}
	for _, table := range []string{"tasks", "TASKS"} {
		require.ErrorIs(t, db.Table(table).Create(row).Error, taskplugin.ErrCatalogInvalid)
		require.ErrorIs(t, db.Table(table).Create([]map[string]any{row}).Error, taskplugin.ErrCatalogInvalid)
	}
	require.ErrorIs(t, db.Model(&Task{}).Create(row).Error, taskplugin.ErrCatalogInvalid)
	var count int64
	require.NoError(t, db.Model(&Task{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Create(&Task{TaskID: "typed-task"}).Error)
}

func TestProtectedTablesRejectPartialStructCreateDestinations(t *testing.T) {
	db := deletionDB(t)
	type channelFields struct {
		Name   string
		Models string
	}
	type taskFields struct {
		TaskID string
	}
	require.ErrorIs(t, db.Table("CHANNELS").Create(&channelFields{Name: "partial", Models: "catalog-model"}).Error, taskplugin.ErrCatalogInvalid)
	require.ErrorIs(t, db.Table("tasks").Create(&[]taskFields{{TaskID: "partial"}}).Error, taskplugin.ErrCatalogInvalid)
	require.ErrorIs(t, db.Model(&channelFields{}).Table("channels").Create(&Channel{Name: "wrong schema"}).Error, taskplugin.ErrCatalogInvalid)
	require.NoError(t, db.Table("CHANNELS").Create(&[]Channel{{Name: "complete"}}).Error)
	require.NoError(t, db.Table("tasks").Create(&[]Task{{TaskID: "complete"}}).Error)
}

func TestTypedCreateGuardAllowsResolvedInsertTargets(t *testing.T) {
	db := deletionDB(t)
	protected := clause.Insert{Table: clause.Table{Name: "tasks"}}
	require.NoError(t, db.Clauses(protected).Create(&Task{TaskID: "explicit-typed-create"}).Error)
	require.NoError(t, db.AutoMigrate(&Option{}))
	other := clause.Insert{Table: clause.Table{Name: "options"}}
	require.NoError(t, db.Table("options").Clauses(other).Create(map[string]any{"key": "explicit-map-create", "value": "ok"}).Error)
	var row Option
	require.NoError(t, db.Where("key = ?", "explicit-map-create").Take(&row).Error)
	require.Equal(t, "ok", row.Value)
}

func TestChannelTableMetadataUpdateStillWorksWithGuard(t *testing.T) {
	db := deletionDB(t)
	row := &Channel{Name: "original", Models: "ordinary"}
	require.NoError(t, db.Create(row).Error)
	table := db.Table("channels").Where("id = ?", row.Id)
	require.ErrorIs(t, table.Updates(map[string]any{"models": "new"}).Error, taskplugin.ErrCatalogInvalid)
	require.NoError(t, db.Table("channels").Where("id = ?", row.Id).Updates(map[string]any{"name": "new"}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", row.Id).Updates(map[string]any{"models": "typed"}).Error)
	var stored Channel
	require.NoError(t, db.First(&stored, row.Id).Error)
	require.Equal(t, "new", stored.Name)
	require.Equal(t, "typed", stored.Models)
}

func TestCatalogGuardResolvesAliasedTableExpression(t *testing.T) {
	table, resolved := taskPluginStatementTable(&gorm.Statement{Table: "c", TableExpr: &clause.Expr{SQL: "`main`.`channels` AS c"}}, false)
	require.True(t, resolved)
	require.Equal(t, "main.channels", table)
	for _, expression := range []string{`ONLY public.CHANNELS AS c`, `ONLY ("public"."channels") AS c`, `PUBLIC.CHANNELS AS c`} {
		table, resolved = taskPluginStatementTable(&gorm.Statement{Table: "c", TableExpr: &clause.Expr{SQL: expression}}, true)
		require.True(t, resolved)
		require.Equal(t, "public.channels", table)
	}
	table, resolved = taskPluginStatementTable(&gorm.Statement{Table: "c", TableExpr: &clause.Expr{SQL: `PUBLIC."Channels" AS c`}}, true)
	require.True(t, resolved)
	require.Equal(t, "public.Channels", table)
}

func TestTaskPrivateReferenceWritesRequireTypedUpdates(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.Callback().Update().Remove("guard_catalog_update_columns"))
	task := &Task{TaskID: "typed-private-data"}
	require.NoError(t, db.Create(task).Error)
	query := db.Table("tasks").Where("id = ?", task.ID)
	require.ErrorIs(t, query.Updates(map[string]any{"private_data": `{}`}).Error, taskplugin.ErrCatalogInvalid)
	require.ErrorIs(t, db.Model(task).Updates(map[string]any{"tasks.private_data": `{}`}).Error, taskplugin.ErrCatalogInvalid)
	require.NoError(t, db.Model(task).Select("progress").Updates(map[string]any{"tasks.private_data": `{}`, "progress": "15%"}).Error)
	require.NoError(t, db.Table("tasks").Where("id = ?", task.ID).Select("progress").
		Updates(map[string]any{"progress": "20%", "private_data": `{}`}).Error)
	require.NoError(t, db.Model(task).Updates(map[string]any{"private_data": `{}`}).Error)
	var stored Task
	require.NoError(t, db.First(&stored, task.ID).Error)
	require.Equal(t, "20%", stored.Progress)
}

func TestCatalogGuardUsesConfiguredSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/qualified.db"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "main."},
	})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, registerTaskPluginTypedCreateGuard(db))
	require.NoError(t, db.Exec("CREATE TABLE main.channels (models TEXT)").Error)
	require.NoError(t, db.Exec("CREATE TABLE main.tasks (private_data TEXT)").Error)
	require.NoError(t, db.Exec("CREATE TABLE main.audit_notes (models TEXT)").Error)
	require.ErrorIs(t, db.Table("main.channels").Create(map[string]any{"models": "fixture"}).Error, taskplugin.ErrCatalogInvalid)
	require.ErrorIs(t, db.Table("main.tasks").Where("1=1").Update("private_data", "{}").Error, taskplugin.ErrCatalogInvalid)
	require.NoError(t, db.Table("main.audit_notes").Create(map[string]any{"models": "fixture"}).Error)
}

func TestCatalogGuardAllowsExplicitQuotedAuditTable(t *testing.T) {
	db := deletionDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE "audit archive" (name TEXT)`).Error)
	table := clause.Table{Name: "audit archive"}
	require.NoError(t, db.Table("unused").Clauses(clause.Insert{Table: table}).Create(map[string]any{"name": "initial"}).Error)
	require.NoError(t, db.Table("unused").Clauses(clause.Update{Table: table}).Where("name = ?", "initial").Update("name", "updated").Error)
	var name string
	require.NoError(t, db.Raw(`SELECT name FROM "audit archive"`).Scan(&name).Error)
	require.Equal(t, "updated", name)
}

func TestTypedChannelUpsertChecksConflictAssignments(t *testing.T) {
	for _, kind := range []string{"models", "model_mapping", "expression", "raw target", "metadata", "update-all", "reused-update-all"} {
		t.Run(kind, func(t *testing.T) {
			db := deletionDB(t)
			catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
			require.NoError(t, err)
			pkg, _ := catalogFixture(t, "stored", "1.0.0", "upsert-reference")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
			original := &Channel{Name: "original", Models: "ordinary"}
			require.NoError(t, db.Create(original).Error)
			values := map[string]any{"name": "renamed"}
			switch kind {
			case "models":
				values["models"] = "catalog-model"
			case "model_mapping":
				values["model_mapping"] = `{"alias":"catalog-model"}`
			case "expression":
				values["models"] = gorm.Expr("?", "catalog-model")
			}
			conflict := clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(values)}
			if kind == "raw target" {
				conflict.DoUpdates = append(conflict.DoUpdates, clause.Assignment{Column: clause.Column{Name: "models", Raw: true}, Value: "ordinary"})
			}
			if kind == "update-all" || kind == "reused-update-all" {
				conflict = clause.OnConflict{UpdateAll: true}
			}
			query := db.Clauses(conflict)
			err = query.Create(&Channel{Id: original.Id, Name: "typed", Models: "ordinary"}).Error
			if kind == "reused-update-all" {
				require.NoError(t, err)
				err = query.Create(&Channel{Id: original.Id, Name: "typed-again", Models: "ordinary"}).Error
			}
			switch kind {
			case "models", "model_mapping":
				require.ErrorIs(t, err, taskplugin.ErrBindingRejected)
			case "expression", "raw target":
				require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid)
			default:
				require.NoError(t, err)
			}
			var fresh Channel
			require.NoError(t, db.First(&fresh, original.Id).Error)
			require.Equal(t, "ordinary", fresh.Models)
			require.Nil(t, fresh.ModelMapping)
			expectedName := "original"
			switch kind {
			case "metadata":
				expectedName = "renamed"
			case "update-all":
				expectedName = "typed"
			case "reused-update-all":
				expectedName = "typed-again"
			}
			require.Equal(t, expectedName, fresh.Name)
		})
	}
}

func TestChannelRejectsInvalidPopulatedReceiptIndex(t *testing.T) {
	for _, index := range []string{"null", "[]", `[""]`, `[" catalog-model"]`, `["catalog-model","catalog-model"]`} {
		t.Run(index, func(t *testing.T) {
			db := deletionDB(t)
			catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
			require.NoError(t, err)
			pkg, _ := catalogFixture(t, "stored", "1.0.0", "empty-index")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
			require.NoError(t, db.Model(&TaskPluginDeletion{}).Where("1 = 1").UpdateColumn("models", index).Error)
			require.ErrorIs(t, db.Create(&Channel{Name: "corrupt-index", Models: "catalog-model"}).Error, taskplugin.ErrCatalogInvalid)
		})
	}
}

func TestTaskPluginChannelReferencesCannotOutliveDeletion(t *testing.T) {
	db := deletionDB(t)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	require.NoError(t, db.AutoMigrate(&Ability{}))
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "channel-reference")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	safe := Channel{Name: "existing", Models: "ordinary-model", Group: "default"}
	require.NoError(t, safe.Insert())
	var order []string
	headTable := db.NamingStrategy.TableName("TaskPluginCatalogHead")
	channelTable := db.NamingStrategy.TableName("Channel")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("observe_reference_head_lock", func(tx *gorm.DB) {
		if tx.Statement.Table == headTable {
			order = append(order, "head")
		}
	}))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("observe_reference_insert", func(tx *gorm.DB) {
		if tx.Statement.Table == channelTable {
			order = append(order, "channel")
		}
	}))
	channel := Channel{Name: "referencing", Models: "catalog-model", Group: "default"}
	require.NoError(t, channel.Insert())
	require.Equal(t, []string{"head", "channel"}, order, "reference validation and write use the catalog head lock")
	require.ErrorIs(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}), taskplugin.ErrCatalogInUse)
	require.NoError(t, db.Delete(&channel).Error)
	require.NoError(t, catalog.MutateExpected(t.Context(), 1, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	late := Channel{Name: "late", Models: "catalog-model", Group: "default"}
	require.ErrorIs(t, late.Insert(), taskplugin.ErrBindingRejected, "a write starting after deletion also cannot create an orphan reference")
	safe.Models = "catalog-model"
	require.ErrorIs(t, safe.UpdateFields("models"), taskplugin.ErrBindingRejected)
	var stored Channel
	require.NoError(t, db.First(&stored, safe.Id).Error)
	require.Equal(t, "ordinary-model", stored.Models, "rejected update rolls back")
	mapping := `{"alias":"catalog-model"}`
	safe.ModelMapping = &mapping
	require.ErrorIs(t, safe.UpdateFields("model_mapping"), taskplugin.ErrBindingRejected)
	require.ErrorIs(t, db.Model(&Channel{}).Where("id = ?", safe.Id).Updates(map[string]any{"model_mapping": mapping}).Error, taskplugin.ErrBindingRejected)
	require.ErrorIs(t, db.Model(&Channel{}).Where("id = ?", safe.Id).Updates(Channel{Models: "catalog-model"}).Error, taskplugin.ErrBindingRejected)
	require.ErrorIs(t, safe.Save(), taskplugin.ErrBindingRejected)
	require.ErrorIs(t, safe.SaveWithoutKey(), taskplugin.ErrBindingRejected)
	var beforeCount int64
	require.NoError(t, db.Model(&Channel{}).Count(&beforeCount).Error)
	require.ErrorIs(t, BatchInsertChannels([]Channel{{Name: "ordinary", Models: "ordinary-model", Group: "default"}, {Name: "deleted", Models: "catalog-model", Group: "default"}}), taskplugin.ErrBindingRejected)
	var afterCount int64
	require.NoError(t, db.Model(&Channel{}).Count(&afterCount).Error)
	require.Equal(t, beforeCount, afterCount, "a rejected batch rolls back preceding channel and ability writes")
	var beforeAbilities, afterAbilities int64
	require.NoError(t, db.Model(&Ability{}).Count(&beforeAbilities).Error)
	chunked := make([]Channel, 51)
	for i := range chunked {
		chunked[i] = Channel{Name: fmt.Sprintf("chunked-%d", i), Models: "ordinary-model", Group: "default"}
	}
	chunked[50].Models = "catalog-model"
	require.ErrorIs(t, BatchInsertChannels(chunked), taskplugin.ErrBindingRejected)
	require.NoError(t, db.Model(&Channel{}).Count(&afterCount).Error)
	require.NoError(t, db.Model(&Ability{}).Count(&afterAbilities).Error)
	require.Equal(t, beforeCount, afterCount, "a rejected later chunk rolls back completed channel writes")
	require.Equal(t, beforeAbilities, afterAbilities, "a rejected later chunk rolls back completed ability writes")
	safe.Models = "ordinary-model"
	safe.ModelMapping = nil
	require.NoError(t, safe.UpdateFields("models"), "unrelated model writes remain supported")
	tag := "guarded-tag"
	safe.Tag = &tag
	require.NoError(t, safe.UpdateFields("tag"))
	deletedModel := "catalog-model"
	require.ErrorIs(t, EditChannelByTag(tag, true, nil, nil, &deletedModel, nil, nil, nil, nil, nil), taskplugin.ErrBindingRejected)
	safe.Name = "rename"
	require.NoError(t, safe.UpdateFields("name"), "metadata changes do not need a plugin")
	require.NoError(t, catalog.Install(t.Context(), pkg), "explicit identical reimport restores model availability")
	require.NoError(t, late.Insert())
}

func TestTaskPluginChannelBeforeFirstInstallation(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		t.Run(fmt.Sprint(mapped), func(t *testing.T) {
			db := deletionDB(t)
			oldDB := DB
			DB = db
			t.Cleanup(func() { DB = oldDB })
			require.NoError(t, db.AutoMigrate(&Ability{}))
			catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
			require.NoError(t, err)
			channel := Channel{Name: "first", Models: "ordinary-model", Group: "default"}
			if mapped {
				value := `{"alias":"ordinary-model"}`
				channel.Models = ""
				channel.ModelMapping = &value
			}
			require.NoError(t, channel.Insert())
			rows, revision, err := catalog.ManagementSnapshot(t.Context())
			require.NoError(t, err, "an empty head created by a channel must remain readable")
			require.Empty(t, rows)
			require.EqualValues(t, 1, revision)
			pkg, _ := catalogFixture(t, "stored", "1.0.0", "first-install")
			require.ErrorIs(t, catalog.MutateExpected(t.Context(), 0, taskplugin.ManagementMutation{Action: "install", Package: pkg}), taskplugin.ErrCatalogConflict, "the prior observed zero revision cannot overwrite a committed bootstrap")
			require.NoError(t, catalog.MutateExpected(t.Context(), revision, taskplugin.ManagementMutation{Action: "install", Package: pkg}))
			restarted, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
			require.NoError(t, err)
			rows, revision, err = restarted.ManagementSnapshot(t.Context())
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.EqualValues(t, 2, revision)
		})
	}
}

func TestTaskPluginRequiresTypedChannelForExplicitUpdateTarget(t *testing.T) {
	db := deletionDB(t)
	channel := &Channel{Name: "original", Models: "ordinary"}
	require.NoError(t, db.Create(channel).Error)
	change := func(column string, value any) error {
		return db.Table("scratch").Clauses(clause.Update{Table: clause.Table{Name: "channels"}}).
			Where("id = ?", channel.Id).Update(column, value).Error
	}
	require.ErrorIs(t, change("models", "other-model"), taskplugin.ErrCatalogInvalid)
	require.NoError(t, change("name", "metadata-only"))
	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, "ordinary", stored.Models)
	require.Equal(t, "metadata-only", stored.Name)
}

func TestTaskPluginChannelMapAliasesCannotBypassDeletion(t *testing.T) {
	db := deletionDB(t)
	// GORM itself supports both DB and Go field names. This regression exercises
	// that boundary rather than the separate business update-map schema guard.
	require.NoError(t, db.Callback().Update().Remove("guard_catalog_update_columns"))
	catalog, err := taskplugin.OpenCatalog(t.Context(), NewTaskPluginCatalogStore(db))
	require.NoError(t, err)
	pkg, _ := catalogFixture(t, "stored", "1.0.0", "alias-reference")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	channel := Channel{Name: "ordinary", Models: "ordinary-model"}
	require.NoError(t, db.Create(&channel).Error)
	_, revision, err := catalog.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.NoError(t, catalog.MutateExpected(t.Context(), revision, taskplugin.ManagementMutation{Action: "delete", Key: "stored", Version: "1.0.0"}))
	for _, values := range []map[string]any{
		{"Models": "catalog-model"},
		{"ModelMapping": `{"alias":"catalog-model"}`},
	} {
		require.ErrorIs(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Updates(values).Error, taskplugin.ErrBindingRejected)
	}
	require.ErrorIs(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{"models": "ordinary-model", "Models": "catalog-model"}).Error, taskplugin.ErrCatalogInvalid, "ambiguous aliases are rejected deterministically")
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Omit("Models").Updates(map[string]any{"Models": "catalog-model", "Name": "renamed"}).Error, "excluded model fields cannot introduce references")
	var after Channel
	require.NoError(t, db.First(&after, channel.Id).Error)
	require.Equal(t, "ordinary-model", after.Models)
	require.Equal(t, "renamed", after.Name)
}

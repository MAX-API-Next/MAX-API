// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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
	safe.Models = "ordinary-model"
	safe.ModelMapping = nil
	require.NoError(t, safe.UpdateFields("models"), "unrelated model writes remain supported")
	tag := "guarded-tag"
	safe.Tag = &tag
	require.NoError(t, safe.UpdateFields("tag"))
	deletedModel := "catalog-model"
	require.ErrorIs(t, EditChannelByTag(tag, nil, nil, &deletedModel, nil, nil, nil, nil, nil), taskplugin.ErrBindingRejected)
	safe.Name = "rename"
	require.NoError(t, safe.UpdateFields("name"), "metadata changes do not need a plugin")
	require.NoError(t, catalog.Install(t.Context(), pkg), "explicit identical reimport restores model availability")
	require.NoError(t, late.Insert())
}

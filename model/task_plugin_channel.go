// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BeforeSave covers Insert, Save, UpdateFields, batch and tag-based writes.
// Only emitted model references participate; status/metadata-only patches do not.
func (channel *Channel) BeforeSave(tx *gorm.DB) error {
	models, mapping, err := channelReferenceUpdate(tx, channel)
	if err != nil {
		return err
	}
	if models == nil && mapping == nil || !tx.Migrator().HasTable(&TaskPluginDeletion{}) {
		return nil
	}
	references := map[string]bool{}
	if models != nil {
		for _, model := range strings.Split(*models, ",") {
			if model = strings.TrimSpace(model); model != "" {
				references[model] = true
			}
		}
	}
	if mapping != nil && strings.TrimSpace(*mapping) != "" {
		var values map[string]string
		if common.UnmarshalJsonStr(*mapping, &values) != nil {
			return taskplugin.ErrCatalogInvalid
		}
		for _, model := range values {
			if model != "" {
				references[model] = true
			}
		}
	}
	if len(references) == 0 {
		return nil
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return taskplugin.ErrCatalogUnavailable
	}
	if !tx.Migrator().HasColumn(&TaskPluginDeletion{}, "models") {
		return taskplugin.ErrCatalogUnavailable
	}
	// Bootstrap the shared lock even before the first catalog installation.
	// DoNothing does not overwrite an existing revision or deletion marker.
	head := TaskPluginCatalogHead{ID: 1, MutationID: common.GetUUID()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head).Error; err != nil {
		return err
	}
	if err := tx.Model(&TaskPluginCatalogHead{}).Where("id = ?", 1).UpdateColumn("mutation_id", gorm.Expr("mutation_id")).Error; err != nil {
		return err
	}
	var receipts []TaskPluginDeletion
	// Current reads avoid a stale MySQL REPEATABLE READ transaction snapshot.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("models").Find(&receipts).Error; err != nil {
		return err
	}
	deletedModels := map[string]bool{}
	for _, receipt := range receipts {
		if receipt.Models == "" {
			continue
		} // Legacy receipts predate the model index.
		var names []string
		if len(receipt.Models) > taskplugin.MaxPackageBytes || common.UnmarshalJsonStr(receipt.Models, &names) != nil {
			return taskplugin.ErrCatalogInvalid
		}
		for _, name := range names {
			if references[name] {
				deletedModels[name] = true
			}
		}
	}
	if len(deletedModels) == 0 {
		return nil
	}
	var versions []TaskPluginVersion
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&versions).Error; err != nil {
		return err
	}
	for _, version := range versions {
		if version.Revoked {
			continue
		}
		pkg, err := taskplugin.LoadPackage(bytes.NewReader(version.Document), version.Digest)
		if err != nil {
			return taskplugin.ErrCatalogInvalid
		}
		for _, name := range pkg.Manifest().Models {
			delete(deletedModels, name)
		}
	}
	if len(deletedModels) != 0 {
		return taskplugin.ErrBindingRejected
	}
	return nil
}

func channelReferenceUpdate(tx *gorm.DB, channel *Channel) (*string, *string, error) {
	selected, restricted := tx.Statement.SelectAndOmitColumns(false, true)
	included := func(column string) bool { value, exists := selected[column]; return value || !exists && !restricted }
	var models, mapping *string
	if values, ok := tx.Statement.Dest.(map[string]any); ok {
		for _, column := range []string{"models", "model_mapping"} {
			value, exists := values[column]
			if !exists || !included(column) || value == nil {
				continue
			}
			var text string
			switch v := value.(type) {
			case string:
				text = v
			case *string:
				if v == nil {
					continue
				}
				text = *v
			default:
				return nil, nil, taskplugin.ErrCatalogInvalid
			}
			if column == "models" {
				models = &text
			} else {
				mapping = &text
			}
		}
		return models, mapping, nil
	}
	candidate := channel
	switch value := tx.Statement.Dest.(type) {
	case *Channel:
		candidate = value
	case Channel:
		candidate = &value
	}
	if candidate == nil {
		return nil, nil, taskplugin.ErrCatalogInvalid
	}
	// Struct Updates omits zero values unless explicitly selected; Save selects *.
	if included("models") && (candidate.Models != "" || selected["models"]) {
		models = &candidate.Models
	}
	if included("model_mapping") && candidate.ModelMapping != nil {
		mapping = candidate.ModelMapping
	}
	return models, mapping, nil
}

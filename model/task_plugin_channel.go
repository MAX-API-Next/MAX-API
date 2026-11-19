// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"errors"
	"reflect"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BeforeSave covers Insert, Save, UpdateFields, batch and tag-based writes.
// Only emitted model references participate; status/metadata-only patches do not.
type channelEmittedReferences struct{ models, mapping *string }

const channelBatchReferenceCheck = "max:channel_batch_reference_check"

// GORM skips model hooks for map creates. Install this on the application
// handle before channel writes; map updates continue to use model hooks.
func registerTaskPluginChannelCreateGuard(db *gorm.DB) error {
	channelTable := db.NamingStrategy.TableName("Channel")
	return db.Callback().Create().Before("gorm:before_create").Register("max:channel_typed_create", func(tx *gorm.DB) {
		if tx.Statement.SkipHooks || tx.Statement.Table != channelTable {
			return
		}
		value := reflect.ValueOf(tx.Statement.Dest)
		for value.IsValid() && value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		if !value.IsValid() {
			return
		}
		typ := value.Type()
		if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
			for typ.Kind() == reflect.Pointer {
				typ = typ.Elem()
			}
		}
		if typ.Kind() == reflect.Map {
			tx.AddError(taskplugin.ErrCatalogInvalid)
		}
	})
}

func (channel *Channel) BeforeSave(tx *gorm.DB) error {
	statement := tx.Statement
	value := reflect.ValueOf(statement.Dest)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	batch := value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array)
	if checked, ok := statement.Settings.Load(channelBatchReferenceCheck); batch && statement.CurDestIndex > 0 && ok && checked == statement {
		return nil
	}
	receivers := []*Channel{channel}
	if batch {
		receivers = make([]*Channel, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			row := value.Index(i)
			for row.IsValid() && row.Kind() == reflect.Pointer {
				if row.IsNil() {
					return taskplugin.ErrCatalogInvalid
				}
				row = row.Elem()
			}
			if !row.IsValid() || row.Type() != reflect.TypeOf(Channel{}) || !row.CanAddr() {
				return taskplugin.ErrCatalogInvalid
			}
			receivers = append(receivers, row.Addr().Interface().(*Channel))
		}
	}
	updates := make([]channelEmittedReferences, 0, len(receivers))
	for _, receiver := range receivers {
		models, mapping, err := channelReferenceUpdate(tx, receiver)
		if err != nil {
			return err
		}
		if models != nil || mapping != nil {
			updates = append(updates, channelEmittedReferences{models, mapping})
		}
	}
	if len(updates) == 0 {
		return nil
	}
	if err := validateChannelReferenceSet(tx, updates); err != nil {
		return err
	}
	// Later rows reuse this execution's union. GORM resets CurDestIndex before
	// every create, including reuse of an initialized handle and its settings.
	if batch {
		statement.Settings.Store(channelBatchReferenceCheck, statement)
	}
	return nil
}

func validateChannelReferenceSet(tx *gorm.DB, updates []channelEmittedReferences) error {
	if err := taskPluginSQLiteWriteIntent(tx, &Channel{}); err != nil {
		return err
	}
	present, err := taskPluginDeletionTableAvailable(tx)
	if err != nil {
		return err
	}
	if !present {
		return validateTaskPluginAbsentReceiptSet(tx)
	}
	references := map[string]bool{}
	for _, update := range updates {
		models, mapping := update.models, update.mapping
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
	if err := lockTaskPluginCatalogHead(tx); err != nil {
		return err
	}
	var receipts []TaskPluginDeletion
	// Current reads avoid a stale MySQL REPEATABLE READ transaction snapshot.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("models").Find(&receipts).Error; err != nil {
		return err
	}
	head, err := taskPluginChannelHead(tx)
	if err != nil {
		return err
	}
	if err := validateTaskPluginDeletionPresence(head, len(receipts) != 0); err != nil {
		return err
	}
	deletedModels := map[string]bool{}
	for _, receipt := range receipts {
		if receipt.Models == "" {
			continue
		} // Legacy receipts predate model-name protection; see management upgrade guide.
		var names []string
		if len(receipt.Models) > maxTaskPluginModelIndexBytes || common.UnmarshalJsonStr(string(receipt.Models), &names) != nil || len(names) == 0 {
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
		manifest, err := taskPluginRetainedManifest(version)
		if err != nil {
			return taskplugin.ErrCatalogInvalid
		}
		for _, name := range manifest.Models {
			delete(deletedModels, name)
		}
	}
	if len(deletedModels) != 0 {
		return taskplugin.ErrBindingRejected
	}
	return nil
}

func taskPluginChannelHead(tx *gorm.DB) (TaskPluginCatalogHead, error) {
	var head TaskPluginCatalogHead
	err := tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.Locking{Strength: "UPDATE"}).First(&head, 1).Error
	if err == nil && (head.Revision <= 0 || head.MutationID == "") {
		err = taskplugin.ErrCatalogInvalid
	}
	return head, err
}

func validateTaskPluginAbsentReceiptSet(tx *gorm.DB) error {
	headPresent, err := taskPluginTableAvailable(tx, &TaskPluginCatalogHead{})
	if err != nil || !headPresent {
		return err
	}
	head, err := taskPluginChannelHead(tx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return validateTaskPluginDeletionPresence(head, false)
}

func channelReferenceUpdate(tx *gorm.DB, channel *Channel) (*string, *string, error) {
	var models, mapping *string
	for _, column := range []string{"models", "model_mapping"} {
		value, emitted, err := taskPluginEmittedField(tx, channel, column)
		if err != nil {
			return nil, nil, err
		}
		if !emitted || value == nil {
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

// Match GORM's dereferenced map/struct assignment fields, including partial
// destination schemas. For batch creates the hook receiver is the current row.
func taskPluginEmittedField(tx *gorm.DB, receiver any, column string) (any, bool, error) {
	selected, restricted := tx.Statement.SelectAndOmitColumns(false, true)
	value := reflect.ValueOf(tx.Statement.Dest)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil, false, nil
	}
	if values, ok := value.Interface().(map[string]any); ok {
		var result any
		found := false
		for key, value := range values {
			name := key
			if tx.Statement.Schema != nil {
				if field := tx.Statement.Schema.LookUpField(key); field != nil && field.DBName != "" {
					name = field.DBName
				}
			}
			// Unknown map keys are emitted verbatim by GORM. Select/Omit must
			// therefore use that spelling before applying DB identifier folding.
			if include, exists := selected[name]; !include && (exists || restricted) {
				continue
			}
			if !strings.EqualFold(name, column) {
				continue
			}
			if found {
				return nil, false, taskplugin.ErrCatalogInvalid
			}
			result, found = value, true
		}
		return result, found, nil
	}
	if include, exists := selected[column]; !include && (exists || restricted) {
		return nil, false, nil
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		value = reflect.Indirect(reflect.ValueOf(receiver))
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil, false, taskplugin.ErrCatalogInvalid
	}
	statement := &gorm.Statement{DB: tx}
	if err := statement.Parse(value.Interface()); err != nil {
		return nil, false, taskplugin.ErrCatalogInvalid
	}
	field := statement.Schema.LookUpField(column)
	if field == nil || !field.Updatable {
		return nil, false, nil
	}
	result, zero := field.ValueOf(tx.Statement.Context, value)
	return result, !zero || selected[column], nil
}

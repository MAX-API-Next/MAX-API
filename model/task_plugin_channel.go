// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"errors"
	"reflect"
	"strings"
	"unicode"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BeforeSave covers Insert, Save, UpdateFields, batch and tag-based writes.
// Only emitted model references participate; status/metadata-only patches do not.
type channelEmittedReferences struct{ models, mapping *string }

const channelBatchReferenceCheck = "max:channel_batch_reference_check"

// GORM skips model hooks for map creates, so reference-bearing rows must be
// inserted through their typed models.
func registerTaskPluginTypedCreateGuard(db *gorm.DB) error {
	channelTable := db.NamingStrategy.TableName("Channel")
	taskTable := db.NamingStrategy.TableName("Task")
	channelType, taskType := reflect.TypeFor[Channel](), reflect.TypeFor[Task]()
	dialect := db.Dialector.Name()
	foldTableNames := dialect == "sqlite" || dialect == "mysql"
	if foldTableNames {
		channelTable = foldTaskPluginTableName(channelTable)
		taskTable = foldTaskPluginTableName(taskTable)
	}
	if err := db.Callback().Create().Before("gorm:before_create").Register("max:task_plugin_typed_create", func(tx *gorm.DB) {
		if tx.Statement.SkipHooks {
			return
		}
		table, resolved := taskPluginStatementTable(tx.Statement)
		if explicit, exists := tx.Statement.Clauses["INSERT"]; exists {
			insert, knownInsert := explicit.Expression.(clause.Insert)
			if !knownInsert || insert.Table.Raw || insert.Table.Alias != "" || strings.ContainsAny(insert.Table.Name, "\"`[]") {
				tx.AddError(taskplugin.ErrCatalogInvalid)
				return
			}
			if name := insert.Table.Name; name != "" && name != clause.CurrentTable {
				table, resolved = taskPluginPlainTableName(name)
			}
		}
		if !resolved {
			tx.AddError(taskplugin.ErrCatalogInvalid)
			return
		}
		if foldTableNames {
			table = foldTaskPluginTableName(table)
		}
		guardedType := channelType
		if table == taskTable {
			guardedType = taskType
		} else if table != channelTable {
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
		if typ != guardedType || tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != guardedType {
			tx.AddError(taskplugin.ErrCatalogInvalid)
		}
	}); err != nil {
		return err
	}
	return db.Callback().Update().Before("gorm:before_update").Register("max:task_plugin_channel_update", func(tx *gorm.DB) {
		if tx.Statement.SkipHooks {
			return
		}
		table, resolved := taskPluginStatementTable(tx.Statement)
		if explicit, exists := tx.Statement.Clauses["UPDATE"]; exists {
			update, known := explicit.Expression.(clause.Update)
			if !known || update.Table.Raw || update.Table.Alias != "" || strings.ContainsAny(update.Table.Name, "\"\x60[]") {
				tx.AddError(taskplugin.ErrCatalogInvalid)
				return
			}
			if name := update.Table.Name; name != "" && name != clause.CurrentTable {
				table, resolved = taskPluginPlainTableName(name)
				if !resolved {
					tx.AddError(taskplugin.ErrCatalogInvalid)
					return
				}
			}
		}
		statementTable := tx.Statement.Table
		if foldTableNames {
			table = foldTaskPluginTableName(table)
			statementTable = foldTaskPluginTableName(statementTable)
		}
		if !resolved && statementTable == channelTable {
			tx.AddError(taskplugin.ErrCatalogInvalid)
			return
		}
		if table != channelTable {
			return
		}
		if tx.Statement.Schema != nil && tx.Statement.Schema.ModelType == channelType {
			return
		}
		if _, explicit := tx.Statement.Clauses["SET"]; explicit {
			tx.AddError(taskplugin.ErrCatalogInvalid)
			return
		}
		value := reflect.Indirect(reflect.ValueOf(tx.Statement.Dest))
		if !value.IsValid() || value.Kind() != reflect.Map || value.Type().Key().Kind() != reflect.String {
			tx.AddError(taskplugin.ErrCatalogInvalid)
			return
		}
		for _, key := range value.MapKeys() {
			column := key.String()
			column = strings.Trim(column[strings.LastIndexByte(column, '.')+1:], "\"`[]")
			switch strings.ToLower(column) {
			case "models", "modelmapping", "model_mapping":
				tx.AddError(taskplugin.ErrCatalogInvalid)
				return
			}
		}
	})
}

func taskPluginStatementTable(statement *gorm.Statement) (string, bool) {
	if statement.TableExpr == nil {
		return statement.Table, true
	}
	expression := statement.TableExpr
	parts := strings.Fields(expression.SQL)
	if len(expression.Vars) != 0 || len(parts) == 0 || len(parts) > 3 || len(parts) == 3 && !strings.EqualFold(parts[1], "AS") {
		return "", false
	}
	return taskPluginPlainTableName(parts[0])
}

func taskPluginPlainTableName(name string) (string, bool) {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		part = strings.Trim(part, "\"`[]")
		if part == "" || strings.ContainsFunc(part, func(r rune) bool {
			return r != '_' && r != '$' && r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			return "", false
		}
		parts[i] = part
	}
	return parts[len(parts)-1], true
}

func foldTaskPluginTableName(table string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, table)
}

func (channel *Channel) BeforeSave(tx *gorm.DB) error {
	statement := tx.Statement
	if _, explicit := statement.Clauses["SET"]; explicit {
		return taskplugin.ErrCatalogInvalid
	}
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
	if conflict, ok := statement.Clauses["ON CONFLICT"].Expression.(clause.OnConflict); ok {
		for _, assignment := range conflict.DoUpdates {
			column := assignment.Column.Name
			if !strings.EqualFold(column, "models") && !strings.EqualFold(column, "model_mapping") {
				continue
			}
			if assignment.Value == nil {
				continue
			}
			var text string
			switch value := assignment.Value.(type) {
			case string:
				text = value
			case *string:
				if value == nil {
					continue
				}
				text = *value
			case clause.Column:
				if value.Table == "excluded" && strings.EqualFold(value.Name, column) {
					continue // GORM UpdateAll reuses the already-validated inserted column.
				}
				return taskplugin.ErrCatalogInvalid
			default:
				return taskplugin.ErrCatalogInvalid // Expressions cannot certify the assigned reference.
			}
			update := channelEmittedReferences{}
			if strings.EqualFold(column, "models") {
				update.models = &text
			} else {
				update.mapping = &text
			}
			updates = append(updates, update)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	// Later rows reuse this execution's union. GORM resets CurDestIndex before
	// every create, including reuse of an initialized handle and its settings.
	// GORM retains the first hook error while continuing the remaining rows.
	if batch {
		statement.Settings.Store(channelBatchReferenceCheck, statement)
	}
	return validateChannelReferenceSet(tx, updates)
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
		if len(receipt.Models) > maxTaskPluginModelIndexBytes || common.UnmarshalJsonStr(string(receipt.Models), &names) != nil {
			return taskplugin.ErrCatalogInvalid
		}
		modelNames, err := taskplugin.ValidateModelNames(names)
		if err != nil {
			return taskplugin.ErrCatalogInvalid
		}
		for name := range modelNames {
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

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func loadTaskPluginDeletions(tx *gorm.DB) ([]taskplugin.PackageDeletion, error) {
	// Old isolated catalogs remain readable. Deletion itself requires migration.
	if !tx.Migrator().HasTable(&TaskPluginDeletion{}) {
		return nil, nil
	}
	var records []TaskPluginDeletion
	if err := tx.Order("identity").Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]taskplugin.PackageDeletion, 0, len(records))
	for _, record := range records {
		if record.Identity != taskPluginIdentity(record.PluginKey, record.Version) {
			return nil, taskplugin.ErrCatalogInvalid
		}
		result = append(result, taskplugin.PackageDeletion{Key: record.PluginKey, Version: record.Version, Digest: record.Digest, Revoked: record.Revoked})
	}
	return result, nil
}

func taskPluginIdentity(key, version string) string {
	sum := sha256.Sum256([]byte(key + "@" + version))
	return hex.EncodeToString(sum[:])
}

func validateTaskPluginDeletionHead(head TaskPluginCatalogHead, deletions []taskplugin.PackageDeletion) error {
	if head.LastDeletionRevision < 0 || head.LastDeletionRevision > head.Revision ||
		(head.LastDeletionRevision > 0) != (len(deletions) > 0) {
		return taskplugin.ErrCatalogInvalid
	}
	return nil
}

// DeleteVersion is deliberately separate from ordinary CAS. It never promotes
// another version, deletes a bundled distribution file, or erases an audit ID.
// The same head lock serializes task binding persistence with reference checks.
func (store *TaskPluginCatalogStore) DeleteVersion(ctx context.Context, revision int64, key, version string) (int64, error) {
	if store == nil || store.db == nil || ctx == nil {
		return 0, taskplugin.ErrCatalogUnavailable
	}
	if revision <= 0 || revision == math.MaxInt64 || key == "" || version == "" {
		return 0, taskplugin.ErrCatalogInvalid
	}
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{&TaskPluginDeletion{}, &Task{}, &Channel{}, &Option{}} {
			if !tx.Migrator().HasTable(table) {
				return taskplugin.ErrCatalogUnavailable
			}
		}
		var head TaskPluginCatalogHead
		// Match pricing writers before taking the catalog head or reading references.
		if err := lockModelPricingRows(tx); err != nil {
			return err
		}
		if !tx.Migrator().HasColumn(&TaskPluginDeletion{}, "models") {
			return taskplugin.ErrCatalogUnavailable
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&head, 1).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return taskplugin.ErrCatalogConflict
			}
			return err
		}
		if head.Revision != revision {
			return taskplugin.ErrCatalogConflict
		}
		deletions, err := loadTaskPluginDeletions(tx)
		if err != nil {
			return err
		}
		if err := validateTaskPluginDeletionHead(head, deletions); err != nil {
			return err
		}
		result := tx.Model(&TaskPluginCatalogHead{}).Where("id = ? AND revision = ?", 1, revision).
			Updates(map[string]any{"revision": revision + 1, "mutation_id": common.GetUUID(), "last_deletion_revision": revision + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskplugin.ErrCatalogConflict
		}
		rows, identities, err := loadTaskPluginVersions(tx)
		if err != nil {
			return err
		}
		identity := taskPluginIdentity(key, version)
		index := -1
		for i, id := range identities {
			if id == identity {
				index = i
				break
			}
		}
		if index < 0 {
			return taskplugin.ErrBindingRejected
		}
		entry := rows[index]
		pkg, err := taskplugin.LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
		if err != nil {
			return taskplugin.ErrCatalogInvalid
		}
		if err := rejectTaskPluginReferences(tx, pkg); err != nil {
			return err
		}
		receipt := taskplugin.PackageDeletion{Key: key, Version: version, Digest: entry.Digest, Revoked: entry.Revoked}
		nextDeletions := append([]taskplugin.PackageDeletion(nil), deletions...)
		found := false
		for i := range nextDeletions {
			if nextDeletions[i].Key == key && nextDeletions[i].Version == version {
				if nextDeletions[i].Digest != entry.Digest {
					return taskplugin.ErrCatalogInvalid
				}
				receipt.Revoked = receipt.Revoked || nextDeletions[i].Revoked
				nextDeletions[i], found = receipt, true
			}
		}
		if !found {
			nextDeletions = append(nextDeletions, receipt)
		}
		nextRows := append(append([]taskplugin.StoredPackage(nil), rows[:index]...), rows[index+1:]...)
		previous := taskplugin.CatalogSnapshot{Revision: revision, Packages: rows, Deletions: deletions}
		next := taskplugin.CatalogSnapshot{Revision: revision + 1, Packages: nextRows, Deletions: nextDeletions}
		if err := taskplugin.ValidateCatalogTransition(previous, next); err != nil {
			return err
		}
		models, err := common.Marshal(pkg.Manifest().Models)
		if err != nil {
			return err
		}
		if len(models) > maxTaskPluginModelIndexBytes {
			return taskplugin.ErrCatalogInvalid
		}
		record := TaskPluginDeletion{Identity: identity, PluginKey: key, Version: version, Digest: entry.Digest, Revoked: receipt.Revoked, Models: taskPluginModelIndex(models)}
		if found {
			if err := tx.Model(&TaskPluginDeletion{}).Where("identity = ?", identity).Updates(map[string]any{"revoked": receipt.Revoked, "models": record.Models}).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = tx.Where("identity = ? AND digest = ?", identity, entry.Digest).Delete(&TaskPluginVersion{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskplugin.ErrCatalogConflict
		}
		return nil
	})
	if err != nil {
		for _, known := range []error{taskplugin.ErrCatalogInUse, taskplugin.ErrCatalogConflict, taskplugin.ErrCatalogInvalid, taskplugin.ErrBindingRejected} {
			if errors.Is(err, known) {
				return 0, known
			}
		}
		return 0, taskplugin.ErrCatalogUnavailable
	}
	return revision + 1, nil
}

func rejectTaskPluginReferences(tx *gorm.DB, pkg *taskplugin.Package) error {
	m := pkg.Manifest()
	// Inspect all historical statuses. A terminal task still owns its binding,
	// frozen pricing and recovery evidence. Never log or return private JSON.
	rows, err := tx.Model(&Task{}).Select("private_data").Where("private_data IS NOT NULL").Rows()
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var value any
		if err := common.Unmarshal(raw, &value); err != nil {
			return taskplugin.ErrCatalogUnavailable
		}
		if taskPluginReference(value, m.Key, m.Version, pkg.Digest()) {
			return taskplugin.ErrCatalogInUse
		}
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// MAX currently derives plugin contexts from declared/mapped channel models,
	// rather than a dedicated channel plugin-version column. Disabled channels
	// also retain these configurations, so do not filter them out.
	var channels []struct {
		Models       string
		ModelMapping *string
	}
	if err := tx.Model(&Channel{}).Select("models", "model_mapping").Find(&channels).Error; err != nil {
		return err
	}
	models := map[string]bool{}
	for _, model := range m.Models {
		models[model] = true
	}
	for _, channel := range channels {
		for _, model := range strings.Split(channel.Models, ",") {
			if models[strings.TrimSpace(model)] {
				return taskplugin.ErrCatalogInUse
			}
		}
		if channel.ModelMapping != nil && strings.TrimSpace(*channel.ModelMapping) != "" {
			var mapping map[string]string
			if common.UnmarshalJsonStr(*channel.ModelMapping, &mapping) != nil {
				return taskplugin.ErrCatalogUnavailable
			}
			for _, model := range mapping {
				if models[model] {
					return taskplugin.ErrCatalogInUse
				}
			}
		}
	}
	var options []Option
	if err := tx.Where(map[string]any{"key": billing_setting.PluginBillingExprOption}).Find(&options).Error; err != nil {
		return err
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) == "" {
			continue
		}
		var variants map[string]string
		if common.UnmarshalJsonStr(option.Value, &variants) != nil {
			return taskplugin.ErrCatalogUnavailable
		}
		for composite := range variants {
			key, _, ok := splitPluginPriceKey(composite)
			if !ok {
				return taskplugin.ErrCatalogUnavailable
			}
			if key == m.Key {
				return taskplugin.ErrCatalogInUse
			}
		}
	}
	return nil
}

func taskPluginReference(value any, key, version, digest string) bool {
	switch v := value.(type) {
	case map[string]any:
		if v["package_digest"] == digest {
			return true
		}
		if v["selected_plugin_key"] == key {
			return true
		}
		if v["plugin_key"] == key {
			boundVersion, _ := v["version"].(string)
			if explicit, ok := v["plugin_version"].(string); ok {
				boundVersion = explicit
			}
			if boundVersion == "" || boundVersion == version {
				return true
			}
		}
		for _, child := range v {
			if taskPluginReference(child, key, version, digest) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if taskPluginReference(child, key, version, digest) {
				return true
			}
		}
	}
	return false
}

func guardTaskPluginPersistence(tx *gorm.DB, task *Task) error {
	if values, ok := tx.Statement.Dest.(map[string]any); ok {
		selected, restricted := tx.Statement.SelectAndOmitColumns(false, true)
		seen := false
		for name, value := range values {
			field := tx.Statement.Schema.LookUpField(name)
			if field == nil || field.DBName != "private_data" {
				continue
			}
			if include, exists := selected["private_data"]; !include && (exists || restricted) {
				continue
			}
			if seen {
				return taskplugin.ErrCatalogInvalid
			}
			seen = true
			var private TaskPrivateData
			switch data := value.(type) {
			case TaskPrivateData:
				private = data
			case *TaskPrivateData:
				if data != nil {
					private = *data
				}
			case string:
				if common.UnmarshalJsonStr(data, &private) != nil {
					return taskplugin.ErrCatalogInvalid
				}
			case []byte:
				if common.Unmarshal(data, &private) != nil {
					return taskplugin.ErrCatalogInvalid
				}
			case nil:
			default:
				return taskplugin.ErrCatalogInvalid
			}
			candidate := *task
			candidate.PrivateData = private
			task = &candidate
		}
	} else {
		var update *Task
		switch value := tx.Statement.Dest.(type) {
		case Task:
			update = &value
		case *Task:
			update = value
		}
		if update != nil {
			selected, restricted := tx.Statement.SelectAndOmitColumns(false, true)
			include, exists := selected["private_data"]
			if (include || !exists && !restricted) && (update.PrivateData != (TaskPrivateData{}) || include) {
				candidate := *task
				candidate.PrivateData = update.PrivateData
				task = &candidate
			}
		}
	}
	key, version, digest := "", "", ""
	if binding := task.PrivateData.PluginBinding; binding != nil {
		key, version, digest = binding.PluginKey, binding.Version, binding.PackageDigest
	} else if context := task.PrivateData.BillingContext; context != nil && context.TaskBillingPlan != nil && context.TaskBillingPlan.ExpressionPlan != nil {
		p := context.TaskBillingPlan.ExpressionPlan
		key, version, digest = p.PluginKey, p.PluginVersion, p.PackageDigest
	}
	if key == "" || !tx.Migrator().HasTable(&TaskPluginDeletion{}) {
		return nil
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return taskplugin.ErrCatalogUnavailable
	}
	// A no-op UPDATE acquires the write lock on SQLite too. MySQL may report
	// zero affected rows for it; existence is checked separately, not guessed.
	if err := tx.Model(&TaskPluginCatalogHead{}).Where("id = ?", 1).UpdateColumn("mutation_id", gorm.Expr("mutation_id")).Error; err != nil {
		return err
	}
	var head TaskPluginCatalogHead
	if err := tx.First(&head, 1).Error; err != nil {
		return taskplugin.ErrBindingRejected
	}
	var record TaskPluginVersion
	// A locking/current read matters under MySQL REPEATABLE READ: a funding
	// transaction may have established its consistent snapshot before deletion.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("identity", "digest").Where("identity = ? AND digest = ?", taskPluginIdentity(key, version), digest).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return taskplugin.ErrBindingRejected
		}
		return err
	}
	return nil
}

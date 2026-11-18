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
// Deletion owns its transaction so the reference scan cannot inherit a read view.
func (store *TaskPluginCatalogStore) DeleteVersion(ctx context.Context, revision int64, key, version string) (int64, error) {
	if store == nil || store.db == nil || ctx == nil {
		return 0, taskplugin.ErrCatalogUnavailable
	}
	if revision <= 0 || revision == math.MaxInt64 || key == "" || version == "" {
		return 0, taskplugin.ErrCatalogInvalid
	}
	db := store.db.WithContext(ctx)
	if _, inherited := db.Statement.ConnPool.(gorm.TxCommitter); inherited {
		return 0, taskplugin.ErrCatalogUnavailable
	}
	// Keep schema metadata reads outside the fresh transaction. Its first
	// nonlocking read must follow the pricing/head locks, regardless of whether
	// a server's INFORMATION_SCHEMA reads establish a consistent snapshot.
	for _, table := range []any{&TaskPluginDeletion{}, &Task{}, &Channel{}, &Option{}} {
		if !db.Migrator().HasTable(table) {
			return 0, taskplugin.ErrCatalogUnavailable
		}
	}
	if !db.Migrator().HasColumn(&TaskPluginDeletion{}, "models") {
		return 0, taskplugin.ErrCatalogUnavailable
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		var head TaskPluginCatalogHead
		// Match pricing writers before taking the catalog head or reading references.
		if err := lockModelPricingRows(tx); err != nil {
			return err
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
		if _, object := value.(map[string]any); object {
			// Match the typed persistence decoder's case-insensitive keys. Keep
			// the recursive check above for legacy maps/arrays and unknown fields.
			var private TaskPrivateData
			if err := common.Unmarshal(raw, &private); err != nil {
				return taskplugin.ErrCatalogUnavailable
			}
			if taskPluginTypedReference(private, m.Key, m.Version, pkg.Digest()) {
				return taskplugin.ErrCatalogInUse
			}
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

func taskPluginTypedReference(private TaskPrivateData, key, version, digest string) bool {
	if binding := private.PluginBinding; binding != nil &&
		(binding.PackageDigest == digest || binding.PluginKey == key && (binding.Version == "" || binding.Version == version)) {
		return true
	}
	if context := private.BillingContext; context != nil {
		if context.TaskBillingPlan != nil && context.TaskBillingPlan.ExpressionPlan != nil {
			p := context.TaskBillingPlan.ExpressionPlan
			if p.PackageDigest == digest || p.SelectedPluginKey == key || p.PluginKey == key && (p.PluginVersion == "" || p.PluginVersion == version) {
				return true
			}
		}
		return context.PricingUsageEnvelope != nil && context.PricingUsageEnvelope.PackageDigest == digest
	}
	return false
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
	value, emitted, err := taskPluginEmittedField(tx, task, "private_data")
	if err != nil {
		return err
	}
	if !emitted {
		return nil
	}
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
	type reference struct{ key, version, digest string }
	references := make([]reference, 0, 3)
	if binding := task.PrivateData.PluginBinding; binding != nil {
		if binding.PluginKey != "" || binding.PackageDigest != "" {
			references = append(references, reference{binding.PluginKey, binding.Version, binding.PackageDigest})
		}
	}
	if context := task.PrivateData.BillingContext; context != nil {
		if context.TaskBillingPlan != nil && context.TaskBillingPlan.ExpressionPlan != nil {
			p := context.TaskBillingPlan.ExpressionPlan
			// Frozen plans permit an empty selection or the same plugin identity.
			// Reject malformed selections even when another valid binding exists.
			if p.SelectedPluginKey != "" && p.SelectedPluginKey != p.PluginKey {
				return taskplugin.ErrBindingRejected
			}
			if p.PluginKey != "" || p.PackageDigest != "" {
				references = append(references, reference{p.PluginKey, p.PluginVersion, p.PackageDigest})
			}
		}
		if envelope := context.PricingUsageEnvelope; envelope != nil && envelope.PackageDigest != "" {
			references = append(references, reference{digest: envelope.PackageDigest})
		}
	}
	if len(references) == 0 || !tx.Migrator().HasTable(&TaskPluginDeletion{}) {
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
	// A locking/current read matters under MySQL REPEATABLE READ: a funding
	// transaction may have established its consistent snapshot before deletion.
	for _, reference := range references {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("identity", "digest").Where("digest = ?", reference.digest)
		if reference.key != "" {
			query = query.Where("identity = ?", taskPluginIdentity(reference.key, reference.version))
		}
		var record TaskPluginVersion
		if err := query.First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return taskplugin.ErrBindingRejected
			}
			return err
		}
	}
	return nil
}

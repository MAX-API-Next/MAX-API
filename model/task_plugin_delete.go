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
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
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
	if err := tx.Select("identity", "plugin_key", "version", "digest", "revoked").Order("identity").Find(&records).Error; err != nil {
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
		if err := rejectTaskPluginDefaultRemoval(tx, pkg, nextRows); err != nil {
			return err
		}
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
		result := tx.Model(&TaskPluginCatalogHead{}).Where("id = ? AND revision = ?", 1, revision).
			Updates(map[string]any{"revision": revision + 1, "mutation_id": common.GetUUID(), "last_deletion_revision": revision + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskplugin.ErrCatalogConflict
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
			unversionedSelection := p.SelectedPluginKey == key && (p.PluginKey != p.SelectedPluginKey || p.PluginVersion == "")
			if p.PackageDigest == digest || unversionedSelection || p.PluginKey == key && (p.PluginVersion == "" || p.PluginVersion == version) {
				return true
			}
		}
		return context.PricingUsageEnvelope != nil && context.PricingUsageEnvelope.PackageDigest == digest
	}
	return false
}

// Called under pricing/head locks before changing the catalog. Only reject
// newly invalid defaults; do not rewrite prices or block removal when another
// declared source still satisfies the existing configuration contract.
func rejectTaskPluginDefaultRemoval(tx *gorm.DB, pkg *taskplugin.Package, remaining []taskplugin.StoredPackage) error {
	values, err := readModelPricingMaps(tx)
	if err != nil {
		return taskplugin.ErrCatalogUnavailable
	}
	var before *pricingAuthority
	for _, model := range pkg.Manifest().Models {
		draft := effectiveModelPricing(values, model)
		expression, _ := draft["billing_setting.billing_expr"].(string)
		if draft["billing_setting.billing_mode"] != "tiered_expr" {
			continue
		}
		keys, err := billingexpr.TaskUsageKeys(expression)
		if err != nil || len(keys) == 0 {
			continue
		}
		if before == nil {
			before, err = readPricingAuthority(tx, false)
			if err != nil {
				return taskplugin.ErrCatalogUnavailable
			}
		}
		if validateDefaultTaskPricing(expression, draft, before.contexts(model), keys) != nil {
			continue // Existing diagnosed drift is not introduced by this deletion.
		}
		after := *before
		after.plugins = remaining
		if validateDefaultTaskPricing(expression, draft, after.contexts(model), keys) != nil {
			return taskplugin.ErrCatalogInUse
		}
	}
	return nil
}

type taskPluginReferenceIdentity struct{ key, version, digest string }

// Keep legacy deletion scans and serialized write validation on the same
// reference vocabulary. Returning true from visit stops the traversal.
func visitTaskPluginReferences(value any, visit func(taskPluginReferenceIdentity) bool) bool {
	switch v := value.(type) {
	case map[string]any:
		key, _ := v["plugin_key"].(string)
		boundVersion, _ := v["version"].(string)
		if explicit, ok := v["plugin_version"].(string); ok {
			boundVersion = explicit
		}
		if digest, ok := v["package_digest"].(string); ok && digest != "" && visit(taskPluginReferenceIdentity{digest: digest}) {
			return true
		}
		if selected, ok := v["selected_plugin_key"].(string); ok && selected != "" {
			ref := taskPluginReferenceIdentity{key: selected}
			if selected == key {
				ref.version = boundVersion
			}
			if visit(ref) {
				return true
			}
		}
		if key != "" {
			if visit(taskPluginReferenceIdentity{key: key, version: boundVersion}) {
				return true
			}
		}
		for _, child := range v {
			if visitTaskPluginReferences(child, visit) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if visitTaskPluginReferences(child, visit) {
				return true
			}
		}
	}
	return false
}

func taskPluginReference(value any, key, version, digest string) bool {
	return visitTaskPluginReferences(value, func(ref taskPluginReferenceIdentity) bool {
		return ref.digest != "" && ref.digest == digest || ref.key == key && (ref.version == "" || ref.version == version)
	})
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
	var serialized []byte
	serializedInput := false
	switch data := value.(type) {
	case TaskPrivateData:
		private = data
	case *TaskPrivateData:
		if data != nil {
			private = *data
		}
	case string:
		serialized = []byte(data)
		serializedInput = true
	case []byte:
		serialized = data
		serializedInput = true
	case nil:
	default:
		return taskplugin.ErrCatalogInvalid
	}
	legacy := map[taskPluginReferenceIdentity]bool{}
	if serializedInput {
		var raw any
		if common.Unmarshal(serialized, &private) != nil || common.Unmarshal(serialized, &raw) != nil {
			return taskplugin.ErrCatalogInvalid
		}
		visitTaskPluginReferences(raw, func(ref taskPluginReferenceIdentity) bool {
			legacy[ref] = true
			return false
		})
	}
	references := make([]taskPluginReferenceIdentity, 0, 3)
	if binding := private.PluginBinding; binding != nil {
		if binding.PluginKey != "" || binding.PackageDigest != "" {
			references = append(references, taskPluginReferenceIdentity{binding.PluginKey, binding.Version, binding.PackageDigest})
		}
	}
	if context := private.BillingContext; context != nil {
		if context.TaskBillingPlan != nil && context.TaskBillingPlan.ExpressionPlan != nil {
			p := context.TaskBillingPlan.ExpressionPlan
			// Frozen plans permit an empty selection or the same plugin identity.
			// Reject malformed selections even when another valid binding exists.
			if p.SelectedPluginKey != "" && p.SelectedPluginKey != p.PluginKey {
				return taskplugin.ErrBindingRejected
			}
			if p.PluginKey != "" || p.PackageDigest != "" {
				references = append(references, taskPluginReferenceIdentity{p.PluginKey, p.PluginVersion, p.PackageDigest})
			}
		}
		if envelope := context.PricingUsageEnvelope; envelope != nil && envelope.PackageDigest != "" {
			references = append(references, taskPluginReferenceIdentity{digest: envelope.PackageDigest})
		}
	}
	if len(references) == 0 && len(legacy) == 0 {
		return nil
	}
	if err := taskPluginSQLiteWriteIntent(tx, &Task{}); err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&TaskPluginDeletion{}) {
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
		// Serialized canonical fields have already passed the stronger tuple
		// check. Avoid extra queries for their individual legacy projections.
		delete(legacy, taskPluginReferenceIdentity{digest: reference.digest})
		if reference.key != "" {
			delete(legacy, taskPluginReferenceIdentity{key: reference.key, version: reference.version})
			delete(legacy, taskPluginReferenceIdentity{key: reference.key})
		}
	}
	var retainedKeys map[string]bool
	for reference := range legacy {
		if reference.key != "" && reference.version == "" {
			if retainedKeys == nil {
				// Only legacy key-only references need manifests. Read immutable
				// metadata without compiling JavaScript inside the caller's transaction.
				var records []TaskPluginVersion
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&records).Error; err != nil {
					return err
				}
				retainedKeys = map[string]bool{}
				for _, record := range records {
					manifest, err := taskPluginRetainedManifest(record)
					if err != nil {
						return err
					}
					retainedKeys[manifest.Key] = true
				}
			}
			if !retainedKeys[reference.key] {
				return taskplugin.ErrBindingRejected
			}
			continue
		}
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("identity")
		if reference.key != "" {
			query = query.Where("identity = ?", taskPluginIdentity(reference.key, reference.version))
		} else {
			query = query.Where("digest = ?", reference.digest)
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

// Immutable rows have already passed the executable loader on installation.
// Metadata-only reference checks verify their bytes and identity without
// executing JavaScript again while the catalog head is locked.
func taskPluginRetainedManifest(record TaskPluginVersion) (taskplugin.Manifest, error) {
	var document taskplugin.PackageDocument
	sum := sha256.Sum256(record.Document)
	if len(record.Document) > taskplugin.MaxPackageBytes || hex.EncodeToString(sum[:]) != record.Digest || common.Unmarshal(bytes.TrimPrefix(record.Document, []byte{0xef, 0xbb, 0xbf}), &document) != nil || taskPluginIdentity(document.Manifest.Key, document.Manifest.Version) != record.Identity || taskplugin.ValidateManifest(document.Manifest) != nil {
		return taskplugin.Manifest{}, taskplugin.ErrCatalogInvalid
	}
	if document.FormatVersion < 1 || document.FormatVersion > 3 {
		return taskplugin.Manifest{}, taskplugin.ErrCatalogInvalid
	}
	return document.Manifest, nil
}

func taskPluginSQLiteWriteIntent(tx *gorm.DB, target any) error {
	if tx.Dialector.Name() != "sqlite" {
		return nil
	}
	if _, transactional := tx.Statement.ConnPool.(gorm.TxCommitter); !transactional {
		return nil // Legacy stores without the receipt schema still permit writes.
	}
	// A zero-row write takes SQLite write intent before schema SELECTs can
	// establish a WAL snapshot. No task/channel values or global PRAGMAs change.
	return tx.Session(&gorm.Session{NewDB: true, SkipHooks: true, SkipDefaultTransaction: true}).
		Model(target).Where("1 = 0").UpdateColumn("id", gorm.Expr("id")).Error
}

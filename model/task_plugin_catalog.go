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
	"encoding/hex"
	"errors"
	"math"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// One revision serializes catalog mutations across nodes, including bootstrap.
// MutationID distinguishes a duplicate INSERT under MySQL CLIENT_FOUND_ROWS.
type TaskPluginCatalogHead struct {
	ID         uint   `gorm:"primaryKey;autoIncrement:false"`
	Revision   int64  `gorm:"not null"`
	MutationID string `gorm:"size:64;not null"`
}

type taskPluginDocument string

func (taskPluginDocument) GormDataType() string { return "string" }
func (taskPluginDocument) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "longtext"
	}
	return "text"
}

// Identity hashes key/version to keep the primary index within MySQL 5.7's
// older utf8mb4 limits. The pinned document remains the identity authority.
// Documents are immutable, private, and never changed by metadata updates.
type TaskPluginVersion struct {
	Identity string             `gorm:"size:64;primaryKey" json:"-"`
	Digest   string             `gorm:"size:64;not null" json:"-"`
	Document taskPluginDocument `gorm:"not null" json:"-"`
	Active   bool               `gorm:"not null"`
	Enabled  bool               `gorm:"not null"`
	Revoked  bool               `gorm:"not null"`
}

type TaskPluginCatalogStore struct{ db *gorm.DB }

func NewTaskPluginCatalogStore(db *gorm.DB) *TaskPluginCatalogStore {
	if db == nil {
		return &TaskPluginCatalogStore{}
	}
	// A driver error can embed SQL arguments containing a package's source.
	return &TaskPluginCatalogStore{db: db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)})}
}

var _ taskplugin.CatalogStore = (*TaskPluginCatalogStore)(nil)

// SchemaReady only observes the two catalog tables. Replica startup must not
// perform DDL; the master remains responsible for migrations.
func (store *TaskPluginCatalogStore) SchemaReady(ctx context.Context) bool {
	if store == nil || store.db == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	db := store.db.WithContext(ctx)
	return db.Migrator().HasTable(&TaskPluginCatalogHead{}) && db.Migrator().HasTable(&TaskPluginVersion{})
}

func loadTaskPluginVersions(tx *gorm.DB) ([]taskplugin.StoredPackage, error) {
	var records []TaskPluginVersion
	if err := tx.Order("identity").Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]taskplugin.StoredPackage, 0, len(records))
	for _, record := range records {
		entry := taskplugin.StoredPackage{Document: []byte(record.Document), Digest: record.Digest, Active: record.Active, Enabled: record.Enabled, Revoked: record.Revoked}
		identity, err := taskPluginStorageIdentity(entry)
		if err != nil || identity != record.Identity {
			return nil, taskplugin.ErrCatalogInvalid
		}
		result = append(result, entry)
	}
	return result, nil
}

func taskPluginStorageIdentity(entry taskplugin.StoredPackage) (string, error) {
	p, err := taskplugin.LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
	if err != nil {
		return "", taskplugin.ErrCatalogInvalid
	}
	manifest := p.Manifest()
	digest := sha256.Sum256([]byte(manifest.Key + "@" + manifest.Version))
	return hex.EncodeToString(digest[:]), nil
}

func (store *TaskPluginCatalogStore) Load(ctx context.Context) (taskplugin.CatalogSnapshot, error) {
	if store == nil || store.db == nil || ctx == nil {
		return taskplugin.CatalogSnapshot{}, taskplugin.ErrCatalogUnavailable
	}
	var snapshot taskplugin.CatalogSnapshot
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var head TaskPluginCatalogHead
		err := tx.First(&head, 1).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		if !missing && (head.Revision <= 0 || head.MutationID == "") {
			return taskplugin.ErrCatalogInvalid
		}
		rows, err := loadTaskPluginVersions(tx)
		if err != nil {
			return err
		}
		if missing && len(rows) != 0 {
			return taskplugin.ErrCatalogInvalid
		}
		var after TaskPluginCatalogHead
		err = tx.First(&after, 1).Error
		// Under READ COMMITTED, reject a mixed snapshot. MySQL's consistent
		// snapshot and SQLite's transaction also satisfy this invariant.
		if missing {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return taskplugin.ErrCatalogConflict
			}
		} else if err != nil || head != after {
			return taskplugin.ErrCatalogConflict
		}
		snapshot = taskplugin.CatalogSnapshot{Revision: head.Revision, Packages: rows}
		return nil
	})
	if err != nil {
		if errors.Is(err, taskplugin.ErrCatalogInvalid) || errors.Is(err, taskplugin.ErrCatalogConflict) {
			return taskplugin.CatalogSnapshot{}, err
		}
		return taskplugin.CatalogSnapshot{}, taskplugin.ErrCatalogUnavailable
	}
	return snapshot, nil
}

func (store *TaskPluginCatalogStore) CompareAndSwap(ctx context.Context, revision int64, packages []taskplugin.StoredPackage) (int64, error) {
	if store == nil || store.db == nil || ctx == nil {
		return 0, taskplugin.ErrCatalogUnavailable
	}
	if revision < 0 || revision == math.MaxInt64 {
		return 0, taskplugin.ErrCatalogInvalid
	}
	next := taskplugin.CatalogSnapshot{Revision: revision + 1, Packages: packages}
	if err := taskplugin.ValidateCatalogTransition(taskplugin.CatalogSnapshot{}, next); err != nil {
		return 0, err
	}
	mutationID := common.GetUUID()
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if revision == 0 {
			head := TaskPluginCatalogHead{ID: 1, Revision: 1, MutationID: mutationID}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head).Error; err != nil {
				return err
			}
			var claimed TaskPluginCatalogHead
			if err := tx.First(&claimed, 1).Error; err != nil {
				return err
			}
			if claimed.MutationID != mutationID || claimed.Revision != 1 {
				return taskplugin.ErrCatalogConflict
			}
		} else {
			result := tx.Model(&TaskPluginCatalogHead{}).Where("id = ? AND revision = ?", 1, revision).
				Updates(map[string]any{"revision": revision + 1, "mutation_id": mutationID})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return taskplugin.ErrCatalogConflict
			}
		}
		previousRows, err := loadTaskPluginVersions(tx)
		if err != nil {
			return err
		}
		if revision == 0 && len(previousRows) != 0 {
			return taskplugin.ErrCatalogInvalid
		}
		previous := taskplugin.CatalogSnapshot{Revision: revision, Packages: previousRows}
		if err := taskplugin.ValidateCatalogTransition(previous, next); err != nil {
			return err
		}
		known := make(map[string]bool, len(previousRows))
		for _, old := range previousRows {
			identity, err := taskPluginStorageIdentity(old)
			if err != nil {
				return err
			}
			known[identity] = true
		}
		for _, entry := range next.Packages {
			identity, err := taskPluginStorageIdentity(entry)
			if err != nil {
				return err
			}
			if known[identity] {
				if err := tx.Model(&TaskPluginVersion{}).Where("identity = ?", identity).
					Updates(map[string]any{"active": entry.Active, "enabled": entry.Enabled, "revoked": entry.Revoked}).Error; err != nil {
					return err
				}
			} else {
				record := TaskPluginVersion{Identity: identity, Digest: entry.Digest, Document: taskPluginDocument(entry.Document), Active: entry.Active, Enabled: entry.Enabled, Revoked: entry.Revoked}
				if err := tx.Create(&record).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, taskplugin.ErrCatalogConflict) || errors.Is(err, taskplugin.ErrCatalogInvalid) {
			return 0, err
		}
		return 0, taskplugin.ErrCatalogUnavailable
	}
	return revision + 1, nil
}

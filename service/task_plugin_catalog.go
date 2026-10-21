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

package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
)

var taskPluginCatalog atomic.Pointer[taskplugin.Catalog]

// WaitForTaskPluginCatalogSchema lets non-master nodes wait for the master to
// create both tables, without DDL or exposing traffic. It waits at most ten
// minutes and respects an earlier caller deadline or cancellation.
func WaitForTaskPluginCatalogSchema(ctx context.Context, db *gorm.DB) error {
	return waitForTaskPluginCatalogSchema(ctx, db, time.Second)
}

func waitForTaskPluginCatalogSchema(ctx context.Context, db *gorm.DB, interval time.Duration) error {
	if ctx == nil || db == nil {
		return taskplugin.ErrCatalogUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	store := model.NewTaskPluginCatalogStore(db)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if store.SchemaReady(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// InitializeTaskPluginCatalog restores all pinned versions and revocations
// before startup opens traffic. Corrupt/missing storage must stop startup.
func InitializeTaskPluginCatalog(ctx context.Context, db *gorm.DB) error {
	catalog, err := taskplugin.OpenCatalog(ctx, model.NewTaskPluginCatalogStore(db))
	if err != nil {
		return err
	}
	taskPluginCatalog.Store(catalog)
	return nil
}

// CurrentTaskPluginCatalog is internal; no public management or relay routes
// are registered here. Call Refresh to observe other nodes' committed changes.
func CurrentTaskPluginCatalog() (*taskplugin.Catalog, error) {
	catalog := taskPluginCatalog.Load()
	if catalog == nil {
		return nil, taskplugin.ErrCatalogUnavailable
	}
	return catalog, nil
}

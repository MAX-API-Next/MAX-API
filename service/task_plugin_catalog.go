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

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
)

var taskPluginCatalog atomic.Pointer[taskplugin.Catalog]

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

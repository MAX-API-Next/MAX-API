// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

const taskPluginAdminBodyLimit = 6*taskplugin.MaxPackageBytes + 4096

func taskPluginAdminError(c *gin.Context, err error) {
	status, message := http.StatusBadRequest, "Invalid task plugin request"
	switch {
	case errors.Is(err, taskplugin.ErrCatalogConflict):
		status, message = http.StatusConflict, "Task plugin catalog changed; refresh before retrying"
	case errors.Is(err, taskplugin.ErrBindingRejected):
		status, message = http.StatusNotFound, "Task plugin version unavailable"
	case errors.Is(err, taskplugin.ErrCatalogUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusServiceUnavailable, "Task plugin catalog unavailable; reread before another mutation"
	case errors.Is(err, taskplugin.ErrCatalogInvalid):
		message = "Task plugin catalog transition rejected"
	}
	c.JSON(status, gin.H{"success": false, "message": message}) // Never return source/driver errors.
}

func taskPluginAdminFields(c *gin.Context, allowed map[string]string) (map[string]json.RawMessage, error) {
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, taskPluginAdminBodyLimit+1))
	if err != nil || len(data) > taskPluginAdminBodyLimit {
		return nil, taskplugin.ErrCatalogInvalid
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, taskplugin.ErrCatalogInvalid
	}
	for key, raw := range fields {
		if kind, ok := allowed[key]; !ok || common.GetJsonType(raw) != kind {
			return nil, taskplugin.ErrCatalogInvalid
		}
	}
	return fields, nil
}
func taskPluginAdminString(fields map[string]json.RawMessage, key string) string {
	var s string
	_ = common.Unmarshal(fields[key], &s)
	return s
}
func taskPluginAdminRevision(fields map[string]json.RawMessage) (int64, error) {
	s := taskPluginAdminString(fields, "expected_revision")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != s {
		return 0, taskplugin.ErrCatalogInvalid
	}
	return n, nil
}
func taskPluginAdminPackage(fields map[string]json.RawMessage) (*taskplugin.Package, error) {
	return taskplugin.LoadPackage(bytes.NewReader([]byte(taskPluginAdminString(fields, "document"))), taskPluginAdminString(fields, "expected_sha256"))
}

func GetTaskPluginManagement(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	catalog, err := service.CurrentTaskPluginCatalog()
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	result, err := service.ReadTaskPluginManagement(ctx, catalog)
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ValidateTaskPluginPackage(c *gin.Context) {
	fields, err := taskPluginAdminFields(c, map[string]string{"document": "string", "expected_sha256": "string"})
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	pkg, err := taskPluginAdminPackage(fields)
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	common.ApiSuccess(c, service.TaskPluginManagementView(taskplugin.ManagementEntry{Manifest: pkg.Manifest(), Digest: pkg.Digest(), FormatVersion: pkg.FormatVersion()}))
}

func MutateTaskPluginManagement(c *gin.Context) {
	fields, err := taskPluginAdminFields(c, map[string]string{"expected_revision": "string", "action": "string", "key": "string", "version": "string", "document": "string", "expected_sha256": "string", "format2_nodes_ready": "boolean"})
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	expected, err := taskPluginAdminRevision(fields)
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	mutation := taskplugin.ManagementMutation{Action: taskPluginAdminString(fields, "action"), Key: taskPluginAdminString(fields, "key"), Version: taskPluginAdminString(fields, "version")}
	if mutation.Action == "install" {
		mutation.Package, err = taskPluginAdminPackage(fields)
		if err != nil {
			taskPluginAdminError(c, err)
			return
		}
		mutation.Key, mutation.Version = mutation.Package.Manifest().Key, mutation.Package.Manifest().Version
	} else if fields["document"] != nil || fields["expected_sha256"] != nil || mutation.Key == "" || mutation.Version == "" {
		taskPluginAdminError(c, taskplugin.ErrCatalogInvalid)
		return
	}
	catalog, err := service.CurrentTaskPluginCatalog()
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if mutation.Action == "activate" || mutation.Action == "install" {
		format := 0
		if mutation.Package != nil {
			format = mutation.Package.FormatVersion()
		} else {
			rows, revision, readErr := catalog.ManagementSnapshot(ctx)
			if readErr != nil {
				taskPluginAdminError(c, readErr)
				return
			}
			if revision != expected {
				taskPluginAdminError(c, taskplugin.ErrCatalogConflict)
				return
			}
			for _, row := range rows {
				if row.Manifest.Key == mutation.Key && row.Manifest.Version == mutation.Version {
					format = row.FormatVersion
				}
			}
		}
		var ready bool
		_ = common.Unmarshal(fields["format2_nodes_ready"], &ready)
		if format == 2 && !ready {
			taskPluginAdminError(c, taskplugin.ErrCatalogInvalid)
			return
		}
	}
	if err := catalog.MutateExpected(ctx, expected, mutation); err != nil {
		taskPluginAdminError(c, err)
		return
	}
	recordManageAudit(c, "task_plugin."+mutation.Action, map[string]any{"plugin_key": mutation.Key, "version": mutation.Version, "catalog_revision": strconv.FormatInt(expected+1, 10)})
	common.ApiSuccess(c, gin.H{"committed": true, "revision": strconv.FormatInt(expected+1, 10), "execution_ready": false, "admission_ready": false})
}

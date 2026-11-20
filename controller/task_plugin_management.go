// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

const taskPluginAdminBodyLimit = 6*taskplugin.MaxPackageBytes + 4096

func GetBundledTaskPlugins(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	entries, err := service.BundledTaskPlugins()
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	common.ApiSuccess(c, entries)
}

func GetBundledTaskPluginSource(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	source, pin, err := service.BundledTaskPluginSource(c.Param("key"), c.Param("version"))
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"document": source, "expected_sha256": pin})
}

func taskPluginAdminError(c *gin.Context, err error) {
	status, message := http.StatusBadRequest, "Invalid task plugin request"
	var mapping *taskplugin.CatalogChannelMappingError
	var taskData *taskplugin.CatalogTaskDataError
	var pricingData *taskplugin.CatalogPricingDataError
	response := gin.H{"success": false}
	switch {
	case errors.As(err, &mapping):
		status, message = http.StatusUnprocessableEntity, fmt.Sprintf("Channel %d has an invalid model mapping; repair it before deleting the API extension", mapping.ChannelID)
		response["code"], response["channel_id"] = "invalid_channel_mapping", strconv.Itoa(mapping.ChannelID)
	case errors.As(err, &taskData):
		status, message = http.StatusUnprocessableEntity, fmt.Sprintf("Task record %d has invalid private data; repair it before deleting the API extension", taskData.TaskID)
		response["code"], response["task_id"] = "invalid_task_data", strconv.FormatInt(taskData.TaskID, 10)
	case errors.As(err, &pricingData):
		status, message = http.StatusUnprocessableEntity, fmt.Sprintf("Plugin pricing option %s is invalid; repair it before deleting the API extension", pricingData.OptionKey)
		response["code"], response["option_key"] = "invalid_plugin_pricing", pricingData.OptionKey
	case errors.Is(err, taskplugin.ErrCatalogConflict):
		status, message = http.StatusConflict, "Task plugin catalog changed; refresh before retrying"
	case errors.Is(err, taskplugin.ErrCatalogNotCommitted):
		status, message = http.StatusServiceUnavailable, "Task plugin catalog mutation stopped before commit; refresh and retry"
		response["code"], response["committed"] = "catalog_operation_not_committed", false
	case errors.Is(err, taskplugin.ErrCatalogInUse):
		status, message = http.StatusUnprocessableEntity, "Task plugin version is still referenced; remove task, channel or pricing references before deleting"
	case errors.Is(err, taskplugin.ErrCatalogRevoked):
		status, message = http.StatusUnprocessableEntity, "Task plugin version was permanently revoked; contact the administrator to resolve the historical revocation record"
	case errors.Is(err, taskplugin.ErrCatalogContentChanged):
		status, message = http.StatusUnprocessableEntity, "Task plugin version has different content; import the original file or use a new version"
	case errors.Is(err, taskplugin.ErrBindingRejected):
		status, message = http.StatusNotFound, "Task plugin version unavailable"
	case errors.Is(err, taskplugin.ErrCatalogUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusServiceUnavailable, "Task plugin catalog unavailable; reread before another mutation"
	case errors.Is(err, taskplugin.ErrCatalogInvalid):
		message = "Task plugin catalog transition rejected"
	}
	response["message"] = message
	c.JSON(status, response) // Never return source/driver errors.
}

func taskPluginAdminFields(c *gin.Context, allowed map[string]string) (map[string]json.RawMessage, error) {
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, taskPluginAdminBodyLimit+1))
	if err != nil || len(data) > taskPluginAdminBodyLimit || !utf8.Valid(data) {
		return nil, taskplugin.ErrCatalogInvalid
	}
	decoder := common.NewJSONDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, taskplugin.ErrCatalogInvalid
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, taskplugin.ErrCatalogInvalid
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, taskplugin.ErrCatalogInvalid
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, taskplugin.ErrCatalogInvalid
		}
		fields[key] = raw
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return nil, taskplugin.ErrCatalogInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, taskplugin.ErrCatalogInvalid
	}
	for key, raw := range fields {
		if kind, ok := allowed[key]; !ok || common.GetJsonType(raw) != kind {
			return nil, taskplugin.ErrCatalogInvalid
		}
		if key == "document" && !taskPluginDocumentUnicodeValid(raw) {
			return nil, taskplugin.ErrCatalogInvalid
		}
	}
	return fields, nil
}

// The JSON decoder replaces lone surrogate escapes. Reject them before an
// optional pin can identify replacement text instead of the submitted package.
// JSON syntax is checked by taskPluginAdminFields before this scan.
func taskPluginDocumentUnicodeValid(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		unit, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if unit >= 0xD800 && unit <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		} else if unit >= 0xDC00 && unit <= 0xDFFF {
			return false
		}
	}
	return true
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
func taskPluginAdminPackage(ctx context.Context, fields map[string]json.RawMessage) (*taskplugin.Package, error) {
	return taskplugin.LoadUploadContext(ctx, bytes.NewReader([]byte(taskPluginAdminString(fields, "document"))), taskPluginAdminString(fields, "expected_sha256"))
}

func taskPluginInstallPackage(ctx context.Context, fields map[string]json.RawMessage) (*taskplugin.Package, error) {
	pkg, err := taskPluginAdminPackage(ctx, fields)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		err = errors.Join(taskplugin.ErrCatalogNotCommitted, err)
	}
	return pkg, err
}

func GetTaskPluginManagement(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	catalog, err := service.CurrentTaskPluginCatalog()
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
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
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
	defer cancel()
	pkg, err := taskPluginAdminPackage(ctx, fields)
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	catalog, err := service.CurrentTaskPluginCatalog()
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	if err := catalog.ValidateInstall(ctx, pkg); err != nil {
		taskPluginAdminError(c, err)
		return
	}
	common.ApiSuccess(c, service.TaskPluginManagementView(taskplugin.ManagementEntry{Manifest: pkg.Manifest(), Digest: pkg.Digest(), SourceDigest: pkg.SourceDigest(), FormatVersion: pkg.FormatVersion(), BOMPrefixed: pkg.BOMPrefixed()}))
}

func MutateTaskPluginManagement(c *gin.Context) {
	fields, err := taskPluginAdminFields(c, map[string]string{"expected_revision": "string", "action": "string", "key": "string", "version": "string", "document": "string", "expected_sha256": "string", "format2_nodes_ready": "boolean", "format3_nodes_ready": "boolean", "bom_nodes_ready": "boolean", "deletion_nodes_ready": "boolean"})
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	expected, err := taskPluginAdminRevision(fields)
	if err != nil {
		taskPluginAdminError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
	defer cancel()
	mutation := taskplugin.ManagementMutation{Action: taskPluginAdminString(fields, "action"), Key: taskPluginAdminString(fields, "key"), Version: taskPluginAdminString(fields, "version")}
	if mutation.Action == "delete" {
		var ready bool
		_ = common.Unmarshal(fields["deletion_nodes_ready"], &ready)
		if !ready {
			taskPluginAdminError(c, taskplugin.ErrCatalogInvalid)
			return
		}
	}
	if mutation.Action == "install" {
		mutation.Package, err = taskPluginInstallPackage(ctx, fields)
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
	if mutation.Action == "activate" || mutation.Action == "install" {
		format := 0
		bomPrefixed := false
		if mutation.Package != nil {
			format = mutation.Package.FormatVersion()
			bomPrefixed = mutation.Package.BOMPrefixed()
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
					bomPrefixed = row.BOMPrefixed
				}
			}
		}
		var ready bool
		_ = common.Unmarshal(fields["format2_nodes_ready"], &ready)
		if format == 3 {
			ready = false
			_ = common.Unmarshal(fields["format3_nodes_ready"], &ready)
		}
		if format >= 2 && !ready {
			taskPluginAdminError(c, taskplugin.ErrCatalogInvalid)
			return
		}
		if bomPrefixed {
			ready = false
			_ = common.Unmarshal(fields["bom_nodes_ready"], &ready)
			if !ready {
				taskPluginAdminError(c, taskplugin.ErrCatalogInvalid)
				return
			}
		}
	}
	if err := catalog.MutateExpected(ctx, expected, mutation); err != nil {
		taskPluginAdminError(c, err)
		return
	}
	recordManageAudit(c, "task_plugin."+mutation.Action, map[string]any{"plugin_key": mutation.Key, "version": mutation.Version, "catalog_revision": strconv.FormatInt(expected+1, 10)})
	common.ApiSuccess(c, gin.H{"committed": true, "revision": strconv.FormatInt(expected+1, 10), "execution_ready": false, "admission_ready": false})
}

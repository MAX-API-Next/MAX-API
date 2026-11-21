// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Channel settings reference: QuantumNous/new-api 8542c9d6 (AGPL-3.0-or-later).
package model

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskPluginChannelSettings struct {
	Key      string          `json:"task_plugin_key"`
	Upstream string          `json:"task_plugin_upstream"`
	Config   json.RawMessage `json:"task_plugin_config"`
}

func (channel *Channel) BeforeCreate(tx *gorm.DB) error {
	if channel.Type == constant.ChannelTypeTaskPlugin {
		return validateTaskPluginChannelWrite(tx, channel)
	}
	return nil
}

func ReadTaskPluginChannelSettings(setting *string) (TaskPluginChannelSettings, error) {
	var result TaskPluginChannelSettings
	if setting == nil || common.UnmarshalJsonStr(*setting, &result) != nil || result.Key == "" {
		return result, taskplugin.ErrBindingRejected
	}
	return result, nil
}

func (channel *Channel) TaskPluginModels() (map[string]string, error) {
	mapping := map[string]string{}
	if value := channel.GetModelMapping(); strings.TrimSpace(value) != "" {
		if common.UnmarshalJsonStr(value, &mapping) != nil || mapping == nil {
			return nil, taskplugin.ErrBindingRejected
		}
	}
	models := make(map[string]string)
	for _, raw := range strings.Split(channel.Models, ",") {
		name := strings.TrimSpace(raw)
		if name == "" || len(name) > 255 {
			return nil, taskplugin.ErrBindingRejected
		}
		target, mapped := mapping[name]
		if !mapped {
			target = name
		}
		if target == "" || strings.TrimSpace(target) != target {
			return nil, taskplugin.ErrBindingRejected
		}
		models[name] = target
		delete(mapping, name)
	}
	if len(mapping) != 0 {
		return nil, taskplugin.ErrBindingRejected
	}
	return models, nil
}

// The observed digest is a write precondition. Only the plugin key is stored.
// The catalog head serializes this check with activation, revocation and deletion.
func validateTaskPluginChannelWrite(tx *gorm.DB, channel *Channel) error {
	if channel.BaseURL == nil || taskplugin.ValidateChannelBaseURL(*channel.BaseURL) != nil ||
		strings.TrimSpace(channel.Key) == "" || channel.ChannelInfo.IsMultiKey {
		return errors.New("Task plugin channels require an upstream URL and a single credential")
	}
	settings, err := ReadTaskPluginChannelSettings(channel.Setting)
	if err != nil {
		return errors.New("Select an active task plugin")
	}
	if channel.TaskPluginExpectedDigest == "" {
		return errors.New("Refresh task plugin options before saving")
	}
	if err := lockTaskPluginCatalogHead(tx); err != nil {
		return taskplugin.ErrCatalogUnavailable
	}
	var record TaskPluginVersion
	err = tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("digest = ? AND active = ? AND enabled = ? AND revoked = ?", channel.TaskPluginExpectedDigest, true, true, false).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("Task plugin options changed or the plugin is unavailable. Refresh and select again")
	}
	if err != nil {
		return taskplugin.ErrCatalogUnavailable
	}
	pkg, err := (taskplugin.StoredPackage{Document: []byte(record.Document), Digest: record.Digest}).LoadRetainedPackage(tx.Statement.Context)
	if err != nil || !pkg.Executable() {
		return errors.New("Select an active task plugin with the channel SDK")
	}
	manifest := pkg.Manifest()
	if manifest.Key != settings.Key {
		return errors.New("Select an active task plugin with the channel SDK")
	}
	if _, err := manifest.ResolveChannelConfig(settings.Config, settings.Upstream); err != nil {
		return errors.New("Task plugin parameters do not match the active declaration")
	}
	models, err := channel.TaskPluginModels()
	if err != nil {
		return errors.New("Invalid task plugin model mapping")
	}
	for _, upstreamModel := range models {
		if !slices.Contains(manifest.Models, upstreamModel) {
			return errors.New("Every mapped model must be declared by the active task plugin")
		}
	}
	if channel.Other != "" && !slices.Contains(manifest.ChannelTypes, constant.ChannelTypeVertexAi) {
		return errors.New("The active task plugin does not declare a region parameter")
	}
	return nil
}

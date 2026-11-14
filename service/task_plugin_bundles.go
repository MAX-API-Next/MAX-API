// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/plugins"
)

type BundledTaskPlugin struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type taskBundleIndex struct {
	Plugins          []BundledTaskPlugin `json:"plugins"`
	RetainedVersions []BundledTaskPlugin `json:"retainedVersions"`
}

func readTaskBundleIndex() (taskBundleIndex, error) {
	data, err := plugins.TaskSources.ReadFile("SOURCES.json")
	var index taskBundleIndex
	if err != nil || common.Unmarshal(data, &index) != nil {
		return index, taskplugin.ErrPluginCompile
	}
	return index, nil
}

// The reviewed embedded index is the only source for download identities.
// No caller-controlled URL/path or live marketplace request is accepted here.
func BundledTaskPlugins() ([]BundledTaskPlugin, error) {
	index, err := readTaskBundleIndex()
	if err != nil {
		return nil, taskplugin.ErrPluginCompile
	}
	out := make([]BundledTaskPlugin, 0, len(index.Plugins))
	for _, p := range index.Plugins {
		out = append(out, BundledTaskPlugin{Key: p.Key, Version: p.Version, SHA256: p.SHA256})
	}
	return out, nil
}

func BundledTaskPluginSource(key, version string) (string, string, error) {
	index, err := readTaskBundleIndex()
	if err != nil {
		return "", "", err
	}
	// Retained, pinned releases stay available for audit/recovery and explicit
	// old references. Only current recommendations appear in the picker.
	entries := append(index.Plugins, index.RetainedVersions...)
	for _, entry := range entries {
		if entry.Key == key && entry.Version == version {
			data, err := plugins.TaskSources.ReadFile("tasks/" + entry.Key + "/" + entry.Version + "/plugin.js")
			sum := sha256.Sum256(data)
			if err != nil || hex.EncodeToString(sum[:]) != entry.SHA256 {
				return "", "", taskplugin.ErrPluginCompile
			}
			return string(data), entry.SHA256, nil
		}
	}
	return "", "", taskplugin.ErrBindingRejected
}

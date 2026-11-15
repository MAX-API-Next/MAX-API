// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Selective interaction reference: QuantumNous/new-api 70e6af64 task plugin
// list/upload/version activation. MAX owns package validation/history and CAS.
package service

import (
	"context"
	"strconv"

	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
)

type TaskPluginProtocolView struct {
	Name     string   `json:"name"`
	Supports []string `json:"supports"`
	Models   []string `json:"models,omitempty"`
}
type TaskPluginRouteView struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Type   string `json:"type"`
}
type TaskPluginProfileView struct {
	Models []string                        `json:"models"`
	Schema map[string]types.TaskUsageField `json:"schema"`
}
type TaskPluginManagementEntry struct {
	Key            string                          `json:"key"`
	Name           string                          `json:"name"`
	Version        string                          `json:"version"`
	Digest         string                          `json:"digest"`
	SourceDigest   string                          `json:"source_sha256,omitempty"`
	FormatVersion  int                             `json:"format_version"`
	Active         bool                            `json:"active"`
	Enabled        bool                            `json:"enabled"`
	Revoked        bool                            `json:"revoked"`
	Models         []string                        `json:"models"`
	Protocols      []TaskPluginProtocolView        `json:"protocols"`
	Routes         []TaskPluginRouteView           `json:"routes"`
	UsageSchema    map[string]types.TaskUsageField `json:"usage_schema"`
	UsageProfiles  []TaskPluginProfileView         `json:"usage_profiles"`
	ExecutionReady bool                            `json:"execution_ready"`
	AdmissionReady bool                            `json:"admission_ready"`
}
type TaskPluginManagementSnapshot struct {
	// Decimal equality token: JSON numbers cannot represent every int64 in JS.
	Revision string                      `json:"revision"`
	Entries  []TaskPluginManagementEntry `json:"entries"`
}

func taskPluginUsageView(input map[string]taskplugin.UsageField) map[string]types.TaskUsageField {
	out := map[string]types.TaskUsageField{}
	for k, v := range input {
		out[k] = types.TaskUsageField{Type: v.Type, Unit: v.Unit, Enum: append([]string(nil), v.Enum...)}
	}
	return out
}
func TaskPluginManagementView(input taskplugin.ManagementEntry) TaskPluginManagementEntry {
	m := input.Manifest
	out := TaskPluginManagementEntry{Key: m.Key, Name: m.Name, Version: m.Version, Digest: input.Digest, FormatVersion: input.FormatVersion,
		SourceDigest: input.SourceDigest, Active: input.Active, Enabled: input.Enabled, Revoked: input.Revoked, Models: m.Models,
		Protocols: []TaskPluginProtocolView{}, Routes: []TaskPluginRouteView{}, UsageProfiles: []TaskPluginProfileView{}, UsageSchema: taskPluginUsageView(m.UsageSchema)}
	for _, p := range m.Protocols {
		out.Protocols = append(out.Protocols, TaskPluginProtocolView{Name: p.Name, Supports: append([]string{}, p.Supports...), Models: append([]string(nil), p.Models...)})
	}
	for _, r := range m.Routes {
		out.Routes = append(out.Routes, TaskPluginRouteView{Method: r.Method, Path: r.Path, Type: r.Type})
	}
	for _, p := range m.UsageProfiles {
		out.UsageProfiles = append(out.UsageProfiles, TaskPluginProfileView{Models: p.Models, Schema: taskPluginUsageView(p.Schema)})
	}
	return out
}
func ReadTaskPluginManagement(ctx context.Context, catalog *taskplugin.Catalog) (*TaskPluginManagementSnapshot, error) {
	rows, revision, err := catalog.ManagementSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := &TaskPluginManagementSnapshot{Revision: strconv.FormatInt(revision, 10), Entries: []TaskPluginManagementEntry{}}
	for _, row := range rows {
		out.Entries = append(out.Entries, TaskPluginManagementView(row))
	}
	return out, nil
}

// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"

	"github.com/MAX-API-Next/MAX-API/common"
)

// LoadJavaScript verifies the publisher's hash of the exact raw JS bytes, then
// derives every host-owned field from executed meta. The persisted envelope
// has a separate digest; neither one is inferred from user-supplied metadata.
func LoadJavaScript(reader io.Reader, expectedSHA256 string) (*Package, error) {
	if reader == nil {
		return nil, ErrPluginCompile
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxPackageBytes+1))
	if err != nil || len(data) > MaxPackageBytes {
		return nil, ErrPluginCompile
	}
	pin, err := hex.DecodeString(expectedSHA256)
	sum := sha256.Sum256(data)
	if err != nil || len(pin) != sha256.Size || !bytes.Equal(pin, sum[:]) {
		return nil, ErrPluginCompile
	}
	program, err := CompileJavaScript(string(data), RuntimeOptions{})
	if err != nil {
		return nil, err
	}
	m, err := program.Manifest(context.Background())
	if err != nil {
		return nil, err
	}
	document, err := common.Marshal(PackageDocument{FormatVersion: 3, Manifest: m, Source: string(data)})
	if err != nil || len(document) > MaxPackageBytes {
		return nil, ErrPluginCompile
	}
	digest := sha256.Sum256(document)
	return &Package{manifest: cloneManifest(m), source: string(data), digest: hex.EncodeToString(digest[:]), document: document, size: len(document), formatVersion: 3, program: program}, nil
}

// LoadUpload supports existing JSON packages and exact raw official plugin.js.
// Legacy envelopes keep their data-only behavior, regardless of their Source.
func LoadUpload(reader io.Reader, pin string) (*Package, error) {
	if reader == nil {
		return nil, ErrPluginCompile
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxPackageBytes+1))
	if err != nil || len(data) > MaxPackageBytes {
		return nil, ErrPluginCompile
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return LoadPackage(bytes.NewReader(data), pin)
	}
	return LoadJavaScript(bytes.NewReader(data), pin)
}

func (p *Package) SourceDigest() string {
	if p == nil {
		return ""
	}
	s := sha256.Sum256([]byte(p.source))
	return hex.EncodeToString(s[:])
}
func (p *Package) Executable() bool { return p != nil && p.formatVersion == 3 && p.program != nil }
func (p *Package) Call(ctx context.Context, path string, args ...any) (json.RawMessage, error) {
	if !p.Executable() {
		return nil, ErrPluginHook
	}
	return p.program.Call(ctx, path, args...)
}

func (p *Program) Manifest(ctx context.Context) (Manifest, error) {
	raw, err := p.Export(ctx, "meta")
	if err != nil {
		return Manifest{}, err
	}
	var meta struct {
		APIVersion   int               `json:"apiVersion"`
		Key          string            `json:"key"`
		Name         string            `json:"name"`
		Version      string            `json:"version"`
		Models       []string          `json:"models"`
		ChannelTypes []int             `json:"channelTypes"`
		Protocols    []json.RawMessage `json:"protocols"`
		Routes       []struct {
			Method, Path, Type, Decode, Render, Action string
			TaskIDParam                                string `json:"taskIdParam"`
			Models                                     []string
			RetainResult                               *bool `json:"retainResult"`
		} `json:"routes"`
		BaseURL       string                `json:"baseUrl"`
		AllowedHosts  []string              `json:"allowedHosts"`
		Upstreams     []string              `json:"upstreams"`
		UsageSchema   map[string]UsageField `json:"usageSchema"`
		UsageProfiles []UsageProfile        `json:"usageProfiles"`
		Capabilities  []string              `json:"requiredCapabilities"`
	}
	if common.Unmarshal(raw, &meta) != nil {
		return Manifest{}, ErrPluginCompile
	}
	// Capabilities are promises by the host, not merely informational strings.
	seenCapabilities := map[string]bool{}
	for _, capability := range meta.Capabilities {
		if seenCapabilities[capability] {
			return Manifest{}, ErrPluginSDK
		}
		seenCapabilities[capability] = true
		if capability != "json-clone@1" && capability != "duration-auto@1" {
			return Manifest{}, ErrPluginSDK
		}
	}
	m := Manifest{APIVersion: meta.APIVersion, Key: meta.Key, Name: meta.Name, Version: meta.Version, Models: meta.Models, ChannelTypes: meta.ChannelTypes, BaseURL: meta.BaseURL, AllowedHosts: meta.AllowedHosts, Upstreams: meta.Upstreams, UsageSchema: meta.UsageSchema, UsageProfiles: meta.UsageProfiles}
	for _, protocol := range meta.Protocols {
		var bare string
		if common.Unmarshal(protocol, &bare) == nil {
			m.Protocols = append(m.Protocols, ProtocolClaim{Name: bare, Bare: true})
			continue
		}
		var claim ProtocolClaim
		if common.Unmarshal(protocol, &claim) != nil {
			return Manifest{}, ErrPluginCompile
		}
		m.Protocols = append(m.Protocols, claim)
	}
	for _, r := range meta.Routes {
		m.Routes = append(m.Routes, Route{Method: r.Method, Path: r.Path, Type: r.Type, Decode: r.Decode, Render: r.Render, Action: r.Action, TaskIDParam: r.TaskIDParam, Models: r.Models, RetainResult: r.RetainResult})
	}
	normalizeSchema := func(schema map[string]UsageField) {
		for k, f := range schema {
			if f.Type == "" && len(f.Enum) > 0 {
				f.Type = "enum"
				schema[k] = f
			}
		}
	}
	normalizeSchema(m.UsageSchema)
	for i := range m.UsageProfiles {
		normalizeSchema(m.UsageProfiles[i].Schema)
	}
	// New API allows a profile to override a subset and inherits its base
	// schema for other models. Materialize that inheritance for MAX's exact map.
	if len(m.UsageProfiles) > 0 {
		covered := map[string]bool{}
		for _, profile := range m.UsageProfiles {
			for _, model := range profile.Models {
				covered[model] = true
			}
		}
		for _, model := range m.Models {
			if !covered[model] {
				m.UsageProfiles = append(m.UsageProfiles, UsageProfile{Models: []string{model}, Schema: cloneManifest(Manifest{UsageSchema: m.UsageSchema}).UsageSchema})
			}
		}
	}
	if ValidateManifest(m) != nil {
		return Manifest{}, ErrPluginCompile
	}
	hooks := []string{"buildSubmitRequest", "parseSubmitResponse", "buildQueryRequest", "parseTaskResult"}
	for _, route := range m.Routes {
		if route.Decode != "" {
			hooks = append(hooks, "native."+route.Decode)
		}
		hooks = append(hooks, "native."+route.Render)
	}
	for _, claim := range m.Protocols {
		hooks = append(hooks, "protocols."+claim.Name+".decodeRequest")
		if claim.Name == "openai_responses" {
			for _, mode := range claim.Supports {
				if mode == "stream" {
					hooks = append(hooks, "protocols.openai_responses.renderEvents")
				} else {
					hooks = append(hooks, "protocols.openai_responses.renderFinal")
				}
			}
		} else {
			hooks = append(hooks, "protocols."+claim.Name+".render")
		}
	}
	if !p.hasFunctions(ctx, hooks) {
		return Manifest{}, ErrPluginCompile
	}
	return m, nil
}

func (p *Program) matchesManifest(m Manifest) bool {
	compiled, err := p.Manifest(context.Background())
	return err == nil && reflect.DeepEqual(compiled, m)
}

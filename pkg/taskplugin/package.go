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

package taskplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
)

const MaxPackageBytes = 1 << 20

// PackageDocument versions 1/2 are data-only and never execute Source. Version
// 3 is explicit JavaScript; its manifest must equal compiled source metadata.
type PackageDocument struct {
	FormatVersion int      `json:"format_version"`
	Manifest      Manifest `json:"manifest"`
	Source        string   `json:"source"`
}

// Package has already passed an administrator-supplied digest check. The hash
// covers the entire document, including routing and network permissions.
type Package struct {
	manifest              Manifest
	source                string
	digest                string
	size                  int
	document              []byte
	formatVersion         int
	program               *Program
	legacyRepeatedMembers bool
}

// LoadPackage accepts exactly one bounded document and requires a trusted hash
// from outside the document. Computing a hash from an untrusted upload alone
// does not establish publisher authenticity.
func LoadPackage(reader io.Reader, expectedSHA256 string) (*Package, error) {
	return LoadPackageContext(context.Background(), reader, expectedSHA256)
}

// LoadPackageContext also bounds executable metadata evaluation by the caller's
// context. The byte, manifest and trusted-pin contract is identical to LoadPackage.
func LoadPackageContext(ctx context.Context, reader io.Reader, expectedSHA256 string) (*Package, error) {
	return loadPackageContext(ctx, reader, expectedSHA256, false)
}

// LoadRetainedPackage reads digest-pinned bytes already retained by a catalog.
// Legacy packages retain their original JSON interpretation and bytes.
func (entry StoredPackage) LoadRetainedPackage(ctx context.Context) (*Package, error) {
	return loadPackageContext(ctx, bytes.NewReader(entry.Document), entry.Digest, true)
}

func loadPackageContext(ctx context.Context, reader io.Reader, expectedSHA256 string, retained bool) (*Package, error) {
	if ctx == nil {
		return nil, ErrCatalogUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := hex.DecodeString(expectedSHA256)
	if reader == nil || err != nil || len(expected) != sha256.Size {
		return nil, errors.New("task plugin package requires a SHA-256 pin")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxPackageBytes+1))
	if err != nil || len(data) > MaxPackageBytes || !utf8.Valid(data) {
		return nil, errors.New("task plugin package unreadable or exceeds size limit")
	}
	actual := sha256.Sum256(data)
	if hex.EncodeToString(expected) != hex.EncodeToString(actual[:]) {
		return nil, errors.New("task plugin package digest mismatch")
	}
	// A UTF-8 BOM is permitted only at the start of the JSON document. Hash,
	// size and persisted identity continue to cover the original upload bytes.
	jsonData := bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	legacyRepeatedMembers := false
	if !validPackageFields(jsonData) {
		if !retained || !validLegacyPackageFields(jsonData) {
			return nil, errors.New("unsupported task plugin package fields or repeated JSON member")
		}
		legacyRepeatedMembers = true
	}
	var document PackageDocument
	if err := common.Unmarshal(jsonData, &document); err != nil {
		return nil, errors.New("invalid task plugin package document")
	}
	if (document.FormatVersion != 1 && document.FormatVersion != 2 && document.FormatVersion != 3) || document.Source == "" || !utf8.ValidString(document.Source) {
		return nil, errors.New("unsupported task plugin package format or invalid source")
	}
	// Old hosts ignore unknown manifest fields. Version 2 makes them reject a
	// profile-bearing package rather than silently using its fallback schema.
	if len(document.Manifest.UsageProfiles) > 0 && document.FormatVersion < 2 {
		return nil, errors.New("model usage profiles require package format 2")
	}
	for _, claim := range document.Manifest.Protocols {
		if len(claim.Models) > 0 && document.FormatVersion < 3 {
			return nil, errors.New("protocol model restrictions require package format 3")
		}
	}
	if err := ValidateManifest(document.Manifest); err != nil {
		// Validation errors can contain attacker-supplied metadata. Keep source
		// and credentials out of any caller's logs and API error response.
		return nil, errors.New("invalid task plugin package manifest")
	}
	var program *Program
	if document.FormatVersion == 3 {
		program, err = compileJavaScriptContext(ctx, document.Source, RuntimeOptions{})
		if err != nil || !program.matchesManifestContext(ctx, document.Manifest) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrPluginCompile
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Package{manifest: cloneManifest(document.Manifest), source: document.Source, digest: hex.EncodeToString(actual[:]), size: len(data), document: data, formatVersion: document.FormatVersion, program: program, legacyRepeatedMembers: legacyRepeatedMembers}, nil
}

func validLegacyPackageFields(data []byte) bool {
	var fields map[string]any
	if common.Unmarshal(data, &fields) != nil || len(fields) != 3 {
		return false
	}
	for _, name := range []string{"format_version", "manifest", "source"} {
		if _, exists := fields[name]; !exists {
			return false
		}
	}
	return true
}

func validPackageFields(data []byte) bool {
	decoder := common.NewJSONDecoder(bytes.NewReader(data))
	// Use the same number range as pre-duplicate-check catalog readers.
	// Token scanning never persists or round-trips the decoded number.
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false
	}
	fields, err := readUniquePackageFields(decoder, opening, reflect.TypeFor[PackageDocument]())
	if err != nil || len(fields) != 3 || !fields["format_version"] || !fields["manifest"] || !fields["source"] {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}

func readUniquePackageFields(decoder *json.Decoder, opening json.Token, valueType reflect.Type) (map[string]bool, error) {
	var fields map[string]bool
	var childType reflect.Type
	switch opening {
	case json.Delim('{'):
		fields = make(map[string]bool)
	case json.Delim('['):
		if valueType != nil && valueType.Kind() == reflect.Slice {
			childType = valueType.Elem()
		}
	default:
		return nil, nil
	}
	for decoder.More() {
		if fields != nil {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok {
				return nil, errors.New("invalid or repeated package JSON member")
			}
			var name string
			name, childType = packageJSONField(valueType, key)
			if fields[name] || valueType == reflect.TypeFor[PackageDocument]() && name != key {
				return nil, errors.New("invalid or repeated package JSON member")
			}
			fields[name] = true
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if _, err := readUniquePackageFields(decoder, token, childType); err != nil {
			return nil, err
		}
	}
	_, err := decoder.Token()
	return fields, err
}

func packageJSONField(valueType reflect.Type, key string) (string, reflect.Type) {
	if valueType == nil {
		return key, nil
	}
	if valueType.Kind() == reflect.Map {
		return key, valueType.Elem()
	}
	if valueType.Kind() == reflect.Struct {
		for field := range valueType.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" {
				name = field.Name
			}
			if name != "-" && strings.EqualFold(key, name) {
				return name, field.Type
			}
		}
	}
	return key, nil
}

func (p *Package) Manifest() Manifest { return cloneManifest(p.manifest) }
func (p *Package) Digest() string     { return p.digest }
func (p *Package) FormatVersion() int { return p.formatVersion }
func (p *Package) BOMPrefixed() bool {
	return p != nil && bytes.HasPrefix(p.document, []byte{0xef, 0xbb, 0xbf})
}

// ModelUsageSchema returns detached metadata for the exact mapped model.
// Profile packages never fall back for an unprofiled/unknown model.
func (p *Package) ModelUsageSchema(model string) (map[string]UsageField, error) {
	if p == nil {
		return nil, errors.New("missing task plugin package")
	}
	m := p.Manifest()
	declared := false
	for _, name := range m.Models {
		declared = declared || name == model
	}
	if !declared {
		return nil, errors.New("model is not declared by pinned package")
	}
	if len(m.UsageProfiles) == 0 {
		return m.UsageSchema, nil
	}
	for _, profile := range m.UsageProfiles {
		for _, name := range profile.Models {
			if name == model {
				return profile.Schema, nil
			}
		}
	}
	return nil, errors.New("model has no exact usage profile")
}

// Document returns the exact pinned bytes, not a re-encoded manifest/source.
func (p *Package) Document() []byte { return append([]byte(nil), p.document...) }

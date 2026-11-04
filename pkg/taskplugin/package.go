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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
)

const MaxPackageBytes = 1 << 20

// PackageDocument is MAX-API's data-only transport envelope, not executable
// JavaScript metadata. Source is retained for a future sandbox, never evaluated.
type PackageDocument struct {
	FormatVersion int      `json:"format_version"`
	Manifest      Manifest `json:"manifest"`
	Source        string   `json:"source"`
}

// Package has already passed an administrator-supplied digest check. The hash
// covers the entire document, including routing and network permissions.
type Package struct {
	manifest      Manifest
	source        string
	digest        string
	size          int
	document      []byte
	formatVersion int
}

// LoadPackage accepts exactly one bounded document and requires a trusted hash
// from outside the document. Computing a hash from an untrusted upload alone
// does not establish publisher authenticity.
func LoadPackage(reader io.Reader, expectedSHA256 string) (*Package, error) {
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
	var fields map[string]any
	if common.Unmarshal(data, &fields) != nil || len(fields) != 3 {
		return nil, errors.New("unsupported task plugin package fields")
	}
	for _, name := range []string{"format_version", "manifest", "source"} {
		if _, ok := fields[name]; !ok {
			return nil, errors.New("unsupported task plugin package fields")
		}
	}
	var document PackageDocument
	if err := common.Unmarshal(data, &document); err != nil {
		return nil, errors.New("invalid task plugin package document")
	}
	if (document.FormatVersion != 1 && document.FormatVersion != 2) || document.Source == "" || !utf8.ValidString(document.Source) {
		return nil, errors.New("unsupported task plugin package format or invalid source")
	}
	// Old hosts ignore unknown manifest fields. Version 2 makes them reject a
	// profile-bearing package rather than silently using its fallback schema.
	if len(document.Manifest.UsageProfiles) > 0 && document.FormatVersion != 2 {
		return nil, errors.New("model usage profiles require package format 2")
	}
	if err := ValidateManifest(document.Manifest); err != nil {
		// Validation errors can contain attacker-supplied metadata. Keep source
		// and credentials out of any caller's logs and API error response.
		return nil, errors.New("invalid task plugin package manifest")
	}
	return &Package{manifest: cloneManifest(document.Manifest), source: document.Source, digest: hex.EncodeToString(actual[:]), size: len(data), document: data, formatVersion: document.FormatVersion}, nil
}

func (p *Package) Manifest() Manifest { return cloneManifest(p.manifest) }
func (p *Package) Digest() string     { return p.digest }
func (p *Package) FormatVersion() int { return p.formatVersion }

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

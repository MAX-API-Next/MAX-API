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
	manifest Manifest
	source   string
	digest   string
	size     int
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
	if document.FormatVersion != 1 || document.Source == "" || !utf8.ValidString(document.Source) {
		return nil, errors.New("unsupported task plugin package format or invalid source")
	}
	if err := ValidateManifest(document.Manifest); err != nil {
		// Validation errors can contain attacker-supplied metadata. Keep source
		// and credentials out of any caller's logs and API error response.
		return nil, errors.New("invalid task plugin package manifest")
	}
	return &Package{manifest: cloneManifest(document.Manifest), source: document.Source, digest: hex.EncodeToString(actual[:]), size: len(data)}, nil
}

func (p *Package) Manifest() Manifest { return cloneManifest(p.manifest) }
func (p *Package) Digest() string     { return p.digest }

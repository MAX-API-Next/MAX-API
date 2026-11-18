// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const validUploadSource = `export const meta = {apiVersion: 1, key: "custom-upload", name: "Custom", version: "1.0.0", models: ["custom-model"]}; export function buildSubmitRequest(){return {}} export function parseSubmitResponse(){return {}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {}}`

func TestLoadUploadAllowsOmittedPinButPreservesComputedIdentity(t *testing.T) {
	source := []byte(validUploadSource)
	legacy := testPackage(t, validManifest(), "data-only source")
	for name, data := range map[string][]byte{"javascript": source, "json": legacy.Document()} {
		t.Run(name, func(t *testing.T) {
			pin := fmt.Sprintf("%x", sha256.Sum256(data))
			with, err := LoadUpload(bytes.NewReader(data), pin)
			require.NoError(t, err)
			without, err := LoadUpload(bytes.NewReader(data), "")
			require.NoError(t, err)
			require.Equal(t, with.Document(), without.Document())
			require.Equal(t, with.Digest(), without.Digest())
			if name == "javascript" {
				require.Equal(t, pin, without.SourceDigest())
				require.Equal(t, 3, without.FormatVersion())
			} else {
				require.Equal(t, pin, without.Digest())
			}
			for _, invalid := range []string{"wrong", strings.Repeat("0", 64), strings.Repeat("f", 63), "   "} {
				_, err := LoadUpload(bytes.NewReader(data), invalid)
				require.Error(t, err, "a supplied pin must never silently fall back to optional mode")
			}
		})
	}
}

func TestOptionalUploadPinDoesNotRelaxInternalLoadersOrPackageValidation(t *testing.T) {
	legacy := testPackage(t, validManifest(), "data-only source")
	_, err := LoadPackage(bytes.NewReader(legacy.Document()), "")
	require.Error(t, err, "persisted packages still require their exact digest")
	_, err = LoadJavaScript(strings.NewReader(validUploadSource), fmt.Sprintf("%x", sha256.Sum256([]byte(validUploadSource))))
	require.NoError(t, err)
	_, err = LoadJavaScript(strings.NewReader(validUploadSource), "")
	require.Error(t, err, "official/internal JS loading remains explicitly pinned")
	for _, invalid := range []string{"", "{}", `export const meta = {};`, strings.Repeat("x", MaxPackageBytes+1)} {
		_, err := LoadUpload(strings.NewReader(invalid), "")
		require.Error(t, err, "optional pin does not bypass manifest, compilation or size checks")
	}
}

func TestLoadUploadWhitespaceBOMBoundary(t *testing.T) {
	data := []byte(" \ufeff{};\r\n" + validUploadSource)
	pkg, err := LoadUpload(bytes.NewReader(data), fmt.Sprintf("%x", sha256.Sum256(data)))
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), pkg.SourceDigest())
	require.NotEqual(t, pkg.SourceDigest(), pkg.Digest())
}

func TestLoadUploadPreservesUTF8BOMIdentity(t *testing.T) {
	source := []byte(`export const meta = {apiVersion: 1, key: "bom-upload", name: "BOM", version: "1.0.0", models: ["bom-model"]}; export function buildSubmitRequest(){return {}} export function parseSubmitResponse(){return {}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {}}`)
	legacy := testPackage(t, validManifest(), "data-only source")
	for name, body := range map[string][]byte{"javascript": source, "json": legacy.Document()} {
		t.Run(name, func(t *testing.T) {
			data := append([]byte{0xef, 0xbb, 0xbf}, body...)
			pin := fmt.Sprintf("%x", sha256.Sum256(data))
			pkg, err := LoadUpload(bytes.NewReader(data), pin)
			require.NoError(t, err)
			unpinned, err := LoadUpload(bytes.NewReader(data), "")
			require.NoError(t, err)
			require.Equal(t, pkg.Document(), unpinned.Document())
			if name == "javascript" {
				require.Equal(t, pin, pkg.SourceDigest())
				require.True(t, pkg.Executable())
			} else {
				require.Equal(t, data, pkg.Document())
				require.Equal(t, pin, pkg.Digest())
				require.Equal(t, len(data), pkg.size)
			}
			_, err = LoadUpload(bytes.NewReader(data), fmt.Sprintf("%x", sha256.Sum256(body)))
			require.Error(t, err, "removing BOM before hashing must not match the original file pin")
			reloaded, err := LoadPackage(bytes.NewReader(pkg.Document()), pkg.Digest())
			require.NoError(t, err, "persisted BOM identities remain readable by the strictly pinned loader")
			require.Equal(t, pkg.Document(), reloaded.Document())
			_, err = LoadPackage(bytes.NewReader(pkg.Document()), "")
			require.Error(t, err)
		})
	}
}

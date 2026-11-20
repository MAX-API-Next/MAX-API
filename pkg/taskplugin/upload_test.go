// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"

	"github.com/stretchr/testify/require"
)

const validUploadSource = `export const meta = {apiVersion: 1, key: "custom-upload", name: "Custom", version: "1.0.0", models: ["custom-model"]}; export function buildSubmitRequest(){return {}} export function parseSubmitResponse(){return {}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {}}`

func TestPinnedPackageRejectsRepeatedJSONMembers(t *testing.T) {
	for _, prefix := range []string{"", "\xef\xbb\xbf"} {
		for _, repeated := range []string{
			`{"format_version":1,"manifest":{"APIVersion":1,"Key":"duplicate","Name":"Duplicate","Version":"1.0.0","Models":["model"]},"source":"first","source":"second"}`,
			`{"format_version":1,"manifest":{"APIVersion":1,"Key":"duplicate","Name":"First","Name":"Second","Version":"1.0.0","Models":["model"]},"source":"source"}`,
			`{"format_version":1,"manifest":{"APIVersion":1,"Key":"duplicate","Name":"Duplicate","Version":"1.0.0","Models":["model"],"unknown":[{"x":1,"x":2}]},"source":"source"}`,
		} {
			data := []byte(prefix + repeated)
			_, err := LoadPackage(bytes.NewReader(data), fmt.Sprintf("%x", sha256.Sum256(data)))
			require.ErrorContains(t, err, "repeated")
		}
	}
}

func TestFormatOneUploadUsesLegacyReaderNumberRange(t *testing.T) {
	const document = `{"format_version":1,"manifest":{"APIVersion":1,"Key":"range-check","Name":"Range Check","Version":"1.0.0","Models":["model"],"future":[{"counter":%s}]},"source":"source"}`
	tolerated := fmt.Sprintf(document, "1e308")
	require.True(t, validLegacyPackageFields([]byte(tolerated)))
	_, err := LoadUpload(strings.NewReader(tolerated), "")
	require.NoError(t, err)
	overflow := fmt.Sprintf(document, "1e1000")
	require.False(t, validLegacyPackageFields([]byte(overflow)))
	_, err = LoadUpload(strings.NewReader(overflow), "")
	require.Error(t, err)
}

type uploadMetadataStartContext struct {
	context.Context
	onStart func()
	once    sync.Once
}

func (ctx *uploadMetadataStartContext) Deadline() (time.Time, bool) {
	// The parser/compiler use Err only. Program.runInEnvironment first
	// asks for the deadline when it sets up the metadata runtime watchdog.
	ctx.once.Do(ctx.onStart)
	return ctx.Context.Deadline()
}

func TestLoadUploadContextBoundsRawAndEnvelopeMetadata(t *testing.T) {
	original, err := LoadUpload(strings.NewReader(validUploadSource), "")
	require.NoError(t, err)
	control, err := common.Marshal(PackageDocument{FormatVersion: 3, Manifest: original.Manifest(), Source: validUploadSource})
	require.NoError(t, err)
	_, err = LoadUpload(bytes.NewReader(control), "")
	require.NoError(t, err, "the exact envelope without its loop is valid")
	source := "for(;;){};" + validUploadSource
	document, err := common.Marshal(PackageDocument{FormatVersion: 3, Manifest: original.Manifest(), Source: source})
	require.NoError(t, err)
	for name, data := range map[string][]byte{"raw": []byte(source), "envelope": document} {
		t.Run(name, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			var started time.Time
			var timer *time.Timer
			ctx := &uploadMetadataStartContext{Context: parent, onStart: func() {
				started = time.Now()
				timer = time.AfterFunc(100*time.Millisecond, cancel)
			}}
			_, err := LoadUploadContext(ctx, bytes.NewReader(data), "")
			require.NotNil(t, timer, "the metadata runtime must start before cancellation")
			defer timer.Stop()
			require.ErrorIs(t, err, context.Canceled)
			require.Less(t, time.Since(started), 2*time.Second)
		})
	}
	bounded, err := LoadUploadContext(t.Context(), bytes.NewReader([]byte(validUploadSource)), "")
	require.NoError(t, err)
	require.Equal(t, original.Document(), bounded.Document())
	require.Equal(t, original.Digest(), bounded.Digest())
}

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
	for _, prefix := range []string{"\ufeff{};\r\n", " \ufeff{};\r\n", "{};\r\n"} {
		data := []byte(prefix + validUploadSource)
		pkg, err := LoadUpload(bytes.NewReader(data), fmt.Sprintf("%x", sha256.Sum256(data)))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), pkg.SourceDigest())
		require.NotEqual(t, pkg.SourceDigest(), pkg.Digest())
	}
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

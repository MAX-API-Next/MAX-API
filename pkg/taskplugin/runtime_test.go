// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/plugins"
)

func officialKling(t *testing.T) *Package {
	t.Helper()
	source, err := plugins.TaskSources.ReadFile("tasks/kling/1.2.0/plugin.js")
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadJavaScript(bytes.NewReader(source), "f4b949679dba31e287d2b21d4b3b4c09980c9bb744da9ede5153f1801cf84240")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOfficialKlingExecutionAndUsage(t *testing.T) {
	p := officialKling(t)
	if p.FormatVersion() != 3 || p.Manifest().Key != "kling" {
		t.Fatal("official metadata not compiled")
	}
	ctx := context.Background()
	input := map[string]any{"requestBody": map[string]any{"model": "kling-v2-5-turbo", "duration": 5, "mode": "std", "prompt": "a test"}, "model": "kling-v2-5-turbo", "upstreamModel": "kling-v2-5-turbo", "baseUrl": "https://provider.invalid", "apiKey": "synthetic-test", "action": "text_to_video", "upstream": map[string]any{"kind": "vendor"}}
	raw, err := p.Call(ctx, "extractUsage", input)
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]float64
	if common.Unmarshal(raw, &facts) != nil || facts["units"] != 1.5 {
		t.Fatalf("5 seconds * 0.3 credit = 1.5; got %s", raw)
	}
	raw, err = p.Call(ctx, "buildSubmitRequest", input)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		URL  string
		Body struct {
			Model    string `json:"model_name"`
			Duration string
		}
	}
	if common.Unmarshal(raw, &request) != nil || request.URL != "https://provider.invalid/v1/videos/text2video" || request.Body.Model != "kling-v2-5-turbo" || request.Body.Duration != "5" {
		t.Fatalf("unexpected request %s", raw)
	}
	for _, sample := range []struct {
		body     string
		expected string
	}{{`{"data":{"final_unit_deduction":"0.6"}}`, `{"units":0.6}`}, {`{"data":{"final_unit_deduction":"0"}}`, `{"units":0}`}, {`{"data":{}}`, `null`}} {
		var body any
		if err := common.Unmarshal([]byte(sample.body), &body); err != nil {
			t.Fatal(err)
		}
		got, err := p.Call(ctx, "extractUsageOnComplete", input, nil, body)
		if err != nil || string(got) != sample.expected {
			t.Fatalf("raw %s: got %s, %v", sample.body, got, err)
		}
	}
	loaded, err := LoadPackage(bytes.NewReader(p.Document()), p.Digest())
	if err != nil || loaded.SourceDigest() != p.SourceDigest() {
		t.Fatalf("executable restart: %v", err)
	}
}

func TestRuntimeInterruptIsolationAndRedaction(t *testing.T) {
	source := `export const meta={apiVersion:1,key:"test",name:"Test",version:"1.0.0",models:["m"]}; export function spin(){for(;;){}} export function fail(){throw Error("SECRET-do-not-return")} export function value(){return {zero:0}};`
	program, err := CompileJavaScript(source, RuntimeOptions{Timeout: 25 * time.Millisecond, MemoryLimit: 2 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = program.Call(context.Background(), "spin"); err == nil {
		t.Fatal("loop was not interrupted")
	}
	if _, err = program.Call(context.Background(), "fail"); err == nil || bytes.Contains([]byte(err.Error()), []byte("SECRET")) {
		t.Fatal("hook error leaked or missing")
	}
	raw, err := program.Call(context.Background(), "value")
	if err != nil || string(raw) != `{"zero":0}` {
		t.Fatalf("runtime did not recover: %s %v", raw, err)
	}
}

func TestOfficialTaskBundleCompilation(t *testing.T) {
	raw, err := plugins.TaskSources.ReadFile("SOURCES.json")
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Plugins []struct{ Key, Version, Path, SHA256 string }
	}
	if err := common.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Plugins) != 11 {
		t.Fatal("task bundle coverage changed")
	}
	for _, entry := range index.Plugins {
		t.Run(entry.Key, func(t *testing.T) {
			source, err := plugins.TaskSources.ReadFile(strings.TrimPrefix(entry.Path, "plugins/"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := LoadJavaScript(bytes.NewReader(source), entry.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			if p.Manifest().Key != entry.Key || p.Manifest().Version != entry.Version || p.SourceDigest() != entry.SHA256 {
				t.Fatal("source/index identity differs")
			}
			if entry.Key == "alibaba" {
				for _, claim := range p.Manifest().Protocols {
					if claim.Name == "openai_image" {
						if !slices.Contains(claim.Models, "qwen-image") || slices.Contains(claim.Models, "wan2.7-t2v") {
							t.Fatal("image protocol lost its explicit model scope")
						}
					}
				}
			}
		})
	}
}

func TestRuntimeMemoryOutputAndAsyncLimits(t *testing.T) {
	source := `export const meta={key:"probe"}; export function memory(){const retained=[];for(;;){retained.push("x".repeat(65536));}} export function output(){return "x".repeat(1048577)} export async function asyncResult(){return 1}`
	p, err := CompileJavaScript(source, RuntimeOptions{Timeout: time.Second, MemoryLimit: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"memory", "output", "asyncResult"} {
		t.Run(hook, func(t *testing.T) {
			if _, err := p.Call(t.Context(), hook); err == nil {
				t.Fatal("unbounded/async result accepted")
			}
		})
	}
}

func TestExecutableMetadataCannotBeForgedAndLegacyNeverRuns(t *testing.T) {
	p := officialKling(t)
	var document PackageDocument
	if err := common.Unmarshal(p.Document(), &document); err != nil {
		t.Fatal(err)
	}
	document.Manifest.Name = "forged display/routing metadata"
	wire, err := common.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire))); err == nil {
		t.Fatal("trusted envelope hash cannot replace compiled metadata proof")
	}
	legacy := PackageDocument{FormatVersion: 1, Manifest: validManifest(), Source: "for(;;){}"}
	wire, err = common.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	old, err := LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	if err != nil {
		t.Fatal(err)
	}
	if old.Executable() {
		t.Fatal("legacy source started executing")
	}
	if _, err = old.Call(t.Context(), "buildSubmitRequest"); err == nil {
		t.Fatal("legacy call accepted")
	}
}

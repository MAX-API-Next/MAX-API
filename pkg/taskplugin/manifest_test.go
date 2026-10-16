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
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		APIVersion: 1,
		Key:        "vendor",
		Name:       "Vendor",
		Version:    "1.0.0",
		Models:     []string{"vendor-model"},
		Protocols:  []ProtocolClaim{{Name: "openai_responses", Supports: []string{"stream", "sync", "background"}}, {Name: "openai_video", Bare: true}},
		Routes: []Route{
			{Method: "POST", Path: "/vendor/v1/jobs", Type: "submit", Decode: "createJob", Render: "jobCreated"},
			{Method: "GET", Path: "/vendor/v1/jobs/:task_id", Type: "query", Render: "jobStatus"},
		},
		BaseURL:      "https://api.vendor.example",
		AllowedHosts: []string{"api.vendor.example:443"},
		Upstreams:    []string{"vendor", "new_api"},
		UsageSchema:  map[string]UsageField{"duration": {Type: "number", Unit: "second"}, "resolution": {Type: "enum", Enum: []string{"720p", "1080p"}}},
	}
}

func TestValidateManifestAcceptsVersionedProtocolContract(t *testing.T) {
	if err := ValidateManifest(validManifest()); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestValidateManifestRejectsRouteShapeConflict(t *testing.T) {
	manifest := validManifest()
	manifest.Routes = append(manifest.Routes, Route{
		Method: "GET", Path: "/vendor/v1/jobs/:id", Type: "query", Render: "jobStatus",
	})
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "duplicate task plugin route") {
		t.Fatalf("expected normalized route conflict, got %v", err)
	}
}

func TestValidateManifestRejectsUnsafeBaseURLAndHost(t *testing.T) {
	manifest := validManifest()
	manifest.BaseURL = "https://user:password@api.vendor.example"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected base URL credentials to be rejected")
	}
	manifest = validManifest()
	manifest.AllowedHosts = []string{"https://api.vendor.example/path"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected allowed-host URL form to be rejected")
	}
}

func TestValidateManifestRejectsInvalidProtocolAndUsage(t *testing.T) {
	manifest := validManifest()
	manifest.Protocols[0].Supports = []string{"stream", "stream"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected duplicate protocol mode to be rejected")
	}
	manifest = validManifest()
	manifest.UsageSchema["duration"] = UsageField{Type: "number", Unit: "usd"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected unsupported usage unit to be rejected")
	}
	manifest = validManifest()
	manifest.UsageSchema["resolution"] = UsageField{Type: "enum", Enum: []string{"720p", "720p"}}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected duplicate enum value to be rejected")
	}
}

func TestValidateManifestRejectsUndeclaredRouteModel(t *testing.T) {
	manifest := validManifest()
	manifest.Routes[0].Models = []string{"missing-model"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected undeclared route model to be rejected")
	}
}

func TestValidateManifestRejectsInvalidQueryTaskIDParameter(t *testing.T) {
	manifest := validManifest()
	manifest.Routes[1].TaskIDParam = "missing"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected query route task id parameter to match a path parameter")
	}
	manifest = validManifest()
	manifest.Routes[1].TaskIDParam = "task_id"
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("expected matching query task id parameter to pass: %v", err)
	}
}

func TestValidateManifestRejectsPaddedModelNames(t *testing.T) {
	manifest := validManifest()
	manifest.Models[0] = " vendor-model"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected padded model name to be rejected")
	}
}

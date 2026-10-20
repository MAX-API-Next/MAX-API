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
	"fmt"
	"sync"
	"testing"
)

func registryManifest(key string, route string) Manifest {
	manifest := validManifest()
	manifest.Key = key
	manifest.Name = key
	manifest.Routes = []Route{{Method: "GET", Path: route, Type: "query", Render: "jobStatus"}}
	return manifest
}

func TestRegistryPublishesAtomicSnapshotAndMatchesRoute(t *testing.T) {
	registry := NewRegistry()
	firstGeneration, err := registry.Publish(registryManifest("alpha", "/alpha/jobs/:task_id"))
	if err != nil {
		t.Fatalf("publish first manifest: %v", err)
	}
	before := registry.Snapshot()
	if before.Generation() != firstGeneration {
		t.Fatalf("expected generation %d, got %d", firstGeneration, before.Generation())
	}
	if _, ok := before.MatchRoute("GET", "/beta/jobs/1", "vendor-model"); ok {
		t.Fatal("unexpected route match before second publication")
	}
	secondGeneration, err := registry.Publish(registryManifest("beta", "/beta/jobs/:task_id"))
	if err != nil {
		t.Fatalf("publish second manifest: %v", err)
	}
	if secondGeneration != firstGeneration+1 {
		t.Fatalf("expected generation %d, got %d", firstGeneration+1, secondGeneration)
	}
	if _, ok := before.Plugin("beta"); ok {
		t.Fatal("old snapshot observed a later publication")
	}
	match, ok := registry.Snapshot().MatchRoute("GET", "/beta/jobs/1", "vendor-model")
	if !ok || match.PluginKey != "beta" || match.Route.Path != "/beta/jobs/:task_id" {
		t.Fatalf("unexpected route match: %#v, %v", match, ok)
	}
}

func TestRegistryBreaksEqualSpecificityTiesDeterministically(t *testing.T) {
	for _, order := range [][]string{{"beta", "alpha"}, {"alpha", "beta"}} {
		registry := NewRegistry()
		routes := map[string]string{
			"alpha": "/jobs/:id",
			"beta":  "/:kind/latest",
		}
		for _, key := range order {
			if _, err := registry.Publish(registryManifest(key, routes[key])); err != nil {
				t.Fatalf("publish %s: %v", key, err)
			}
		}
		match, ok := registry.Snapshot().MatchRoute("GET", "/jobs/latest", "vendor-model")
		if !ok || match.PluginKey != "alpha" {
			t.Fatalf("expected alpha to win equal-specificity tie, got %#v, %v", match, ok)
		}
	}
}

func TestRegistryRejectsDuplicatePluginAndRouteConflict(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Publish(registryManifest("alpha", "/jobs/:task_id")); err != nil {
		t.Fatalf("publish first manifest: %v", err)
	}
	if _, err := registry.Publish(registryManifest("alpha", "/other/:id")); err == nil {
		t.Fatal("expected duplicate plugin key to be rejected")
	}
	if _, err := registry.Publish(registryManifest("beta", "/jobs/:id")); err == nil {
		t.Fatal("expected normalized route conflict to be rejected")
	}
}

func TestRegistryIndexesModelCandidatesInStableOrder(t *testing.T) {
	registry := NewRegistry()
	for _, key := range []string{"zeta", "alpha", "middle"} {
		manifest := registryManifest(key, "/"+key)
		if _, err := registry.Publish(manifest); err != nil {
			t.Fatalf("publish %s: %v", key, err)
		}
	}
	candidates := registry.Snapshot().ModelCandidates("vendor-model")
	if len(candidates) != 3 || candidates[0].Key != "alpha" || candidates[1].Key != "middle" || candidates[2].Key != "zeta" {
		t.Fatalf("unexpected model candidates: %#v", candidates)
	}
}

func TestRegistryConcurrentPublicationPreservesAllUniquePlugins(t *testing.T) {
	registry := NewRegistry()
	const count = 12
	var group sync.WaitGroup
	errors := make(chan error, count)
	for index := 0; index < count; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			key := fmt.Sprintf("p%02d", index)
			if _, err := registry.Publish(registryManifest(key, "/"+key)); err != nil {
				errors <- err
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent publication failed: %v", err)
	}
	snapshot := registry.Snapshot()
	if snapshot.Generation() != count {
		t.Fatalf("expected generation %d, got %d", count, snapshot.Generation())
	}
	if len(snapshot.ModelCandidates("vendor-model")) != count {
		t.Fatalf("expected %d model candidates", count)
	}
}

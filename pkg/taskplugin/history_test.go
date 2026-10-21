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
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestHostRetainRestoresHistoryWithoutChangingActive(t *testing.T) {
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "retained-task"}
	previous := testPackage(t, validManifest(), "original-v1")
	original := NewHost()
	_, err := original.Install(previous)
	require.NoError(t, err)
	binding, err := original.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)

	latest := validManifest()
	latest.Version = "2.0.0"
	current := testPackage(t, latest, "active-v2")
	restarted := NewHost()
	_, err = restarted.Install(current)
	require.NoError(t, err)
	before := restarted.state.Load()
	_, err = restarted.Retain(previous)
	require.NoError(t, err)
	restored, err := restarted.Resolve(binding, actor)
	require.NoError(t, err)
	require.Equal(t, previous.Digest(), restored.Binding().PackageDigest)
	next, err := restarted.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	require.Equal(t, "2.0.0", next.Version, "restoring a historical task must preserve the chosen active version")
	require.Equal(t, before.routes, restarted.state.Load().routes)
}

func TestHostRetainMultipleVersionsDoesNotActivateRoutes(t *testing.T) {
	host := NewHost()
	first := testPackage(t, validManifest(), "first")
	secondManifest := validManifest()
	secondManifest.Version = "2.0.0"
	second := testPackage(t, secondManifest, "second")
	generation, err := host.Retain(second, first)
	require.NoError(t, err)
	require.Equal(t, generation, host.state.Load().generation)
	require.Len(t, host.state.Load().history, 2)
	require.Empty(t, host.state.Load().active)
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "history-only"}
	_, err = host.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, ErrBindingRejected)

	_, err = host.Install(second)
	require.NoError(t, err)
	binding, err := host.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	require.Equal(t, "2.0.0", binding.Version)
	// Install is an explicit activation decision, including a chosen rollback.
	_, err = host.Install(first)
	require.NoError(t, err)
	binding, err = host.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	require.Equal(t, "1.0.0", binding.Version)
}

func TestHostRetainInvalidBatchLeavesGenerationUntouched(t *testing.T) {
	for _, invalid := range []string{"empty", "nil", "unverified", "duplicate", "digest conflict", "revoked", "capacity"} {
		t.Run(invalid, func(t *testing.T) {
			host := NewHost()
			original := testPackage(t, validManifest(), "original")
			_, err := host.Install(original)
			require.NoError(t, err)
			additional := testPackage(t, registryManifest("historical", "/historical/:id"), "historical")
			packages := []*Package{additional}
			switch invalid {
			case "empty":
				packages = nil
			case "nil":
				packages = append(packages, nil)
			case "unverified":
				packages = append(packages, &Package{})
			case "duplicate":
				packages = append(packages, additional)
			case "digest conflict":
				packages = append(packages, testPackage(t, validManifest(), "different bytes"))
			case "revoked":
				require.NoError(t, host.Revoke(original.manifest.Key, original.manifest.Version))
				packages = append(packages, original)
			case "capacity":
				// Exercise the shared accounting limit without allocating a
				// 64 MiB fixture; LoadPackage's 1 MiB cap is tested separately.
				additional.size = MaxRetainedPackageBytes
			}
			before := host.state.Load()
			_, err = host.Retain(packages...)
			require.Error(t, err)
			require.Same(t, before, host.state.Load(), "failed batches must publish nothing")
			require.NotContains(t, host.state.Load().history, packageIdentity(additional.manifest.Key, additional.manifest.Version))
		})
	}
	for _, host := range []*Host{nil, {}} {
		_, err := host.Retain()
		require.Error(t, err)
	}
}

func TestHostConcurrentRetainPreservesActivationAndRevocation(t *testing.T) {
	host := NewHost()
	activeManifest := validManifest()
	activeManifest.Version = "3.0.0"
	_, err := host.Install(testPackage(t, activeManifest, "active"))
	require.NoError(t, err)
	previous := testPackage(t, validManifest(), "previous")
	_, err = host.Retain(previous)
	require.NoError(t, err)

	const count = 8
	packages := make([]*Package, count)
	for i := range packages {
		manifest := validManifest()
		manifest.Version = fmt.Sprintf("1.0.%d", i+1)
		packages[i] = testPackage(t, manifest, "historical")
	}
	results := make(chan error, count+2)
	var workers sync.WaitGroup
	for _, p := range packages {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := host.Retain(p)
			results <- err
		}()
	}
	unrelated := testPackage(t, registryManifest("unrelated", "/unrelated/:id"), "unrelated")
	workers.Add(2)
	go func() { defer workers.Done(); _, err := host.Install(unrelated); results <- err }()
	go func() { defer workers.Done(); results <- host.Revoke(previous.manifest.Key, previous.manifest.Version) }()
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Len(t, host.state.Load().history, count+3)
	require.Len(t, host.state.Load().active, 2)
	require.Equal(t, "3.0.0", host.state.Load().active[activeManifest.Key].manifest.Version)
	for _, p := range packages {
		require.Equal(t, p.Digest(), host.state.Load().history[packageIdentity(p.manifest.Key, p.manifest.Version)].Digest())
	}
	_, err = host.Retain(previous)
	require.ErrorContains(t, err, "revoked")
}

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
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func testPackage(t *testing.T, manifest Manifest, source string) *Package {
	t.Helper()
	data, err := common.Marshal(PackageDocument{FormatVersion: 1, Manifest: manifest, Source: source})
	require.NoError(t, err)
	hash := sha256.Sum256(data)
	p, err := LoadPackage(bytes.NewReader(data), fmt.Sprintf("%x", hash))
	require.NoError(t, err)
	return p
}

func TestLoadPackagePinsWholeDocumentAndClonesMetadata(t *testing.T) {
	manifest := validManifest()
	p := testPackage(t, manifest, "opaque source - never evaluated")
	require.Equal(t, manifest.UsageSchema, p.Manifest().UsageSchema)
	copy := p.Manifest()
	copy.Models[0] = "changed"
	copy.UsageSchema["resolution"] = UsageField{}
	require.Equal(t, manifest, p.Manifest())
	_, err := LoadPackage(strings.NewReader("{}"), strings.Repeat("0", 64))
	require.ErrorContains(t, err, "digest mismatch")
	_, err = LoadPackage(strings.NewReader(strings.Repeat("x", MaxPackageBytes+1)), strings.Repeat("0", 64))
	require.ErrorContains(t, err, "size limit")
	_, err = LoadPackage(strings.NewReader("{}"), "")
	require.Error(t, err)
}

func TestHostBatchAtomicityAndVersionPinning(t *testing.T) {
	host := NewHost()
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "request-one"}
	first := testPackage(t, validManifest(), "first")
	_, err := host.Install(first)
	require.NoError(t, err)
	binding, err := host.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	manifest := validManifest()
	manifest.Version = "2.0.0"
	_, err = host.Install(testPackage(t, manifest, "second"))
	require.NoError(t, err)
	session, err := host.Resolve(binding, actor)
	require.NoError(t, err)
	require.Equal(t, first.Digest(), session.Binding().PackageDigest)
	_, err = host.Install(testPackage(t, validManifest(), "silently changed"))
	require.ErrorContains(t, err, "source changed")
	conflict := registryManifest("conflict", "/vendor/v1/jobs/:id")
	newPlugin := registryManifest("new", "/new/:id")
	before := host.state.Load().generation
	_, err = host.Install(testPackage(t, newPlugin, "new"), testPackage(t, conflict, "conflict"))
	require.Error(t, err)
	require.Equal(t, before, host.state.Load().generation)
	require.NotContains(t, host.state.Load().active, "new")
	_, err = host.Bind(actor, "POST", "/vendor/v1/jobs", "undeclared-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, ErrBindingRejected)
	other := actor
	other.TokenID++
	_, err = host.Resolve(binding, other)
	require.ErrorIs(t, err, ErrBindingRejected)
	corrupt := binding
	corrupt.ContractDigest = strings.Repeat("0", 64)
	_, err = host.Resolve(corrupt, actor)
	require.ErrorIs(t, err, ErrBindingRejected)

	// Rebuild after a process restart from retained, externally pinned packages.
	restarted := NewHost()
	_, err = restarted.Install(first)
	require.NoError(t, err)
	_, err = restarted.Resolve(binding, actor)
	require.NoError(t, err)
	require.NoError(t, host.Revoke(binding.PluginKey, binding.Version))
	_, err = session.UsageEnvelope(nil)
	require.ErrorIs(t, err, ErrBindingRejected)
}

func TestHostConcurrentInstallPreservesEveryPackage(t *testing.T) {
	host := NewHost()
	const count = 8
	packages := make([]*Package, count)
	for i := range packages {
		packages[i] = testPackage(t, registryManifest(fmt.Sprintf("p%d", i), fmt.Sprintf("/p%d/:id", i)), "original")
	}
	var group sync.WaitGroup
	errors := make(chan error, count)
	for _, p := range packages {
		group.Add(1)
		go func() { defer group.Done(); _, err := host.Install(p); errors <- err }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Len(t, host.state.Load().active, count)
}

func TestHostEnvelopePreservesExplicitZeroAndRejectsInvalidFacts(t *testing.T) {
	host := NewHost()
	_, err := host.Install(testPackage(t, validManifest(), "original"))
	require.NoError(t, err)
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "generation-one"}
	binding, err := host.Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.DoubaoVideoContract())
	require.NoError(t, err)
	session, err := host.Resolve(binding, actor)
	require.NoError(t, err)
	zero := int64(0)
	envelope, err := session.UsageEnvelope(&types.TaskUsage{CompletionTokens: &zero, TotalTokens: &zero, Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete})
	require.NoError(t, err)
	require.Equal(t, types.TaskUsagePresencePresentZero, envelope.Presence)
	_, err = session.UsageEnvelope(&types.TaskUsage{Source: types.TaskUsageSourceRequestEstimate, Completeness: types.TaskUsageCompletenessComplete})
	require.Error(t, err)
}

func TestHostMappedBindingPreservesOriginAndUpstreamIdentity(t *testing.T) {
	host := NewHost()
	_, err := host.Install(testPackage(t, validManifest(), "original"))
	require.NoError(t, err)
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "mapped-request"}
	binding, err := host.BindMapped(actor, "POST", "/vendor/v1/jobs", "client-alias", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	require.Equal(t, "client-alias", binding.Model)
	require.Equal(t, "vendor-model", binding.UpstreamModel)
	_, err = host.Resolve(binding, actor)
	require.NoError(t, err)
	binding.UpstreamModel = "unknown"
	_, err = host.Resolve(binding, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
}

func TestHostOverlappingRoutesRespectManifestModels(t *testing.T) {
	host := NewHost()
	alpha := registryManifest("alpha", "/jobs/:id")
	alpha.Models = []string{"model-a"}
	beta := registryManifest("beta", "/:kind/latest")
	beta.Models = []string{"model-b"}
	_, err := host.Install(testPackage(t, alpha, "alpha"), testPackage(t, beta, "beta"))
	require.NoError(t, err)
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "overlapping-path"}
	for _, item := range []struct{ model, key string }{{"model-a", "alpha"}, {"model-b", "beta"}} {
		binding, err := host.Bind(actor, "GET", "/jobs/latest", item.model, taskusage.MiniMaxH3Contract())
		require.NoError(t, err)
		require.Equal(t, item.key, binding.PluginKey)
	}
	_, err = host.Bind(actor, "GET", "/jobs/latest", "unknown-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, ErrBindingRejected)
}

func TestLoadPackageRejectsUnsupportedEnvelopeFields(t *testing.T) {
	data, err := common.Marshal(map[string]any{"format_version": 1, "manifest": validManifest(), "source": "original", "enabled": true})
	require.NoError(t, err)
	hash := sha256.Sum256(data)
	_, err = LoadPackage(bytes.NewReader(data), fmt.Sprintf("%x", hash))
	require.ErrorContains(t, err, "unsupported task plugin package fields")
}

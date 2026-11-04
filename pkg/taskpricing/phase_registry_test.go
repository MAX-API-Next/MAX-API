// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func phasePackage(t *testing.T, version, code string, c types.TaskFactContract) *taskplugin.Package {
	t.Helper()
	m := taskplugin.Manifest{APIVersion: 1, Key: "kling", Name: "Isolated phase fixture", Version: version, Models: []string{"kling-v1"},
		Routes: []taskplugin.Route{{Method: "POST", Path: "/phase/jobs", Type: "submit", Decode: "submit", Render: "created"}}, UsageSchema: map[string]taskplugin.UsageField{}}
	for k, f := range c.UsageSchema {
		m.UsageSchema[k] = taskplugin.UsageField{Type: f.Type, Unit: f.Unit, Enum: f.Enum}
	}
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Source: code, Manifest: m})
	require.NoError(t, err)
	p, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	return p
}

func TestPhaseRegistryRequiresExactReviewedPackageAndDetachedPolicy(t *testing.T) {
	c, ok := KnownContract("kling", "kling-v1")
	require.True(t, ok)
	p := phasePackage(t, "1.0.0", "synthetic source; never executed", c)
	entry := PhaseApproval{PluginKey: "kling", Version: "1.0.0", PackageDigest: p.Digest(), Model: "kling-v1", Action: "generate", Contract: c, Evidence: "isolated fixture policy; not a real package review"}
	r, err := NewPhaseRegistry([]PhaseApproval{entry})
	require.NoError(t, err)
	a, err := r.Resolve(p, "kling-v1", "generate")
	require.NoError(t, err)
	firstID := a.ApprovalID
	a.Contract.FieldPolicy["units"] = types.TaskFactPolicy{Phase: "request"}
	c.FieldPolicy["units"] = types.TaskFactPolicy{Phase: "request"}
	a, err = r.Resolve(p, "kling-v1", "generate")
	require.NoError(t, err)
	require.Equal(t, "actual", a.Contract.FieldPolicy["units"].Phase)
	require.Equal(t, firstID, a.ApprovalID)
	for _, other := range []*taskplugin.Package{phasePackage(t, "2.0.0", "upgraded", entry.Contract), phasePackage(t, "1.0.0", "changed bytes", entry.Contract)} {
		_, err := r.Resolve(other, "kling-v1", "generate")
		require.Error(t, err, "same schema/name is not phase approval")
	}
	_, err = r.Resolve(p, "alias", "generate")
	require.Error(t, err)
	_, err = r.Resolve(p, "kling-v1", "other")
	require.Error(t, err)
	wrongContract, _ := KnownContract("kling", "kling-v1")
	field := wrongContract.UsageSchema["units"]
	field.Unit = "token"
	wrongContract.UsageSchema["units"] = field
	wrongPackage := phasePackage(t, "1.0.0", "wrong units", wrongContract)
	originalContract, _ := KnownContract("kling", "kling-v1")
	wrongRegistry, err := NewPhaseRegistry([]PhaseApproval{{PluginKey: "kling", Version: "1.0.0", PackageDigest: wrongPackage.Digest(),
		Model: "kling-v1", Action: "generate", Contract: originalContract, Evidence: "synthetic mismatched policy"}})
	require.NoError(t, err)
	_, err = wrongRegistry.Resolve(wrongPackage, "kling-v1", "generate")
	require.Error(t, err, "exact package identity cannot excuse a credit/token schema mismatch")
	var missing *PhaseRegistry
	_, err = missing.Resolve(p, "kling-v1", "generate")
	require.Error(t, err)
	_, err = NewPhaseRegistry([]PhaseApproval{entry, entry})
	require.Error(t, err)
	entry.Contract, _ = KnownContract("kling", "kling-v1")
	entry.Evidence = ""
	_, err = NewPhaseRegistry([]PhaseApproval{entry})
	require.Error(t, err)

	host := taskplugin.NewHost()
	_, err = host.Install(p)
	require.NoError(t, err)
	actor := taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "phase-fixture"}
	b, _, err := r.Bind(host, actor, "POST", "/phase/jobs", "alias", "kling-v1", "generate")
	require.NoError(t, err)
	require.Equal(t, p.Digest(), b.PackageDigest)
	require.Equal(t, a.ContractDigest, b.ContractDigest)
	_, _, err = missing.Bind(host, actor, "POST", "/phase/jobs", "alias", "kling-v1", "generate")
	require.Error(t, err)
}

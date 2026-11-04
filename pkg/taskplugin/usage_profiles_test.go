// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func profilePackage(t *testing.T, m Manifest, format int) (*Package, error) {
	t.Helper()
	wire, err := common.Marshal(PackageDocument{FormatVersion: format, Manifest: m, Source: "synthetic profile fixture; never executed"})
	require.NoError(t, err)
	return LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
}

func TestModelUsageProfilesAreExactDetachedAndRequireFormat2(t *testing.T) {
	m := validManifest()
	legacy, err := profilePackage(t, m, 1)
	require.NoError(t, err)
	legacyWire := legacy.Document()
	require.NotContains(t, string(legacyWire), "usageProfiles")
	m.Models = []string{"vendor-model", "other-model"}
	m.UsageProfiles = []UsageProfile{
		{Models: []string{"vendor-model"}, Schema: map[string]UsageField{"duration": {Type: "number", Unit: "second"}}},
		{Models: []string{"other-model"}, Schema: map[string]UsageField{"audio": {Type: "boolean"}, "mode": {Type: "enum", Enum: []string{"on", "off"}}}},
	}
	_, err = profilePackage(t, m, 1)
	require.Error(t, err)
	p, err := profilePackage(t, m, 2)
	require.NoError(t, err)
	s, err := p.ModelUsageSchema("vendor-model")
	require.NoError(t, err)
	require.Len(t, s, 1, "the fallback union must not become the concrete model schema")
	s, err = p.ModelUsageSchema("other-model")
	require.NoError(t, err)
	require.Equal(t, "boolean", s["audio"].Type, "profiles may add fields absent from the fallback schema")
	s["mode"].Enum[0] = "changed"
	m.UsageProfiles[1].Schema["mode"].Enum[0] = "changed-source"
	s, err = p.ModelUsageSchema("other-model")
	require.NoError(t, err)
	require.Equal(t, "on", s["mode"].Enum[0])
	host := NewHost()
	_, err = host.Install(p)
	require.NoError(t, err)
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "profile-schema"}
	binding, err := host.BindPricingMapped(actor, "POST", "/vendor/v1/jobs", "alias", "other-model", PricingContractIdentity{Action: "generate", SchemaVersion: 1, ContractDigest: strings.Repeat("a", 64)})
	require.NoError(t, err)
	session, err := host.Resolve(binding, actor)
	require.NoError(t, err)
	require.Equal(t, "boolean", session.PricingSchema()["audio"].Type)
	require.NotContains(t, session.PricingSchema(), "duration")
	invalid := binding
	invalid.PhaseApprovalID = "bad-review"
	_, err = host.Resolve(invalid, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	invalid.Profile = ""
	invalid.PhaseApprovalID = strings.Repeat("b", 64)
	_, err = host.Resolve(invalid, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	require.Equal(t, types.TaskPluginExpressionProfile, binding.Profile)
	_, err = p.ModelUsageSchema("alias")
	require.Error(t, err)
	for _, edit := range []func(*Manifest){
		func(m *Manifest) { m.UsageProfiles = m.UsageProfiles[:1] },
		func(m *Manifest) { m.UsageProfiles[1].Models = []string{"vendor-model"} },
		func(m *Manifest) { m.UsageProfiles[1].Models = []string{"unknown"} },
		func(m *Manifest) { m.UsageProfiles[1].Schema = nil },
	} {
		bad := p.Manifest()
		edit(&bad)
		require.Error(t, ValidateManifest(bad))
	}
}

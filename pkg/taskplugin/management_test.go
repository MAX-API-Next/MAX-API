// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagementUsesObservedRevisionAndPreservesOldSessions(t *testing.T) {
	store := &memoryCatalogStore{}
	a, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	b, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	m := validManifest()
	first := testPackage(t, m, "private-source-marker")
	require.NoError(t, a.MutateExpected(t.Context(), 0, ManagementMutation{Action: "install", Package: first}))
	require.ErrorIs(t, b.MutateExpected(t.Context(), 0, ManagementMutation{Action: "install", Package: first}), ErrCatalogConflict)
	rows, rev, err := b.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, rev)
	require.Len(t, rows, 1)
	require.Equal(t, first.Digest(), rows[0].Digest)
	rows[0].Manifest.Models[0] = "changed"
	rows, _, err = b.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, "vendor-model", rows[0].Manifest.Models[0])
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "management-history"}
	identity := PricingContractIdentity{Action: "generate", SchemaVersion: 1, ContractDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	binding, err := a.Host().BindPricingMapped(actor, "POST", "/vendor/v1/jobs", "vendor-model", "vendor-model", identity)
	require.NoError(t, err)
	require.NoError(t, a.MutateExpected(t.Context(), 1, ManagementMutation{Action: "disable", Key: m.Key, Version: m.Version}))
	_, err = a.Host().Resolve(binding, actor)
	require.NoError(t, err)
	_, err = a.Host().BindPricingMapped(actor, "POST", "/vendor/v1/jobs", "vendor-model", "vendor-model", identity)
	require.ErrorIs(t, err, ErrBindingRejected)
	m.Version = "2.0.0"
	second := testPackage(t, m, "new opaque source")
	require.NoError(t, a.MutateExpected(t.Context(), 2, ManagementMutation{Action: "install", Package: second}))
	require.ErrorIs(t, a.MutateExpected(t.Context(), 3, ManagementMutation{Action: "enable", Key: m.Key, Version: "1.0.0"}), ErrCatalogConflict)
	require.NoError(t, a.MutateExpected(t.Context(), 3, ManagementMutation{Action: "activate", Key: m.Key, Version: "1.0.0"}))
	require.NoError(t, a.MutateExpected(t.Context(), 4, ManagementMutation{Action: "revoke", Key: m.Key, Version: "1.0.0"}))
	_, err = a.Host().Resolve(binding, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	require.ErrorIs(t, a.MutateExpected(t.Context(), 5, ManagementMutation{Action: "activate", Key: m.Key, Version: "1.0.0"}), ErrBindingRejected)
	_, rev, err = a.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 5, rev)
}

type unknownManagementStore struct {
	CatalogStore
	lose bool
}

func (s *unknownManagementStore) CompareAndSwap(ctx context.Context, r int64, p []StoredPackage) (int64, error) {
	n, err := s.CatalogStore.CompareAndSwap(ctx, r, p)
	if err == nil && s.lose {
		return 0, errors.New("synthetic lost receipt")
	}
	return n, err
}
func TestManagementLostCommitReceiptRequiresAuthoritativeRead(t *testing.T) {
	store := &unknownManagementStore{CatalogStore: &memoryCatalogStore{}, lose: true}
	c, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	p := testPackage(t, validManifest(), "synthetic source")
	require.ErrorIs(t, c.MutateExpected(t.Context(), 0, ManagementMutation{Action: "install", Package: p}), ErrCatalogUnavailable)
	_, err = c.Host().PricingPackage("POST", "/vendor/v1/jobs", "vendor-model")
	require.ErrorIs(t, err, ErrBindingRejected)
	rows, rev, err := c.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rev)
	require.ErrorIs(t, c.MutateExpected(t.Context(), 0, ManagementMutation{Action: "install", Package: p}), ErrCatalogConflict)
}

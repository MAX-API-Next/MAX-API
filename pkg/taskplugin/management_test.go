// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
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
	require.ErrorIs(t, a.MutateExpected(t.Context(), 3, ManagementMutation{Action: "enable", Key: m.Key, Version: "1.0.0"}), ErrCatalogInvalid)
	require.NoError(t, a.MutateExpected(t.Context(), 3, ManagementMutation{Action: "activate", Key: m.Key, Version: "1.0.0"}))
	require.ErrorIs(t, a.MutateExpected(t.Context(), 4, ManagementMutation{Action: "revoke", Key: m.Key, Version: "1.0.0"}), ErrCatalogInvalid, "management no longer exposes permanent revocation")
	_, rev, err = a.ManagementSnapshot(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 4, rev, "rejected legacy action must not commit")
	// Runtime compatibility still rejects versions revoked by an older release.
	require.NoError(t, a.Revoke(t.Context(), m.Key, "1.0.0"))
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

type installValidationStore struct {
	memoryCatalogStore
	writes int
}

func (store *installValidationStore) CompareAndSwap(ctx context.Context, revision int64, packages []StoredPackage) (int64, error) {
	store.writes++
	return store.memoryCatalogStore.CompareAndSwap(ctx, revision, packages)
}

func TestManagementInstallPreflightIsReadOnlyAndDetectsHistoricalRestrictions(t *testing.T) {
	pkg := testPackage(t, validManifest(), "source")
	changed := testPackage(t, validManifest(), "different-source")
	m := pkg.Manifest()
	for _, tc := range []struct {
		name     string
		packages []StoredPackage
		receipts []PackageDeletion
		want     error
	}{
		{name: "new"},
		{name: "deleted identical", receipts: []PackageDeletion{{Key: m.Key, Version: m.Version, Digest: pkg.Digest()}}},
		{name: "deleted revoked", receipts: []PackageDeletion{{Key: m.Key, Version: m.Version, Digest: pkg.Digest(), Revoked: true}}, want: ErrCatalogRevoked},
		{name: "deleted changed", receipts: []PackageDeletion{{Key: m.Key, Version: m.Version, Digest: changed.Digest()}}, want: ErrCatalogContentChanged},
		{name: "installed revoked", packages: []StoredPackage{{Document: pkg.Document(), Digest: pkg.Digest(), Active: true, Revoked: true}}, want: ErrCatalogRevoked},
		{name: "installed changed", packages: []StoredPackage{{Document: changed.Document(), Digest: changed.Digest(), Active: true, Enabled: true}}, want: ErrCatalogContentChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &installValidationStore{memoryCatalogStore: memoryCatalogStore{snapshot: CatalogSnapshot{Revision: 1, Packages: tc.packages, Deletions: tc.receipts}}}
			catalog, err := OpenCatalog(t.Context(), store)
			require.NoError(t, err)
			before := cloneCatalogSnapshot(store.snapshot)
			err = catalog.ValidateInstall(t.Context(), pkg)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			require.Equal(t, before, store.snapshot, "preflight never changes persisted state")
			require.Equal(t, before, catalog.snapshot, "preflight never publishes a tentative active version")
			require.Zero(t, store.writes)
		})
	}
	store := &installValidationStore{}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	require.NoError(t, catalog.ValidateInstall(t.Context(), pkg))
	require.NoError(t, catalog.Install(t.Context(), pkg))
	require.ErrorIs(t, catalog.MutateExpected(t.Context(), 0, ManagementMutation{Action: "install", Package: pkg}), ErrCatalogConflict, "a successful preflight does not bypass write CAS")
	require.Equal(t, 1, store.writes)
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

type conflictingDeletionStore struct {
	memoryCatalogStore
	deleteError error
}

func (s *conflictingDeletionStore) DeleteVersion(context.Context, int64, string, string) (int64, error) {
	return 0, s.deleteError
}

func TestManagementDeletionConflictSuspendsStaleHost(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprint(wrapped), func(t *testing.T) {
			store := &conflictingDeletionStore{deleteError: ErrCatalogConflict}
			if wrapped {
				store.deleteError = fmt.Errorf("store conflict: %w", ErrCatalogConflict)
			}
			catalog, err := OpenCatalog(t.Context(), store)
			require.NoError(t, err)
			pkg := testPackage(t, validManifest(), "source")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "stale-delete"}
			binding, err := catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
			require.NoError(t, err)
			err = catalog.MutateExpected(t.Context(), 1, ManagementMutation{Action: "delete", Key: pkg.Manifest().Key, Version: pkg.Manifest().Version})
			require.ErrorIs(t, err, ErrCatalogConflict)
			_, err = catalog.Host().Resolve(binding, actor)
			require.ErrorIs(t, err, ErrBindingRejected, "a conflicting remote generation must not remain usable")
		})
	}
}

func TestManagementExpiredPreflightDoesNotUnpublishHost(t *testing.T) {
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg := testPackage(t, validManifest(), "source")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	published, calls := catalog.host.state.Load(), store.loadCalls
	ctx, cancel := context.WithCancel(t.Context())
	catalog.mu.Lock()
	started, done := make(chan struct{}), make(chan error, 1)
	go func() { close(started); done <- catalog.ValidateInstall(ctx, pkg) }()
	<-started
	cancel()
	catalog.mu.Unlock()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Same(t, published, catalog.host.state.Load())
	require.Equal(t, calls, store.loadCalls, "expired preflight never reaches storage")
	_, _, err = catalog.ManagementSnapshot(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Same(t, published, catalog.host.state.Load(), "an expired administrative read also leaves the healthy publication intact")
	require.Equal(t, calls, store.loadCalls)
}

type confirmedDeletionStore struct {
	memoryCatalogStore
	afterCommit func()
}

func (s *confirmedDeletionStore) DeleteVersion(_ context.Context, expected int64, key, version string) (int64, error) {
	s.Lock()
	defer s.Unlock()
	if s.snapshot.Revision != expected || len(s.snapshot.Packages) != 1 {
		return 0, ErrCatalogConflict
	}
	row := s.snapshot.Packages[0]
	s.snapshot = CatalogSnapshot{Revision: expected + 1, Deletions: []PackageDeletion{{Key: key, Version: version, Digest: row.Digest}}}
	s.failLoad = true
	if s.afterCommit != nil {
		s.afterCommit()
	}
	return expected + 1, nil
}

func TestManagementConfirmedDeletionSurvivesRefreshFailure(t *testing.T) {
	for _, expires := range []bool{false, true} {
		t.Run(fmt.Sprint(expires), func(t *testing.T) {
			store := &confirmedDeletionStore{}
			catalog, err := OpenCatalog(t.Context(), store)
			require.NoError(t, err)
			pkg := testPackage(t, validManifest(), "source")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if expires {
				store.afterCommit = cancel
			}
			require.NoError(t, catalog.MutateExpected(ctx, 1, ManagementMutation{Action: "delete", Key: pkg.Manifest().Key, Version: pkg.Manifest().Version}), "store-confirmed commit must still produce a successful receipt")
			require.Nil(t, catalog.host.state.Load(), "failed local publication remains suspended")
			store.failLoad = false
			entries, revision, err := catalog.ManagementSnapshot(t.Context())
			require.NoError(t, err)
			require.Empty(t, entries)
			require.EqualValues(t, 2, revision)
			require.NotNil(t, catalog.host.state.Load(), "fresh authoritative read recovers the Host")
		})
	}
}

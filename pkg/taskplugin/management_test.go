// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MoeForge/moejs"
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

type acquisitionSignalContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *acquisitionSignalContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

func TestManagementExpiredPreflightDoesNotUnpublishHost(t *testing.T) {
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg := testPackage(t, validManifest(), "source")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	published, calls := catalog.host.state.Load(), store.loadCalls
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &acquisitionSignalContext{Context: base, entered: make(chan struct{})}
	catalog.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- catalog.ValidateInstall(ctx, pkg) }()
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		catalog.mu.Unlock()
		<-done
		t.Fatal("preflight never reached context-aware acquisition")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		catalog.mu.Unlock()
		<-done
		t.Fatal("canceled preflight must return while the catalog lock is still held")
	}
	require.Same(t, published, catalog.host.state.Load())
	require.Equal(t, calls, store.loadCalls, "expired preflight never reaches storage")
	catalog.mu.Unlock()
	_, _, err = catalog.ManagementSnapshot(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Same(t, published, catalog.host.state.Load(), "an expired administrative read also leaves the healthy publication intact")
	require.Equal(t, calls, store.loadCalls)
	require.ErrorIs(t, catalog.MutateExpected(ctx, 1, ManagementMutation{Action: "delete", Key: pkg.Manifest().Key, Version: pkg.Manifest().Version}), context.Canceled)
	require.Same(t, published, catalog.host.state.Load())
	require.Equal(t, calls, store.loadCalls)
}

func TestCatalogOperationsCancelWhileWaitingWithoutStorage(t *testing.T) {
	for _, operation := range []string{"delete", "refresh", "list", "read", "install"} {
		t.Run(operation, func(t *testing.T) {
			store := &memoryCatalogStore{}
			catalog, err := OpenCatalog(t.Context(), store)
			require.NoError(t, err)
			pkg := testPackage(t, validManifest(), "source")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			published, calls := catalog.host.state.Load(), store.loadCalls
			base, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := &acquisitionSignalContext{Context: base, entered: make(chan struct{})}
			catalog.mu.Lock()
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "delete":
					err = catalog.MutateExpected(ctx, 1, ManagementMutation{Action: "delete", Key: pkg.Manifest().Key, Version: pkg.Manifest().Version})
				case "refresh":
					err = catalog.Refresh(ctx)
				case "list":
					_, _, err = catalog.List(ctx)
				case "read":
					_, _, err = catalog.ManagementSnapshot(ctx)
				case "install":
					err = catalog.Install(ctx, pkg)
				}
				done <- err
			}()
			select {
			case <-ctx.entered:
			case <-time.After(time.Second):
				catalog.mu.Unlock()
				<-done
				t.Fatal("operation never reached context-aware acquisition")
			}
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				catalog.mu.Unlock()
				<-done
				t.Fatal("canceled operation waited for the held mutex")
			}
			require.Same(t, published, catalog.host.state.Load())
			require.Equal(t, calls, store.loadCalls)
			catalog.mu.Unlock()
		})
	}
}

func TestManagementConfirmedDeletionSuspendsBeforePendingRefresh(t *testing.T) {
	store := &confirmedDeletionStore{}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg := testPackage(t, validManifest(), "source")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	defer close(release)
	store.afterCommit = func() { store.onLoad = func(int) { close(entered); <-release } }
	go func() {
		done <- catalog.MutateExpected(t.Context(), 1, ManagementMutation{Action: "delete", Key: pkg.Manifest().Key, Version: pkg.Manifest().Version})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("follow-up refresh did not begin")
	}
	require.Nil(t, catalog.host.state.Load(), "the committed old generation must be unavailable while refresh is pending")
	// Release without closing twice, and wait for the goroutine before cleanup.
	release <- struct{}{}
	require.NoError(t, <-done)
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

func TestManagementKnownMappingPreconditionPreservesHost(t *testing.T) {
	store := &conflictingDeletionStore{deleteError: &CatalogChannelMappingError{ChannelID: 23}}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	pkg := testPackage(t, validManifest(), "source")
	require.NoError(t, catalog.Install(t.Context(), pkg))
	published := catalog.host.state.Load()
	err = catalog.MutateExpected(t.Context(), 1, ManagementMutation{Action: "delete", Key: pkg.Manifest().Key, Version: pkg.Manifest().Version})
	var mapping *CatalogChannelMappingError
	require.ErrorAs(t, err, &mapping)
	require.Equal(t, 23, mapping.ChannelID)
	require.NotNil(t, catalog.host.state.Load())
	require.Equal(t, published.generation, catalog.host.state.Load().generation)
	require.Equal(t, pkg.Digest(), catalog.host.state.Load().active[pkg.Manifest().Key].Digest())
}

func TestManagementRetainedRevocationPrecedesReceiptContentConflict(t *testing.T) {
	pkg := testPackage(t, validManifest(), "source")
	changed := testPackage(t, validManifest(), "changed")
	m := pkg.Manifest()
	store := &memoryCatalogStore{snapshot: CatalogSnapshot{Revision: 2, Packages: []StoredPackage{{Document: pkg.Document(), Digest: pkg.Digest(), Active: true, Revoked: true}}, Deletions: []PackageDeletion{{Key: m.Key, Version: m.Version, Digest: pkg.Digest()}}}}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	require.ErrorIs(t, catalog.ValidateInstall(t.Context(), changed), ErrCatalogRevoked)
}

func TestManagementPreflightDeadlineReachesRetainedMetadata(t *testing.T) {
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(t.Context(), store)
	require.NoError(t, err)
	manifest := Manifest{APIVersion: 1, Key: "custom-upload", Name: "Custom", Version: "1.0.0", Models: []string{"custom-model"}}
	document, err := common.Marshal(PackageDocument{FormatVersion: 3, Manifest: manifest, Source: `for(;;){};` + validUploadSource})
	require.NoError(t, err)
	program, err := moejs.Compile("retained-preflight.js", `for(;;){};`+validUploadSource)
	require.NoError(t, err)
	require.Contains(t, program.Exports(), "meta")
	store.snapshot = CatalogSnapshot{Revision: 1, Packages: []StoredPackage{{Document: document, Digest: fmt.Sprintf("%x", sha256.Sum256(document)), Active: true}}}
	candidate := testPackage(t, validManifest(), "candidate-source")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = catalog.ValidateInstall(ctx, candidate)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.ErrorIs(t, err, ErrCatalogUnavailable)
	require.Less(t, time.Since(started), 2*time.Second, "metadata must stop at the request deadline, not the independent five-second call limit")
}

func TestManagementPreflightReadCancellationPreservesOnlyUnchangedHost(t *testing.T) {
	for _, kind := range []string{"read-cancel", "changed-generation", "invalid-generation", "conflict-then-cancel"} {
		t.Run(kind, func(t *testing.T) {
			store := &memoryCatalogStore{}
			catalog, err := OpenCatalog(t.Context(), store)
			require.NoError(t, err)
			pkg := testPackage(t, validManifest(), "source")
			require.NoError(t, catalog.Install(t.Context(), pkg))
			published := catalog.host.state.Load()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "changed-generation":
				store.snapshot.Revision++
			case "invalid-generation":
				store.loadErrors = []error{ErrCatalogInvalid}
			case "conflict-then-cancel":
				store.loadErrors = []error{ErrCatalogConflict, context.Canceled}
			}
			store.onLoad = func(int) {
				if kind != "invalid-generation" && !(kind == "conflict-then-cancel" && len(store.loadErrors) == 2) {
					cancel()
					if kind == "read-cancel" {
						store.loadErrors = []error{ctx.Err()}
					}
				}
			}
			err = catalog.ValidateInstall(ctx, testPackage(t, validManifest(), "candidate"))
			require.Error(t, err)
			if kind == "read-cancel" {
				require.Same(t, published, catalog.host.state.Load())
				_, err = catalog.Host().PricingPackage("POST", "/vendor/v1/jobs", "vendor-model")
				require.NoError(t, err)
			} else {
				require.Nil(t, catalog.host.state.Load(), "authoritative invalid/change/conflict stays fail-closed")
			}
		})
	}
}

func TestCatalogVersionChecksHonorCancellation(t *testing.T) {
	pkg := testPackage(t, validManifest(), "source")
	for _, action := range []string{"activate", "enable", "revoke"} {
		t.Run(action, func(t *testing.T) {
			snapshot := CatalogSnapshot{Revision: 1, Packages: []StoredPackage{{Document: pkg.Document(), Digest: pkg.Digest(), Active: true, Enabled: true}}}
			before := cloneCatalogSnapshot(snapshot)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var err error
			switch action {
			case "activate":
				err = activateCatalogVersion(ctx, &snapshot, pkg.Manifest().Key, pkg.Manifest().Version)
			case "enable":
				err = setCatalogEnabled(ctx, &snapshot, pkg.Manifest().Key, false)
			case "revoke":
				err = revokeCatalogVersion(ctx, &snapshot, pkg.Manifest().Key, pkg.Manifest().Version)
			}
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, before, snapshot)
		})
	}
}

func TestPreflightSnapshotOrderDoesNotSuspendUnchangedHost(t *testing.T) {
	for _, change := range []string{"reordered", "changed-flags", "changed-bytes", "changed-receipt"} {
		t.Run(change, func(t *testing.T) {
			a := testPackage(t, validManifest(), "package-a")
			manifest := validManifest()
			manifest.Key = "vendor-b"
			manifest.Routes[0].Path = "/vendor-b/v1/jobs"
			manifest.Routes[1].Path = "/vendor-b/v1/jobs/:task_id"
			b := testPackage(t, manifest, "package-b")
			store := &memoryCatalogStore{snapshot: CatalogSnapshot{Revision: 3,
				Packages:  []StoredPackage{{Document: a.Document(), Digest: a.Digest(), Active: true, Enabled: true}, {Document: b.Document(), Digest: b.Digest(), Active: true, Enabled: true}},
				Deletions: []PackageDeletion{{Key: "removed-a", Version: "1.0.0", Digest: fmt.Sprintf("%064d", 1)}, {Key: "removed-b", Version: "1.0.0", Digest: fmt.Sprintf("%064d", 2)}},
			}}
			catalog, err := OpenCatalog(t.Context(), store)
			require.NoError(t, err)
			published := catalog.host.state.Load()
			store.snapshot.Packages[0], store.snapshot.Packages[1] = store.snapshot.Packages[1], store.snapshot.Packages[0]
			store.snapshot.Deletions[0], store.snapshot.Deletions[1] = store.snapshot.Deletions[1], store.snapshot.Deletions[0]
			switch change {
			case "changed-flags":
				store.snapshot.Packages[0].Enabled = false
			case "changed-bytes":
				store.snapshot.Packages[0].Document = append(append([]byte(nil), store.snapshot.Packages[0].Document...), ' ')
			case "changed-receipt":
				store.snapshot.Deletions[0].Revoked = true
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store.onLoad = func(int) { cancel() }
			err = catalog.ValidateInstall(ctx, a)
			require.ErrorIs(t, err, context.Canceled)
			if change == "reordered" {
				require.Same(t, published, catalog.host.state.Load())
				_, err = catalog.Host().PricingPackage("POST", "/vendor/v1/jobs", "vendor-model")
				require.NoError(t, err)
			} else {
				require.Nil(t, catalog.host.state.Load(), "content changes remain authoritative even at the same revision")
			}
		})
	}
}

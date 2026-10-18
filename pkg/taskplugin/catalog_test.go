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
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/stretchr/testify/require"
)

type memoryCatalogStore struct {
	sync.Mutex
	snapshot             CatalogSnapshot
	failLoad, failCommit bool
	commitUnknown        bool
	loadErrors           []error
	loadCalls            int
	onLoad               func(int)
}

func (s *memoryCatalogStore) Load(context.Context) (CatalogSnapshot, error) {
	s.Lock()
	defer s.Unlock()
	s.loadCalls++
	if s.onLoad != nil {
		s.onLoad(s.loadCalls)
	}
	if len(s.loadErrors) > 0 {
		err := s.loadErrors[0]
		s.loadErrors = s.loadErrors[1:]
		if err != nil {
			return CatalogSnapshot{}, err
		}
	}
	if s.failLoad {
		return CatalogSnapshot{}, errors.New("synthetic load failure")
	}
	return cloneCatalogSnapshot(s.snapshot), nil
}

func (s *memoryCatalogStore) CompareAndSwap(_ context.Context, revision int64, packages []StoredPackage) (int64, error) {
	s.Lock()
	defer s.Unlock()
	if s.failCommit {
		return 0, errors.New("synthetic commit failure")
	}
	if s.snapshot.Revision != revision {
		return 0, ErrCatalogConflict
	}
	s.snapshot = cloneCatalogSnapshot(CatalogSnapshot{Revision: revision + 1, Packages: packages})
	if s.commitUnknown {
		return 0, errors.New("synthetic lost commit acknowledgment")
	}
	return s.snapshot.Revision, nil
}

func TestCatalogRestartRetainsVersionsDisablingAndRevocation(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	first := testPackage(t, validManifest(), "first")
	require.NoError(t, catalog.Install(ctx, first))
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "catalog-task"}
	binding, err := catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	manifest := validManifest()
	manifest.Version = "2.0.0"
	require.NoError(t, catalog.Install(ctx, testPackage(t, manifest, "second")))
	require.NoError(t, catalog.SetEnabled(ctx, "vendor", false))
	restarted, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	_, err = restarted.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, ErrBindingRejected)
	session, err := restarted.Host().Resolve(binding, actor)
	require.NoError(t, err)
	require.Equal(t, first.Digest(), session.Binding().PackageDigest)
	require.NoError(t, restarted.Revoke(ctx, "vendor", "1.0.0"))
	_, err = session.UsageEnvelope(nil)
	require.ErrorIs(t, err, ErrBindingRejected)
	third, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	_, err = third.Host().Resolve(binding, actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	require.Error(t, third.Activate(ctx, "vendor", "1.0.0"))
	require.NoError(t, third.Activate(ctx, "vendor", "2.0.0"))
	_, err = third.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
}

func TestCatalogValidatesBeforeCommitAndFailsClosedOnStorageFailure(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	first := testPackage(t, validManifest(), "original")
	require.NoError(t, catalog.Install(ctx, first))
	before := store.snapshot.Revision
	require.Error(t, catalog.Install(ctx, testPackage(t, registryManifest("conflict", "/vendor/v1/jobs/:id"), "conflict")))
	require.Equal(t, before, store.snapshot.Revision)
	require.Error(t, catalog.Install(ctx, testPackage(t, validManifest(), "changed")))
	require.Equal(t, before, store.snapshot.Revision)
	_, err = catalog.Host().Install(first)
	require.Error(t, err, "managed host must not bypass persistence")
	require.Error(t, catalog.Host().Revoke("vendor", "1.0.0"))
	store.failCommit = true
	require.Error(t, catalog.SetEnabled(ctx, "vendor", false))
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "closed-on-error"}
	_, err = catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.ErrorIs(t, err, ErrBindingRejected)
	store.failCommit = false
	require.NoError(t, catalog.Refresh(ctx))
	_, err = catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	store.failLoad = true
	require.Error(t, catalog.Refresh(ctx))
	store.failLoad = false
	store.snapshot.Packages[0].Document[0] ^= 1
	_, err = OpenCatalog(ctx, store)
	require.Error(t, err, "corrupt package must prevent startup")
}

func TestCatalogRejectsHistoryDeletionRevocationRollbackAndRevisionRollback(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(ctx, testPackage(t, validManifest(), "original")))
	require.NoError(t, catalog.Revoke(ctx, "vendor", "1.0.0"))
	good := cloneCatalogSnapshot(store.snapshot)
	store.snapshot.Packages[0].Revoked = false
	store.snapshot.Revision++
	require.Error(t, catalog.Refresh(ctx))
	store.snapshot = cloneCatalogSnapshot(good)
	store.snapshot.Packages = nil
	store.snapshot.Revision++
	require.Error(t, catalog.Refresh(ctx))
	store.snapshot = cloneCatalogSnapshot(good)
	store.snapshot.Revision--
	require.Error(t, catalog.Refresh(ctx))
	store.snapshot = good
	require.NoError(t, catalog.Refresh(ctx))
}

func TestCatalogUnknownCommitRecoversOnlyPersistedOutcome(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(ctx, testPackage(t, validManifest(), "original")))
	store.commitUnknown = true
	require.ErrorIs(t, catalog.Revoke(ctx, "vendor", "1.0.0"), ErrCatalogUnavailable)
	require.Nil(t, catalog.Host().state.Load(), "unknown outcome must suspend the managed host")
	store.commitUnknown = false
	require.NoError(t, catalog.Refresh(ctx))
	info, revision, err := catalog.List(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, revision)
	require.True(t, info[0].Revoked)
	require.Error(t, catalog.SetEnabled(ctx, "vendor", true))
}

func TestCatalogConcurrentReadersTolerateSuspensionAndRefresh(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(ctx, testPackage(t, validManifest(), "original")))
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "reader"}
	binding, err := catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 2000 {
				_, _ = catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
				_, _ = catalog.Host().Resolve(binding, actor)
			}
		}()
	}
	for range 20 {
		catalog.host.state.Store(nil)
		require.NoError(t, catalog.Refresh(ctx))
	}
	group.Wait()
}

func TestCatalogRefreshAppliesAnotherNodesRevocationToExistingSession(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	first, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	require.NoError(t, first.Install(ctx, testPackage(t, validManifest(), "original")))
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "two-nodes"}
	binding, err := first.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	session, err := first.Host().Resolve(binding, actor)
	require.NoError(t, err)
	second, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	require.NoError(t, second.Revoke(ctx, "vendor", "1.0.0"))
	require.NoError(t, first.Refresh(ctx))
	_, err = session.UsageEnvelope(nil)
	require.ErrorIs(t, err, ErrBindingRejected)
}

func TestCatalogReadConflictRetriesRestoreAuthoritativeRevocation(t *testing.T) {
	ctx := context.Background()
	store := &memoryCatalogStore{}
	catalog, err := OpenCatalog(ctx, store)
	require.NoError(t, err)
	require.NoError(t, catalog.Install(ctx, testPackage(t, validManifest(), "original")))
	actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "read-conflict"}
	binding, err := catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	session, err := catalog.Host().Resolve(binding, actor)
	require.NoError(t, err)
	store.snapshot.Revision++
	store.snapshot.Packages[0].Revoked = true
	store.loadCalls = 0
	store.loadErrors = []error{ErrCatalogConflict, fmt.Errorf("mixed snapshot: %w", ErrCatalogConflict)}
	store.onLoad = func(call int) {
		if call > 1 {
			require.Nil(t, catalog.Host().state.Load(), "a conflict must suspend stale admission during retries")
		}
	}
	require.NoError(t, catalog.Refresh(ctx))
	require.Equal(t, 3, store.loadCalls)
	_, err = session.UsageEnvelope(nil)
	require.ErrorIs(t, err, ErrBindingRejected, "only the authoritative revoked state may be published")
}

func TestCatalogReadFailuresAreBoundedAndSuspendExistingSessions(t *testing.T) {
	for _, test := range []struct {
		name   string
		errors []error
		calls  int
		cancel bool
	}{
		{name: "persistent conflict", errors: []error{ErrCatalogConflict, ErrCatalogConflict, ErrCatalogConflict, nil}, calls: 3},
		{name: "storage failure", errors: []error{errors.New("storage unavailable"), nil}, calls: 1},
		{name: "cancelled conflict", errors: []error{ErrCatalogConflict, nil}, calls: 1, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &memoryCatalogStore{}
			catalog, err := OpenCatalog(ctx, store)
			require.NoError(t, err)
			require.NoError(t, catalog.Install(ctx, testPackage(t, validManifest(), "original")))
			actor := Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "suspended-session"}
			binding, err := catalog.Host().Bind(actor, "POST", "/vendor/v1/jobs", "vendor-model", taskusage.MiniMaxH3Contract())
			require.NoError(t, err)
			session, err := catalog.Host().Resolve(binding, actor)
			require.NoError(t, err)
			store.loadCalls = 0
			store.loadErrors = test.errors
			if test.cancel {
				store.onLoad = func(int) { cancel() }
			}
			require.ErrorIs(t, catalog.Refresh(ctx), ErrCatalogUnavailable)
			require.Equal(t, test.calls, store.loadCalls)
			require.Nil(t, catalog.Host().state.Load())
			_, err = session.UsageEnvelope(nil)
			require.ErrorIs(t, err, ErrBindingRejected)
			_, err = session.NewFetchScope(context.Background(), "https://example.com", "")
			require.ErrorIs(t, err, ErrBindingRejected)
		})
	}
}

func TestCatalogReadRetriesApplyToListAndMutation(t *testing.T) {
	for _, operation := range []string{"list", "mutation"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			store := &memoryCatalogStore{}
			catalog, err := OpenCatalog(ctx, store)
			require.NoError(t, err)
			require.NoError(t, catalog.Install(ctx, testPackage(t, validManifest(), "original")))
			before := store.snapshot.Revision
			store.loadCalls = 0
			store.loadErrors = []error{ErrCatalogConflict}
			if operation == "list" {
				_, _, err = catalog.List(ctx)
				require.Equal(t, before, store.snapshot.Revision)
			} else {
				err = catalog.SetEnabled(ctx, "vendor", false)
				require.Equal(t, before+1, store.snapshot.Revision, "retrying a read must not repeat a write")
			}
			require.NoError(t, err)
			require.Equal(t, 2, store.loadCalls)
		})
	}
}

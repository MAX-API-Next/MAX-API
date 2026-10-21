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
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

var (
	ErrCatalogConflict    = errors.New("task plugin catalog changed; refresh before retrying")
	ErrCatalogInvalid     = errors.New("task plugin catalog is invalid")
	ErrCatalogUnavailable = errors.New("task plugin catalog storage is unavailable")
)

// StoredPackage contains host-private pinned bytes. Stores must preserve every
// historical identity, reject changed bytes and make revocation irreversible.
type StoredPackage struct {
	Document                 []byte
	Digest                   string
	Active, Enabled, Revoked bool
}

type CatalogSnapshot struct {
	Revision int64
	Packages []StoredPackage
}

// CatalogStore commits a complete version set and the revision in one
// transaction. A failed CAS must not leave any changed rows behind.
type CatalogStore interface {
	Load(context.Context) (CatalogSnapshot, error)
	CompareAndSwap(context.Context, int64, []StoredPackage) (int64, error)
}

type PackageInfo struct {
	Key, Version, Digest     string
	Active, Enabled, Revoked bool
}

// Catalog coordinates durable metadata with a single published Host. Disable
// affects admission only; revoke also invalidates already-issued sessions.
// Refresh must be called explicitly for changes committed by another node.
type Catalog struct {
	mu       sync.Mutex
	store    CatalogStore
	host     *Host
	snapshot CatalogSnapshot
	state    *hostState
}

func OpenCatalog(ctx context.Context, store CatalogStore) (*Catalog, error) {
	if store == nil || ctx == nil {
		return nil, ErrCatalogUnavailable
	}
	catalog := &Catalog{store: store, host: &Host{managed: true}}
	if err := catalog.Refresh(ctx); err != nil {
		return nil, err
	}
	return catalog, nil
}

// Host supports binding/restoration, but rejects direct Install/Retain/Revoke calls.
func (catalog *Catalog) Host() *Host { return catalog.host }

func (catalog *Catalog) Refresh(ctx context.Context) error {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	return catalog.refreshLocked(ctx)
}

func (catalog *Catalog) refreshLocked(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		catalog.host.state.Store(nil)
		return ErrCatalogUnavailable
	}
	var snapshot CatalogSnapshot
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err = catalog.store.Load(ctx)
		if !errors.Is(err, ErrCatalogConflict) {
			break
		}
		// A concurrent commit may revoke a version. Suspend stale sessions
		// while retrying reads; writes and unknown outcomes are never retried.
		catalog.host.state.Store(nil)
		if ctx.Err() != nil {
			break
		}
	}
	if err != nil || ctx.Err() != nil {
		catalog.host.state.Store(nil)
		if ctx.Err() == nil && errors.Is(err, ErrCatalogInvalid) {
			return ErrCatalogInvalid
		}
		return ErrCatalogUnavailable
	}
	state, err := buildCatalogState(snapshot, catalog.snapshot, catalog.state != nil)
	if err != nil {
		catalog.host.state.Store(nil)
		return err
	}
	catalog.publish(snapshot, state)
	return nil
}

func (catalog *Catalog) publish(snapshot CatalogSnapshot, state *hostState) {
	catalog.snapshot = cloneCatalogSnapshot(snapshot)
	catalog.state = state
	catalog.host.state.Store(state)
}

// ValidateCatalogTransition is shared with durable stores so even a direct
// store call cannot rewrite history, clear revocations or publish bad routes.
func ValidateCatalogTransition(previous, next CatalogSnapshot) error {
	_, err := buildCatalogState(next, previous, true)
	return err
}

func buildCatalogState(snapshot, previous CatalogSnapshot, checkPrevious bool) (*hostState, error) {
	if snapshot.Revision < 0 || snapshot.Revision == 0 && len(snapshot.Packages) != 0 {
		return nil, ErrCatalogInvalid
	}
	state := &hostState{generation: uint64(snapshot.Revision), active: map[string]*Package{}, history: map[string]*Package{}, revoked: map[string]bool{}}
	activeKeys := map[string]bool{}
	total := 0
	for _, entry := range snapshot.Packages {
		if len(entry.Document) > MaxPackageBytes || total > MaxRetainedPackageBytes-len(entry.Document) {
			return nil, ErrCatalogInvalid
		}
		total += len(entry.Document)
		p, err := LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
		if err != nil || entry.Digest != p.digest {
			return nil, ErrCatalogInvalid
		}
		id := packageIdentity(p.manifest.Key, p.manifest.Version)
		if state.history[id] != nil {
			return nil, ErrCatalogInvalid
		}
		state.history[id] = p
		state.revoked[id] = entry.Revoked
		if entry.Active {
			if activeKeys[p.manifest.Key] {
				return nil, ErrCatalogInvalid
			}
			activeKeys[p.manifest.Key] = true
			if entry.Enabled && !entry.Revoked {
				state.active[p.manifest.Key] = p
			}
		}
	}
	registry := NewRegistry()
	keys := make([]string, 0, len(state.active))
	for key := range state.active {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := registry.Publish(state.active[key].manifest); err != nil {
			return nil, ErrCatalogInvalid
		}
	}
	state.routes = registry.Snapshot()
	if checkPrevious {
		if snapshot.Revision < previous.Revision || snapshot.Revision == previous.Revision && catalogFingerprint(snapshot) != catalogFingerprint(previous) {
			return nil, ErrCatalogInvalid
		}
		for _, old := range previous.Packages {
			p, err := LoadPackage(bytes.NewReader(old.Document), old.Digest)
			if err != nil {
				return nil, ErrCatalogInvalid
			}
			id := packageIdentity(p.manifest.Key, p.manifest.Version)
			if current := state.history[id]; current == nil || current.digest != old.Digest || old.Revoked && !state.revoked[id] {
				return nil, ErrCatalogInvalid
			}
		}
	}
	return state, nil
}

func catalogFingerprint(snapshot CatalogSnapshot) string {
	entries := make([]string, 0, len(snapshot.Packages))
	for _, entry := range snapshot.Packages {
		entries = append(entries, fmt.Sprintf("%s:%t:%t:%t", entry.Digest, entry.Active, entry.Enabled, entry.Revoked))
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

func cloneCatalogSnapshot(snapshot CatalogSnapshot) CatalogSnapshot {
	snapshot.Packages = append([]StoredPackage(nil), snapshot.Packages...)
	for i := range snapshot.Packages {
		snapshot.Packages[i].Document = append([]byte(nil), snapshot.Packages[i].Document...)
	}
	return snapshot
}

func (catalog *Catalog) update(ctx context.Context, change func(*CatalogSnapshot) error) error {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.refreshLocked(ctx); err != nil {
		return err
	}
	if catalog.snapshot.Revision == math.MaxInt64 {
		return ErrCatalogInvalid
	}
	next := cloneCatalogSnapshot(catalog.snapshot)
	if err := change(&next); err != nil {
		return err
	}
	next.Revision++
	state, err := buildCatalogState(next, catalog.snapshot, true)
	if err != nil {
		return err
	}
	revision, err := catalog.store.CompareAndSwap(ctx, catalog.snapshot.Revision, next.Packages)
	if err != nil || revision != next.Revision {
		// A DB error may have happened after commit. Do not publish a guessed
		// outcome; suspend until Refresh reads the authoritative persisted set.
		catalog.host.state.Store(nil)
		if errors.Is(err, ErrCatalogConflict) {
			return ErrCatalogConflict
		}
		return ErrCatalogUnavailable
	}
	catalog.publish(next, state)
	return nil
}

func (catalog *Catalog) Install(ctx context.Context, packages ...*Package) error {
	if len(packages) == 0 {
		return ErrCatalogInvalid
	}
	return catalog.update(ctx, func(next *CatalogSnapshot) error {
		seen := map[string]bool{}
		for _, p := range packages {
			if p == nil || seen[p.manifest.Key] {
				return ErrCatalogInvalid
			}
			seen[p.manifest.Key] = true
			index := -1
			for i := range next.Packages {
				old, err := LoadPackage(bytes.NewReader(next.Packages[i].Document), next.Packages[i].Digest)
				if err != nil {
					return ErrCatalogInvalid
				}
				if old.manifest.Key != p.manifest.Key {
					continue
				}
				if old.manifest.Version == p.manifest.Version {
					if old.digest != p.digest || next.Packages[i].Revoked {
						return ErrCatalogInvalid
					}
					index = i
				}
				next.Packages[i].Active = false
			}
			if index < 0 {
				next.Packages = append(next.Packages, StoredPackage{Document: p.Document(), Digest: p.Digest()})
				index = len(next.Packages) - 1
			}
			next.Packages[index].Active, next.Packages[index].Enabled = true, true
		}
		return nil
	})
}

func catalogVersion(snapshot *CatalogSnapshot, key, version string, activeOnly bool) (int, error) {
	for i, entry := range snapshot.Packages {
		p, err := LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
		if err != nil {
			return -1, ErrCatalogInvalid
		}
		if p.manifest.Key == key && (activeOnly && entry.Active || !activeOnly && p.manifest.Version == version) {
			return i, nil
		}
	}
	return -1, ErrBindingRejected
}

func (catalog *Catalog) Activate(ctx context.Context, key, version string) error {
	return catalog.update(ctx, func(next *CatalogSnapshot) error {
		index, err := catalogVersion(next, key, version, false)
		if err != nil {
			return err
		}
		if next.Packages[index].Revoked {
			return ErrBindingRejected
		}
		for i, entry := range next.Packages {
			p, _ := LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
			if p.manifest.Key == key {
				next.Packages[i].Active = i == index
			}
		}
		next.Packages[index].Enabled = true
		return nil
	})
}

func (catalog *Catalog) SetEnabled(ctx context.Context, key string, enabled bool) error {
	return catalog.update(ctx, func(next *CatalogSnapshot) error {
		index, err := catalogVersion(next, key, "", true)
		if err != nil {
			return err
		}
		if enabled && next.Packages[index].Revoked {
			return ErrBindingRejected
		}
		next.Packages[index].Enabled = enabled
		return nil
	})
}

func (catalog *Catalog) Revoke(ctx context.Context, key, version string) error {
	return catalog.update(ctx, func(next *CatalogSnapshot) error {
		index, err := catalogVersion(next, key, version, false)
		if err != nil {
			return err
		}
		next.Packages[index].Revoked, next.Packages[index].Enabled = true, false
		return nil
	})
}

// List refreshes durable state and returns metadata without source bytes.
func (catalog *Catalog) List(ctx context.Context) ([]PackageInfo, int64, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.refreshLocked(ctx); err != nil {
		return nil, 0, err
	}
	result := make([]PackageInfo, 0, len(catalog.snapshot.Packages))
	for _, entry := range catalog.snapshot.Packages {
		p, _ := LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
		result = append(result, PackageInfo{Key: p.manifest.Key, Version: p.manifest.Version, Digest: entry.Digest, Active: entry.Active, Enabled: entry.Enabled, Revoked: entry.Revoked})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key != result[j].Key {
			return result[i].Key < result[j].Key
		}
		return result[i].Version < result[j].Version
	})
	return result, catalog.snapshot.Revision, nil
}

// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"errors"
	"math"
	"sort"
)

// ManagementEntry intentionally contains no source/document or execution lease.
type ManagementEntry struct {
	Manifest                 Manifest
	Digest                   string
	SourceDigest             string
	FormatVersion            int
	BOMPrefixed              bool
	Active, Enabled, Revoked bool
}

func (catalog *Catalog) ManagementSnapshot(ctx context.Context) ([]ManagementEntry, int64, error) {
	if err := catalog.acquire(ctx); err != nil {
		return nil, 0, err
	}
	defer catalog.mu.Unlock()
	if err := managementContextError(ctx); err != nil {
		return nil, 0, err
	}
	if err := catalog.refreshLocked(ctx); err != nil {
		return nil, 0, err
	}
	entries := make([]ManagementEntry, 0, len(catalog.snapshot.Packages))
	byDigest := make(map[string]*Package, len(catalog.state.history))
	for _, pkg := range catalog.state.history {
		byDigest[pkg.digest] = pkg
	}
	for _, row := range catalog.snapshot.Packages {
		// The published generation already validated these exact immutable bytes.
		pkg := byDigest[row.Digest]
		entries = append(entries, ManagementEntry{Manifest: pkg.Manifest(), Digest: row.Digest, SourceDigest: pkg.SourceDigest(), FormatVersion: pkg.FormatVersion(), BOMPrefixed: pkg.BOMPrefixed(), Active: row.Active, Enabled: row.Enabled, Revoked: row.Revoked})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Manifest.Key != entries[j].Manifest.Key {
			return entries[i].Manifest.Key < entries[j].Manifest.Key
		}
		return entries[i].Manifest.Version < entries[j].Manifest.Version
	})
	return entries, catalog.snapshot.Revision, nil
}

type ManagementMutation struct {
	Action, Key, Version string
	Package              *Package
}

// ValidateInstall checks the current catalog without committing an installation.
// The eventual write still requires the caller's observed revision and CAS.
func (catalog *Catalog) ValidateInstall(ctx context.Context, pkg *Package) error {
	if err := catalog.acquire(ctx); err != nil {
		return err
	}
	defer catalog.mu.Unlock()
	if err := managementContextError(ctx); err != nil {
		return err
	}
	if err := catalog.refreshLocked(ctx); err != nil {
		return err
	}
	if catalog.snapshot.Revision == math.MaxInt64 {
		return ErrCatalogInvalid
	}
	next := cloneCatalogSnapshot(catalog.snapshot)
	if err := installCatalogPackagesContext(ctx, &next, pkg); err != nil {
		return err
	}
	next.Revision++
	_, err := buildCatalogStateContext(ctx, next, catalog.snapshot, true)
	return err
}

func managementContextError(ctx context.Context) error {
	if ctx == nil {
		return ErrCatalogUnavailable
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrCatalogUnavailable, err)
	}
	return nil
}

// MutateExpected uses the existing transition helpers and durable store CAS.
// A stale operator cannot act on a newly active version; writes are never retried.
func (catalog *Catalog) MutateExpected(ctx context.Context, expected int64, mutation ManagementMutation) error {
	if expected < 0 || expected == math.MaxInt64 {
		return ErrCatalogInvalid
	}
	if mutation.Action == "delete" {
		return catalog.deleteExpected(ctx, expected, mutation.Key, mutation.Version)
	}
	return catalog.updateExpected(ctx, &expected, func(next *CatalogSnapshot) error {
		switch mutation.Action {
		case "install":
			if mutation.Package == nil {
				return ErrCatalogInvalid
			}
			return installCatalogPackagesContext(ctx, next, mutation.Package)
		case "activate":
			return activateCatalogVersion(next, mutation.Key, mutation.Version)
		case "enable", "disable":
			index, err := catalogVersion(next, mutation.Key, mutation.Version, false)
			if err != nil {
				return err
			}
			if !next.Packages[index].Active {
				return ErrCatalogInvalid
			}
			return setCatalogEnabled(next, mutation.Key, mutation.Action == "enable")
		default:
			return ErrCatalogInvalid
		}
	})
}

func (catalog *Catalog) deleteExpected(ctx context.Context, expected int64, key, version string) error {
	if err := catalog.acquire(ctx); err != nil {
		return err
	}
	defer catalog.mu.Unlock()
	if err := managementContextError(ctx); err != nil {
		return err
	}
	if err := catalog.refreshLocked(ctx); err != nil {
		return err
	}
	if catalog.snapshot.Revision != expected {
		return ErrCatalogConflict
	}
	store, ok := catalog.store.(CatalogVersionDeleter)
	if !ok {
		return ErrCatalogUnavailable
	}
	revision, err := store.DeleteVersion(ctx, expected, key, version)
	if err != nil {
		if errors.Is(err, ErrCatalogConflict) {
			catalog.host.state.Store(nil)
			return ErrCatalogConflict
		}
		if errors.Is(err, ErrCatalogInUse) || errors.Is(err, ErrBindingRejected) || errors.Is(err, ErrCatalogInvalid) {
			return err
		}
		catalog.host.state.Store(nil)
		return ErrCatalogUnavailable
	}
	if revision != expected+1 {
		catalog.host.state.Store(nil)
		return ErrCatalogUnavailable
	}
	// The durable store has confirmed the exact commit. Publication failure
	// suspends the local Host, but cannot turn that receipt into an unknown write.
	catalog.host.state.Store(nil)
	_ = catalog.refreshLocked(ctx)
	return nil
}

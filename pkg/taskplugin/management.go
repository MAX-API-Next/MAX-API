// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"math"
	"sort"
)

// ManagementEntry intentionally contains no source/document or execution lease.
type ManagementEntry struct {
	Manifest                 Manifest
	Digest                   string
	SourceDigest             string
	FormatVersion            int
	Active, Enabled, Revoked bool
}

func (catalog *Catalog) ManagementSnapshot(ctx context.Context) ([]ManagementEntry, int64, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
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
		entries = append(entries, ManagementEntry{Manifest: pkg.Manifest(), Digest: row.Digest, SourceDigest: pkg.SourceDigest(), FormatVersion: pkg.FormatVersion(), Active: row.Active, Enabled: row.Enabled, Revoked: row.Revoked})
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

// MutateExpected uses the existing transition helpers and durable store CAS.
// A stale operator cannot act on a newly active version; writes are never retried.
func (catalog *Catalog) MutateExpected(ctx context.Context, expected int64, mutation ManagementMutation) error {
	if expected < 0 || expected == math.MaxInt64 {
		return ErrCatalogInvalid
	}
	return catalog.updateExpected(ctx, &expected, func(next *CatalogSnapshot) error {
		switch mutation.Action {
		case "install":
			if mutation.Package == nil {
				return ErrCatalogInvalid
			}
			return installCatalogPackages(next, mutation.Package)
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
		case "revoke":
			return revokeCatalogVersion(next, mutation.Key, mutation.Version)
		default:
			return ErrCatalogInvalid
		}
	})
}

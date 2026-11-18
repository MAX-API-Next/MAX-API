// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogRemovalNeedsMatchingDurableReceipt(t *testing.T) {
	pkg := testPackage(t, validManifest(), "source")
	manifest := pkg.Manifest()
	previous := CatalogSnapshot{Revision: 1, Packages: []StoredPackage{{Document: pkg.Document(), Digest: pkg.Digest(), Active: true}}}
	next := CatalogSnapshot{Revision: 2}
	require.ErrorIs(t, ValidateCatalogTransition(previous, next), ErrCatalogInvalid)
	receipt := PackageDeletion{Key: manifest.Key, Version: manifest.Version, Digest: pkg.Digest()}
	next.Deletions = []PackageDeletion{receipt}
	require.NoError(t, ValidateCatalogTransition(previous, next))
	next.Deletions[0].Digest = "0" + receipt.Digest[1:]
	if next.Deletions[0].Digest == receipt.Digest {
		next.Deletions[0].Digest = "1" + receipt.Digest[1:]
	}
	require.ErrorIs(t, ValidateCatalogTransition(previous, next), ErrCatalogInvalid)
	next.Deletions[0] = receipt
	previous.Packages[0].Revoked = true
	require.ErrorIs(t, ValidateCatalogTransition(previous, next), ErrCatalogInvalid)
	next.Deletions[0].Revoked = true
	require.NoError(t, ValidateCatalogTransition(previous, next))
	require.ErrorIs(t, ValidateCatalogTransition(next, CatalogSnapshot{Revision: 3}), ErrCatalogInvalid, "deletion receipts cannot be dropped")
	next.Revision = 0
	require.ErrorIs(t, ValidateCatalogTransition(CatalogSnapshot{}, next), ErrCatalogInvalid)
	_, err := buildCatalogState(CatalogSnapshot{}, CatalogSnapshot{}, false)
	require.NoError(t, err)
	_, err = buildCatalogState(next, CatalogSnapshot{}, false)
	require.ErrorIs(t, err, ErrCatalogInvalid, "cold start also rejects revision-zero receipts without comparing fingerprints")
}

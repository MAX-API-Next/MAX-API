// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type cancelDuringReceiptScan struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (ctx *cancelDuringReceiptScan) Err() error {
	ctx.checks++
	if ctx.checks == 3 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestCatalogReceiptValidationHonorsCancellation(t *testing.T) {
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &cancelDuringReceiptScan{Context: base, cancel: cancel}
	next := CatalogSnapshot{Revision: 1, Deletions: []PackageDeletion{{Key: "old", Version: "1.0.0", Digest: strings.Repeat("a", 64)}, {Key: "another", Version: "1.0.0", Digest: strings.Repeat("b", 64)}}}
	require.NoError(t, ValidateCatalogTransition(CatalogSnapshot{}, next))
	require.ErrorIs(t, ValidateCatalogTransitionContext(ctx, CatalogSnapshot{}, next), context.Canceled)
}

func TestCatalogReceiptDigestMustBeCanonical(t *testing.T) {
	next := CatalogSnapshot{Revision: 1, Deletions: []PackageDeletion{{Key: "old", Version: "1.0.0", Digest: strings.Repeat("A", 64)}}}
	require.ErrorIs(t, ValidateCatalogTransition(CatalogSnapshot{}, next), ErrCatalogInvalid)
	next.Deletions[0].Digest = strings.ToLower(next.Deletions[0].Digest)
	require.NoError(t, ValidateCatalogTransition(CatalogSnapshot{}, next))
}

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

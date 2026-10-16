/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupVerifyAndRestoreWithWALAndConfigFingerprint(t *testing.T) {
	source := t.TempDir()
	primary := filepath.Join(source, "ledger.sqlite")
	logPath := filepath.Join(source, "logs.sqlite")
	config := filepath.Join(source, "runtime.env")
	require.NoError(t, os.WriteFile(primary, []byte("ledger-v1"), 0o600))
	require.NoError(t, os.WriteFile(primary+"-wal", []byte("ledger-wal"), 0o600))
	require.NoError(t, os.WriteFile(logPath, []byte("logs-v1"), 0o600))
	require.NoError(t, os.WriteFile(logPath+"-shm", []byte("logs-shm"), 0o600))
	require.NoError(t, os.WriteFile(config, []byte("SECRET=must-not-be-copied\n"), 0o600))

	archive := filepath.Join(t.TempDir(), "archive")
	root, err := prepareDestination(archive, primary, logPath, config)
	require.NoError(t, err)
	sources, err := collectSources(primary, logPath)
	require.NoError(t, err)
	value := manifest{
		SchemaVersion: manifestVersion,
		CreatedAt:     "synthetic",
		Tool:          "test",
	}
	for _, sourceFile := range sources {
		entry, entryErr := archiveSource(root, sourceFile)
		require.NoError(t, entryErr)
		value.Files = append(value.Files, entry)
	}
	fingerprint, err := fingerprintFile(config)
	require.NoError(t, err)
	fingerprint.Copied = false
	value.Configuration = &fingerprint
	require.NoError(t, writeManifest(root, value))
	require.NoError(t, verifyArchive(root))

	restore := filepath.Join(t.TempDir(), "restore")
	require.NoError(t, os.MkdirAll(restore, 0o700))
	require.NoError(t, runRestore([]string{"--backup", root, "--target", restore}))
	require.Equal(t, []byte("ledger-v1"), mustRead(t, filepath.Join(restore, "primary", "ledger.sqlite")))
	require.Equal(t, []byte("ledger-wal"), mustRead(t, filepath.Join(restore, "primary", "ledger.sqlite-wal")))
	require.Equal(t, []byte("logs-v1"), mustRead(t, filepath.Join(restore, "log", "logs.sqlite")))
	require.NoFileExists(t, filepath.Join(restore, "runtime.env"))
	manifestBytes := mustRead(t, filepath.Join(restore, manifestName))
	require.NotContains(t, string(manifestBytes), "SECRET=must-not-be-copied")
}

func TestBackupRequiresQuiesced(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "ledger.sqlite")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	err := runBackup([]string{"--primary", primary, "--destination", filepath.Join(t.TempDir(), "archive")})
	require.ErrorContains(t, err, "--quiesced")
}

func TestVerifyRejectsTamperingAndUnlistedFiles(t *testing.T) {
	source := t.TempDir()
	primary := filepath.Join(source, "ledger.sqlite")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	archive := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, runBackup([]string{"--quiesced", "--primary", primary, "--destination", archive}))
	require.NoError(t, os.WriteFile(filepath.Join(archive, "primary", "ledger.sqlite"), []byte("tampered"), 0o600))
	require.ErrorContains(t, verifyArchive(archive), "archive file changed")

	archive2 := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, runBackup([]string{"--quiesced", "--primary", primary, "--destination", archive2}))
	require.NoError(t, os.WriteFile(filepath.Join(archive2, "extra.txt"), []byte("mixed"), 0o600))
	require.ErrorContains(t, verifyArchive(archive2), "unlisted file")
}

func TestVerifyRejectsManifestWithoutPrimaryDatabase(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "ledger.sqlite")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	archive := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, runBackup([]string{"--quiesced", "--primary", primary, "--destination", archive}))
	value, err := readManifest(archive)
	require.NoError(t, err)
	for index := range value.Files {
		value.Files[index].Roles = []string{"log"}
	}
	require.NoError(t, writeManifest(archive, value))
	require.ErrorContains(t, verifyArchive(archive), "missing the primary database")
}

func TestVerifyRejectsSymlinkedArchiveFile(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "ledger.sqlite")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	archive := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, runBackup([]string{"--quiesced", "--primary", primary, "--destination", archive}))
	archiveFile := filepath.Join(archive, "primary", "ledger.sqlite")
	target := filepath.Join(t.TempDir(), "target.sqlite")
	require.NoError(t, os.WriteFile(target, []byte("ledger"), 0o600))
	require.NoError(t, os.Remove(archiveFile))
	if err := os.Symlink(target, archiveFile); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	require.ErrorContains(t, verifyArchive(archive), "regular and not a symlink")
}

func TestBackupRejectsDestinationInsideSource(t *testing.T) {
	source := t.TempDir()
	primary := filepath.Join(source, "ledger.sqlite")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	err := runBackup([]string{
		"--quiesced",
		"--primary", primary,
		"--destination", filepath.Join(source, "archive"),
	})
	require.ErrorContains(t, err, "source directory")
}

func TestBackupFailureLeavesDestinationEmpty(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "ledger.sqlite")
	config := filepath.Join(t.TempDir(), "config-directory")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	require.NoError(t, os.Mkdir(config, 0o700))
	destination := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, os.Mkdir(destination, 0o700))
	err := runBackup([]string{
		"--quiesced",
		"--primary", primary,
		"--config", config,
		"--destination", destination,
	})
	require.ErrorContains(t, err, "not a regular file")
	entries, readErr := os.ReadDir(destination)
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func TestBackupRejectsDestinationInsideSymlinkedSource(t *testing.T) {
	source := t.TempDir()
	primary := filepath.Join(source, "ledger.sqlite")
	require.NoError(t, os.WriteFile(primary, []byte("ledger"), 0o600))
	alias := filepath.Join(t.TempDir(), "source-link")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	err := runBackup([]string{
		"--quiesced",
		"--primary", primary,
		"--destination", filepath.Join(alias, "archive"),
	})
	require.ErrorContains(t, err, "source directory")
}

func TestSamePathUsesExactOrExistingFileIdentity(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "ledger.sqlite")
	require.NoError(t, os.WriteFile(file, []byte("ledger"), 0o600))
	require.True(t, samePath(file, filepath.Join(root, ".", "ledger.sqlite")))
	require.False(t, samePath(file, filepath.Join(root, "other.sqlite")))
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

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

// sqliteops provides a conservative, offline archive for a quiesced SQLite
// business database and its optional independent log database. It never opens
// or mutates either database and never copies configuration contents.
package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
)

const (
	manifestName    = "manifest.json"
	manifestVersion = 1
)

type manifest struct {
	SchemaVersion int                `json:"schema_version"`
	CreatedAt     string             `json:"created_at"`
	Tool          string             `json:"tool"`
	Files         []manifestFile     `json:"files"`
	Configuration *configurationHash `json:"configuration,omitempty"`
}

type manifestFile struct {
	Roles       []string `json:"roles"`
	ArchivePath string   `json:"archive_path"`
	SourceName  string   `json:"source_name"`
	Size        int64    `json:"size"`
	SHA256      string   `json:"sha256"`
	Required    bool     `json:"required"`
}

type configurationHash struct {
	SourceName string `json:"source_name"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Copied     bool   `json:"copied"`
}

type sourceFile struct {
	roles       []string
	path        string
	archivePath string
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "backup":
		err = runBackup(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "restore":
		err = runRestore(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqliteops:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sqliteops backup --quiesced --primary PATH --destination DIR [--log PATH] [--config PATH]")
	fmt.Fprintln(os.Stderr, "       sqliteops verify --backup DIR")
	fmt.Fprintln(os.Stderr, "       sqliteops restore --backup DIR --target DIR")
}

func runBackup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	primary := flags.String("primary", "", "business SQLite file")
	logPath := flags.String("log", "", "independent log SQLite file")
	config := flags.String("config", "", "configuration file to fingerprint, never copied")
	destination := flags.String("destination", "", "new archive directory")
	quiesced := flags.Bool("quiesced", false, "assert that all application and background writers are stopped")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*quiesced {
		return errors.New("refusing a backup without --quiesced; stop application and background writers first")
	}
	if strings.TrimSpace(*primary) == "" || strings.TrimSpace(*destination) == "" {
		return errors.New("--primary and --destination are required")
	}

	root, err := prepareDestination(*destination, *primary, *logPath, *config)
	if err != nil {
		return err
	}
	stageRoot, err := createStagingDirectory(root)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stageRoot)
		}
	}()
	sources, err := collectSources(*primary, *logPath)
	if err != nil {
		return err
	}
	m := manifest{
		SchemaVersion: manifestVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Tool:          "MAX-API sqliteops",
	}
	for _, source := range sources {
		entry, err := archiveSource(stageRoot, source)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, entry)
	}
	if *config != "" {
		fingerprint, err := fingerprintFile(*config)
		if err != nil {
			return fmt.Errorf("fingerprint config: %w", err)
		}
		fingerprint.Copied = false
		m.Configuration = &fingerprint
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].ArchivePath < m.Files[j].ArchivePath })
	if err := writeManifest(stageRoot, m); err != nil {
		return err
	}
	if err := verifyArchive(stageRoot); err != nil {
		return err
	}
	if err := publishStagingDirectory(stageRoot, root); err != nil {
		return err
	}
	published = true
	return nil
}

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	backup := flags.String("backup", "", "archive directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*backup) == "" {
		return errors.New("--backup is required")
	}
	root, err := filepath.Abs(*backup)
	if err != nil {
		return err
	}
	return verifyArchive(root)
}

func runRestore(args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	backup := flags.String("backup", "", "archive directory")
	target := flags.String("target", "", "new empty restore directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*backup) == "" || strings.TrimSpace(*target) == "" {
		return errors.New("--backup and --target are required")
	}
	backupRoot, err := filepath.Abs(*backup)
	if err != nil {
		return err
	}
	if err := verifyArchive(backupRoot); err != nil {
		return err
	}
	targetRoot, err := filepath.Abs(*target)
	if err != nil {
		return err
	}
	if err := ensureEmptyDestination(targetRoot); err != nil {
		return err
	}
	if pathsOverlap(backupRoot, targetRoot) {
		return errors.New("restore target must not overlap the backup archive")
	}
	stageRoot, err := createStagingDirectory(targetRoot)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stageRoot)
		}
	}()
	m, err := readManifest(backupRoot)
	if err != nil {
		return err
	}
	for _, item := range m.Files {
		source, err := safeArchivePath(backupRoot, item.ArchivePath)
		if err != nil {
			return err
		}
		destination, err := safeArchivePath(stageRoot, item.ArchivePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if err := copyRegularFile(source, destination, 0o600); err != nil {
			return err
		}
	}
	if err := copyRegularFile(filepath.Join(backupRoot, manifestName), filepath.Join(stageRoot, manifestName), 0o600); err != nil {
		return err
	}
	if err := publishStagingDirectory(stageRoot, targetRoot); err != nil {
		return err
	}
	published = true
	return nil
}

func collectSources(primary, logPath string) ([]sourceFile, error) {
	primaryAbs, err := filepath.Abs(primary)
	if err != nil {
		return nil, err
	}
	if err := requireRegularFile(primaryAbs); err != nil {
		return nil, fmt.Errorf("primary: %w", err)
	}
	sources := []sourceFile{{roles: []string{"primary"}, path: primaryAbs, archivePath: filepath.Join("primary", filepath.Base(primaryAbs))}}
	if strings.TrimSpace(logPath) == "" || samePath(primaryAbs, logPath) {
		sources[0].roles = append(sources[0].roles, "log")
		return expandSidecars(sources)
	}
	logAbs, err := filepath.Abs(logPath)
	if err != nil {
		return nil, err
	}
	if err := requireRegularFile(logAbs); err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	sources = append(sources, sourceFile{roles: []string{"log"}, path: logAbs, archivePath: filepath.Join("log", filepath.Base(logAbs))})
	return expandSidecars(sources)
}

func expandSidecars(sources []sourceFile) ([]sourceFile, error) {
	var expanded []sourceFile
	for _, source := range sources {
		expanded = append(expanded, source)
		for _, suffix := range []string{"-wal", "-shm"} {
			path := source.path + suffix
			if _, err := os.Stat(path); err == nil {
				if err := requireRegularFile(path); err != nil {
					return nil, err
				}
				expanded = append(expanded, sourceFile{roles: source.roles, path: path, archivePath: source.archivePath + suffix})
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	return expanded, nil
}

func archiveSource(root string, source sourceFile) (manifestFile, error) {
	destination, err := safeArchivePath(root, source.archivePath)
	if err != nil {
		return manifestFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return manifestFile{}, err
	}
	if err := copyRegularFile(source.path, destination, 0o600); err != nil {
		return manifestFile{}, err
	}
	fingerprint, err := fingerprintFile(destination)
	if err != nil {
		return manifestFile{}, err
	}
	return manifestFile{
		Roles:       source.roles,
		ArchivePath: filepath.ToSlash(source.archivePath),
		SourceName:  filepath.Base(source.path),
		Size:        fingerprint.Size,
		SHA256:      fingerprint.SHA256,
		Required:    true,
	}, nil
}

func prepareDestination(destination, primary, logPath, config string) (string, error) {
	root, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := ensureEmptyDestination(root); err != nil {
		return "", err
	}
	for _, source := range []string{primary, logPath, config} {
		if source == "" {
			continue
		}
		sourceAbs, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		if pathsOverlap(root, filepath.Dir(sourceAbs)) {
			return "", errors.New("destination must not be inside a source directory or overlap a source")
		}
	}
	return root, nil
}

func createStagingDirectory(destination string) (string, error) {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, "."+filepath.Base(destination)+".tmp-")
}

func publishStagingDirectory(stage, destination string) error {
	if info, err := os.Stat(destination); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("destination is not a directory: %s", destination)
		}
		entries, err := os.ReadDir(destination)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("destination must remain empty until publish: %s", destination)
		}
		if err := os.Remove(destination); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(stage, destination)
}

func verifyArchive(root string) error {
	m, err := readManifest(root)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, item := range m.Files {
		if item.ArchivePath == "" || item.SHA256 == "" || item.Size < 0 || !item.Required {
			return fmt.Errorf("invalid manifest entry %q", item.ArchivePath)
		}
		if _, exists := seen[item.ArchivePath]; exists {
			return fmt.Errorf("duplicate manifest entry %q", item.ArchivePath)
		}
		seen[item.ArchivePath] = struct{}{}
		path, err := safeArchivePath(root, item.ArchivePath)
		if err != nil {
			return err
		}
		if err := requireRegularFile(path); err != nil {
			return fmt.Errorf("verify %s: %w", item.ArchivePath, err)
		}
		actual, err := fingerprintFile(path)
		if err != nil {
			return fmt.Errorf("verify %s: %w", item.ArchivePath, err)
		}
		if actual.Size != item.Size || actual.SHA256 != item.SHA256 {
			return fmt.Errorf("archive file changed: %s", item.ArchivePath)
		}
	}
	return rejectUnlistedFiles(root, seen)
}

func rejectUnlistedFiles(root string, listed map[string]struct{}) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == manifestName {
			return nil
		}
		if _, ok := listed[filepath.ToSlash(rel)]; !ok {
			return fmt.Errorf("archive contains unlisted file %s", filepath.ToSlash(rel))
		}
		return nil
	})
}

func readManifest(root string) (manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var result manifest
	if err := common.Unmarshal(data, &result); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if result.SchemaVersion != manifestVersion || len(result.Files) == 0 {
		return manifest{}, errors.New("unsupported or empty archive manifest")
	}
	roles := map[string]bool{}
	mainRoles := map[string]bool{}
	for _, item := range result.Files {
		for _, role := range item.Roles {
			roles[role] = true
			if !strings.HasSuffix(item.ArchivePath, "-wal") && !strings.HasSuffix(item.ArchivePath, "-shm") {
				mainRoles[role] = true
			}
		}
	}
	if !mainRoles["primary"] {
		return manifest{}, errors.New("archive manifest is missing the primary database")
	}
	if roles["log"] && !mainRoles["log"] {
		return manifest{}, errors.New("archive manifest is missing the log database")
	}
	return result, nil
}

func writeManifest(root string, value manifest) error {
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, manifestName), data, 0o600)
}

func fingerprintFile(path string) (result configurationHash, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return configurationHash{}, err
	}
	if !info.Mode().IsRegular() {
		return configurationHash{}, fmt.Errorf("not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return configurationHash{}, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return configurationHash{}, err
	}
	return configurationHash{SourceName: filepath.Base(path), Size: info.Size(), SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	if err := requireRegularFile(source); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	inputClosed := false
	defer func() {
		if !inputClosed {
			_ = input.Close()
		}
	}()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = output.Close()
		if remove {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	inputClosed = true
	if err := input.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func ensureEmptyDestination(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("destination is not a directory: %s", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("destination must be new or empty: %s", path)
	}
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("file must be regular and not a symlink: %s", path)
	}
	return nil
}

func safeArchivePath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) == "." || strings.HasPrefix(filepath.ToSlash(filepath.Clean(relative)), "../") {
		return "", fmt.Errorf("unsafe archive path: %s", relative)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !isWithin(rootAbs, pathAbs) {
		return "", fmt.Errorf("archive path escapes root: %s", relative)
	}
	return pathAbs, nil
}

func pathsOverlap(left, right string) bool {
	leftResolved := resolveOverlapPath(left)
	rightResolved := resolveOverlapPath(right)
	return isWithin(leftResolved, rightResolved) || isWithin(rightResolved, leftResolved)
}

func resolveOverlapPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	current := filepath.Clean(abs)
	var missing []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return filepath.Clean(abs)
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved)
		} else if !os.IsNotExist(err) {
			return filepath.Clean(abs)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(abs)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func isWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if samePath(root, path) {
		return true
	}
	separator := string(filepath.Separator)
	return strings.HasPrefix(path, root+separator)
}

func samePath(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	leftClean := filepath.Clean(leftAbs)
	rightClean := filepath.Clean(rightAbs)
	if leftClean == rightClean {
		return true
	}
	leftInfo, leftErr := os.Stat(leftClean)
	rightInfo, rightErr := os.Stat(rightClean)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return os.SameFile(leftInfo, rightInfo)
}

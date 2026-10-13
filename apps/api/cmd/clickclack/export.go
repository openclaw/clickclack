package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

const exportTempPattern = ".clickclack-export-*"

var errExportDestinationOverlap = errors.New("export destination overlaps the SQLite database or its journal files")

type exportPathRules struct {
	ignoresCase     bool
	ignoredTrailing string
}

func exportDestinationPath(path string) (string, error) {
	// Resolve the parent before cleaning: a symlink followed by ".." must
	// follow filesystem traversal, rather than lexical path simplification.
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	parent, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(parent, name))
}

func exportDirectoryRules(file *os.File) (exportPathRules, error) {
	info, err := file.Stat()
	if err != nil {
		return exportPathRules{}, err
	}
	// The temp name is ASCII; probe the output directory without creating or
	// modifying any SQLite companion file.
	upperPath := filepath.Join(filepath.Dir(file.Name()), strings.ToUpper(filepath.Base(file.Name())))
	ignoresCase, err := exportFileAlias(info, upperPath)
	if err != nil {
		return exportPathRules{}, err
	}
	rules := exportPathRules{ignoresCase: ignoresCase}
	for _, suffix := range []string{".", " "} {
		ignored, err := exportFileAlias(info, file.Name()+suffix)
		if err != nil {
			return exportPathRules{}, err
		}
		if ignored {
			rules.ignoredTrailing += suffix
		}
	}
	return rules, nil
}

func exportFileAlias(info os.FileInfo, path string) (bool, error) {
	aliasInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(info, aliasInfo), nil
}

func validateExportDestination(st databaseStore, sourceDB, destination string, rules exportPathRules) error {
	if _, ok := st.(*sqlitestore.Store); !ok {
		return nil
	}
	dbPath, err := sqlitestore.DatabasePath(sourceDB)
	if err != nil {
		return err
	}
	dbPath, err = filepath.EvalSymlinks(dbPath)
	if err != nil {
		return err
	}
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		return err
	}
	destinationInfo, err := os.Stat(destination)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if destinationInfo != nil && os.SameFile(destinationInfo, dbInfo) {
		return errExportDestinationOverlap
	}
	sidecarDestination := destination
	if rules.ignoredTrailing != "" {
		// Win32 can normalize these even when the companion is absent.
		sidecarDestination = strings.TrimRight(sidecarDestination, rules.ignoredTrailing)
	}
	offset := strings.LastIndexByte(sidecarDestination, '-')
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		protected := dbPath + suffix
		protectedInfo, err := os.Stat(protected)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if destinationInfo != nil && protectedInfo != nil && os.SameFile(destinationInfo, protectedInfo) {
			return errExportDestinationOverlap
		}
		if offset < 0 || (sidecarDestination[offset:] != suffix && !(rules.ignoresCase && strings.EqualFold(sidecarDestination[offset:], suffix))) {
			continue
		}
		// An absent sidecar has no inode. Resolve its database stem instead,
		// retaining the filesystem's case and Unicode alias rules.
		stemInfo, err := os.Stat(sidecarDestination[:offset])
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if stemInfo != nil && os.SameFile(stemInfo, dbInfo) {
			return errExportDestinationOverlap
		}
	}
	return nil
}

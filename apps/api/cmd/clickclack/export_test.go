package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestExportDataPreservesSQLiteFiles(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"database", "bare path", "relative segments", "source symlink", "source symlink parent traversal", "directory symlink", "symlink parent traversal", "hard link", "WAL", "shared memory", "rollback journal"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			st, dbPath, userID := exportTestDatabase(t)
			source, destination := "sqlite://"+dbPath, dbPath
			switch variant {
			case "bare path":
				source = dbPath
			case "relative segments":
				dir := filepath.Join(filepath.Dir(dbPath), "child")
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				destination = dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(dbPath)
			case "source symlink":
				link := filepath.Join(filepath.Dir(dbPath), "source.db")
				if err := os.Symlink(dbPath, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				source = "sqlite://" + link
			case "source symlink parent traversal":
				other := t.TempDir()
				child := filepath.Join(other, "child")
				if err := os.Mkdir(child, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(other, filepath.Base(dbPath)), []byte("unrelated file"), 0o600); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(filepath.Dir(dbPath), "alias")
				if err := os.Symlink(child, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				source = "sqlite://" + link + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(dbPath)
			case "directory symlink":
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(filepath.Dir(dbPath), link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				destination = filepath.Join(link, filepath.Base(dbPath))
			case "symlink parent traversal":
				child := filepath.Join(filepath.Dir(dbPath), "child")
				if err := os.Mkdir(child, 0o755); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(child, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				destination = link + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(dbPath)
			case "hard link":
				destination = filepath.Join(filepath.Dir(dbPath), "alias.db")
				if err := os.Link(dbPath, destination); err != nil {
					t.Skipf("hard links unavailable: %v", err)
				}
			case "WAL":
				destination += "-wal"
			case "shared memory":
				destination += "-shm"
			case "rollback journal":
				destination += "-journal"
			}
			err := exportData([]string{"--db", source, "--out", destination})
			if err == nil || !strings.Contains(err.Error(), "export destination overlaps") {
				t.Fatalf("export over SQLite file returned %v, want an overlap error", err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := sqlitestore.Open(dbPath)
			if err != nil {
				t.Fatalf("source database cannot be reopened: %v", err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			if _, err := reopened.GetUser(context.Background(), userID); err != nil {
				t.Fatalf("source database lost its user: %v", err)
			}
		})
	}
}

func TestExportDataPreservesLiteralSQLiteURLFilename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("colon is not a valid Windows filename")
	}
	st, dbPath, userID := exportTestDatabase(t)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	literalPath := filepath.Join(filepath.Dir(dbPath), "sqlite:")
	if err := os.Rename(dbPath, literalPath); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(dbPath))
	err := exportData([]string{"--db", "sqlite://", "--out", literalPath})
	if err == nil || !strings.Contains(err.Error(), "export destination overlaps") {
		t.Fatalf("export over literal SQLite URL filename returned %v, want an overlap error", err)
	}
	reopened, err := sqlitestore.Open(literalPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if _, err := reopened.GetUser(context.Background(), userID); err != nil {
		t.Fatalf("source database lost its user: %v", err)
	}
}

func TestExportDataHandlesFilesystemJournalAliases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, databaseAlias, destination string }{
		{"uppercase suffix", "CLICKCLACK.DB", "clickclack.db-JOURNAL"},
		{"uppercase stem and suffix", "CLICKCLACK.DB", "CLICKCLACK.DB-JOURNAL"},
		{"trailing period", "clickclack.db.", "clickclack.db-journal."},
		{"trailing space", "clickclack.db ", "clickclack.db-journal "},
		{"trailing periods and spaces", "clickclack.db.  ", "clickclack.db-journal.  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, dbPath, userID := exportTestDatabase(t)
			info, err := os.Stat(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			aliasPath := filepath.Join(filepath.Dir(dbPath), tc.databaseAlias)
			aliasInfo, err := os.Stat(aliasPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			aliasesSource := aliasInfo != nil && os.SameFile(info, aliasInfo)
			if _, err := os.Stat(dbPath + "-journal"); !os.IsNotExist(err) {
				t.Fatalf("expected an absent rollback journal: %v", err)
			}
			destination := filepath.Join(filepath.Dir(dbPath), tc.destination)
			err = exportData([]string{"--db", "sqlite://" + dbPath, "--out", destination})
			if aliasesSource {
				if err == nil || !strings.Contains(err.Error(), "export destination overlaps") {
					t.Fatalf("journal alias returned %v, want an overlap error", err)
				}
				if _, err := os.Stat(destination); !os.IsNotExist(err) {
					t.Fatalf("rejected journal destination was created: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(destination)
			if err != nil || !json.Valid(body) || !strings.Contains(string(body), userID) {
				t.Fatalf("distinct output filename was not exported correctly: %v", err)
			}
		})
	}
}

func TestExportDestinationWindowsJournalAliases(t *testing.T) {
	t.Parallel()
	st, dbPath, _ := exportTestDatabase(t)
	// Exercise Win32 filename policy with synthetic paths on every test host.
	for _, tail := range []string{".", " ", ".  "} {
		destination := dbPath + "-journal" + tail
		if err := validateExportDestination(st, dbPath, destination, exportPathRules{ignoredTrailing: " ."}); !errors.Is(err, errExportDestinationOverlap) {
			t.Fatalf("Windows journal alias %q returned %v", tail, err)
		}
		if err := validateExportDestination(st, dbPath, destination, exportPathRules{}); err != nil {
			t.Fatalf("literal non-Windows filename was rejected: %v", err)
		}
		ordinary := filepath.Join(filepath.Dir(dbPath), "export.json"+tail)
		if err := validateExportDestination(st, dbPath, ordinary, exportPathRules{ignoredTrailing: " ."}); err != nil {
			t.Fatalf("ordinary Windows export was rejected: %v", err)
		}
	}
}

func TestExportDataReplacesUnrelatedOutput(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"regular path", "symlink parent traversal"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			_, dbPath, userID := exportTestDatabase(t)
			destination := filepath.Join(filepath.Dir(dbPath), "export.json")
			outputArg := destination
			if variant == "symlink parent traversal" {
				child := filepath.Join(filepath.Dir(dbPath), "child")
				if err := os.Mkdir(child, 0o755); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(child, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				outputArg = link + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "export.json"
			}
			if err := os.WriteFile(destination, []byte("previous export"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := exportData([]string{"--db", "sqlite://" + dbPath, "--out", outputArg}); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			var exported struct {
				Users []struct {
					ID string `json:"id"`
				} `json:"users"`
			}
			if err := json.Unmarshal(body, &exported); err != nil {
				t.Fatal(err)
			}
			if len(exported.Users) != 1 || exported.Users[0].ID != userID {
				t.Fatalf("export did not include the source user: %#v", exported.Users)
			}
		})
	}
}

func exportTestDatabase(t *testing.T) (*sqlitestore.Store, string, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Export fixture", Email: "export@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return st, dbPath, user.ID
}

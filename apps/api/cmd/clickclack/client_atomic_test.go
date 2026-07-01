package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteClientConfigFileCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := []byte("{\"server\":\"https://example.test\"}\n")
	if err := writeClientConfigFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("config = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("mode = %o, want 600", gotMode)
	}
}

func TestWriteClientConfigFilePreservesWritableMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeClientConfigFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o640 {
		t.Fatalf("mode = %o, want 640", gotMode)
	}
}

func TestWriteClientConfigFileRefusesReadOnlyTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := writeClientConfigFile(path, []byte("new")); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want permission error", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("read-only config changed to %q", got)
	}
}

func TestWriteClientConfigFilePreservesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real-config.json")
	link := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeClientConfigFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("config symlink was replaced")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("symlink target = %q, want new", got)
	}
}

func TestWriteClientConfigFilePreservesDanglingSymlinkOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink("missing.json", link); err != nil {
		t.Fatal(err)
	}
	if err := writeClientConfigFile(link, []byte("new")); err == nil {
		t.Fatal("expected dangling symlink error")
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling config symlink was replaced")
	}
}

func TestWriteClientConfigFileCleansTempAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	renameErr := errors.New("forced rename failure")
	err := writeClientConfigFileWithRename(target, []byte("new"), 0o600, func(string, string) error {
		return renameErr
	})
	if !errors.Is(err, renameErr) {
		t.Fatalf("error = %v, want %v", err, renameErr)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".config.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should not exist after failed rename: %v", err)
	}
}

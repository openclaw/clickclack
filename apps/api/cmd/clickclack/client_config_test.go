package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldStderr := os.Stderr
	outputFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = outputFile
	defer func() {
		os.Stderr = oldStderr
		_ = outputFile.Close()
	}()
	fn()
	if _, err := outputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(outputFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestDefaultClientOptionsWarnsAboutUnparseableConfig(t *testing.T) {
	path := useTempClientConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"server":"https://chat.example.com","tok`), 0o600); err != nil {
		t.Fatal(err)
	}
	var defaults clientDefaults
	stderr := captureStderr(t, func() { defaults = defaultClientOptions() })
	if !strings.Contains(stderr, "ignoring stored client config") || !strings.Contains(stderr, path) || !strings.Contains(stderr, "clickclack login") {
		t.Fatalf("stderr = %q, want a warning naming %s and the fix", stderr, path)
	}
	if defaults.hasConfig || defaults.opts.Server != "http://localhost:8080" {
		t.Fatalf("defaults = %+v, want the built-in server and no stored config", defaults)
	}
}

func TestDefaultClientOptionsIsQuietWithoutConfig(t *testing.T) {
	useTempClientConfig(t)
	var defaults clientDefaults
	stderr := captureStderr(t, func() { defaults = defaultClientOptions() })
	if stderr != "" {
		t.Fatalf("stderr = %q, want nothing when no config file exists", stderr)
	}
	if defaults.hasConfig {
		t.Fatalf("hasConfig = true without a config file")
	}
}

func TestDefaultClientOptionsIsQuietWithValidConfig(t *testing.T) {
	useTempClientConfig(t)
	writeStoredClientConfig(t, clientConfig{Server: "https://chat.example.com", Token: "ses_stored"})
	var defaults clientDefaults
	stderr := captureStderr(t, func() { defaults = defaultClientOptions() })
	if stderr != "" {
		t.Fatalf("stderr = %q, want nothing for a valid config", stderr)
	}
	if !defaults.hasConfig || defaults.opts.Server != "https://chat.example.com" {
		t.Fatalf("defaults = %+v, want the stored server", defaults)
	}
}

func TestHelpListsCommandsAndClientFlags(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var err error
	output := captureStderr(t, func() {
		err = runArgs([]string{"clickclack", "help"})
	})
	if err != nil {
		t.Fatalf("help returned %v", err)
	}
	for _, want := range []string{"Commands:", "  serve ", "  admin ", "  login ", "  messages list", "-server", "-workspace"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
	}
}

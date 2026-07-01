package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsEnvAndFile(t *testing.T) {
	t.Setenv("CLICKCLACK_ADDR", ":9000")
	t.Setenv("CLICKCLACK_DATA", "/tmp/clickclack")
	t.Setenv("CLICKCLACK_DB", "sqlite:///tmp/clickclack.db")
	t.Setenv("CLICKCLACK_UPLOADS", "r2://clickclack-uploads/prod")
	t.Setenv("CLICKCLACK_PUBLIC_URL", "https://clickclack.test")
	t.Setenv("CLICKCLACK_DEV_BOOTSTRAP", "false")
	t.Setenv("CLICKCLACK_GITHUB_CLIENT_ID", "client")
	t.Setenv("CLICKCLACK_GITHUB_CLIENT_SECRET", "secret")
	t.Setenv("CLICKCLACK_GITHUB_ALLOWED_ORG", "openclaw")
	t.Setenv("CLICKCLACK_GITHUB_MODERATOR_ORG", "openclaw")
	t.Setenv("CLICKCLACK_PUSHOVER_API_TOKEN", "app-token")
	t.Setenv("CLICKCLACK_R2_ACCOUNT_ID", "account")
	t.Setenv("CLICKCLACK_R2_ACCESS_KEY_ID", "access")
	t.Setenv("CLICKCLACK_R2_SECRET_ACCESS_KEY", "secret-access")
	t.Setenv("CLICKCLACK_R2_ENDPOINT", "https://r2.example.com")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9000" || cfg.Data != "/tmp/clickclack" || cfg.DB != "sqlite:///tmp/clickclack.db" || cfg.Uploads != "r2://clickclack-uploads/prod" || cfg.PublicURL != "https://clickclack.test" || cfg.DevBootstrap || cfg.GitHubClientID != "client" || cfg.GitHubClientSecret != "secret" || cfg.GitHubAllowedOrg != "openclaw" || cfg.GitHubModeratorOrg != "openclaw" || cfg.PushoverAPIToken != "app-token" || cfg.R2AccountID != "account" || cfg.R2AccessKeyID != "access" || cfg.R2SecretAccessKey != "secret-access" || cfg.R2Endpoint != "https://r2.example.com" {
		t.Fatalf("unexpected env config: %#v", cfg)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"addr":":7000","data":"/data"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9000" || cfg.Data != "/tmp/clickclack" || cfg.DB != "sqlite:///tmp/clickclack.db" {
		t.Fatalf("expected env to override file config: %#v", cfg)
	}

	t.Setenv("CLICKCLACK_ADDR", "")
	t.Setenv("CLICKCLACK_DATA", "")
	t.Setenv("CLICKCLACK_DB", "")
	t.Setenv("CLICKCLACK_UPLOADS", "")
	t.Setenv("CLICKCLACK_PUBLIC_URL", "")
	t.Setenv("CLICKCLACK_DEV_BOOTSTRAP", "")
	t.Setenv("CLICKCLACK_GITHUB_CLIENT_ID", "")
	t.Setenv("CLICKCLACK_GITHUB_CLIENT_SECRET", "")
	t.Setenv("CLICKCLACK_GITHUB_ALLOWED_ORG", "")
	t.Setenv("CLICKCLACK_GITHUB_MODERATOR_ORG", "")
	t.Setenv("CLICKCLACK_PUSHOVER_API_TOKEN", "")
	t.Setenv("CLICKCLACK_R2_ACCOUNT_ID", "")
	t.Setenv("CLICKCLACK_R2_ACCESS_KEY_ID", "")
	t.Setenv("CLICKCLACK_R2_SECRET_ACCESS_KEY", "")
	t.Setenv("CLICKCLACK_R2_ENDPOINT", "")
	emptyPath := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(emptyPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(emptyPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Data != "./data" || cfg.DevBootstrap {
		t.Fatalf("unexpected fallback config: %#v", cfg)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing config error")
	}
	t.Setenv("CLICKCLACK_DEV_BOOTSTRAP", "not-bool")
	if _, err := Load(""); err == nil {
		t.Fatal("expected bad bool env error")
	}
	overrideBoolPath := filepath.Join(t.TempDir(), "override-bool.json")
	if err := os.WriteFile(overrideBoolPath, []byte(`{"dev_bootstrap":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(overrideBoolPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DevBootstrap {
		t.Fatalf("expected file boolean to override invalid env: %#v", cfg)
	}
	t.Setenv("CLICKCLACK_DEV_BOOTSTRAP", "")
	badPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badPath, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(badPath); err == nil {
		t.Fatal("expected bad json error")
	}
}

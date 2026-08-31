package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/passwordauth"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

// pipeStdin points os.Stdin at a regular file, which is never a terminal, so
// the command takes its non-interactive path.
func pipeStdin(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = handle
	t.Cleanup(func() {
		os.Stdin = original
		_ = handle.Close()
	})
}

func newPasswordCLIFixture(t *testing.T) (string, string, store.User) {
	t.Helper()
	dataDir := t.TempDir()
	dbURL := "sqlite://" + filepath.Join(dataDir, "clickclack.db")
	st, err := sqlitestore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return dataDir, dbURL, user
}

func readPasswordHash(t *testing.T, dbURL, identifier string) string {
	t.Helper()
	st, err := sqlitestore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	login, err := st.GetPasswordLogin(context.Background(), identifier)
	if err != nil {
		t.Fatal(err)
	}
	return login.PasswordHash
}

func TestAdminUserSetPasswordFromPipedStdin(t *testing.T) {
	dataDir, dbURL, _ := newPasswordCLIFixture(t)
	pipeStdin(t, "a good long password\n")

	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--email", "maggie@example.com"}); err != nil {
		t.Fatal(err)
	}

	hash := readPasswordHash(t, dbURL, "maggie@example.com")
	if hash == "" {
		t.Fatal("expected a password hash to be stored")
	}
	// The trailing newline from a piped value must not become part of the
	// secret, or the password typed at the browser would never match.
	matched, err := passwordauth.Verify(hash, "a good long password")
	if err != nil || !matched {
		t.Fatalf("expected the piped password to verify, got matched=%v err=%v", matched, err)
	}
	if strings.Contains(hash, "a good long password") {
		t.Fatal("expected the stored value to be a hash, not the password")
	}
	if login := readPasswordHash(t, dbURL, "MAGGIE@example.com"); login != hash {
		t.Fatal("expected the identifier lookup to fold casing")
	}
}

func TestAdminUserSetPasswordResolvesByUserID(t *testing.T) {
	dataDir, dbURL, user := newPasswordCLIFixture(t)
	pipeStdin(t, "another good password")

	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--user", user.ID}); err != nil {
		t.Fatal(err)
	}
	if readPasswordHash(t, dbURL, "maggie@example.com") == "" {
		t.Fatal("expected a password hash for the named user id")
	}
}

func TestAdminUserSetPasswordClearsPassword(t *testing.T) {
	dataDir, dbURL, _ := newPasswordCLIFixture(t)
	pipeStdin(t, "a good long password")
	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--email", "maggie@example.com"}); err != nil {
		t.Fatal(err)
	}

	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--email", "maggie@example.com", "--clear"}); err != nil {
		t.Fatal(err)
	}
	if hash := readPasswordHash(t, dbURL, "maggie@example.com"); hash != "" {
		t.Fatalf("expected the password to be cleared, got %q", hash)
	}
}

func TestAdminUserSetPasswordRejectsBadInput(t *testing.T) {
	dataDir, dbURL, _ := newPasswordCLIFixture(t)

	pipeStdin(t, "short")
	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--email", "maggie@example.com"}); err == nil {
		t.Fatal("expected a too-short password to be rejected")
	}
	if hash := readPasswordHash(t, dbURL, "maggie@example.com"); hash != "" {
		t.Fatal("expected no hash to be written for a rejected password")
	}

	pipeStdin(t, "a good long password")
	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--email", "nobody@example.com"}); err == nil {
		t.Fatal("expected an unknown account to be rejected")
	}

	pipeStdin(t, "a good long password")
	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL}); err == nil {
		t.Fatal("expected a missing selector to be rejected")
	}

	pipeStdin(t, "a good long password")
	if err := adminUserSetPassword([]string{"--data", dataDir, "--db", dbURL, "--email", "maggie@example.com", "--user", "usr_x"}); err == nil {
		t.Fatal("expected two selectors to be rejected")
	}
}

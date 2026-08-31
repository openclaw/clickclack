package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestUserPasswordLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "Maggie@Example.com"})
	if err != nil {
		t.Fatal(err)
	}

	// An account with no password still resolves, with an empty hash, so the
	// HTTP layer can reject it the same way it rejects a wrong password.
	login, err := st.GetPasswordLogin(ctx, "maggie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if login.User.ID != user.ID || login.PasswordHash != "" {
		t.Fatalf("expected an enrolled-free login row, got %#v", login)
	}

	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$stored"); err != nil {
		t.Fatal(err)
	}
	if login, err = st.GetPasswordLogin(ctx, "maggie@example.com"); err != nil || login.PasswordHash != "$argon2id$stored" {
		t.Fatalf("expected the stored hash, got %#v err=%v", login, err)
	}

	// Setting a second time replaces rather than conflicting.
	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$replaced"); err != nil {
		t.Fatal(err)
	}
	if login, err = st.GetPasswordLogin(ctx, "maggie@example.com"); err != nil || login.PasswordHash != "$argon2id$replaced" {
		t.Fatalf("expected the replacement hash, got %#v err=%v", login, err)
	}

	if err := st.ClearUserPassword(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if login, err = st.GetPasswordLogin(ctx, "maggie@example.com"); err != nil || login.PasswordHash != "" {
		t.Fatalf("expected the password to be cleared, got %#v err=%v", login, err)
	}
	if err := st.ClearUserPassword(ctx, user.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected clearing twice to report no rows, got %v", err)
	}
}

func TestGetPasswordLoginIdentifierMatching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      user.ID,
		DisplayName: "Maggie",
		Handle:      "maggie",
	}); err != nil {
		t.Fatal(err)
	}

	for _, identifier := range []string{"maggie@example.com", "MAGGIE@EXAMPLE.COM", "maggie", "Maggie"} {
		login, err := st.GetPasswordLogin(ctx, identifier)
		if err != nil || login.User.ID != user.ID {
			t.Fatalf("expected %q to resolve to %s, got %#v err=%v", identifier, user.ID, login, err)
		}
	}
	for _, identifier := range []string{"", "   ", "nobody@example.com", "nobody"} {
		if _, err := st.GetPasswordLogin(ctx, identifier); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected %q to report no rows, got %v", identifier, err)
		}
	}
}

func TestGetPasswordLoginIgnoresBots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspaces[0].ID,
		CreatedBy:   owner.ID,
		DisplayName: "helper bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Bots authenticate with tokens; a password must never resolve to one.
	if _, err := st.GetPasswordLogin(ctx, bot.Handle); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected a bot handle to be unusable for password login, got %v", err)
	}
}

func TestRevokeSessionIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionUser(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionUser(ctx, session.Token); err == nil {
		t.Fatal("expected a revoked session to stop resolving")
	}
	if err := st.RevokeSession(ctx, session.Token); err != nil {
		t.Fatalf("expected repeat revocation to succeed, got %v", err)
	}
	if err := st.RevokeSession(ctx, "sst_never_issued"); err != nil {
		t.Fatalf("expected revoking an unknown token to succeed, got %v", err)
	}
}

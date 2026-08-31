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

func TestGetUserPasswordHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	// "No password on file" is a value, not a lookup failure: callers branch on
	// the empty string rather than on sql.ErrNoRows.
	hash, err := st.GetUserPasswordHash(ctx, user.ID)
	if err != nil || hash != "" {
		t.Fatalf("expected an empty hash for an unenrolled account, got %q err=%v", hash, err)
	}
	if hash, err = st.GetUserPasswordHash(ctx, "usr_never_created"); err != nil || hash != "" {
		t.Fatalf("expected an empty hash for an unknown account, got %q err=%v", hash, err)
	}

	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$stored"); err != nil {
		t.Fatal(err)
	}
	if hash, err = st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "$argon2id$stored" {
		t.Fatalf("expected the stored hash, got %q err=%v", hash, err)
	}
	if err := st.ClearUserPassword(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if hash, err = st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "" {
		t.Fatalf("expected the cleared hash to read empty, got %q err=%v", hash, err)
	}
}

func TestRevokeOtherUserSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Ari", Email: "ari@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var revoked []store.Session
	for i := 0; i < 2; i++ {
		session, err := st.CreateSession(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		revoked = append(revoked, session)
	}
	bystander, err := st.CreateSession(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}

	count, err := st.RevokeOtherUserSessions(ctx, user.ID, kept.Token)
	if err != nil || count != 2 {
		t.Fatalf("expected two sessions revoked, got %d err=%v", count, err)
	}
	if _, err := st.GetSessionUser(ctx, kept.Token); err != nil {
		t.Fatalf("expected the kept session to survive, got %v", err)
	}
	for _, session := range revoked {
		if _, err := st.GetSessionUser(ctx, session.Token); err == nil {
			t.Fatal("expected the other sessions to stop resolving")
		}
	}
	// One account's password change must never sign another account out.
	if _, err := st.GetSessionUser(ctx, bystander.Token); err != nil {
		t.Fatalf("expected another account's session to be untouched, got %v", err)
	}

	// Already-revoked sessions are not counted twice.
	if count, err = st.RevokeOtherUserSessions(ctx, user.ID, kept.Token); err != nil || count != 0 {
		t.Fatalf("expected a repeat revocation to touch nothing, got %d err=%v", count, err)
	}
	// A caller that cannot name its own session revokes every session, which is
	// the safe direction.
	if count, err = st.RevokeOtherUserSessions(ctx, user.ID, ""); err != nil || count != 1 {
		t.Fatalf("expected an empty keep token to revoke the last session, got %d err=%v", count, err)
	}
	if _, err := st.GetSessionUser(ctx, kept.Token); err == nil {
		t.Fatal("expected an empty keep token to revoke every session")
	}
	if _, err := st.RevokeOtherUserSessions(ctx, "  ", kept.Token); err == nil {
		t.Fatal("expected a missing user id to be rejected")
	}
}

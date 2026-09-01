package passwordtest

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type fixture struct {
	store.Store
	db *sql.DB
}

func Run(t *testing.T, open func(*testing.T) (store.Store, *sql.DB)) {
	t.Helper()
	for _, tc := range []struct {
		name string
		run  func(*testing.T, fixture)
	}{
		{"UserPasswordLifecycle", testUserPasswordLifecycle},
		{"GetPasswordLoginIdentifierMatching", testGetPasswordLoginIdentifierMatching},
		{"GetPasswordLoginIgnoresBots", testGetPasswordLoginIgnoresBots},
		{"RevokeSessionIsIdempotent", testRevokeSessionIsIdempotent},
		{"GetUserPasswordHash", testGetUserPasswordHash},
		{"CreateSessionForVerifiedPassword", testCreateSessionForVerifiedPassword},
		{"ChangeUserPasswordReplacesAndRevokesInOneCommit", testChangeUserPasswordReplacesAndRevokesInOneCommit},
		{"ChangeUserPasswordRefusesStaleVerificationsAndDeadSessions", testChangeUserPasswordRefusesStaleVerificationsAndDeadSessions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, db := open(t)
			tc.run(t, fixture{st, db})
		})
	}
}

func testUserPasswordLifecycle(t *testing.T, st fixture) {
	ctx := context.Background()

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

func testGetPasswordLoginIdentifierMatching(t *testing.T, st fixture) {
	ctx := context.Background()

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

func testGetPasswordLoginIgnoresBots(t *testing.T, st fixture) {
	ctx := context.Background()

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

func testRevokeSessionIsIdempotent(t *testing.T, st fixture) {
	ctx := context.Background()

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

func testGetUserPasswordHash(t *testing.T, st fixture) {
	ctx := context.Background()

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

func testCreateSessionForVerifiedPassword(t *testing.T, st fixture) {
	ctx := context.Background()

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$stored"); err != nil {
		t.Fatal(err)
	}

	session, err := st.CreateSessionForVerifiedPassword(ctx, user.ID, "$argon2id$stored")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionUser(ctx, session.Token); err != nil {
		t.Fatalf("expected the minted session to resolve, got %v", err)
	}

	// A password change that lands between verification and this commit must
	// leave the caller holding nothing, however good its secret used to be.
	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$rotated"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSessionForVerifiedPassword(ctx, user.ID, "$argon2id$stored"); !errors.Is(err, store.ErrPasswordVerificationStale) {
		t.Fatalf("expected a stale verification to be refused, got %v", err)
	}
	if got := countUserSessions(t, st, user.ID); got != 1 {
		t.Fatalf("expected the refused login to write no session, got %d sessions", got)
	}
	if err := st.ClearUserPassword(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSessionForVerifiedPassword(ctx, user.ID, "$argon2id$rotated"); !errors.Is(err, store.ErrPasswordVerificationStale) {
		t.Fatalf("expected a cleared credential to reject its verified hash, got %v", err)
	}
	if got := countUserSessions(t, st, user.ID); got != 1 {
		t.Fatalf("cleared credential created a session: got %d sessions", got)
	}

	// An account with no password on file has nothing to compare against.
	unenrolled, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Ari", Email: "ari@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSessionForVerifiedPassword(ctx, unenrolled.ID, "$argon2id$stored"); !errors.Is(err, store.ErrPasswordVerificationStale) {
		t.Fatalf("expected an unenrolled account to be refused, got %v", err)
	}
	if _, err := st.CreateSessionForVerifiedPassword(ctx, "  ", "$argon2id$stored"); err == nil {
		t.Fatal("expected a missing user id to be rejected")
	}
	if _, err := st.CreateSessionForVerifiedPassword(ctx, user.ID, "  "); err == nil {
		t.Fatal("expected a missing verified hash to be rejected")
	}
}

func testChangeUserPasswordReplacesAndRevokesInOneCommit(t *testing.T, st fixture) {
	ctx := context.Background()

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Ari", Email: "ari@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$stored"); err != nil {
		t.Fatal(err)
	}
	kept, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var elsewhere []store.Session
	for i := 0; i < 2; i++ {
		session, err := st.CreateSession(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		elsewhere = append(elsewhere, session)
	}
	bystander, err := st.CreateSession(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}

	count, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
		UserID:           user.ID,
		VerifiedHash:     "$argon2id$stored",
		NewHash:          "$argon2id$replaced",
		KeepSessionToken: kept.Token,
	})
	if err != nil || count != 2 {
		t.Fatalf("expected two sessions revoked, got %d err=%v", count, err)
	}
	if hash, err := st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "$argon2id$replaced" {
		t.Fatalf("expected the replacement hash, got %q err=%v", hash, err)
	}
	if _, err := st.GetSessionUser(ctx, kept.Token); err != nil {
		t.Fatalf("expected the kept session to survive, got %v", err)
	}
	for _, session := range elsewhere {
		if _, err := st.GetSessionUser(ctx, session.Token); err == nil {
			t.Fatal("expected the other sessions to stop resolving")
		}
	}
	// One account's password change must never sign another account out.
	if _, err := st.GetSessionUser(ctx, bystander.Token); err != nil {
		t.Fatalf("expected another account's session to be untouched, got %v", err)
	}

	// A caller that cannot name its own session revokes every session, which is
	// the safe direction.
	count, err = st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
		UserID:           user.ID,
		VerifiedHash:     "$argon2id$replaced",
		NewHash:          "$argon2id$third",
		KeepSessionToken: "",
	})
	if err != nil || count != 1 {
		t.Fatalf("expected an empty keep token to revoke the last session, got %d err=%v", count, err)
	}
	if _, err := st.GetSessionUser(ctx, kept.Token); err == nil {
		t.Fatal("expected an empty keep token to revoke every session")
	}
	if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
		UserID:       "  ",
		VerifiedHash: "$argon2id$third",
		NewHash:      "$argon2id$fourth",
	}); err == nil {
		t.Fatal("expected a missing user id to be rejected")
	}
}

func testChangeUserPasswordRefusesStaleVerificationsAndDeadSessions(t *testing.T, st fixture) {
	ctx := context.Background()

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Maggie", Email: "maggie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, user.ID, "$argon2id$stored"); err != nil {
		t.Fatal(err)
	}
	caller, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The hash moved on since it was verified: the rotation loses the race, and
	// loses all of it. Nothing is written, so the winner's password stands and
	// the winner's other sessions are still live.
	if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
		UserID:           user.ID,
		VerifiedHash:     "$argon2id$verified-a-while-ago",
		NewHash:          "$argon2id$replaced",
		KeepSessionToken: caller.Token,
	}); !errors.Is(err, store.ErrPasswordVerificationStale) {
		t.Fatalf("expected a stale verification to be refused, got %v", err)
	}
	if hash, err := st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "$argon2id$stored" {
		t.Fatalf("expected the stored hash to be untouched, got %q err=%v", hash, err)
	}
	if _, err := st.GetSessionUser(ctx, elsewhere.Token); err != nil {
		t.Fatalf("expected a refused change to revoke nothing, got %v", err)
	}

	// The caller's own session died while it was working: a change it can no
	// longer authenticate must not commit either.
	if err := st.RevokeSession(ctx, caller.Token); err != nil {
		t.Fatal(err)
	}
	for _, verified := range []string{"$argon2id$stored", "$argon2id$stale"} {
		if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID: user.ID, VerifiedHash: verified, NewHash: "$argon2id$replaced", KeepSessionToken: caller.Token,
		}); !errors.Is(err, store.ErrSessionRevoked) {
			t.Fatalf("expected a revoked session to take precedence over credential staleness, got %v", err)
		}
	}
	if hash, err := st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "$argon2id$stored" {
		t.Fatalf("expected the stored hash to be untouched, got %q err=%v", hash, err)
	}
	if _, err := st.GetSessionUser(ctx, elsewhere.Token); err != nil {
		t.Fatalf("expected a refused change to revoke nothing, got %v", err)
	}

	// Unknown tokens and another account's token cannot authorize this change.
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := st.CreateSession(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"sst_never_issued", foreign.Token} {
		if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID: user.ID, VerifiedHash: "$argon2id$stored", NewHash: "$argon2id$replaced", KeepSessionToken: token,
		}); !errors.Is(err, store.ErrSessionRevoked) {
			t.Fatalf("expected an invalid caller session to be refused, got %v", err)
		}
		if hash, err := st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "$argon2id$stored" {
			t.Fatalf("invalid caller changed password: hash=%q err=%v", hash, err)
		}
		for _, session := range []store.Session{elsewhere, foreign} {
			if _, err := st.GetSessionUser(ctx, session.Token); err != nil {
				t.Fatalf("invalid caller revoked a session: %v", err)
			}
		}
	}

	// Expired and malformed expiries must also roll the credential write back.
	expired, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, expiry := range []string{time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), "invalid"} {
		if _, err := st.db.ExecContext(ctx, `UPDATE sessions SET expires_at = $1 WHERE id = $2`, expiry, expired.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetSessionUser(ctx, expired.Token); !errors.Is(err, store.ErrSessionExpired) {
			t.Fatalf("expected expired session classification for %q, got %v", expiry, err)
		}
		if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID: user.ID, VerifiedHash: "$argon2id$stored", NewHash: "$argon2id$replaced", KeepSessionToken: expired.Token,
		}); !errors.Is(err, store.ErrSessionRevoked) {
			t.Fatalf("expected expiry %q to be refused, got %v", expiry, err)
		}
		if hash, err := st.GetUserPasswordHash(ctx, user.ID); err != nil || hash != "$argon2id$stored" {
			t.Fatalf("expected the stored hash to be untouched, got %q err=%v", hash, err)
		}
		if _, err := st.GetSessionUser(ctx, elsewhere.Token); err != nil {
			t.Fatalf("invalid expiry revoked another session: %v", err)
		}
	}
}

func countUserSessions(t *testing.T, st fixture, userID string) int {
	t.Helper()
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

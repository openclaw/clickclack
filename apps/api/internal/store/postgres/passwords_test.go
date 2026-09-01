package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/passwordtest"
)

func TestPasswordStoreContract(t *testing.T) {
	passwordtest.Run(t, func(t *testing.T) (store.Store, *sql.DB) {
		st := newIsolatedPostgresTestStore(t)
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		return st, st.db
	})
}

func TestPasswordLoginCannotSurviveConcurrentRotation(t *testing.T) {
	st, user, caller, other := newPostgresPasswordFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Pause rotation at revocation, after its credential update. The users
	// row holds an unlocked login at its FK check until rotation commits.
	revocationGate := mustBeginPostgresTx(t, ctx, st.db)
	lockPasswordTestRow(t, ctx, revocationGate, `SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, other.ID)
	loginGate := mustBeginPostgresTx(t, ctx, st.db)
	lockPasswordTestRow(t, ctx, loginGate, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, user.ID)
	rotation := startPostgresPasswordChange(ctx, st, user.ID, caller.Token, "rotated")
	waitForBlockedPostgresQuery(t, ctx, st.db, "RevokeUserSessionsExceptTokenHash")
	login := make(chan passwordLoginResult, 1)
	go func() {
		session, err := st.CreateSessionForVerifiedPassword(ctx, user.ID, "original")
		login <- passwordLoginResult{session, err}
	}()
	waitForBlockedPostgresQuery(t, ctx, st.db, "InsertSessionForVerifiedPassword")
	if err := revocationGate.Commit(); err != nil {
		t.Fatal(err)
	}
	if result := awaitPasswordResult(t, ctx, rotation); result.err != nil || result.revoked != 1 {
		t.Fatalf("rotation: revoked=%d err=%v", result.revoked, result.err)
	}
	if err := loginGate.Commit(); err != nil {
		t.Fatal(err)
	}
	result := awaitPasswordResult(t, ctx, login)
	if result.err == nil {
		_, sessionErr := st.GetSessionUser(ctx, result.session.Token)
		t.Fatalf("old-password login committed after rotation: session still usable=%t (lookup err=%v)", sessionErr == nil, sessionErr)
	}
	if !errors.Is(result.err, store.ErrPasswordVerificationStale) || result.session != (store.Session{}) {
		t.Fatalf("expected stale verification without a session, got %#v", result)
	}
	assertPostgresPasswordState(t, ctx, st, user.ID, "rotated", 2)
	if _, err := st.GetSessionUser(ctx, caller.Token); err != nil {
		t.Fatalf("rotation lost its caller session: %v", err)
	}
	if _, err := st.GetSessionUser(ctx, other.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rotation left another session usable: %v", err)
	}
}

func TestPasswordRotationRejectsConcurrentLogout(t *testing.T) {
	st, user, caller, other := newPostgresPasswordFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	gate := mustBeginPostgresTx(t, ctx, st.db)
	lockPasswordTestRow(t, ctx, gate, `SELECT user_id FROM user_passwords WHERE user_id = $1 FOR UPDATE`, user.ID)
	rotation := startPostgresPasswordChange(ctx, st, user.ID, caller.Token, "rotated")
	waitForBlockedPostgresQuery(t, ctx, st.db, "ReplaceVerifiedUserPassword")
	if err := st.RevokeSession(ctx, caller.Token); err != nil {
		t.Fatal(err)
	}
	if err := gate.Commit(); err != nil {
		t.Fatal(err)
	}
	result := awaitPasswordResult(t, ctx, rotation)
	if !errors.Is(result.err, store.ErrSessionRevoked) || result.revoked != 0 {
		t.Errorf("rotation committed after caller logout: revoked=%d err=%v", result.revoked, result.err)
	}
	assertPostgresPasswordState(t, ctx, st, user.ID, "original", 2)
	if _, err := st.GetSessionUser(ctx, other.Token); err != nil {
		t.Fatalf("rejected rotation revoked another session: %v", err)
	}
}

func TestPasswordRotationHoldsCallerUntilCommit(t *testing.T) {
	st, user, caller, other := newPostgresPasswordFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	gate := mustBeginPostgresTx(t, ctx, st.db)
	lockPasswordTestRow(t, ctx, gate, `SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, other.ID)
	rotation := startPostgresPasswordChange(ctx, st, user.ID, caller.Token, "rotated")
	waitForBlockedPostgresQuery(t, ctx, st.db, "RevokeUserSessionsExceptTokenHash")
	logout := make(chan error, 1)
	go func() { logout <- st.RevokeSession(ctx, caller.Token) }()
	waitForPasswordLogoutBlocked(t, ctx, st.db, logout)
	if err := gate.Commit(); err != nil {
		t.Fatal(err)
	}
	if result := awaitPasswordResult(t, ctx, rotation); result.err != nil || result.revoked != 1 {
		t.Fatalf("rotation: revoked=%d err=%v", result.revoked, result.err)
	}
	if err := awaitPasswordResult(t, ctx, logout); err != nil {
		t.Fatalf("logout after rotation: %v", err)
	}
	assertPostgresPasswordState(t, ctx, st, user.ID, "rotated", 2)
	for _, session := range []store.Session{caller, other} {
		if _, err := st.GetSessionUser(ctx, session.Token); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("session survived rotation followed by logout: %v", err)
		}
	}
}

func TestPasswordRotationsFromTwoDevicesDoNotDeadlock(t *testing.T) {
	st, user, caller, other := newPostgresPasswordFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Allow caller row reads/locks, but pause the first session UPDATE. A
	// session-first rotation on the other device would create a lock cycle.
	gate := mustBeginPostgresTx(t, ctx, st.db)
	if _, err := gate.ExecContext(ctx, `LOCK TABLE sessions IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	first := startPostgresPasswordChange(ctx, st, user.ID, caller.Token, "first")
	waitForBlockedPostgresQuery(t, ctx, st.db, "RevokeUserSessionsExceptTokenHash")
	second := startPostgresPasswordChange(ctx, st, user.ID, other.Token, "second")
	waitForBlockedPostgresQuery(t, ctx, st.db, "ReplaceVerifiedUserPassword")
	if err := gate.Commit(); err != nil {
		t.Fatal(err)
	}
	if result := awaitPasswordResult(t, ctx, first); result.err != nil || result.revoked != 1 {
		t.Fatalf("first rotation: revoked=%d err=%v", result.revoked, result.err)
	}
	if result := awaitPasswordResult(t, ctx, second); !errors.Is(result.err, store.ErrSessionRevoked) || result.revoked != 0 {
		t.Fatalf("second rotation must lose without deadlock: revoked=%d err=%v", result.revoked, result.err)
	}
	assertPostgresPasswordState(t, ctx, st, user.ID, "first", 2)
	if _, err := st.GetSessionUser(ctx, caller.Token); err != nil {
		t.Fatalf("losing rotation revoked the winner: %v", err)
	}
	if _, err := st.GetSessionUser(ctx, other.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("losing device retained its session: %v", err)
	}
}

type passwordChangeResult struct {
	revoked int64
	err     error
}

type passwordLoginResult struct {
	session store.Session
	err     error
}

func newPostgresPasswordFixture(t *testing.T) (*Store, store.User, store.Session, store.Session) {
	t.Helper()
	st := newIsolatedPostgresTestStore(t)
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Password Owner", Email: "password-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, user.ID, "original"); err != nil {
		t.Fatal(err)
	}
	caller, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return st, user, caller, other
}

func lockPasswordTestRow(t *testing.T, ctx context.Context, tx *sql.Tx, query, id string) {
	t.Helper()
	var locked string
	if err := tx.QueryRowContext(ctx, query, id).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	if locked != id {
		t.Fatalf("locked %q, wanted %q", locked, id)
	}
}

func startPostgresPasswordChange(ctx context.Context, st *Store, userID, token, newHash string) <-chan passwordChangeResult {
	result := make(chan passwordChangeResult, 1)
	go func() {
		revoked, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID: userID, VerifiedHash: "original", NewHash: newHash, KeepSessionToken: token,
		})
		result <- passwordChangeResult{revoked, err}
	}()
	return result
}

func awaitPasswordResult[T any](t *testing.T, ctx context.Context, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-ctx.Done():
		t.Fatalf("password operation did not complete: %v", ctx.Err())
		var zero T
		return zero
	}
}

func assertPostgresPasswordState(t *testing.T, ctx context.Context, st *Store, userID, hash string, sessions int) {
	t.Helper()
	if got, err := st.GetUserPasswordHash(ctx, userID); err != nil || got != hash {
		t.Errorf("stored password=%q, want %q; err=%v", got, hash, err)
	}
	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != sessions {
		t.Errorf("session rows=%d, want %d", count, sessions)
	}
}

func waitForPasswordLogoutBlocked(t *testing.T, ctx context.Context, db *sql.DB, result <-chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			t.Fatalf("logout finished before rotation committed: %v", err)
		case <-ctx.Done():
			t.Fatalf("logout did not reach its session lock: %v", ctx.Err())
		case <-ticker.C:
			var blocked bool
			if err := db.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity
					WHERE datname = current_database()
					  AND application_name = current_schema()
					  AND pid <> pg_backend_pid()
					  AND wait_event_type = 'Lock'
					  AND cardinality(pg_blocking_pids(pid)) > 0
					  AND position('RevokeSessionByTokenHash' in query) > 0
				)`).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				return
			}
		}
	}
}

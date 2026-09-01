package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/passwordauth"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// Argon2 verification runs outside the write it authorizes, which leaves a wide
// window between reading a credential and committing against it. These stores
// stand in for that window: the first read of the credential releases a
// competing change that commits before the paused request resumes, which is the
// same interleaving a debugger produces by pausing the handler at that line.
type racingPasswordLoginStore struct {
	store.Store
	once sync.Once
	race func()
}

func (s *racingPasswordLoginStore) GetPasswordLogin(ctx context.Context, identifier string) (store.PasswordLogin, error) {
	login, err := s.Store.GetPasswordLogin(ctx, identifier)
	if err == nil {
		s.once.Do(s.race)
	}
	return login, err
}

type racingPasswordHashStore struct {
	store.Store
	once sync.Once
	race func()
}

func (s *racingPasswordHashStore) GetUserPasswordHash(ctx context.Context, userID string) (string, error) {
	hash, err := s.Store.GetUserPasswordHash(ctx, userID)
	if err == nil && hash != "" {
		s.once.Do(s.race)
	}
	return hash, err
}

// hashFor is the operator-side path: a hash for a secret, as
// clickclack admin user set-password would store it.
func hashFor(t *testing.T, secret string) string {
	t.Helper()
	hash, err := passwordauth.Hash(t.Context(), secret)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// storedHash reads what is on file right now. Argon2 hashes carry a random
// salt, so a competing change has to commit against the stored string rather
// than against a freshly derived hash of the same secret.
func storedHash(t *testing.T, st store.Store, userID string) string {
	t.Helper()
	hash, err := st.GetUserPasswordHash(context.Background(), userID)
	if err != nil {
		t.Error(err)
	}
	return hash
}

func assertPasswordGuessBudgetAfterRace(t *testing.T, serverURL string, auth func(*http.Request)) {
	t.Helper()
	limit := passwordLoginIDLimit
	if auth != nil {
		limit = passwordChangeLimit
	}
	for i := 0; i <= limit; i++ {
		var resp *http.Response
		if auth == nil {
			resp, _ = passwordLogin(t, serverURL, "enrolled@example.com", "a wrong password after the race")
		} else {
			resp, _ = changePasswordRequest(t, serverURL, "a wrong password after the race", changedPasswordSecret, auth, nil)
		}
		want := http.StatusUnauthorized
		if i == limit {
			want = http.StatusTooManyRequests
		}
		if resp.StatusCode != want {
			t.Fatalf("guess %d after race: got HTTP %d, want %d with an intact budget", i, resp.StatusCode, want)
		}
	}
}

func TestPasswordLoginRefusesASessionForASupersededPassword(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	enrolled, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Enrolled", Email: "enrolled@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, enrolled.ID, hashFor(t, passwordTestSecret)); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateSession(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The owner changes the password while the login sits on its verification.
	racing := &racingPasswordLoginStore{Store: st}
	racing.race = func() {
		if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID:           enrolled.ID,
			VerifiedHash:     storedHash(t, st, enrolled.ID),
			NewHash:          hashFor(t, changedPasswordSecret),
			KeepSessionToken: owner.Token,
		}); err != nil {
			t.Error(err)
		}
	}
	server := newPasswordTestServerForStore(t, racing, true)

	resp, body := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a superseded password to be refused, got %d %s", resp.StatusCode, body)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatalf("expected no session cookie on the refused login, got %#v", resp.Cookies())
	}
	// Nothing about the account moved: the owner keeps the password they set and
	// the session they kept.
	hash, err := st.GetUserPasswordHash(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := passwordauth.Verify(t.Context(), hash, changedPasswordSecret); err != nil || !matched {
		t.Fatalf("expected the new password to stand, matched=%v err=%v", matched, err)
	}
	if _, err := st.GetSessionUser(ctx, owner.Token); err != nil {
		t.Fatalf("expected the owner's session to survive, got %v", err)
	}
	assertPasswordGuessBudgetAfterRace(t, server.URL, nil)
}

func TestChangePasswordLosesToACompetingChangeOnTheSameAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	enrolled, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Enrolled", Email: "enrolled@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, enrolled.ID, hashFor(t, passwordTestSecret)); err != nil {
		t.Fatal(err)
	}
	caller, err := st.CreateSession(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	const competingSecret = "the password that got there first"
	// The competing change keeps the calling session alive, so the compare on
	// the stored hash is the only thing standing between the paused request and
	// a second successful rotation.
	racing := &racingPasswordHashStore{Store: st}
	racing.race = func() {
		if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID:           enrolled.ID,
			VerifiedHash:     storedHash(t, st, enrolled.ID),
			NewHash:          hashFor(t, competingSecret),
			KeepSessionToken: caller.Token,
		}); err != nil {
			t.Error(err)
		}
	}
	server := newPasswordTestServerForStore(t, racing, true)

	resp, body := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withBearer(caller.Token), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 when the password moved under the request, got %d %s", resp.StatusCode, body)
	}
	hash, err := st.GetUserPasswordHash(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := passwordauth.Verify(t.Context(), hash, competingSecret); err != nil || !matched {
		t.Fatalf("expected the winning password to stand, matched=%v err=%v", matched, err)
	}
	assertPasswordGuessBudgetAfterRace(t, server.URL, withBearer(caller.Token))
}

func TestChangePasswordCannotCommitAfterItsOwnSessionIsRevoked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	enrolled, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Enrolled", Email: "enrolled@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, enrolled.ID, hashFor(t, passwordTestSecret)); err != nil {
		t.Fatal(err)
	}
	// Two devices: the owner's, and the one whose change is about to lose.
	owner, err := st.CreateSession(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	loser, err := st.CreateSession(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	racing := &racingPasswordHashStore{Store: st}
	racing.race = func() {
		// The owner's change revokes the losing device's session, which is exactly
		// what a password change is for.
		if _, err := st.ChangeUserPassword(ctx, store.ChangeUserPasswordInput{
			UserID:           enrolled.ID,
			VerifiedHash:     storedHash(t, st, enrolled.ID),
			NewHash:          hashFor(t, changedPasswordSecret),
			KeepSessionToken: owner.Token,
		}); err != nil {
			t.Error(err)
		}
	}
	server := newPasswordTestServerForStore(t, racing, true)

	const loserSecret = "what the revoked device wanted"
	resp, body := changePasswordRequest(t, server.URL, passwordTestSecret, loserSecret, withBearer(loser.Token), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after the caller's own session was revoked, got %d %s", resp.StatusCode, body)
	}
	// The owner's password stands, and the session the owner deliberately kept
	// is still signed in.
	hash, err := st.GetUserPasswordHash(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := passwordauth.Verify(t.Context(), hash, changedPasswordSecret); err != nil || !matched {
		t.Fatalf("expected the owner's password to stand, matched=%v err=%v", matched, err)
	}
	if matched, err := passwordauth.Verify(t.Context(), hash, loserSecret); err == nil && matched {
		t.Fatal("expected the revoked device's password never to be written")
	}
	if _, err := st.GetSessionUser(ctx, owner.Token); err != nil {
		t.Fatalf("expected the owner's retained session to survive, got %v", err)
	}
	if _, err := st.GetSessionUser(ctx, loser.Token); err == nil {
		t.Fatal("expected the revoked session to stay revoked")
	}
	assertPasswordGuessBudgetAfterRace(t, server.URL, withBearer(owner.Token))
}

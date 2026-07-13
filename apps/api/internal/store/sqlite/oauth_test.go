package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestOAuthTransactionsAndDesktopGrants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "OAuth User", Email: "oauth@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	transaction := store.OAuthTransaction{
		StateHash:          testOAuthHash("state"),
		BrowserBindingHash: testOAuthHash("binding"),
		Mode:               store.OAuthModeDesktop,
		PKCEVerifier:       "pkce-verifier",
		DesktopChallenge:   "desktop-challenge",
		DesktopProtocol:    2,
		CreatedAt:          now,
		ExpiresAt:          now.Add(10 * time.Minute),
	}
	if err := st.CreateOAuthTransaction(ctx, transaction); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, testOAuthHash("wrong-binding"), now); !errors.Is(err, store.ErrOAuthTransactionInvalid) {
		t.Fatalf("expected wrong binding rejection, got %v", err)
	}
	consumed, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, transaction.BrowserBindingHash, now)
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Mode != store.OAuthModeDesktop || consumed.PKCEVerifier != transaction.PKCEVerifier || consumed.DesktopChallenge != transaction.DesktopChallenge || consumed.DesktopProtocol != 2 {
		t.Fatalf("unexpected consumed transaction: %#v", consumed)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, transaction.BrowserBindingHash, now); !errors.Is(err, store.ErrOAuthTransactionInvalid) {
		t.Fatalf("expected transaction replay rejection, got %v", err)
	}

	expired := transaction
	expired.StateHash = testOAuthHash("expired-state")
	expired.ExpiresAt = now.Add(-time.Second)
	expired.CreatedAt = now.Add(-time.Minute)
	if err := st.CreateOAuthTransaction(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, expired.StateHash, expired.BrowserBindingHash, now); !errors.Is(err, store.ErrOAuthTransactionInvalid) {
		t.Fatalf("expected expired transaction rejection, got %v", err)
	}

	grant := store.DesktopOAuthGrant{
		GrantHash:        testOAuthHash("grant"),
		UserID:           user.ID,
		DesktopChallenge: "desktop-challenge",
		CreatedAt:        now,
		ExpiresAt:        now.Add(5 * time.Minute),
	}
	if err := st.CreateDesktopOAuthGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeDesktopOAuthGrant(ctx, grant.GrantHash, "wrong-challenge", now); !errors.Is(err, store.ErrDesktopOAuthGrantInvalid) {
		t.Fatalf("expected wrong challenge rejection, got %v", err)
	}
	var sessionsBefore int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessionsBefore); err != nil {
		t.Fatal(err)
	}
	if sessionsBefore != 0 {
		t.Fatalf("wrong challenge created %d sessions", sessionsBefore)
	}
	session, err := st.ConsumeDesktopOAuthGrant(ctx, grant.GrantHash, grant.DesktopChallenge, now)
	if err != nil {
		t.Fatal(err)
	}
	if session.UserID != user.ID || session.Token == "" {
		t.Fatalf("unexpected desktop session: %#v", session)
	}
	if _, err := st.GetSessionUser(ctx, session.Token); err != nil {
		t.Fatalf("created session did not authenticate: %v", err)
	}
	if _, err := st.ConsumeDesktopOAuthGrant(ctx, grant.GrantHash, grant.DesktopChallenge, now); !errors.Is(err, store.ErrDesktopOAuthGrantInvalid) {
		t.Fatalf("expected grant replay rejection, got %v", err)
	}
}

func TestOAuthStateSurvivesSQLiteRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	st, err := Open("sqlite://" + path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Restart User", Email: "restart@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	transaction := store.OAuthTransaction{
		StateHash:          testOAuthHash("restart-state"),
		BrowserBindingHash: testOAuthHash("restart-binding"),
		Mode:               store.OAuthModeBrowser,
		PKCEVerifier:       "restart-verifier",
		CreatedAt:          now,
		ExpiresAt:          now.Add(10 * time.Minute),
	}
	grant := store.DesktopOAuthGrant{
		GrantHash:        testOAuthHash("restart-grant"),
		UserID:           user.ID,
		DesktopChallenge: "restart-challenge",
		CreatedAt:        now,
		ExpiresAt:        now.Add(5 * time.Minute),
	}
	if err := st.CreateOAuthTransaction(ctx, transaction); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDesktopOAuthGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open("sqlite://" + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, transaction.BrowserBindingHash, now); err != nil {
		t.Fatalf("consume transaction after restart: %v", err)
	}
	if _, err := st.ConsumeDesktopOAuthGrant(ctx, grant.GrantHash, grant.DesktopChallenge, now); err != nil {
		t.Fatalf("consume grant after restart: %v", err)
	}
}

func testOAuthHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

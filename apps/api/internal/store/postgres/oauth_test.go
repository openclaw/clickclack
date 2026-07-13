package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestPostgresOAuthTransactionsAndDesktopGrants(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "OAuth User", Email: "oauth@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	transaction := store.OAuthTransaction{
		StateHash:          postgresOAuthHash("state"),
		BrowserBindingHash: postgresOAuthHash("binding"),
		Mode:               store.OAuthModeDesktop,
		PKCEVerifier:       "pkce-verifier",
		DesktopChallenge:   "desktop-challenge",
		CreatedAt:          now,
		ExpiresAt:          now.Add(10 * time.Minute),
	}
	if err := st.CreateOAuthTransaction(ctx, transaction); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, postgresOAuthHash("wrong-binding"), now); !errors.Is(err, store.ErrOAuthTransactionInvalid) {
		t.Fatalf("expected wrong binding rejection, got %v", err)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, transaction.BrowserBindingHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeOAuthTransaction(ctx, transaction.StateHash, transaction.BrowserBindingHash, now); !errors.Is(err, store.ErrOAuthTransactionInvalid) {
		t.Fatalf("expected transaction replay rejection, got %v", err)
	}

	grant := store.DesktopOAuthGrant{
		GrantHash:        postgresOAuthHash("grant"),
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
	session, err := st.ConsumeDesktopOAuthGrant(ctx, grant.GrantHash, grant.DesktopChallenge, now)
	if err != nil {
		t.Fatal(err)
	}
	if session.UserID != user.ID || session.Token == "" {
		t.Fatalf("unexpected desktop session: %#v", session)
	}
	if _, err := st.ConsumeDesktopOAuthGrant(ctx, grant.GrantHash, grant.DesktopChallenge, now); !errors.Is(err, store.ErrDesktopOAuthGrantInvalid) {
		t.Fatalf("expected grant replay rejection, got %v", err)
	}
}

func postgresOAuthHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

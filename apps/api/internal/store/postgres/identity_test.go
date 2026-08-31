package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/identitytest"
)

func TestUpsertIdentityUserConcurrentCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	gate := mustBeginPostgresTx(t, ctx, st.db)
	if _, err := gate.ExecContext(ctx, `LOCK TABLE users IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		user store.User
		err  error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			user, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{
				Provider: "github", ProviderSubject: "new-subject", Email: "new@example.com", DisplayName: "New User",
			})
			results <- result{user, err}
		}()
	}
	// Wait for both sign-ins to reach the held write or another sign-in's lock.
	// An absent-row SELECT alone cannot serialize these calls.
	for {
		var blocked int
		if err := st.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND application_name = current_schema()
			  AND wait_event_type = 'Lock'
			  AND cardinality(pg_blocking_pids(pid)) > 0`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked == 2 {
			t.Log("both first sign-ins are blocked before releasing the held users write")
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for two blocked sign-ins: %v (got %d)", ctx.Err(), blocked)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := gate.Commit(); err != nil {
		t.Fatal(err)
	}
	var userID string
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Errorf("concurrent first sign-in: %v", got.err)
			continue
		}
		if userID == "" {
			userID = got.user.ID
		}
		if got.user.ID != userID || got.user.DisplayName != "New User" || got.user.AvatarURL != store.ResolveAvatarURL("", "new@example.com") || got.user.NotificationSettings == nil {
			t.Errorf("unexpected first sign-in result: %#v; want user %s with hydrated settings", got.user, userID)
		}
	}
	var users, identities int
	if err := st.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM identities)`).Scan(&users, &identities); err != nil {
		t.Fatal(err)
	}
	if users != 1 || identities != 1 {
		t.Fatalf("created %d users and %d identities; want exactly one of each", users, identities)
	}
}

func TestIdentityUserPolicies(t *testing.T) {
	identitytest.Run(t, func(t *testing.T) store.Store {
		st := newIsolatedPostgresTestStore(t)
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		return st
	})
}

func TestUpsertIdentityUserWaitsForLateEmailFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	input := store.UpsertIdentityUserInput{Provider: "github", ProviderSubject: "late-email"}
	user, err := st.UpsertIdentityUser(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	// Hold an uncommitted email/fallback update, as another linked identity can do.
	writer := mustBeginPostgresTx(t, ctx, st.db)
	if _, err := writer.ExecContext(ctx, `UPDATE users SET avatar_url = $1 WHERE id = $2`, store.ResolveAvatarURL("", "late@example.com"), user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `UPDATE identities SET email = 'late@example.com' WHERE user_id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	input.AvatarURL = "https://example.com/provider.png"
	done := make(chan error, 1)
	go func() {
		_, err := st.UpsertIdentityUser(ctx, input)
		done <- err
	}()
	waitForBlockedPostgresQuery(t, ctx, st.db, "users")
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, err := st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarURL != input.AvatarURL {
		t.Fatalf("provider avatar lost to concurrently committed email fallback: %q", got.AvatarURL)
	}
}

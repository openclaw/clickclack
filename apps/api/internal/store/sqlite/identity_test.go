package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/identitytest"
)

func TestUpsertIdentityUserConcurrentCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "identity.db")
	const callers = 8
	stores := make([]*Store, callers)
	for i := range stores {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		stores[i] = st
		if i == 0 {
			if err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	type result struct {
		user store.User
		err  error
	}
	results := make(chan result, callers)
	start := make(chan struct{})
	for _, st := range stores {
		go func() {
			<-start
			user, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{
				Provider: "github", ProviderSubject: "new-subject", Email: "new@example.com", DisplayName: "New User",
			})
			results <- result{user, err}
		}()
	}
	close(start)
	var userID string
	for range callers {
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
	if err := stores[0].db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM identities)`).Scan(&users, &identities); err != nil {
		t.Fatal(err)
	}
	if users != 1 || identities != 1 {
		t.Fatalf("created %d users and %d identities; want exactly one of each", users, identities)
	}
}

func TestIdentityUserPolicies(t *testing.T) {
	identitytest.Run(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

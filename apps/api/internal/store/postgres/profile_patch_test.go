package postgres

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

func TestPatchUserMergesConcurrentPartialUpdatesAcrossStores(t *testing.T) {
	ctx := context.Background()
	first := newIsolatedPostgresTestStore(t)
	if err := first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second := &Store{db: first.db, q: storedb.New(first.db)}

	user, err := first.CreateUser(ctx, store.CreateUserInput{DisplayName: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	user, err = first.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      user.ID,
		DisplayName: user.DisplayName,
		Handle:      "original",
		AvatarURL:   "https://example.com/original.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	const pushoverKey = "u12345678901234567890123456789"
	if _, err := first.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          user.ID,
		PushoverUserKey: pushoverKey,
	}); err != nil {
		t.Fatal(err)
	}

	displayName := "Updated Name"
	enabled := true
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := first.PatchUser(ctx, store.PatchUserInput{
			UserID:      user.ID,
			DisplayName: &displayName,
		})
		results <- err
	}()
	go func() {
		<-start
		_, err := second.PatchUser(ctx, store.PatchUserInput{
			UserID: user.ID,
			NotificationSettings: &store.PatchNotificationSettingsInput{
				PushoverEnabled: &enabled,
			},
		})
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}

	updated, err := first.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != displayName || updated.Handle != user.Handle || updated.AvatarURL != user.AvatarURL {
		t.Fatalf("concurrent partial patches clobbered profile fields: %#v", updated)
	}
	if updated.NotificationSettings == nil ||
		!updated.NotificationSettings.PushoverEnabled ||
		updated.NotificationSettings.PushoverUserKey != pushoverKey {
		t.Fatalf("concurrent partial patches clobbered notification fields: %#v", updated.NotificationSettings)
	}
}

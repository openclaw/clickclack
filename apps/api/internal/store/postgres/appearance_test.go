package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestAppearancePreferencesLifecycle(t *testing.T) {
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	ctx := context.Background()
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	user, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Appearance User",
		Email:       "appearance-postgres-" + suffix + "@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	preferences, err := st.GetAppearancePreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences != nil {
		t.Fatalf("expected missing preferences, got %#v", preferences)
	}

	dark := "dark"
	iris := "iris"
	comfortable := "comfortable"
	account, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{
			ColorMode:  &dark,
			BoardTheme: &iris,
			Density:    &comfortable,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	preferences = account.AppearancePreferences
	if preferences == nil || preferences.ColorMode != "dark" || preferences.BoardTheme != "iris" || preferences.MessageLayout != "" || preferences.Density != "" {
		t.Fatalf("unexpected initial preferences: %#v", preferences)
	}

	standard := "standard"
	ember := "ember"
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{
			BoardTheme:    &ember,
			MessageLayout: &standard,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	preferences = account.AppearancePreferences
	if preferences == nil || preferences.ColorMode != "dark" || preferences.BoardTheme != "ember" || preferences.MessageLayout != "" || preferences.Density != "" {
		t.Fatalf("partial update replaced unrelated preferences: %#v", preferences)
	}

	invalid := "roomy"
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{Density: &invalid},
	}); err == nil {
		t.Fatal("expected invalid density to fail")
	}
	preferences, err = st.GetAppearancePreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences == nil || preferences.ColorMode != "dark" || preferences.BoardTheme != "ember" {
		t.Fatalf("invalid update changed preferences: %#v", preferences)
	}

	beforeAccountUpdate, err := st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	changedName := "Should Not Persist"
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		DisplayName:           &changedName,
		AppearancePreferences: &store.AppearancePreferencesPatch{Density: &invalid},
	}); err == nil {
		t.Fatal("expected invalid appearance update to roll back profile changes")
	}
	afterFailedUpdate, err := st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailedUpdate.DisplayName != beforeAccountUpdate.DisplayName {
		t.Fatalf("failed appearance update changed profile: before=%#v after=%#v", beforeAccountUpdate, afterFailedUpdate)
	}

	newHandle := "@appearance-postgres"
	compact := "compact"
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		Handle:                &newHandle,
		AppearancePreferences: &store.AppearancePreferencesPatch{Density: &compact},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.User.DisplayName != beforeAccountUpdate.DisplayName || account.User.Handle != "appearance-postgres" {
		t.Fatalf("partial account update clobbered profile fields: %#v", account.User)
	}
	if account.AppearancePreferences == nil || account.AppearancePreferences.ColorMode != "dark" || account.AppearancePreferences.Density != "compact" {
		t.Fatalf("partial account update lost appearance fields: %#v", account.AppearancePreferences)
	}

	if _, err := st.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	preferences, err = st.GetAppearancePreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences != nil {
		t.Fatalf("user deletion did not cascade preferences: %#v", preferences)
	}
}

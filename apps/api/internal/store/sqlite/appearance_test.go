package sqlite

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestAppearancePreferencesLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Appearance User",
		Email:       "appearance-sqlite@example.com",
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

	account, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{},
	})
	if err != nil {
		t.Fatal(err)
	} else if account.AppearancePreferences != nil {
		t.Fatalf("empty patch created a row: %#v", account.AppearancePreferences)
	}

	system := "system"
	moss := "moss"
	compact := "compact"
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{
			ColorMode:  &system,
			BoardTheme: &moss,
			Density:    &compact,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	preferences = account.AppearancePreferences
	if preferences == nil || preferences.ColorMode != "" || preferences.BoardTheme != "moss" || preferences.MessageLayout != "" || preferences.Density != "compact" {
		t.Fatalf("unexpected initial preferences: %#v", preferences)
	}

	signal := "signal"
	outlined := "outlined"
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{
			BoardTheme:    &signal,
			MessageLayout: &outlined,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	preferences = account.AppearancePreferences
	if preferences == nil || preferences.ColorMode != "" || preferences.BoardTheme != "" || preferences.MessageLayout != "outlined" || preferences.Density != "compact" {
		t.Fatalf("partial update replaced unrelated preferences: %#v", preferences)
	}

	invalid := "sepia"
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{ColorMode: &invalid},
	}); err == nil {
		t.Fatal("expected invalid color mode to fail")
	}
	preferences, err = st.GetAppearancePreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences == nil || preferences.MessageLayout != "outlined" || preferences.Density != "compact" {
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
		AppearancePreferences: &store.AppearancePreferencesPatch{ColorMode: &invalid},
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

	newHandle := "@appearance-user"
	dark := "dark"
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		Handle:                &newHandle,
		AppearancePreferences: &store.AppearancePreferencesPatch{ColorMode: &dark},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.User.DisplayName != beforeAccountUpdate.DisplayName || account.User.Handle != "appearance-user" {
		t.Fatalf("partial account update clobbered profile fields: %#v", account.User)
	}
	if account.AppearancePreferences == nil || account.AppearancePreferences.ColorMode != "dark" || account.AppearancePreferences.Density != "compact" {
		t.Fatalf("partial account update lost appearance fields: %#v", account.AppearancePreferences)
	}

	if _, err := st.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, user.ID); err != nil {
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

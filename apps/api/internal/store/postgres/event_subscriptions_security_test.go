package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestAppEventSubscriptionUsesInstallationBotPrincipal(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Owner",
		Email:       "owner-app-subscription@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{
		Name: "App Subscription Security",
		Slug: "app-subscription-security",
	}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Callback Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	installation, err := st.CreateAppInstallation(ctx, store.CreateAppInstallationInput{
		WorkspaceID: workspace.ID,
		AppSlug:     "callback-app",
		BotUserID:   bot.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	appSubscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID:       workspace.ID,
		AppInstallationID: installation.ID,
		EventTypes:        []string{"message.created"},
		CallbackURL:       "https://app.example.com/events",
		CreatedBy:         owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if appSubscription.CreatedBy != owner.ID {
		t.Fatalf("app subscription creator = %q, want owner %q", appSubscription.CreatedBy, owner.ID)
	}
	userSubscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID: workspace.ID,
		EventTypes:  []string{"message.created"},
		CallbackURL: "https://user.example.com/events",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if userSubscription.CreatedBy != owner.ID {
		t.Fatalf("user subscription principal = %q, want creator %q", userSubscription.CreatedBy, owner.ID)
	}

	botEvent := store.Event{
		ID:               "evt_bot_private",
		Cursor:           "cur_bot_private",
		WorkspaceID:      workspace.ID,
		Type:             "message.created",
		RecipientUserIDs: []string{bot.ID},
	}
	got, err := st.ListEventSubscriptionsForEvent(ctx, botEvent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != appSubscription.ID {
		t.Fatalf("bot-private event subscriptions = %#v, want app subscription", got)
	}

	userEvent := botEvent
	userEvent.ID = "evt_user_private"
	userEvent.Cursor = "cur_user_private"
	userEvent.RecipientUserIDs = []string{owner.ID}
	got, err = st.ListEventSubscriptionsForEvent(ctx, userEvent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != userSubscription.ID {
		t.Fatalf("user-private event subscriptions = %#v, want user subscription", got)
	}

	if _, err := st.RevokeAppInstallation(ctx, installation.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	got, err = st.ListEventSubscriptionsForEvent(ctx, botEvent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("revoked app subscriptions = %#v, want none", got)
	}

	for i := 1; i < maxActiveEventSubscriptionsPerWorkspace; i++ {
		if _, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
			WorkspaceID: workspace.ID,
			EventTypes:  []string{"message.created"},
			CallbackURL: "https://quota.example.com/" + strings.Repeat("x", i),
			CreatedBy:   owner.ID,
		}); err != nil {
			t.Fatalf("create active subscription %d after app revocation: %v", i+1, err)
		}
	}
	if _, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID: workspace.ID,
		EventTypes:  []string{"message.created"},
		CallbackURL: "https://quota.example.com/overflow",
		CreatedBy:   owner.ID,
	}); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("expected active subscription quota error, got %v", err)
	}
}

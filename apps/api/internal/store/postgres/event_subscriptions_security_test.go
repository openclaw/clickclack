package postgres

import (
	"context"
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
	if appSubscription.CreatedBy != bot.ID {
		t.Fatalf("app subscription principal = %q, want bot %q", appSubscription.CreatedBy, bot.ID)
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
}

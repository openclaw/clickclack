package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestEventSubscriptionsRespectPrivateRecipientsAndQuota(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-subscriptions@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-subscriptions@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "outsider-subscriptions@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{member.ID, outsider.ID} {
		if err := st.AddWorkspaceMember(ctx, workspace.ID, userID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
	}

	subscriptions := make(map[string]store.EventSubscription)
	for name, userID := range map[string]string{"owner": owner.ID, "member": member.ID, "outsider": outsider.ID} {
		subscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
			WorkspaceID: workspace.ID,
			EventTypes:  []string{"message.created"},
			CallbackURL: "https://" + name + ".example.com/events",
			CreatedBy:   userID,
		})
		if err != nil {
			t.Fatal(err)
		}
		subscriptions[name] = subscription
	}

	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, event, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "private callback payload",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.ListEventSubscriptionsForEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two DM-recipient subscriptions, got %#v", got)
	}
	for _, subscription := range got {
		if subscription.ID == subscriptions["outsider"].ID {
			t.Fatalf("private DM event selected outsider subscription: %#v", subscription)
		}
	}

	for i := len(subscriptions); i < maxActiveEventSubscriptionsPerWorkspace; i++ {
		if _, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
			WorkspaceID: workspace.ID,
			EventTypes:  []string{"message.created"},
			CallbackURL: "https://quota.example.com/" + strings.Repeat("x", i+1),
			CreatedBy:   owner.ID,
		}); err != nil {
			t.Fatalf("create subscription %d: %v", i+1, err)
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
	if _, err := st.RevokeEventSubscription(ctx, subscriptions["outsider"].ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID: workspace.ID,
		EventTypes:  []string{"message.created"},
		CallbackURL: "https://quota.example.com/replacement",
		CreatedBy:   owner.ID,
	}); err != nil {
		t.Fatalf("expected revoked subscription to free quota: %v", err)
	}
}

func TestAppEventSubscriptionUsesInstallationBotPrincipal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-app-subscription@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
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

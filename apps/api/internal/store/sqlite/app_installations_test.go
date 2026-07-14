package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestRevokeAppInstallationCascadesAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "installation-cascade@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	bot, initialToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Installation Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	first := createTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, "cascade-defaults")
	createTestInstallationRegistrations(t, st, first.ID, workspace.ID, bot.ID, owner.ID, "/cascade-defaults")
	result, err := st.RevokeAppInstallation(ctx, first.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Installation.RevokedAt == nil || result.Revoked.SlashCommands != 1 || result.Revoked.EventSubscriptions != 1 || result.Revoked.BotTokens != 0 {
		t.Fatalf("unexpected default cascade result: %#v", result)
	}
	if _, err := st.GetBotTokenAuth(ctx, initialToken.Token); err != nil {
		t.Fatalf("default cascade revoked the bot token: %v", err)
	}
	repeated, err := st.RevokeAppInstallation(ctx, first.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
		RevokeBotTokens:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Revoked != (store.AppInstallationRevokedCounts{}) {
		t.Fatalf("expected repeated revoke counts to be zero, got %#v", repeated.Revoked)
	}

	second := createTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, "cascade-tokens")
	createTestInstallationRegistrations(t, st, second.ID, workspace.ID, bot.ID, owner.ID, "/cascade-tokens")
	secondToken, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "second",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = st.RevokeAppInstallation(ctx, second.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
		RevokeBotTokens:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revoked.SlashCommands != 1 || result.Revoked.EventSubscriptions != 1 || result.Revoked.BotTokens != 2 {
		t.Fatalf("unexpected explicit cascade result: %#v", result)
	}
	for _, token := range []string{initialToken.Token, secondToken.Token} {
		if _, err := st.GetBotTokenAuth(ctx, token); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected cascaded bot token to stop authenticating, got %v", err)
		}
	}

	third := createTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, "cascade-rollback")
	command, subscription := createTestInstallationRegistrations(t, st, third.ID, workspace.ID, bot.ID, owner.ID, "/cascade-rollback")
	rollbackToken, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "rollback",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		CREATE TRIGGER fail_event_subscription_revoke
		BEFORE UPDATE OF revoked_at ON event_subscriptions
		WHEN OLD.revoked_at IS NULL
		BEGIN
			SELECT RAISE(ABORT, 'induced cascade failure');
		END`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeAppInstallation(ctx, third.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
		RevokeBotTokens:          true,
	}); err == nil {
		t.Fatal("expected induced cascade failure")
	}
	installationAfterFailure, err := st.getAppInstallation(ctx, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	commandAfterFailure, err := st.getSlashCommand(ctx, command.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionAfterFailure, err := st.getEventSubscription(ctx, subscription.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if installationAfterFailure.RevokedAt != nil || commandAfterFailure.RevokedAt != nil || subscriptionAfterFailure.RevokedAt != nil {
		t.Fatalf("cascade failure did not roll back all registrations: installation=%#v command=%#v subscription=%#v", installationAfterFailure, commandAfterFailure, subscriptionAfterFailure)
	}
	if _, err := st.GetBotTokenAuth(ctx, rollbackToken.Token); err != nil {
		t.Fatalf("cascade failure revoked the bot token: %v", err)
	}

	botOwner, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "User Bot Owner",
		Email:       "installation-user-bot-owner@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, botOwner.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	userBot, userBotToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: botOwner.ID,
		DisplayName: "User-owned Installation Bot",
		CreatedBy:   botOwner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	userBotInstallation := createTestAppInstallation(t, st, workspace.ID, userBot.ID, owner.ID, "cascade-user-owned")
	if _, err := st.RevokeAppInstallation(ctx, userBotInstallation.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeBotTokens: true,
	}); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("expected user-owned token cascade to require the bot owner, got %v", err)
	}
	installationAfterDeniedCascade, err := st.getAppInstallation(ctx, userBotInstallation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if installationAfterDeniedCascade.RevokedAt != nil {
		t.Fatalf("denied user-owned token cascade revoked the installation: %#v", installationAfterDeniedCascade)
	}
	if _, err := st.GetBotTokenAuth(ctx, userBotToken.Token); err != nil {
		t.Fatalf("denied user-owned token cascade revoked the token: %v", err)
	}
}

func createTestAppInstallation(t *testing.T, st *Store, workspaceID, botUserID, ownerID, appSlug string) store.AppInstallation {
	t.Helper()
	installation, err := st.CreateAppInstallation(context.Background(), store.CreateAppInstallationInput{
		WorkspaceID: workspaceID,
		AppSlug:     appSlug,
		BotUserID:   botUserID,
		CreatedBy:   ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return installation
}

func createTestInstallationRegistrations(t *testing.T, st *Store, installationID, workspaceID, botUserID, ownerID, commandName string) (store.SlashCommand, store.EventSubscription) {
	t.Helper()
	ctx := context.Background()
	command, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID:       workspaceID,
		AppInstallationID: installationID,
		Command:           commandName,
		CallbackURL:       "https://example.com/slash",
		BotUserID:         botUserID,
		CreatedBy:         ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID:       workspaceID,
		AppInstallationID: installationID,
		EventTypes:        []string{"message.created"},
		CallbackURL:       "https://example.com/events",
		CreatedBy:         ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return command, subscription
}

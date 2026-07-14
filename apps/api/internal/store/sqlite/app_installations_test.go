package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestIntegrationSetupNoncesAreRetrySafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "integration-retry@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]

	botInput := store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Retry Bot",
		Handle:      "retry-bot",
		TokenName:   "setup",
		Scopes:      []string{"bot:write"},
		SetupNonce:  "bot-setup-nonce-0001",
		CreatedBy:   owner.ID,
	}
	bot, firstToken, err := st.CreateBot(ctx, botInput)
	if err != nil {
		t.Fatal(err)
	}
	legacyScopes := make([]string, 0, len(firstToken.Scopes))
	for _, scope := range firstToken.Scopes {
		if scope != store.BotCommandsWriteScope {
			legacyScopes = append(legacyScopes, scope)
		}
	}
	legacyScopesJSON, err := json.Marshal(legacyScopes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE bot_tokens SET scopes_json = ? WHERE id = ?`, legacyScopesJSON, firstToken.ID); err != nil {
		t.Fatal(err)
	}
	replayedBot, replayedToken, err := st.CreateBot(ctx, botInput)
	if err != nil {
		t.Fatal(err)
	}
	if replayedBot.ID != bot.ID || replayedToken.ID != firstToken.ID {
		t.Fatalf("setup replay created duplicate bot credentials: first=%#v/%#v replay=%#v/%#v", bot, firstToken, replayedBot, replayedToken)
	}
	if replayedToken.Token == firstToken.Token {
		t.Fatal("setup replay did not replace the response-lost raw token")
	}
	if _, err := st.GetBotTokenAuth(ctx, firstToken.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old setup token still authenticates after replay: %v", err)
	}
	if _, err := st.GetBotTokenAuth(ctx, replayedToken.Token); err != nil {
		t.Fatalf("replacement setup token does not authenticate: %v", err)
	}
	conflictingBotInput := botInput
	conflictingBotInput.DisplayName = "Different Retry Bot"
	if _, _, err := st.CreateBot(ctx, conflictingBotInput); !errors.Is(err, store.ErrSetupNonceConflict) {
		t.Fatalf("expected changed bot replay to conflict, got %v", err)
	}

	tokenInput := store.CreateBotTokenInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "secondary",
		Scopes:      []string{"bot:admin"},
		SetupNonce:  "token-setup-nonce-0001",
		CreatedBy:   owner.ID,
	}
	secondToken, err := st.CreateBotToken(ctx, tokenInput)
	if err != nil {
		t.Fatal(err)
	}
	secondaryLegacyScopes := make([]string, 0, len(secondToken.Scopes))
	for _, scope := range secondToken.Scopes {
		if scope != store.BotCommandsWriteScope {
			secondaryLegacyScopes = append(secondaryLegacyScopes, scope)
		}
	}
	secondaryLegacyScopesJSON, err := json.Marshal(secondaryLegacyScopes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE bot_tokens SET scopes_json = ? WHERE id = ?`, secondaryLegacyScopesJSON, secondToken.ID); err != nil {
		t.Fatal(err)
	}
	replayedSecondToken, err := st.CreateBotToken(ctx, tokenInput)
	if err != nil {
		t.Fatal(err)
	}
	if replayedSecondToken.ID != secondToken.ID || replayedSecondToken.Token == secondToken.Token {
		t.Fatalf("token setup replay was not stable: first=%#v replay=%#v", secondToken, replayedSecondToken)
	}
	if _, err := st.GetBotTokenAuth(ctx, secondToken.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old secondary token still authenticates after replay: %v", err)
	}

	installationInput := store.CreateAppInstallationInput{
		WorkspaceID: workspace.ID,
		AppSlug:     "openclaw",
		DisplayName: "Retry installation",
		BotUserID:   bot.ID,
		Config:      map[string]any{"default_to": "channel:general"},
		SetupNonce:  "installation-setup-nonce-0001",
		CreatedBy:   owner.ID,
	}
	installation, err := st.CreateAppInstallation(ctx, installationInput)
	if err != nil {
		t.Fatal(err)
	}
	replayedInstallation, err := st.CreateAppInstallation(ctx, installationInput)
	if err != nil {
		t.Fatal(err)
	}
	if replayedInstallation.ID != installation.ID {
		t.Fatalf("installation setup replay created a duplicate: first=%#v replay=%#v", installation, replayedInstallation)
	}
	conflictingInstallationInput := installationInput
	conflictingInstallationInput.AppSlug = "custom"
	if _, err := st.CreateAppInstallation(ctx, conflictingInstallationInput); !errors.Is(err, store.ErrSetupNonceConflict) {
		t.Fatalf("expected changed installation replay to conflict, got %v", err)
	}

	installations, err := st.ListAppInstallations(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(installations) != 1 || len(tokens) != 2 {
		t.Fatalf("setup retries left duplicate rows: installations=%d tokens=%d", len(installations), len(tokens))
	}
}

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
	sameApp, err := st.CreateAppInstallation(ctx, store.CreateAppInstallationInput{
		WorkspaceID: workspace.ID,
		AppSlug:     first.AppSlug,
		BotUserID:   bot.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatalf("create second installation for same app slug: %v", err)
	}
	if sameApp.ID == first.ID {
		t.Fatal("expected separate installations for the same app slug")
	}
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

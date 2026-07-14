package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestPostgresIntegrationLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Integration Owner", "postgres-integrations@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Integration Member", Email: "postgres-integration-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, initialToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Integration Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAppInstallation(ctx, store.CreateAppInstallationInput{
		WorkspaceID: workspace.ID,
		AppSlug:     "member-blocked",
		BotUserID:   bot.ID,
		CreatedBy:   member.ID,
	}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member installation create to require manager, got %v", err)
	}

	first := createPostgresTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, "postgres-defaults")
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
	command, subscription := createPostgresTestInstallationRegistrations(t, st, first.ID, workspace.ID, bot.ID, owner.ID, "/postgres-defaults")
	if _, err := st.RevokeAppInstallation(ctx, first.ID, member.ID, store.RevokeAppInstallationOptions{}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member installation revoke to require manager, got %v", err)
	}
	if _, err := st.RotateSlashCommandSecret(ctx, command.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member slash rotation to require manager, got %v", err)
	}
	rotatedCommand, err := st.RotateSlashCommandSecret(ctx, command.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotatedCommand.ID != command.ID || rotatedCommand.SigningSecret == "" || rotatedCommand.SigningSecret == command.SigningSecret {
		t.Fatalf("unexpected slash rotation: before=%#v after=%#v", command, rotatedCommand)
	}
	if _, err := st.RotateEventSubscriptionSecret(ctx, subscription.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member subscription rotation to require manager, got %v", err)
	}
	rotatedSubscription, err := st.RotateEventSubscriptionSecret(ctx, subscription.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotatedSubscription.ID != subscription.ID || rotatedSubscription.SigningSecret == "" || rotatedSubscription.SigningSecret == subscription.SigningSecret {
		t.Fatalf("unexpected subscription rotation: before=%#v after=%#v", subscription, rotatedSubscription)
	}
	publicMatches, err := st.ListEventSubscriptionsForEvent(ctx, store.Event{
		ID:          "evt_postgres_public",
		Cursor:      "cur_postgres_public",
		Type:        "message.created",
		WorkspaceID: workspace.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(publicMatches) != 1 || publicMatches[0].ID != subscription.ID {
		t.Fatalf("expected public event to match subscription, got %#v", publicMatches)
	}
	privateMatches, err := st.ListEventSubscriptionsForEvent(ctx, store.Event{
		ID:               "evt_postgres_private",
		Cursor:           "cur_postgres_private",
		Type:             "message.created",
		WorkspaceID:      workspace.ID,
		RecipientUserIDs: []string{owner.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(privateMatches) != 0 {
		t.Fatalf("expected recipient-scoped event to match no workspace subscriptions, got %#v", privateMatches)
	}

	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Name:        "integration-deliveries",
		Kind:        "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "delivery",
	})
	if err != nil {
		t.Fatal(err)
	}
	attemptIDs := make([]string, 0, 3)
	for range 3 {
		attempt, err := st.CreateEventDeliveryAttempt(ctx, store.CreateEventDeliveryAttemptInput{
			SubscriptionID: subscription.ID,
			EventID:        event.ID,
			WorkspaceID:    workspace.ID,
			EventType:      event.Type,
			ResponseStatus: 202,
		})
		if err != nil {
			t.Fatal(err)
		}
		attemptIDs = append(attemptIDs, attempt.ID)
	}
	if _, err := st.db.ExecContext(ctx, `
		UPDATE event_delivery_attempts
		SET created_at = '2026-07-14T12:00:00Z'
		WHERE subscription_id = $1`, subscription.ID); err != nil {
		t.Fatal(err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(attemptIDs)))
	firstPage, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, member.ID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, member.ID, 2, firstPage[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != attemptIDs[0] || firstPage[1].ID != attemptIDs[1] || len(secondPage) != 1 || secondPage[0].ID != attemptIDs[2] {
		t.Fatalf("unexpected postgres delivery pages: first=%#v second=%#v", firstPage, secondPage)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM event_delivery_attempts WHERE id = $1`, firstPage[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListEventDeliveryAttempts(ctx, subscription.ID, member.ID, 2, firstPage[1].ID); !errors.Is(err, store.ErrInvalidEventDeliveryCursor) {
		t.Fatalf("expected deleted postgres delivery cursor to be rejected, got %v", err)
	}

	if _, err := st.CreateConnectedAccount(ctx, store.CreateConnectedAccountInput{
		WorkspaceID:       workspace.ID,
		UserID:            member.ID,
		Provider:          "github",
		ProviderAccountID: "member-blocked",
		CreatedBy:         member.ID,
	}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member connected-account create to require manager, got %v", err)
	}
	memberAccount, err := st.CreateConnectedAccount(ctx, store.CreateConnectedAccountInput{
		WorkspaceID:       workspace.ID,
		UserID:            member.ID,
		Provider:          "github",
		ProviderAccountID: "member-self",
		CreatedBy:         owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeConnectedAccount(ctx, memberAccount.ID, member.ID); err != nil {
		t.Fatalf("member could not self-revoke connected account: %v", err)
	}
	ownerAccount, err := st.CreateConnectedAccount(ctx, store.CreateConnectedAccountInput{
		WorkspaceID:       workspace.ID,
		UserID:            owner.ID,
		Provider:          "github",
		ProviderAccountID: "owner-account",
		CreatedBy:         owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeConnectedAccount(ctx, ownerAccount.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member cross-user revoke to require manager, got %v", err)
	}
	if _, err := st.RevokeConnectedAccount(ctx, ownerAccount.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	result, err := st.RevokeAppInstallation(ctx, first.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revoked.SlashCommands != 1 || result.Revoked.EventSubscriptions != 1 || result.Revoked.BotTokens != 0 {
		t.Fatalf("unexpected default postgres cascade: %#v", result)
	}
	if _, err := st.RotateSlashCommandSecret(ctx, command.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected revoked slash command rotation to check manager authority first, got %v", err)
	}
	if _, err := st.RotateEventSubscriptionSecret(ctx, subscription.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected revoked event subscription rotation to check manager authority first, got %v", err)
	}
	if _, err := st.GetBotTokenAuth(ctx, initialToken.Token); err != nil {
		t.Fatalf("default postgres cascade revoked the bot token: %v", err)
	}

	second := createPostgresTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, "postgres-tokens")
	createPostgresTestInstallationRegistrations(t, st, second.ID, workspace.ID, bot.ID, owner.ID, "/postgres-tokens")
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
		t.Fatalf("unexpected explicit postgres cascade: %#v", result)
	}
	for _, token := range []string{initialToken.Token, secondToken.Token} {
		if _, err := st.GetBotTokenAuth(ctx, token); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected postgres cascade token to stop authenticating, got %v", err)
		}
	}

	third := createPostgresTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, "postgres-rollback")
	rollbackCommand, rollbackSubscription := createPostgresTestInstallationRegistrations(t, st, third.ID, workspace.ID, bot.ID, owner.ID, "/postgres-rollback")
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
		CREATE FUNCTION fail_event_subscription_revoke() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'induced cascade failure';
		END;
		$$;
		CREATE TRIGGER fail_event_subscription_revoke
		BEFORE UPDATE OF revoked_at ON event_subscriptions
		FOR EACH ROW
		WHEN (OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL)
		EXECUTE FUNCTION fail_event_subscription_revoke()`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeAppInstallation(ctx, third.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeSlashCommands:      true,
		RevokeEventSubscriptions: true,
		RevokeBotTokens:          true,
	}); err == nil {
		t.Fatal("expected induced postgres cascade failure")
	}
	installationAfterFailure, err := st.getAppInstallation(ctx, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	commandAfterFailure, err := st.getSlashCommand(ctx, rollbackCommand.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionAfterFailure, err := st.getEventSubscription(ctx, rollbackSubscription.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if installationAfterFailure.RevokedAt != nil || commandAfterFailure.RevokedAt != nil || subscriptionAfterFailure.RevokedAt != nil {
		t.Fatalf("postgres cascade failure did not roll back registrations: installation=%#v command=%#v subscription=%#v", installationAfterFailure, commandAfterFailure, subscriptionAfterFailure)
	}
	if _, err := st.GetBotTokenAuth(ctx, rollbackToken.Token); err != nil {
		t.Fatalf("postgres cascade failure revoked the bot token: %v", err)
	}

	userBot, userBotToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: member.ID,
		DisplayName: "Postgres User-owned Installation Bot",
		CreatedBy:   member.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	userBotInstallation := createPostgresTestAppInstallation(t, st, workspace.ID, userBot.ID, owner.ID, "postgres-user-owned")
	if _, err := st.RevokeAppInstallation(ctx, userBotInstallation.ID, owner.ID, store.RevokeAppInstallationOptions{
		RevokeBotTokens: true,
	}); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("expected postgres user-owned token cascade to require the bot owner, got %v", err)
	}
	installationAfterDeniedCascade, err := st.getAppInstallation(ctx, userBotInstallation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if installationAfterDeniedCascade.RevokedAt != nil {
		t.Fatalf("denied postgres user-owned token cascade revoked the installation: %#v", installationAfterDeniedCascade)
	}
	if _, err := st.GetBotTokenAuth(ctx, userBotToken.Token); err != nil {
		t.Fatalf("denied postgres user-owned token cascade revoked the token: %v", err)
	}
}

func TestPostgresRegistrationCreationSerializesWithInstallationRevoke(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Integration Race Owner", "postgres-integrations-race@example.com")
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
		DisplayName: "Integration Race Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		appSlug    string
		create     func(string) error
		activeRows func(string) int
	}{
		{
			name:    "slash command",
			appSlug: "postgres-race-command",
			create: func(installationID string) error {
				_, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
					WorkspaceID:       workspace.ID,
					AppInstallationID: installationID,
					Command:           "/postgres-race",
					CallbackURL:       "https://example.com/slash",
					BotUserID:         bot.ID,
					CreatedBy:         owner.ID,
				})
				return err
			},
			activeRows: func(installationID string) int {
				var count int
				if err := st.db.QueryRowContext(ctx, `
					SELECT COUNT(*)
					FROM slash_commands
					WHERE app_installation_id = $1 AND revoked_at IS NULL`, installationID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				return count
			},
		},
		{
			name:    "event subscription",
			appSlug: "postgres-race-subscription",
			create: func(installationID string) error {
				_, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
					WorkspaceID:       workspace.ID,
					AppInstallationID: installationID,
					EventTypes:        []string{"message.created"},
					CallbackURL:       "https://example.com/events",
					CreatedBy:         owner.ID,
				})
				return err
			},
			activeRows: func(installationID string) int {
				var count int
				if err := st.db.QueryRowContext(ctx, `
					SELECT COUNT(*)
					FROM event_subscriptions
					WHERE app_installation_id = $1 AND revoked_at IS NULL`, installationID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				return count
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installation := createPostgresTestAppInstallation(t, st, workspace.ID, bot.ID, owner.ID, tt.appSlug)
			revokeTx, err := st.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer revokeTx.Rollback()
			var one int
			if err := revokeTx.QueryRowContext(ctx, `
				SELECT 1
				FROM app_installations
				WHERE id = $1
				FOR UPDATE`, installation.ID).Scan(&one); err != nil {
				t.Fatal(err)
			}

			createResult := make(chan error, 1)
			go func() {
				createResult <- tt.create(installation.ID)
			}()
			waitForBlockedInstallationRegistration(t, ctx, st.db)

			revokedAt := now()
			if _, err := revokeTx.ExecContext(ctx, `
				UPDATE app_installations
				SET revoked_at = $1
				WHERE id = $2`, revokedAt, installation.ID); err != nil {
				t.Fatal(err)
			}
			if err := revokeTx.Commit(); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-createResult:
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("registration creation after installation revoke returned %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("registration creation did not resume after installation revoke")
			}
			if count := tt.activeRows(installation.ID); count != 0 {
				t.Fatalf("installation revoke left %d active registrations", count)
			}
		})
	}
}

func waitForBlockedInstallationRegistration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
				  AND cardinality(pg_blocking_pids(pid)) > 0
				  AND position('FROM app_installations' in query) > 0
				  AND position('FOR KEY SHARE' in query) > 0
			)`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("registration creation did not wait for the installation revoke lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func createPostgresTestAppInstallation(t *testing.T, st *Store, workspaceID, botUserID, ownerID, appSlug string) store.AppInstallation {
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

func createPostgresTestInstallationRegistrations(t *testing.T, st *Store, installationID, workspaceID, botUserID, ownerID, commandName string) (store.SlashCommand, store.EventSubscription) {
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

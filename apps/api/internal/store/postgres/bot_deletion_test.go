package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestPostgresDeleteBotReleasesHandleAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "postgres-bot-delete-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]
	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Reusable Bot",
		Handle:      "postgres-reusable-bot",
		SetupNonce:  "postgres-delete-bot-setup-0001",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	botMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  bot.ID,
		Body:      "postgres historical bot message",
	})
	if err != nil {
		t.Fatal(err)
	}
	quotedMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID:       channel.ID,
		AuthorID:        owner.ID,
		Body:            "postgres quoted history",
		QuotedMessageID: &botMessage.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{bot.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	directMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: direct.ID,
		AuthorID:       bot.ID,
		Body:           "postgres historical direct message",
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := st.DeleteBot(ctx, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != bot.ID || deleted.FormerHandle != bot.Handle || deleted.DeletedAt == "" {
		t.Fatalf("unexpected deleted bot: %#v", deleted)
	}
	if _, err := st.GetBotTokenAuth(ctx, token.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted bot token still authenticates: %v", err)
	}
	assertPostgresDeletedMessageAuthor(t, st, ctx, botMessage.ID, owner.ID, bot.ID, bot.Handle)
	assertPostgresDeletedMessageAuthor(t, st, ctx, directMessage.ID, owner.ID, bot.ID, bot.Handle)
	reloadedQuote, err := st.GetMessage(ctx, quotedMessage.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedQuote.QuotedAuthor == nil ||
		reloadedQuote.QuotedAuthor.ID != bot.ID ||
		reloadedQuote.QuotedAuthor.Handle != "" ||
		reloadedQuote.QuotedAuthor.FormerHandle != bot.Handle ||
		reloadedQuote.QuotedAuthor.DeletedAt == nil {
		t.Fatalf("quoted author did not preserve deleted bot identity: %#v", reloadedQuote.QuotedAuthor)
	}
	searchResults, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "postgres historical bot", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(searchResults) != 1 || searchResults[0].Message.Author == nil ||
		searchResults[0].Message.Author.FormerHandle != bot.Handle ||
		searchResults[0].Message.Author.DeletedAt == nil {
		t.Fatalf("search did not preserve deleted bot identity: %#v", searchResults)
	}
	reloadedDirect, err := st.GetDirectConversation(ctx, direct.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var deletedMember *store.User
	for i := range reloadedDirect.Members {
		if reloadedDirect.Members[i].ID == bot.ID {
			deletedMember = &reloadedDirect.Members[i]
			break
		}
	}
	if deletedMember == nil || deletedMember.Handle != "" ||
		deletedMember.FormerHandle != bot.Handle || deletedMember.DeletedAt == nil {
		t.Fatalf("direct conversation did not preserve deleted bot identity: %#v", reloadedDirect.Members)
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: bot.DisplayName,
		Handle:      bot.Handle,
		SetupNonce:  "postgres-delete-bot-setup-0001",
		CreatedBy:   owner.ID,
	}); !errors.Is(err, store.ErrSetupNonceConflict) {
		t.Fatalf("deleted bot setup nonce should not revive its identity: %v", err)
	}

	replacement, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Replacement Bot",
		Handle:      bot.Handle,
		SetupNonce:  "postgres-delete-bot-setup-0002",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == bot.ID || replacement.Handle != bot.Handle ||
		replacement.FormerHandle != "" || replacement.DeletedAt != nil {
		t.Fatalf("replacement inherited deleted identity state: %#v", replacement)
	}
}

func TestPostgresRevokeInstallationCanDeleteBotAndReleaseHandle(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "postgres-installation-delete-owner@example.com")
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
		DisplayName: "Installation Bot",
		Handle:      "postgres-installation-delete-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	installation, err := st.CreateAppInstallation(ctx, store.CreateAppInstallationInput{
		WorkspaceID: workspace.ID,
		AppSlug:     "delete-with-bot",
		DisplayName: "Delete with bot",
		BotUserID:   bot.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := st.RevokeAppInstallation(ctx, installation.ID, owner.ID, store.RevokeAppInstallationOptions{
		DeleteBot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedBot == nil || result.DeletedBot.ID != bot.ID ||
		result.DeletedBot.FormerHandle != bot.Handle || result.Installation.RevokedAt == nil {
		t.Fatalf("integration uninstall did not delete its bot: %#v", result)
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Replacement Installation Bot",
		Handle:      bot.Handle,
		CreatedBy:   owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
}

func assertPostgresDeletedMessageAuthor(t *testing.T, st *Store, ctx context.Context, messageID, readerID, botID, formerHandle string) {
	t.Helper()
	message, err := st.GetMessage(ctx, messageID, readerID)
	if err != nil {
		t.Fatal(err)
	}
	if message.Author == nil ||
		message.Author.ID != botID ||
		message.Author.Handle != "" ||
		message.Author.FormerHandle != formerHandle ||
		message.Author.DeletedAt == nil {
		t.Fatalf("message did not preserve deleted bot identity: %#v", message.Author)
	}
}

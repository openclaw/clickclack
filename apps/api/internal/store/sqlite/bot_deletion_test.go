package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestDeleteBotReleasesHandleAndPreservesHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "bot-delete-owner@example.com")
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
		Handle:      "reusable-bot",
		SetupNonce:  "delete-bot-setup-0001",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	botMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  bot.ID,
		Body:      "historical bot message",
	})
	if err != nil {
		t.Fatal(err)
	}
	quotedMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID:       channel.ID,
		AuthorID:        owner.ID,
		Body:            "quoted history",
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
		Body:           "historical direct message",
	})
	if err != nil {
		t.Fatal(err)
	}
	activePeer, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Active Peer",
		Email:       "bot-delete-active-peer@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, activePeer.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	groupDirect, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{bot.ID, activePeer.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	groupDirectMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: groupDirect.ID,
		AuthorID:       bot.ID,
		Body:           "historical group direct message",
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
	listed, err := st.ListBots(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("deleted bot remains in workspace list: %#v", listed)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, bot.ID, store.WorkspaceRoleBot); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted bot was reattached to the workspace: %v", err)
	}

	assertDeletedMessageAuthor(t, st, ctx, botMessage.ID, owner.ID, bot.ID, bot.Handle)
	assertDeletedMessageAuthor(t, st, ctx, directMessage.ID, owner.ID, bot.ID, bot.Handle)
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
	searchResults, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "historical bot", 10)
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
	directList, err := st.ListDirectConversations(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertDeletedBotInDirectList(t, directList, direct.ID, bot.ID, bot.Handle)
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: direct.ID,
		AuthorID:       owner.ID,
		Body:           "no recipient",
	}); !errors.Is(err, store.ErrDirectConversationNoActivePeer) {
		t.Fatalf("one-to-one DM with deleted bot remained writable: %v", err)
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: directMessage.ID,
		AuthorID:      owner.ID,
		Body:          "no thread recipient",
	}); !errors.Is(err, store.ErrDirectConversationNoActivePeer) {
		t.Fatalf("one-to-one DM thread with deleted bot remained writable: %v", err)
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: groupDirect.ID,
		AuthorID:       owner.ID,
		Body:           "active group recipient",
	}); err != nil {
		t.Fatalf("group DM with an active peer became unwritable: %v", err)
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: groupDirectMessage.ID,
		AuthorID:      owner.ID,
		Body:          "active group thread recipient",
	}); err != nil {
		t.Fatalf("group DM thread with an active peer became unwritable: %v", err)
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: bot.DisplayName,
		Handle:      bot.Handle,
		SetupNonce:  "delete-bot-setup-0001",
		CreatedBy:   owner.ID,
	}); !errors.Is(err, store.ErrSetupNonceConflict) {
		t.Fatalf("deleted bot setup nonce should not revive its identity: %v", err)
	}

	replacement, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Replacement Bot",
		Handle:      bot.Handle,
		SetupNonce:  "delete-bot-setup-0002",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == bot.ID || replacement.Handle != bot.Handle ||
		replacement.FormerHandle != "" || replacement.DeletedAt != nil {
		t.Fatalf("replacement inherited deleted identity state: %#v", replacement)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, owner.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("repeated deletion should not mutate the tombstone: %v", err)
	}
}

func TestDeleteBotWithoutHandle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "handleless-bot-delete-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspaces[0].ID,
		DisplayName: "Handleless Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bot.Handle != "" {
		t.Fatalf("expected handleless bot, got %#v", bot)
	}

	deleted, err := st.DeleteBot(ctx, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != bot.ID || deleted.FormerHandle != "" || deleted.DeletedAt == "" {
		t.Fatalf("unexpected deleted bot: %#v", deleted)
	}
	if _, err := st.GetBotTokenAuth(ctx, token.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted handleless bot token still authenticates: %v", err)
	}
}

func TestWorkspaceDeletionRetiresExclusiveServiceBots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "workspace-bot-delete-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	deletedWorkspace := workspaces[0]
	survivingWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Surviving"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	exclusiveBot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: deletedWorkspace.ID,
		DisplayName: "Exclusive Service Bot",
		Handle:      "exclusive-workspace-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	sharedBot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: deletedWorkspace.ID,
		DisplayName: "Shared Service Bot",
		Handle:      "shared-workspace-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, survivingWorkspace.ID, sharedBot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}

	if _, err := st.DeleteWorkspace(ctx, deletedWorkspace.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	retired, err := st.GetUser(ctx, exclusiveBot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Handle != "" || retired.FormerHandle != exclusiveBot.Handle || retired.DeletedAt == nil {
		t.Fatalf("workspace deletion did not retire its exclusive service bot: %#v", retired)
	}
	replacement, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: survivingWorkspace.ID,
		DisplayName: "Replacement Service Bot",
		Handle:      exclusiveBot.Handle,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == exclusiveBot.ID || replacement.DeletedAt != nil {
		t.Fatalf("expected a new active bot identity to reuse the released handle: %#v", replacement)
	}
	stillShared, err := st.GetUser(ctx, sharedBot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillShared.Handle != sharedBot.Handle || stillShared.DeletedAt != nil {
		t.Fatalf("workspace deletion retired a bot still used elsewhere: %#v", stillShared)
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: survivingWorkspace.ID,
		DisplayName: "Conflicting Shared Bot",
		Handle:      sharedBot.Handle,
		CreatedBy:   owner.ID,
	}); err == nil {
		t.Fatal("expected a shared active bot to retain its handle")
	}
}

func TestRemoveBotFromWorkspaceMarksDirectConversationReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "bot-membership-dm-owner@example.com")
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
		DisplayName: "Membership Bot",
		Handle:      "membership-dm-bot",
		CreatedBy:   owner.ID,
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
	if !direct.CanSend {
		t.Fatalf("new bot DM was marked read-only: %#v", direct)
	}
	root, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: direct.ID,
		AuthorID:       owner.ID,
		Body:           "before membership removal",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := st.GetDirectConversation(ctx, direct.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CanSend {
		t.Fatalf("bot DM remained writable after membership removal: %#v", reloaded)
	}
	listed, err := st.ListDirectConversations(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertDirectConversationReadOnly(t, listed, direct.ID)
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: direct.ID,
		AuthorID:       owner.ID,
		Body:           "after membership removal",
	}); !errors.Is(err, store.ErrDirectConversationNoActivePeer) {
		t.Fatalf("bot DM accepted a message after membership removal: %v", err)
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: root.ID,
		AuthorID:      owner.ID,
		Body:          "thread after membership removal",
	}); !errors.Is(err, store.ErrDirectConversationNoActivePeer) {
		t.Fatalf("bot DM thread accepted a reply after membership removal: %v", err)
	}
}

func TestDeleteServiceBotRequiresManagementAcrossEveryWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "bot-delete-shared-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := workspaces[0]
	second, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Second"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Manager",
		Email:       "bot-delete-shared-manager@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, first.ID, manager.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: first.ID,
		DisplayName: "Shared Bot",
		Handle:      "shared-delete-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, second.ID, bot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, manager.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("manager of only one workspace deleted a shared bot: %v", err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteServiceBotRequiresManagementForPreservedSubscriptionWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "bot-delete-subscription-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := workspaces[0]
	second, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Subscription Workspace"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	third, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Connected Account Workspace"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Manager",
		Email:       "bot-delete-subscription-manager@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, first.ID, manager.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, second.ID, manager.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: first.ID,
		DisplayName: "Subscription Bot",
		Handle:      "subscription-delete-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, second.ID, bot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, third.ID, bot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}
	installation, err := st.CreateAppInstallation(ctx, store.CreateAppInstallationInput{
		WorkspaceID: second.ID,
		AppSlug:     "preserved-subscription",
		DisplayName: "Preserved subscription",
		BotUserID:   bot.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := st.CreateEventSubscription(ctx, store.CreateEventSubscriptionInput{
		WorkspaceID:       second.ID,
		AppInstallationID: installation.ID,
		EventTypes:        []string{"message.created"},
		CallbackURL:       "https://example.com/events",
		CreatedBy:         owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateConnectedAccount(ctx, store.CreateConnectedAccountInput{
		WorkspaceID:       third.ID,
		UserID:            bot.ID,
		Provider:          "github",
		ProviderAccountID: "bot-connected-account",
		DisplayName:       "Bot Connected Account",
		CreatedBy:         owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeAppInstallation(ctx, installation.ID, owner.ID, store.RevokeAppInstallationOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, second.ID, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, third.ID, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := st.DeleteBot(ctx, bot.ID, manager.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("manager without access to the connected-account workspace deleted the bot: %v", err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := st.getEventSubscription(ctx, subscription.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RevokedAt == nil {
		t.Fatalf("bot deletion left the preserved event subscription active: %#v", reloaded)
	}
	reloadedAccount, err := st.getConnectedAccount(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedAccount.RevokedAt == nil {
		t.Fatalf("bot deletion left the connected account active: %#v", reloadedAccount)
	}
}

func TestDeleteOrphanedServiceBotUsesHistoricalWorkspaceAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "bot-delete-orphan-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	manager, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Manager",
		Email:       "bot-delete-orphan-manager@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, manager.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Member",
		Email:       "bot-delete-orphan-member@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Orphaned Bot",
		Handle:      "orphaned-delete-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := st.DeleteBot(ctx, bot.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("ordinary member deleted an orphaned service bot: %v", err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, manager.ID); err != nil {
		t.Fatalf("historical workspace manager could not delete orphaned bot: %v", err)
	}
}

func TestDeleteUserOwnedBotRequiresItsOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	manager, err := st.EnsureBootstrap(ctx, "Manager", "owned-bot-delete-manager@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	botOwner, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Bot Owner",
		Email:       "owned-bot-delete-owner@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, botOwner.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: botOwner.ID,
		DisplayName: "Owned Bot",
		Handle:      "owned-delete-bot",
		CreatedBy:   botOwner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, manager.ID); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("workspace manager deleted another user's bot: %v", err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, botOwner.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRevokeInstallationCanDeleteBotAndReleaseHandle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "installation-delete-owner@example.com")
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
		Handle:      "installation-delete-bot",
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
	revoked, err := st.RevokeAppInstallation(ctx, installation.ID, owner.ID, store.RevokeAppInstallationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Installation.RevokedAt == nil {
		t.Fatalf("ordinary uninstall did not revoke the installation: %#v", revoked)
	}
	originalRevokedAt := *revoked.Installation.RevokedAt
	result, err := st.RevokeAppInstallation(ctx, installation.ID, owner.ID, store.RevokeAppInstallationOptions{
		DeleteBot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedBot == nil || result.DeletedBot.ID != bot.ID ||
		result.DeletedBot.FormerHandle != bot.Handle || result.Installation.RevokedAt == nil {
		t.Fatalf("later bot deletion did not retire the installation bot: %#v", result)
	}
	if *result.Installation.RevokedAt != originalRevokedAt {
		t.Fatalf("later bot deletion changed the installation revocation time: %#v", result)
	}
	replacement, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Replacement Installation Bot",
		Handle:      bot.Handle,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == bot.ID {
		t.Fatalf("replacement reused deleted bot id: %#v", replacement)
	}
}

func assertDeletedMessageAuthor(t *testing.T, st *Store, ctx context.Context, messageID, readerID, botID, formerHandle string) {
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

func assertDeletedBotInDirectList(t *testing.T, conversations []store.DirectConversation, conversationID, botID, formerHandle string) {
	t.Helper()
	for _, conversation := range conversations {
		if conversation.ID != conversationID {
			continue
		}
		for _, member := range conversation.Members {
			if member.ID == botID &&
				member.Handle == "" &&
				member.FormerHandle == formerHandle &&
				member.DeletedAt != nil {
				return
			}
		}
		t.Fatalf("direct conversation list did not preserve deleted bot identity: %#v", conversation.Members)
	}
	t.Fatalf("direct conversation %s missing from list", conversationID)
}

func assertDirectConversationReadOnly(t *testing.T, conversations []store.DirectConversation, conversationID string) {
	t.Helper()
	for _, conversation := range conversations {
		if conversation.ID == conversationID {
			if conversation.CanSend {
				t.Fatalf("direct conversation %q remained writable: %#v", conversationID, conversation)
			}
			return
		}
	}
	t.Fatalf("direct conversation %q was not listed: %#v", conversationID, conversations)
}

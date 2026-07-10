package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestBotManagementAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "moderator-bots@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, moderator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-bots@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	botOwner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bot Owner", Email: "owner-bots@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, botOwner.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Member Service", CreatedBy: member.ID}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member service create to require manager, got %v", err)
	}
	serviceBot, serviceToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Service Bot",
		TokenName:   "initial",
		CreatedBy:   moderator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: serviceBot.ID, Name: "member", CreatedBy: member.ID}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member service token create to require manager, got %v", err)
	}
	if _, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, serviceBot.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member service token list to require manager, got %v", err)
	}
	serviceRotation, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: serviceBot.ID, Name: "manager", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeBotToken(ctx, serviceRotation.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member service token revoke to require manager, got %v", err)
	}
	if _, err := st.RevokeBotToken(ctx, serviceRotation.ID, moderator.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, OwnerUserID: botOwner.ID, DisplayName: "Manager User Bot", CreatedBy: moderator.ID}); !errors.Is(err, store.ErrBotOwnerCreateRequired) {
		t.Fatalf("expected user-owned bot create to require owner caller, got %v", err)
	}
	userBot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: botOwner.ID,
		DisplayName: "User Bot",
		TokenName:   "initial",
		CreatedBy:   botOwner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, userBot.ID, moderator.ID); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("expected manager user-owned token list to require owner, got %v", err)
	}
	if _, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: userBot.ID, Name: "manager", CreatedBy: moderator.ID}); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("expected manager user-owned token create to require owner, got %v", err)
	}
	ownerRotation, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: userBot.ID, Name: "owner", CreatedBy: botOwner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeBotToken(ctx, ownerRotation.ID, moderator.ID); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("expected manager user-owned token revoke to require owner, got %v", err)
	}
	if _, err := st.RevokeBotToken(ctx, ownerRotation.ID, botOwner.ID); err != nil {
		t.Fatal(err)
	}
	managerBots, err := st.ListBots(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := listedBotTokenCount(managerBots, serviceBot.ID); got != 2 {
		t.Fatalf("expected manager to see service bot token metadata, got %d", got)
	}
	if got := listedBotTokenCount(managerBots, userBot.ID); got != 0 {
		t.Fatalf("expected manager not to see user-owned bot token metadata, got %d", got)
	}
	ownerBots, err := st.ListBots(ctx, workspace.ID, botOwner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := listedBotTokenCount(ownerBots, serviceBot.ID); got != 0 {
		t.Fatalf("expected plain member not to see service bot token metadata, got %d", got)
	}
	if got := listedBotTokenCount(ownerBots, userBot.ID); got != 2 {
		t.Fatalf("expected bot owner to see user-owned bot token metadata, got %d", got)
	}
	memberBots, err := st.ListBots(ctx, workspace.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := listedBotTokenCount(memberBots, serviceBot.ID); got != 0 {
		t.Fatalf("expected plain member service token list to be empty, got %d", got)
	}
	if got := listedBotTokenCount(memberBots, userBot.ID); got != 0 {
		t.Fatalf("expected non-owner user bot token list to be empty, got %d", got)
	}

	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, otherWorkspace.ID, botOwner.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, otherWorkspace.ID, userBot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: otherWorkspace.ID, BotUserID: userBot.ID, Name: "other workspace", CreatedBy: botOwner.ID}); err != nil {
		t.Fatal(err)
	}
	owned, err := st.ListBotsOwnedBy(ctx, botOwner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 2 {
		t.Fatalf("expected one owned-bot row per workspace install, got %#v", owned)
	}
	counts := map[string]int{}
	for _, entry := range owned {
		counts[entry.Workspace.ID] = entry.ActiveTokenCount
		if entry.Bot.ID != userBot.ID {
			t.Fatalf("unexpected owned bot row: %#v", entry)
		}
	}
	if counts[workspace.ID] != 1 || counts[otherWorkspace.ID] != 1 {
		t.Fatalf("unexpected active token counts by workspace: %#v", counts)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, botOwner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, userBot.ID, botOwner.ID); !errors.Is(err, store.ErrBotOwnerMembershipRequired) {
		t.Fatalf("expected stale owner token list to require workspace membership, got %v", err)
	}
	if _, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: userBot.ID, Name: "stale owner", CreatedBy: botOwner.ID}); !errors.Is(err, store.ErrBotOwnerMembershipRequired) {
		t.Fatalf("expected stale owner token create to require workspace membership, got %v", err)
	}
	if _, err := st.RevokeBotToken(ctx, ownerRotation.ID, botOwner.ID); !errors.Is(err, store.ErrBotOwnerMembershipRequired) {
		t.Fatalf("expected stale owner token revoke to require workspace membership, got %v", err)
	}
	owned, err = st.ListBotsOwnedBy(ctx, botOwner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 1 || owned[0].Workspace.ID != otherWorkspace.ID {
		t.Fatalf("expected owned bots to hide stale workspace membership, got %#v", owned)
	}

	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, serviceBot.ID, member.ID); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member bot removal to require manager, got %v", err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, serviceBot.ID, moderator.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetBotTokenAuth(ctx, serviceToken.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected removed service bot token to stop authenticating, got %v", err)
	}
}

func listedBotTokenCount(bots []store.BotWithTokens, botUserID string) int {
	for _, bot := range bots {
		if bot.Bot.ID == botUserID {
			return len(bot.Tokens)
		}
	}
	return -1
}

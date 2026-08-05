package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSlashCommandGuestBudgetIndexMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0041_slash_command_guest_budget_index.sql")

	indexCount := func() int {
		t.Helper()
		var count int
		if err := st.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM sqlite_master
			WHERE type = 'index' AND name = 'idx_slash_command_invocations_guest_budget'`,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := indexCount(); got != 0 {
		t.Fatalf("guest budget index existed before its migration: %d", got)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if got := indexCount(); got != 1 {
		t.Fatalf("guest budget migration did not create its index: %d", got)
	}
	var indexColumns string
	if err := st.db.QueryRowContext(ctx, `
		SELECT group_concat(name, ',')
		FROM (SELECT name FROM pragma_index_info('idx_slash_command_invocations_guest_budget') ORDER BY seqno)`,
	).Scan(&indexColumns); err != nil {
		t.Fatal(err)
	}
	if indexColumns != "workspace_id,user_id,channel_id,created_at" {
		t.Fatalf("unexpected guest budget index columns: %s", indexColumns)
	}
}

func TestSlashCommandInvocationRequiresChannelWriteAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "slash-authz-moderator@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "slash-authz-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "slash-authz-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generalChannelID, guestChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "general":
			generalChannelID = channel.ID
		case "guest":
			guestChannelID = channel.ID
		}
	}
	if generalChannelID == "" || guestChannelID == "" {
		t.Fatalf("expected general and guest channels, got %#v", channels)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Slash Bot",
		CreatedBy:   moderator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	command, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID: workspace.ID,
		Command:     "/deploy",
		CallbackURL: "https://example.com/slash",
		BotUserID:   bot.ID,
		CreatedBy:   moderator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	invocationCount := func() int {
		t.Helper()
		var count int
		if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM slash_command_invocations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	invoke := func(userID, channelID string) error {
		t.Helper()
		_, err := st.CreateSlashCommandInvocation(ctx, store.CreateSlashCommandInvocationInput{
			CommandID:   command.ID,
			WorkspaceID: workspace.ID,
			ChannelID:   channelID,
			UserID:      userID,
			Text:        "prod",
			PayloadJSON: `{}`,
		})
		return err
	}
	assertDenied := func(name, userID, channelID string, want error) {
		t.Helper()
		before := invocationCount()
		if _, err := st.GetSlashCommandForChannel(ctx, channelID, "/deploy", userID); !errors.Is(err, want) {
			t.Fatalf("%s lookup: expected %v, got %v", name, want, err)
		}
		if err := invoke(userID, channelID); !errors.Is(err, want) {
			t.Fatalf("%s invocation: expected %v, got %v", name, want, err)
		}
		if after := invocationCount(); after != before {
			t.Fatalf("%s persisted a denied invocation: before=%d after=%d", name, before, after)
		}
	}

	for _, valid := range []struct {
		name      string
		userID    string
		channelID string
	}{
		{name: "member", userID: member.ID, channelID: generalChannelID},
		{name: "guest channel", userID: guest.ID, channelID: guestChannelID},
		{name: "bot", userID: bot.ID, channelID: generalChannelID},
	} {
		if _, err := st.GetSlashCommandForChannel(ctx, valid.channelID, "/deploy", valid.userID); err != nil {
			t.Fatalf("%s lookup should succeed: %v", valid.name, err)
		}
		before := invocationCount()
		if err := invoke(valid.userID, valid.channelID); err != nil {
			t.Fatalf("%s invocation should succeed: %v", valid.name, err)
		}
		if after := invocationCount(); after != before+1 {
			t.Fatalf("%s invocation was not persisted: before=%d after=%d", valid.name, before, after)
		}
	}

	assertDenied("guest hidden channel", guest.ID, generalChannelID, store.ErrModerationRestricted)

	timeoutUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: member.ID,
		TimeoutUntil: &timeoutUntil,
	}); err != nil {
		t.Fatal(err)
	}
	assertDenied("timed-out member", member.ID, generalChannelID, store.ErrModerationRestricted)

	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: member.ID,
		ClearTimeout: true,
		Blocked:      &blocked,
	}); err != nil {
		t.Fatal(err)
	}
	assertDenied("blocked member", member.ID, generalChannelID, store.ErrModerationRestricted)

	for i := 1; i < store.GuestPostLimit; i++ {
		if err := invoke(guest.ID, guestChannelID); err != nil {
			t.Fatalf("guest budget invocation %d failed: %v", i+1, err)
		}
	}
	assertDenied("guest post budget", guest.ID, guestChannelID, store.ErrPostRateLimited)
	members, err := st.ListWorkspaceMembers(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range members {
		if item.User.ID == guest.ID && item.PostsRemaining != 0 {
			t.Fatalf("guest slash invocations did not consume the shared post budget: %#v", item)
		}
	}
}

func TestSlashCommandInvocationRejectsScopeAndStaleAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Scope Owner", Email: "sqlite-slash-scope-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceA, err := st.EnsureDefaultGuestWorkspaceMember(ctx, owner.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Slash Scope B"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	listChannel := func(workspaceID string) (generalID string) {
		t.Helper()
		channels, err := st.ListChannels(ctx, workspaceID, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, channel := range channels {
			if channel.Name == "general" {
				generalID = channel.ID
			}
		}
		if generalID == "" {
			t.Fatalf("expected general channel for workspace %s, got %#v", workspaceID, channels)
		}
		return generalID
	}
	createChannel := func(workspaceID, name string) string {
		t.Helper()
		channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
			WorkspaceID: workspaceID,
			Name:        name,
			UserID:      owner.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return channel.ID
	}
	generalA := listChannel(workspaceA.ID)
	generalB := createChannel(workspaceB.ID, "general")
	sentinelChannelB := createChannel(workspaceB.ID, "sentinel")

	botA, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspaceA.ID, DisplayName: "Scope Bot A", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	botB, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspaceB.ID, DisplayName: "Scope Bot B", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	commandA, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID: workspaceA.ID,
		Command:     "/scope-a",
		CallbackURL: "https://example.com/scope-a",
		BotUserID:   botA.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	commandB, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID: workspaceB.ID,
		Command:     "/scope-b",
		CallbackURL: "https://example.com/scope-b",
		BotUserID:   botB.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	sentinel, err := st.CreateSlashCommandInvocation(ctx, store.CreateSlashCommandInvocationInput{
		CommandID:   commandB.ID,
		WorkspaceID: workspaceB.ID,
		ChannelID:   sentinelChannelB,
		UserID:      owner.ID,
		Text:        "sentinel",
		PayloadJSON: `{"sentinel":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	invocationCount := func() int {
		t.Helper()
		var count int
		if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM slash_command_invocations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	sentinelState := func() (string, int64) {
		t.Helper()
		var payload, responseBody string
		var status int64
		if err := st.db.QueryRowContext(ctx, `
			SELECT payload_json, response_status, response_body
			FROM slash_command_invocations
			WHERE id = ?`, sentinel.ID).Scan(&payload, &status, &responseBody); err != nil {
			t.Fatal(err)
		}
		return payload + "\x00" + responseBody, status
	}
	assertDenied := func(name string, input store.CreateSlashCommandInvocationInput, want error) {
		t.Helper()
		beforeCount := invocationCount()
		beforeState, beforeStatus := sentinelState()
		if _, err := st.CreateSlashCommandInvocation(ctx, input); !errors.Is(err, want) {
			t.Fatalf("%s: expected %v, got %v", name, want, err)
		}
		if afterCount := invocationCount(); afterCount != beforeCount {
			t.Fatalf("%s inserted an invocation: before=%d after=%d", name, beforeCount, afterCount)
		}
		afterState, afterStatus := sentinelState()
		if afterState != beforeState || afterStatus != beforeStatus {
			t.Fatalf("%s modified unrelated invocation: before=(%q,%d) after=(%q,%d)", name, beforeState, beforeStatus, afterState, afterStatus)
		}
	}

	assertDenied("command workspace mismatch", store.CreateSlashCommandInvocationInput{
		CommandID: commandA.ID, WorkspaceID: workspaceB.ID, ChannelID: generalB, UserID: owner.ID,
		PayloadJSON: `{}`,
	}, store.ErrSlashCommandScopeMismatch)
	assertDenied("channel workspace mismatch", store.CreateSlashCommandInvocationInput{
		CommandID: commandA.ID, WorkspaceID: workspaceA.ID, ChannelID: generalB, UserID: owner.ID,
		PayloadJSON: `{}`,
	}, store.ErrSlashCommandScopeMismatch)
	assertDenied("forged supplied workspace", store.CreateSlashCommandInvocationInput{
		CommandID: commandA.ID, WorkspaceID: workspaceB.ID, ChannelID: generalA, UserID: owner.ID,
		PayloadJSON: `{}`,
	}, store.ErrSlashCommandScopeMismatch)

	if _, err := st.RevokeSlashCommand(ctx, commandA.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	assertDenied("revoked command", store.CreateSlashCommandInvocationInput{
		CommandID: commandA.ID, WorkspaceID: workspaceA.ID, ChannelID: generalA, UserID: owner.ID,
		PayloadJSON: `{}`,
	}, sql.ErrNoRows)

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Removed Member", Email: "sqlite-slash-scope-removed@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaceB.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSlashCommandForChannel(ctx, generalB, "/scope-b", member.ID); err != nil {
		t.Fatalf("scope lookup should succeed before membership removal: %v", err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspaceB.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	assertDenied("membership removed after lookup", store.CreateSlashCommandInvocationInput{
		CommandID: commandB.ID, WorkspaceID: workspaceB.ID, ChannelID: generalB, UserID: member.ID,
		PayloadJSON: `{}`,
	}, sql.ErrNoRows)

	if _, err := st.GetSlashCommandForChannel(ctx, generalB, "/scope-b", owner.ID); err != nil {
		t.Fatalf("scope lookup should succeed before channel removal: %v", err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, generalB); err != nil {
		t.Fatal(err)
	}
	assertDenied("channel removed after lookup", store.CreateSlashCommandInvocationInput{
		CommandID: commandB.ID, WorkspaceID: workspaceB.ID, ChannelID: generalB, UserID: owner.ID,
		PayloadJSON: `{}`,
	}, sql.ErrNoRows)
}

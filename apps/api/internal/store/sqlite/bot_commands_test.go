package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func TestBotCommandsLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Bot Command Owner", "bot-command-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bot Command Member", Email: "bot-command-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bot Command Outsider", Email: "bot-command-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	zetaBot, zetaToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Zeta Bot",
		Handle:      "zeta-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(zetaToken.Scopes, store.BotCommandsWriteScope) {
		t.Fatalf("expected default bot:write bundle to include %s, got %#v", store.BotCommandsWriteScope, zetaToken.Scopes)
	}
	alphaBot, alphaToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Alpha Bot",
		Handle:      "alpha-bot",
		AvatarURL:   "https://example.com/alpha.png",
		Scopes:      []string{store.BotCommandsWriteScope},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(alphaToken.Scopes, []string{store.BotCommandsWriteScope}) {
		t.Fatalf("expected standalone %s grant, got %#v", store.BotCommandsWriteScope, alphaToken.Scopes)
	}

	first, err := st.SetBotCommands(ctx, workspace.ID, zetaBot.ID, []store.BotCommandInput{
		{Command: "Status", Description: " Show status "},
		{Command: "/new", Description: "Start a new session", ArgsHint: " [message] "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Command != "/new" || first[1].Command != "/status" {
		t.Fatalf("expected normalized command-sorted rows, got %#v", first)
	}
	for _, command := range first {
		if !strings.HasPrefix(command.ID, "botcmd_") || command.WorkspaceID != workspace.ID || command.BotUserID != zetaBot.ID {
			t.Fatalf("unexpected stored command: %#v", command)
		}
	}
	if first[0].ArgsHint != "[message]" || first[1].Description != "Show status" {
		t.Fatalf("expected trimmed metadata, got %#v", first)
	}
	if _, err := st.SetBotCommands(ctx, workspace.ID, alphaBot.ID, []store.BotCommandInput{
		{Command: "about", Description: "About this agent"},
	}); err != nil {
		t.Fatal(err)
	}

	replaced, err := st.SetBotCommands(ctx, workspace.ID, zetaBot.ID, []store.BotCommandInput{
		{Command: "help", Description: "Show help"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced) != 1 || replaced[0].Command != "/help" {
		t.Fatalf("expected overwrite to replace the prior set, got %#v", replaced)
	}
	assertSQLiteBotCommandSet(t, st, workspace.ID, zetaBot.ID, []string{"/help"})
	assertSQLiteBotCommandSet(t, st, workspace.ID, alphaBot.ID, []string{"/about"})

	tooMany := make([]store.BotCommandInput, 101)
	for i := range tooMany {
		tooMany[i] = store.BotCommandInput{Command: "cmd" + string(rune('a'+i%26)), Description: "valid"}
	}
	validationCases := []struct {
		name     string
		commands []store.BotCommandInput
	}{
		{"bad name", []store.BotCommandInput{{Command: "bad name", Description: "invalid"}}},
		{"missing description", []store.BotCommandInput{{Command: "status"}}},
		{"long description", []store.BotCommandInput{{Command: "status", Description: strings.Repeat("x", 101)}}},
		{"long args hint", []store.BotCommandInput{{Command: "status", Description: "valid", ArgsHint: strings.Repeat("x", 101)}}},
		{"too many", tooMany},
		{"duplicate", []store.BotCommandInput{
			{Command: "status", Description: "first"},
			{Command: "/STATUS", Description: "second"},
		}},
	}
	for _, tc := range validationCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := st.SetBotCommands(ctx, workspace.ID, zetaBot.ID, tc.commands); err == nil {
				t.Fatal("expected validation error")
			}
			assertSQLiteBotCommandSet(t, st, workspace.ID, zetaBot.ID, []string{"/help"})
		})
	}

	listed, err := st.ListBotCommands(ctx, workspace.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Bot.Handle != "alpha-bot" || listed[0].Command != "/about" || listed[1].Bot.Handle != "zeta-bot" {
		t.Fatalf("expected handle-then-command sorting, got %#v", listed)
	}
	if listed[0].Bot.ID != alphaBot.ID || listed[0].Bot.DisplayName != "Alpha Bot" || listed[0].Bot.AvatarURL != "https://example.com/alpha.png" {
		t.Fatalf("expected embedded bot identity, got %#v", listed[0].Bot)
	}
	if _, err := st.ListBotCommands(ctx, workspace.ID, outsider.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected workspace membership requirement, got %v", err)
	}

	if _, err := st.SetBotCommands(ctx, workspace.ID, zetaBot.ID, nil); err != nil {
		t.Fatal(err)
	}
	assertSQLiteBotCommandSet(t, st, workspace.ID, zetaBot.ID, nil)
	assertSQLiteBotCommandSet(t, st, workspace.ID, alphaBot.ID, []string{"/about"})

	if _, err := st.SetBotCommands(ctx, workspace.ID, zetaBot.ID, []store.BotCommandInput{
		{Command: "status", Description: "Show status"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, zetaBot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	assertSQLiteBotCommandSet(t, st, workspace.ID, zetaBot.ID, nil)
	listed, err = st.ListBotCommands(ctx, workspace.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Bot.ID != alphaBot.ID {
		t.Fatalf("expected removal cleanup to leave only the other bot, got %#v", listed)
	}

	staleBot, staleToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Stale Bot",
		Handle:      "stale-bot",
		Scopes:      []string{"bot:admin"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(staleToken.Scopes, store.BotCommandsWriteScope) {
		t.Fatalf("expected bot:admin bundle to include %s, got %#v", store.BotCommandsWriteScope, staleToken.Scopes)
	}
	if _, err := st.SetBotCommands(ctx, workspace.ID, staleBot.ID, []store.BotCommandInput{
		{Command: "stale", Description: "Should be hidden"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, staleBot.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = st.ListBotCommands(ctx, workspace.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Bot.ID != alphaBot.ID {
		t.Fatalf("expected defensive membership join to hide stale bot rows, got %#v", listed)
	}
	assertSQLiteBotCommandSet(t, st, workspace.ID, staleBot.ID, []string{"/stale"})
}

func assertSQLiteBotCommandSet(t *testing.T, st *Store, workspaceID, botUserID string, want []string) {
	t.Helper()
	rows, err := st.q.ListBotCommandsForBot(context.Background(), storedb.ListBotCommandsForBotParams{
		WorkspaceID: workspaceID,
		BotUserID:   botUserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(want) {
		t.Fatalf("expected commands %v, got %#v", want, rows)
	}
	for i, command := range want {
		if rows[i].Command != command {
			t.Fatalf("expected commands %v, got %#v", want, rows)
		}
	}
}
